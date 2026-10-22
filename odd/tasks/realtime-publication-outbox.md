# Durable realtime publication outbox (P6-02)

## Objective

Make realtime JetStream announcements follow committed database state. Producers must persist each publication in the same transaction as the state change; a worker publishes pending rows and acknowledges them only after JetStream accepts them.

## Failure windows

- Frigate events and built-in alarm transitions commit with the review cursor, then publish from callbacks. A crash or publish failure can permanently lose a message.
- Rule and user alarm changes publish after commit and can lose announcements after a crash.
- `notification.created` currently publishes inside the rule transaction, so a later rollback can leave a phantom message.
- `notification_deliveries` is an external-channel delivery outbox; it does not cover realtime announcements.

## Scope

- Add tenant-owned realtime outbox rows with stable IDs, subject/payload, lease fencing, bounded retries, terminal failure visibility, and due-time indexes.
- Add optional per-alarm aggregate ordering metadata so rows for one alarm cannot be claimed out of sequence across dispatcher replicas. Only alarm-related producers need to set this key; unrelated event publications remain unordered.
- Add a dispatcher worker that publishes with a stable JetStream message ID, then marks the row sent. Keep rows retryable after worker crashes.
- Convert producers so outbox insertion shares their state/audit transaction: event ingestion and built-in alarms; rule alarm transitions and in-app notification-created events; user alarm transitions and auto-close.
- Preserve current subjects and payload schemas. Document at-least-once behavior if a crash occurs after JetStream accepts a message and before the row is marked sent.
- Keep P6-01 B4 inactive until all in-scope producers use the outbox.

## Out of scope

- External WAHA/webhook delivery (`notification_deliveries`).
- Changing notification or alarm matching/state-machine semantics.
- Remote deployment or physical-device acceptance.

## Work slices

- [x] Slice A (`fc1e0e0`): tenant isolated schema, due index, sqlc enqueue/claim/ack/retry APIs, lease-token fencing, bounded retries, terminal failure, and RLS integration coverage. Focused integration and `go test ./internal/store` passed; `git diff --check` passed. 229 authored lines plus 175 generated. Runtime publisher harness: N/A (no dispatcher in this slice). Rollback boundary: migration 00065 drops the outbox table and its rows. Independent review found no actionable defects in schema, RLS, or lifecycle queries.
- [x] Slice B: migration 00066 and query changes for optional tenant-scoped alarm aggregate keys and transaction-serialized sequence numbers. Claims block later rows while an earlier aggregate row is pending/processing; sent or terminal failed predecessors release the next row. Integration coverage verifies concurrent claims, independent keys/NULL keys, terminal release, and concurrent enqueue lock order. Focused integration and `go test ./internal/store` passed; `go generate ./internal/store` and `git diff --check` passed. Independent review found no actionable defects. 263 authored lines plus generated sqlc output. Runtime dispatcher harness is deferred to Slice C.
- [ ] Slice C: lease-based dispatcher, JetStream publisher adapter, worker startup/shutdown, retry/dead-letter observability, and tests. It may be implemented behind the ordering contract; do not mark complete until serialization across dispatchers is verified.
- [ ] Slice D: convert event ingestion and built-in alarm publications transactionally.
- [ ] Slice E: convert rule alarms and `notification.created` transactionally; publish none from inside evaluator transactions.
- [ ] Slice F: convert user alarm transitions and auto-close transactionally.
- [ ] Keep each code slice below 400 authored changed lines; split producer families further if required. Include tests with each behavior.

## Acceptance

- Rolling back a producer transaction leaves neither domain state nor an outbox row.
- Committed domain state has a durable publication row with the expected tenant, subject, payload, and stable message ID.
- Dispatcher claims are safe across replicas; stale lease tokens cannot acknowledge or retry a newer claim.
- A publish failure retries with bounded backoff; exhausted rows are visible as terminal failures.
- Successful dispatch marks sent only after JetStream acceptance; retries reuse the same message ID.
- Existing realtime subjects and payload shapes remain compatible.

## Progress

- [x] Investigation map: producer transaction boundaries and existing external delivery outbox were reviewed; realtime publications are not currently durable.
- [x] Slice B tests: `GOCACHE=/tmp/openvms-go-build-cache go test -tags integration ./internal/store -run '^TestRealtimePublication(OutboxLifecycle|AggregateClaimOrdering|ConcurrentEnqueueSequencesFollowLockOrder)$' -count=1`; `GOCACHE=/tmp/openvms-go-build-cache go test ./internal/store`; `git diff --check`.
- [ ] Implementation, focused integration coverage, work-unit commits, and independent review for every slice.
- [ ] Slice B draft exists in the worktree but is not accepted as complete because the current schema cannot preserve per-alarm ordering across replicas. Its focused unit tests passed before the ordering contract was added; rerun after updating claim behavior. Runtime JetStream/PostgreSQL harness remains required where available. At-least-once remains possible after JetStream accepts a message but before the database ack.

P6-01 B4 remains gated until slices D–F are complete and the outbox dispatcher is available.
