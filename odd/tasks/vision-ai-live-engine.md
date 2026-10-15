# Vision AI live engine — real events, real slots, working video

## Objective
Make Vision AI (`/vision-ai` route and the Vision AI virtual sources in Live) 100% functional:
slots are filled from real recent events ranked by the existing `internal/visionai` scoring and
allocation engine, rotate as new events arrive, and render live video.

## Problem and rationale
- `apps/web/src/routes/VisionAI.tsx` renders `MsePlayer` without `className="size-full"`; the
  `ZoomFrame` root has no intrinsic size, so the video collapses to 0px height.
- Both `VisionAI.tsx` and `components/live/LiveVisionTile.tsx` use hardcoded mock events
  (`ev-1..ev-5`, plate `AB123CD`) bound to `cameras[0..4]`; nothing polls, so nothing rotates.
- `internal/visionai` (scoring, allocator, service) is never imported: no API route, no data source.

## Scope and constraints
- Backend: new read-only endpoint `GET /api/v1/vision-ai/slots` (OpenAPI-first, generated strict
  server), events sourced from `events` (+ `lpr_reads` plate confidence, open `alarms`), scoped with
  `store.ScopeFor(actor)` + `access.Load(...).CameraIDs(ctx, authz.EventsView)`.
- Sources (match `LiveExplorer` virtual ids): `general`, `personas`, `vehiculos`, `lpr`,
  `perimetro`, `alarmas`.
- Allocation state (stability/min display/cooldown) kept in-memory per actor+source+layout with
  idle eviction; no new migration, no persistence of configs in this feature.
- Known data gaps (documented, not invented): no per-detection confidence column (neutral default
  unless an LPR read provides one); no watchlist table (`watchlist` stays false).
- Web: poll the endpoint (and invalidate on `event.created` frames), fix player sizing, keep pins
  client-side. Artifacts in English; existing Spanish UI copy kept as is.
- Advisory ~400 authored changed lines per task (heuristic only). No push/merge without the user.

## TDD
- Mode: strict (source: user global CLAUDE.md "Strict TDD Mode: enabled").
- Runners: `go test -race ./internal/visionai ./internal/api/...`; web `cd apps/web && pnpm vitest run <file>`.

## Tasks
- [x] VAI-01 — Backend engine: event source query (tenant/permission scoped), mapping to
      `VisionEvent`, source presets, recency term, per-session allocator with idle eviction, fix
      `gofmt`/subscriber leak. Route: delegated writer (4+ files).
- [x] VAI-02 — API: OpenAPI `getVisionAISlots`, regenerate Go + TS, handler, `apps/api/main.go`
      wiring, handler tests. Route: delegated writer (same writer as VAI-01).
- [ ] VAI-03 — Web `/vision-ai`: consume API (polling + realtime invalidation), player sizing fix,
      client pins, tests. Route: delegated writer.
- [ ] VAI-04 — Web `LiveVisionTile`: consume API by source, rotate camera, fix `isFocused` prop,
      tests. Route: delegated writer (same as VAI-03).

## Acceptance criteria and checks
- `go vet ./...`, `go test -race ./internal/visionai/... ./internal/api/...` green; `gofmt -l` clean.
- `pnpm --filter web typecheck`, `pnpm --filter web lint`, VisionAI/LiveVisionTile/Live vitest green.
- With real events, slots show the highest-scoring cameras and change as new events arrive; empty
  state when no events; video visible in every filled slot.

## Delivery
- Forecast: ~1200 authored changed lines (> 400) → strategy `ask-on-risk`; chain strategy: stacked-to-main (user choice). Slices: PR1 backend+API (VAI-01..02), PR2 web (VAI-03..04).
- RDD: on (global). Assess each work-unit commit.

## Progress and evidence
- Branch `feat/vision-ai-live-engine` created from `main` (9a33433).
- VAI-01 `a08b2e3` feat(visionai): rank real events into per-session slots. TDD RED→GREEN observed
  (recency, allocator require-flags/multi-label/release, presets, mapping, service).
- Prerequisite `21e23ed` fix(inventory): birdseye camera stored with empty zones (demofix setup
  was failing; NOT NULL `cameras.zones`).
- VAI-02 `e6b0b22` feat(api): `GET /api/v1/vision-ai/slots?source=&layout=` (`getVisionAISlots`),
  `VisionAISlots{source,layout,generated_at,slots[]}`, `VisionAISlot{slot,camera_id|null,pinned,...}`.
- Checks: `go build ./...` ok; `go vet ./...` clean; `go test -race ./internal/visionai/... ./internal/api/...`
  pass (parent spot-check re-ran visionai: ok); integration `-run "VisionAI|TestAlarmsListAndRBAC"` pass
  (Docker); `pnpm typecheck` clean. `gofmt -l` lists 3 pre-existing unrelated files (agent/sample.go,
  provision/agent_status_test.go, store/maps_core_test.go).
- Decisions: confidence = LPR score else 0.75; watchlist always false (no source); open alarm = status
  != resolved; platform actors allowed (like events service); pins client-side.
- RDD assess on PR1 slice: medium, `slice_budget_reached` → review due.

## Next step
Native review of PR1 slice, then VAI-03/VAI-04.
