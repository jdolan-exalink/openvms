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
- [ ] M2 Run Go build/vet/tests, web typecheck and suite, and integration tests touching migrations.
- [ ] M3 Deploy locally with `make up` and smoke the stack, including the new relay service.
- [ ] M4 Push the branch and open the PR against `main`.

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

## Next step

M2: integration tests (migration 00040 on a v39 database).
