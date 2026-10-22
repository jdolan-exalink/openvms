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
- [ ] Slice B1 — add job lifecycle columns and claim/ack/retry SQL, with old rows defaulting to `completed`. Keep event enqueue explicitly `completed` until consumer cutover. Add schema/query coverage and regenerate sqlc.
- [ ] Slice B2 — add a transaction-taking rule evaluator while preserving the existing `EvaluateEvent` wrapper. Add focused tests for transaction rollback and compatibility.
- [ ] Slice B3 — implement and test the durable consumer: lease claim, tenant-scoped evaluation plus acknowledgement in one transaction, bounded retry/backoff after rollback, expired lease recovery, and observable terminal failures. Keep it dormant until cutover.
- [ ] Slice B4 — switch event enqueue to `pending`, wire the worker in `apps/worker/main.go`, and remove synchronous post-commit evaluation in the same release. Ensure startup/shutdown and configuration follow existing worker conventions.
- [ ] Each slice stays below 400 authored changed lines; use chained PRs and do not activate pending rows before B3 is deployed.
- [ ] Focused regression tests, work-unit commits, and independent review for each implementation slice.

Slice B4 is the only point where new work becomes pending, and it includes the active consumer and removal of the old evaluator path. This keeps intermediate releases safe and prevents duplicate or stranded evaluations.

JetStream/realtime publication consistency remains P6-02 and is out of this unit.
