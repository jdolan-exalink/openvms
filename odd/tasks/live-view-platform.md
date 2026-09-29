# Live View Platform — P0 (Persistent Player Sessions)

Locator: `odd/tasks/live-view-platform.md` · Engram mirror: `odd/live-view-platform/tasks`
Branch: `feat/live-view-sessions` (from `fix/playback-purity-lint` @46bc543)

## Objective
Make Live View feel immediate: UI changes (drag & drop, layout change, expand/fullscreen,
navigation) must not reconnect media; no black tiles while connecting.

## Problem (evidence: docs/ARCHITECTURE_AUDIT.md)
- `GridTile` keyed by index (`routes/Live.tsx:273`) + `reorderTiles` shifting tiles
  (`lib/liveGrid.ts:29`) → moving cell 1→8 reconnects 8 streams.
- `MsePlayer` effect deps `[cameraId, quality]` (`components/MsePlayer.tsx:181`) → any
  quality change reconnects; expand switches sub→main and unmounts other tiles.
- Leaving `/live` drops every session; no snapshot poster (black while connecting);
  3-state player, no jittered/grouped backoff; `LIVE_VIEWED` audit row per reconnect.

## Scope (authorized 2026-09-29) and product decisions
- Implement P0 of `docs/specs/live-view-platform-brief.md` as 8 work units below.
- Defaults accepted by user: expand keeps the sub stream in P0 (main via seamless switch
  in P1); sessions stay WARM ~30 s after leaving Live (configurable); 25/32 grids pause
  non-visible tiles; last frame kept in memory only, cleared on logout.
- Out of scope now: P1 adaptive/seamless/transport/WebRTC, P2 ONVIF, P3 event bus/
  timeline, P4 AI (open questions in audit §5).

## Constraints
- TDD: OFF (explicit user decision 2026-09-29: "sin test"); ordinary checks (build, vet, typecheck, lint, existing suites) still run. Previously strict (user config). Runners: web `npx vitest run` (apps/web);
  Go `go test ./...` + `go test -tags integration ./internal/...`.
- Feature-flagged rollout; backward compatible; no hardcoded hosts/cameras/codecs.
- ~400 authored changed lines per task (advisory). Conventional Commits, no AI attribution.

## Tasks
- [ ] LV-1 — Feature flags (`GET /api/v1/features` + web hook) and baseline instrumentation: per-session reconnect counter + TTFF, characterization tests that document current reconnect behaviour.
- [ ] LV-2 — Player state machine (UNINITIALIZED…EVICTED, transition cause/timestamp) + `PlayerSession` core; `MsePlayer` becomes a thin adapter.
- [ ] LV-3 — `PlayerSessionManager` + provider mounted in `Layout` (sessions per camera, WARM TTL, eviction).
- [ ] LV-4 — `VideoSurfaceLayer` + `SurfaceSlot` (persistent `<video>` positioned over cells via ResizeObserver/translate3d), behind flag.
- [ ] LV-5 — Tiles keyed by camera: DnD swap semantics, layout change and expand without reconnect; 25/32 grids with visibility pausing. Mandatory tests: cell1→cell8 same session 0 reconnects; grid→expand same session 0 reconnects.
- [ ] LV-6 — Snapshot poster + last frame, offline/unauthorized/error states with Retry, jittered backoff grouped per server; snapshot cache headers. Test: offline shows last snapshot + auto reconnect.
- [ ] LV-7 — Gateway hardening: one audit row per session, ping/deadlines, structured error frame to client, Prometheus counters.
- [ ] LV-8 — Rollout (flag default), `docs/live-view-architecture.md`, changelog.

## Acceptance
Brief §"Core acceptance principle" and mandatory UI tests; reconnect counter shows 0 for
DnD/layout/expand in tests.

## Delivery
Strategy: ask-on-risk (forecast ~2.6k lines > budget; chain strategy to be chosen with the user before opening PRs). Push/PR are user decisions.

## Progress
(none yet)
