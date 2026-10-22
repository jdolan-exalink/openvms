# Durable alarm rule evaluation (P6-01)

## Objective

Persist rule-evaluation work in the same transaction that stores newly observed Frigate events and advances the review cursor. A worker must retry work after crashes and transient errors, while concurrent replicas claim each work item safely. Preserve existing per-rule/per-camera debounce behavior.

## Current failure window

`internal/events/syncer.go` commits event rows and the review cursor before evaluating rules from in-memory `created` and face-recognition lists. Evaluation errors are logged and swallowed. A crash or error after that commit can permanently skip evaluation because later syncs no longer see the event as new.

## Scope

- Add durable work identity and payload for event evaluation, including event kind and face names gained by recognition updates.
- Enqueue work transactionally with event upsert and review cursor advancement.
- Claim work with bounded leases and `FOR UPDATE SKIP LOCKED` so multiple workers do not process the same live lease.
- Apply rule effects and mark work complete atomically; failures remain retryable with bounded backoff and observable terminal state.
- Preserve current camera cooldown/debounce semantics; durable job deduplication must be event-scoped and must not replace `rule_firings` semantics.
- Add integration coverage for crash/retry, concurrent claims, face-update work, and no duplicate effects after successful completion.

## Out of scope

- JetStream/realtime transactional outbox and notification publication (P6-02).
- Changing alarm cooldown or matching semantics.
- Real Frigate deployment or remote/device operations.

## Strict TDD and verification

Write failing regression coverage before implementation. Use the existing PostgreSQL integration harness for transactional, leasing, and replica claims. Focused commands:

- `GOCACHE=/tmp/openvms-go-build-cache go test -tags integration ./internal/events -run 'TestSyncer|TestAlertEvent' -count=1`
- `GOCACHE=/tmp/openvms-go-build-cache go test -tags integration ./internal/rules -run '^TestEvaluateEvent' -count=1`
- `GOCACHE=/tmp/openvms-go-build-cache go test ./internal/events ./internal/rules ./internal/store/...`
- `git diff --check`

## Acceptance

- Event/evaluation work/cursor advancement commit together; a failed transaction persists none of them.
- Retry after process interruption eventually evaluates the event; a completed item is not evaluated twice.
- Concurrent workers cannot process the same active lease.
- Face-recognition updates produce durable rule work with the correct gained names.
- Existing alarm debounce and notification behavior remains covered and unchanged.

## Progress

- [x] Slice A (`390c635`): new-event and gained-face-name rule-evaluation payloads are persisted in `rule_evaluation_jobs` inside the event/cursor transaction. Face-update identity includes the canonical gained-name batch, so later recognitions enqueue separately while repeated syncs deduplicate. Focused integration coverage verifies event/face-update payloads, duplicate syncs, and rollback when ingestion fails after enqueue (alarm correlation failure). Verification: focused and broader event integration, events/rules/store package checks, and `git diff --check` passed. The slice is 233 authored changed lines plus 32 generated lines; independent review found no defects.
- [x] Slice B1 (`5f533c6`): lifecycle columns, skip-locked claims with lease fencing, acknowledgement, retry/backoff, and terminal failed state. Both migration defaults and current enqueue leave rows `completed`. Focused integration test passed; `go generate ./internal/store` and `git diff --check` passed. 184 authored changed lines plus 120 generated; no consumer runtime applies yet. Rollback boundary: migration 00064 down drops the job table and its contents.
- [x] Slice B2 (`8866abb`, follow-up `4a1e529`): extracted `EvaluateEventTx`; `EvaluateEvent` retains its tenant transaction wrapper. Alarm announcements now carry their originating event context, and publish accepts no separate event argument. Focused rollback, wrapper, and bound-context integration tests passed; `git diff --check` passed. 281 authored changed lines across the two commits. Full `internal/rules` integration suite stalled without output in futex wait for ~2 minutes and was stopped; broader coverage remains pending. Runtime harness: N/A (internal transaction API, integration DB fixture used). Rollback boundary: revert `internal/rules/service.go` and `internal/rules/evaluate_integration_test.go`. The crash-after-commit alarm publication gap remains P6-02 and is a cutover limitation. Independent reviews found no actionable defects in B1 or B2; B2 review confirmed the context binding fix.
- [x] Slice B3 (`62b28af`): added the dormant `RuleEvaluationWorker`; it claims under all-tenants scope, resolves each event's tenant, and commits evaluation effects with token-fenced acknowledgement in the tenant transaction. Failures roll back and retry with bounded exponential backoff; exhausted jobs become observable `failed`. Integration coverage exercises event and face-update jobs, expired-lease recovery, one-time notification effects, rollback of tentative rule-firing writes, and terminal retry exhaustion. `GOCACHE=/tmp/openvms-go-build-cache go test -tags integration ./internal/events -run '^TestRuleEvaluationWorker' -count=1` and `git diff --check` passed. Runtime harness: PostgreSQL-backed event fixture; success, expired lease, rollback, and terminal failure scenarios passed. Rollback boundary: remove `internal/events/evaluation_worker.go` and `internal/events/evaluation_worker_integration_test.go`. Worker remains unwired. Independent review found no actionable defects; it confirmed tenant-scoped atomic ack, token fencing, retry bounds, and no activation before B4.
- [x] Slice B4 — switched event enqueue to `pending`, removed synchronous evaluation from `Syncer`, added the polling `RuleEvaluationWorker.Run(ctx)` loop, and wired it in `apps/worker/main.go` with a bounded shutdown wait under `cfg.ShutdownTimeout`. Activated after P6-02 slices D–G provided durable announcements. Focused pending-job, event/face-update worker, rule/face/alarm correlation integration tests, alarm outbox/auto-close integration tests, package tests, and `git diff --check` passed. The sync-created-job regression was RED before changing the enqueue default. The full `internal/events` integration package did not finish within 122 seconds and was interrupted; its result is not counted as passing. Runtime harness: focused PostgreSQL Testcontainers plus worker package build/tests; no deployed worker runtime was exercised. Rollback boundary: revert B4 query, syncer, worker loop/startup, and integration-test changes together. 230 authored changed lines excluding generated sqlc output; no commit created in this slice.
- [ ] Each slice stays below 400 authored changed lines; use chained PRs and do not activate pending rows before B3 is deployed.
- [ ] Focused regression tests, work-unit commits, and independent review for each implementation slice.

Slice B4 is the only point where new work becomes pending, and it includes the active consumer and removal of the old evaluator path. This keeps intermediate releases safe and prevents duplicate or stranded evaluations.

JetStream/realtime publication consistency remains P6-02 and is out of this unit.
