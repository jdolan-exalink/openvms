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
- Add a dispatcher worker that publishes with a stable JetStream message ID, then marks the row sent. Keep rows retryable after worker crashes.
- Convert producers so outbox insertion shares their state/audit transaction: event ingestion and built-in alarms; rule alarm transitions and in-app notification-created events; user alarm transitions and auto-close.
- Preserve current subjects and payload schemas. Document at-least-once behavior if a crash occurs after JetStream accepts a message and before the row is marked sent.
- Keep P6-01 B4 inactive until all in-scope producers use the outbox.

## Out of scope

- External WAHA/webhook delivery (`notification_deliveries`).
- Changing notification or alarm matching/state-machine semantics.
- Remote deployment or physical-device acceptance.

## Work slices

- [x] Slice A (`fc1e0e0`): tenant isolated schema, due index, sqlc enqueue/claim/ack/retry APIs, lease-token fencing, bounded retries, terminal failure, and RLS integration coverage. Focused integration and `go test ./internal/store` passed; `git diff --check` passed. 229 authored lines plus 175 generated. Runtime publisher harness: N/A (no dispatcher in this slice). Rollback boundary: migration 00065 drops the outbox table and its rows. Independent review pending.
- [ ] Slice B: lease-based dispatcher, JetStream publisher adapter, worker startup/shutdown, retry/dead-letter observability, and tests.
- [ ] Slice C: convert event ingestion and built-in alarm publications transactionally.
- [ ] Slice D: convert rule alarms and `notification.created` transactionally; publish none from inside evaluator transactions.
- [ ] Slice E: convert user alarm transitions and auto-close transactionally.
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
- [ ] Implementation, focused integration coverage, work-unit commits, and independent review for every slice.

P6-01 B4 remains gated until slices C–E are complete and the outbox dispatcher is available.
