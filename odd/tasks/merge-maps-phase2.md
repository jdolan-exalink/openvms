# Merge feat/maps-phase2 into main

## Objective

Integrate the 7 unmerged commits of `feat/maps-phase2` into `main` through a reviewed PR.

## Problem

`feat/maps-phase2` branched from `main` at `3d44726` (2026-10-05). `main` has since gained 104 commits. A dry-run merge reports 8 conflicting files, and the branch adds `migrations/00032_event_rollups.sql`, which collides with `main`'s `00032_server_agent_tls.sql`. Databases are already at version 39, so a new 00032 would never apply.

## Scope

- Branch: `merge/maps-phase2` (from `main` at `a6061f8`), merge commit with `--no-ff`.
- Resolve conflicts keeping both sides' behavior.
- Renumber the migration to `00040_event_rollups.sql`.
- Regenerate `internal/api/gen/api.gen.go` (and TS schema) from the resolved `openapi.yaml` via `make generate`.
- Out of scope: new features or refactors beyond what the merge requires.

## Constraints

- Commits: Conventional Commits, no AI attribution.
- TDD: not applicable (integration of existing reviewed-or-authored work; no new behavior). Ordinary functional checks apply.
- Push and PR creation are authorized for this branch; merging to `main` stays with the user.

## Tasks

- [x] M1 Resolve the 8 conflicts, renumber the migration, regenerate code, commit the merge. Route: delegated direct (writer trigger: 7 non-trivial files).
- [x] M2 Run Go build/vet/tests, web typecheck and suite, and integration tests touching migrations.
- [x] M3 Deploy locally with `make up` and smoke the stack, including the new relay service.
- [x] M4 Push the branch and open the PR against `main`.

## Acceptance criteria

- No conflict markers; `go build ./...` and `go vet ./...` pass.
- Go unit tests and web suite pass (or failures are shown to be pre-existing).
- Migrations apply cleanly on the existing database (version 39 → 40).
- Stack starts and the web responds.

## Progress

- Merge started on `merge/maps-phase2`; conflicts: `apps/edge-agent/main.go`, `docker-compose.yml`, `internal/agent/network.go`, `internal/agent/network_test.go`, `internal/api/gen/api.gen.go`, `internal/api/router_test.go`, `internal/platform/config/config.go`, `packages/api-contract/openapi.yaml`.

- M1 done: merge commit `accd02c` (8 conflicts resolved; phase2 interface discovery moved to `internal/agent/interfaces.go`; migration renumbered to `00040_event_rollups.sql`; `api.gen.go` regenerated). Checks: go build/vet/test OK, compose config OK, web tsc clean, vitest 964/964.
- Security correction `42a072d`: the writer had relaxed `TestAPIIsInternalOnlyAndUsesNoHostPortPublication` to publish gRPC 9090 on 0.0.0.0, and phase2 published relay 8554 on 0.0.0.0, while the gRPC server has no TLS. User chose to drop both publications and restore the test from main. go test ./... OK.
- Follow-ups: gRPC TLS before exposing 9090/8554; Prometheus 9090 clash; `internal/store/db/models.go` on main looks stale vs sqlc output.

- RDD, reviewed per phase2 commit in a detached worktree (whole merge exceeded the lens context budget):
  - `8777e6c` (base `3d44726`): high, granted; 2 CRITICAL findings (rollup table never written, so >24h analytics windows returned 0). User chose option 1 (always query raw events). TDD: RED observed (`LongRangeCountsRawEvents` got 0), then GREEN. Validated and acknowledged (`review-de64226b9b6a47b1`). Fix carried to this branch as `c236e1b`.
  - `bfc4ea9` + `79b6408` (base `8777e6c`): high, granted, but `lens_context_budget_exceeded` (≈5.5k lines of generated `.pb.go`). User chose to leave this range unreviewed.
  - `cea2c33` (base `79b6408`): high, granted, approved with advisories (`review-c1ae8c68dd754723`).
  - `1870f73` (base `cea2c33`): medium, granted, approved with advisories (`review-bf2f0dc88ca70950`).
  - `cc39dad` + `fe1a2e4` (base `1870f73`): medium, granted, approved with advisories (`review-af1fdcdbb30fff2f`).
- Whole branch vs `main` exceeded the lens budget again; merge commit `accd02c` conflict resolution stays covered only by functional checks (user choice). Post-merge range (base `accd02c`: `42a072d`, `e440df3`, `c236e1b`, `f722e9b`): high, granted, approved with advisories (`review-f948e34eb67ef4f8`): unbounded raw scan for long analytics windows, relay publication not guarded by the compose contract test.
- M2: `go test -tags integration ./...` all pass except `TestMapsHierarchyPrivatePlanLifecycle`, which fails identically on `main` (pre-existing). Unit/web results from M1 stand.
- Notable advisories for follow-up: `GET /api/v1/system/connections` is mounted outside the strict authenticated handler (`internal/api/router.go`); default edge node ID collides across agents; relay sessions unbounded and upstream has no timeout; ticker interval 0 can panic in `internal/mediasession/session.go`.

- M3: first `make up` failed: the live database already had an identical, empty `event_counts_hourly` from an earlier phase2 deploy, so version 40 hit 42P07 and the API crash-looped. Fix `c705d53` makes the migration idempotent (`IF NOT EXISTS`, drop-then-create policy). After redeploy: goose at 40, api/web/worker/relay up, no ERROR logs, web 200, analytics 1d total=2613 and 30d total=32416 (long window now returns data). `GET /api/v1/system/connections` returns 401 unauthenticated, so the review advisory about it is lower risk than flagged. RDD for `c705d53`: medium, under budget (6 lines), pending in slice.

- M4: pushed `merge/maps-phase2` and opened PR #13; merged by the user as `2aa0f83`. Redeployed with `make up` from `main`: goose at 40, api/web/worker/relay up, web 200, no ERROR logs. Branches `merge/maps-phase2` (local and remote) and `feat/maps-phase2` (local) deleted.

## Next step

Done. Follow-ups listed above remain open.
