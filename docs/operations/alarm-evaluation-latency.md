# Alarm rule evaluation: delivery and latency

A committed event is evaluated against the rules by a durable queue (`rule_evaluation_jobs`) and its
effects (notifications, alarms) are published to clients through an outbox, after the transaction
commits. This page records how that behaves with several replicas and how long it takes.

## Guarantees and where they are tested

| Guarantee | Mechanism | Test (`-tags integration`, `internal/events`) |
| --- | --- | --- |
| A job is claimed by one replica at a time | `ClaimRuleEvaluationJobs` locks rows with `FOR UPDATE SKIP LOCKED` and stamps a `lease_token` | `TestRuleEvaluationWorkersShareOneQueueWithoutDuplicateEffects`: 4 replicas drain 12 events, each with its own rule, and produce exactly 12 notifications |
| A stale replica cannot complete a job it lost | Completion and retry require the `lease_token` they claimed with, and share the transaction with the effects | Same test: with both the row lock and the token check removed it fails; removing only one of them does not, because each protects on its own |
| A replica that is still working keeps its job | A `processing` job is only reclaimable once `lease_until` (2 minutes) has passed | `TestRuleEvaluationWorkerRespectsAnActiveLeaseThenRecoversAfterExpiry` |
| A crashed replica's job is finished once | An expired lease is reclaimed; rerunning completed jobs does not repeat effects | Same test, and `TestRuleEvaluationWorkerProcessesEventAndFaceUpdateJobs` |
| Failures are bounded | Up to 5 attempts with exponential backoff from 2 s, then `failed` | `TestRuleEvaluationWorkerRollsBackEffectsAndFailsAfterBoundedRetries` |
| A rolled-back change emits nothing | Realtime messages are written to `realtime_publication_outbox` in the same transaction as the effects | `TestRuleEvaluationWorkerRollsBackEffectsAndFailsAfterBoundedRetries` asserts no rule firings and an unchanged outbox after every rolled-back attempt |

A rule's cooldown is applied on top of this: repeated matches of one rule inside its cooldown produce a
single notification by design, so "exactly once" is per event and rule, not per event alone.

## Latency

End to end, an event reaches a client after three waits in sequence:

| Step | Typical | Worst case |
| --- | --- | --- |
| Event committed to evaluation claimed | up to the worker poll, 1 s | 1 s |
| Evaluation of the job | about 1.5 ms per job | bounded by the batch of 25 per poll |
| Outbox row to realtime/notification delivery | up to the outbox poll, 1 s | 1 s, then retries from 1 s up to 1 min |

So the delivery delay after an event is committed is dominated by the two 1-second polls: **about 2 s
at worst in the normal case**, plus Frigate's own ingestion time, which this page does not cover.

### Measurement

`TestRuleEvaluationWorkerProcessingTimePerJob` queues 110 jobs and has one worker drain them:

| Run | Drain time | Per job |
| --- | --- | --- |
| 1 | 165 ms | 1.5 ms |
| 2 | 166 ms | 1.5 ms |
| 3 | 153 ms | 1.4 ms |

Measured without `-race` on the development host (about 2 ms per job with it), with PostgreSQL 17 in a local container, no network between worker and
database, and the rule engine given a no-op realtime publisher. Treat it as the cost of evaluation
alone: a remote database adds its round trip to each claim and completion, and the outbox poll and the
real publisher are not part of this figure. The test only fails above 500 ms per job; rerun it with

```
go test -count=1 -tags integration ./internal/events -run TestRuleEvaluationWorkerProcessingTimePerJob -v
```

and read the `PROCESSING` line.

## Not covered

- Latency through the real outbox dispatcher and NATS/WebSocket delivery, measured under load.
- A test with several replicas against a database over a real network.
