# M3 — Global search and LPR

## Objective
Complete milestone M3 (PRD §43-48, §82): global search of events and license plates over the central index, with every minimum filter, index-backed plate search, and permission-safe results.

## Problem and rationale
Core M3 plumbing already exists (central-index search, plate partial/exact search, keyset pagination, permission scoping). Gaps remain against PRD §44 minimum filters, §46 indexes, and test coverage. The first real target is the user's home Frigate 0.18 (`http://10.1.1.252:5000`, auth none, 10 cameras, LPR capability on).

## Scope and constraints
- In scope: tasks below.
- Out of scope (not in PRD for M3): saved searches, plate watchlists/alerts, table partitioning (§48, deferred until real volume), result export (M6 already covers per-event export).
- Contract first: every API change starts in `packages/api-contract/openapi.yaml`, then `make generate`.
- Never leak events or plates of denied cameras or other tenants.
- Do not push or open PRs (no remote configured).

## Tasks
- [x] M3-1: Add explicit cross-tenant negative tests for `/events` and `/lpr/reads` (two tenants, zero leakage). Route: delegated direct.
- [ ] M3-2: Make plate filtering on `/events` index-backed (pg_trgm) instead of `unnest(...) LIKE` scan in `internal/events/service.go`. Migration + correctness test. Route: delegated direct.
- [ ] M3-3: Add `zone` and `sub_label` filters end-to-end (OpenAPI → service SQL → Events UI) plus GIN indexes on `zones`/`sub_labels` (PRD §46). Route: delegated direct.
- [ ] M3-4: Add `camera_group` filter to `/events` and `/lpr/reads`. Route: delegated direct.
- [ ] M3-5: Add `has_snapshot` / `has_preview` event flags and filters (PRD §44). Route: delegated direct.
- [ ] M3-6: Web tests for Events and Plates routes (filters, infinite scroll, `lpr.search` gating). Route: delegated direct.
- [ ] M3-7: Verify LPR ingestion against the real Frigate 0.18; first sync showed events but no plates. Diagnose adapter mapping if plates exist upstream. Route: delegated direct.

## Verification mode
- Strict TDD: enabled (source: global user config `Strict TDD Mode: enabled`). RED → GREEN → REFACTOR with observed evidence.
- Runners: `go test ./...`; `go test -tags integration ./...` (needs Docker); `pnpm --filter web test`; `pnpm typecheck`; `make lint`; `make generate` + clean `git diff` of generated code.

## Acceptance criteria
- All PRD §44 minimum filters available on search, permission-scoped.
- Plate partial search on `/events` uses an index (EXPLAIN shows index usage).
- Cross-tenant and denied-camera tests pass for events and plates.
- All runners above pass (known pre-existing lint finding: `apps/web/src/routes/Playback.tsx:32`, outside M3).

## Delivery
- Branch `feat/m3-search` from baseline `a4443d1`. One work-unit commit per task. Forecast ~1,200-1,800 authored lines; no remote, so PR slicing is deferred until a remote exists.

## Progress and evidence
- Baseline commit `a4443d1` on `main`; CodeGraph initialized.
- M3-1: added `internal/events/cross_tenant_search_integration_test.go` (`TestCrossTenantSearchIsolation`: sanity, operator, full-access admin, raw-SQL RLS). No leak found. `go test ./...` PASS; `go test -tags integration ./internal/events/...` PASS (writer + parent re-run). Test-only task: behavior already held, so no RED phase applies; the sanity subtest proves assertions are non-vacuous.

## Next step
M3-2.
