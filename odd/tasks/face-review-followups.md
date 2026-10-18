# Face recognition review follow-ups

## Objective
Close the reliability advisories left by the FACE-05 reviews (PR #87 four-lens review `review-e6086fc6104c6fa2` and the consolidated branch review `review-e0324d0517e27acc`) that are still present in `main` at `d9ca1fc`.

## Problem and why
- A face recognized on an already ingested event re-evaluates person rules with every `sub_label` of the review, not only the newly gained names (`internal/events/syncer.go:363`). A rule for person A fires again when person B is later recognized in the same review once the debounce window has passed.
- `Service.FaceNames` resolves names through `StoreFaceResolver`, which opens its own transaction while `EvaluateEvent` holds one (`internal/rules/service.go:641` inside `TxRaw`). On a cache miss each evaluation holds one pool connection and waits for a second; a saturated pool can stall.
- Deleting a watchlist counts the rules that use it, and creating/updating a rule counts the watchlists it references; neither takes a lock (`internal/faces/watchlists.go:251`, `internal/rules/service.go:1096`). Running concurrently, they can commit a rule that points at a deleted watchlist, which then silently never fires.

## Scope / tasks
- [x] FRF-01 — Face updates evaluate only the gained `sub_labels`. Route: delegated direct (writer trigger: 3 tasks across 2+ non-trivial files).
- [x] FRF-02 — The face resolver uses the evaluation transaction (no nested connection); the per-tenant cache stays. Route: delegated direct.
- [x] FRF-03 — Watchlist deletion and rule create/update serialize on the watchlist rows (`FOR UPDATE` on delete, `FOR SHARE` on reference checks). Route: delegated direct.
- [x] FRF-04 — A failing face resolver query is isolated in a savepoint so it cannot abort the evaluation transaction (native review advisory `R3-resolver-error-aborts-evaluation-tx`). Route: delegated direct.

## Accepted, not changed
- `R3-cache-staleness`: the 15 s per-tenant snapshot (`DefaultFaceCacheTTL`) is a documented tradeoff; watchlist edits reach the worker within that time.
- Already closed in `main`: stub handlers, `faces.view` gate on rule face conditions, tombstoned persons leaving watchlists, watchlist query tests.

## Out of scope (need a product decision or remote access)
- FACE-03 open questions: live Frigate verification of the train file format and endpoints (needs authorized access to a real server), purging tombstones whose owned face sits on a permanently unreachable server (operator override), tenant picker for platform admins on `/faces`.

## Constraints and checks
- Strict TDD (user global config): observed RED before GREEN. Runners: `GOCACHE=/tmp/openvms-go-build-cache go test -race ./...`, `go test -count=1 -tags integration ./internal/rules/... ./internal/events/... ./internal/faces/... ./internal/api/...`, `golangci-lint run ./...`.
- Forecast: under 400 authored changed lines; single PR, delivery strategy `ask-on-risk`.

## Progress
- 2026-10-18: branch `fix/face-review-followups` from `main` `d9ca1fc`; advisories verified against current code.

## Evidence
### FRF-01
- RED: `go test -count=1 -tags integration -run TestSyncerFaceUpdateEvaluatesOnlyTheGainedNames ./internal/events/` failed: `after the second name: 3 notifications, want 2`.
- GREEN: same test plus `-run 'TestSyncer.*(Face|Person)'` pass after `syncer.go` passes `GainedSubLabels` on face updates. Full checks at the end of the run.

### FRF-02
- RED: `go test -count=1 -tags integration -run TestEvaluateEventResolvesFacesInsideTheEvaluationTransaction ./internal/rules/` failed: `evaluation with a single pooled connection: timeout: context already done: context deadline exceeded` (pool MaxConns=1, empty face cache).
- GREEN: `FaceResolver.FaceNames` takes the caller's `*db.Queries` (nil opens its own transaction); `EvaluateEvent` passes its transaction; `Service.FaceNames` keeps its signature (nil querier) for the API handler. Face/person/watchlist/EvaluateEvent integration tests pass.

### FRF-03
- RED: `go test -count=1 -tags integration -run TestWatchlistDeleteWaitsForAConcurrentRuleReference ./internal/faces/` failed: `delete after the rule committed = <nil>, want conflict`; `-run TestRuleCreateWaitsForAConcurrentWatchlistDelete ./internal/rules/` failed: `rule create finished while the watchlist delete was in flight: <nil>`.
- GREEN: new queries `LockFaceWatchlist` (`FOR UPDATE`, before the rule count in `DeleteWatchlist`) and `LockFaceWatchlistsByIDs` (`FOR SHARE`, replaces `CountFaceWatchlistsByIDs` in `checkFaceRefs`); both tests pass. Person delete has no count-then-delete pattern, so it is unchanged.
- Both tests use deterministic interleavings: a held transaction plays one side with the real lock statement, the real service plays the other, asserting it blocks and then reaches the right outcome.

### FRF-04
- RED: `go test -count=1 -tags integration -run TestEvaluateEventResolverFailureDoesNotAbortOtherRules ./internal/rules/` failed: `evaluation with a failing resolver: ERROR: current transaction is aborted, commands ignored until end of transaction block (SQLSTATE 25P02)`. Rules run in `created_at` order, so the failing person rule precedes the plain rule.
- GREEN: `Service.faceNamesIsolated` runs the resolver on a savepoint (`tx.Begin`), rolled back on error and released on success; `matchesFaces` takes the evaluation `pgx.Tx`. The MaxConns=1 test still passes (no second connection).

## Next step
FRF-01..03 through one bounded writer.
