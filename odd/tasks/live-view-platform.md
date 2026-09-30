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
- [x] LV-1 — Feature flags (`GET /api/v1/features` + web hook) and baseline instrumentation: per-session reconnect counter + TTFF, characterization tests that document current reconnect behaviour.
- [x] LV-2 — Player state machine (UNINITIALIZED…EVICTED, transition cause/timestamp) + `PlayerSession` core; `MsePlayer` becomes a thin adapter.
- [x] LV-3 — `PlayerSessionManager` + provider mounted in `Layout` (sessions per camera, WARM TTL, eviction).
- [x] LV-4 — `VideoSurfaceLayer` + `SurfaceSlot` (persistent `<video>` positioned over cells via ResizeObserver/translate3d), behind flag. Done: 1f632e0.
- [x] LV-5 — Tiles keyed by camera: DnD swap semantics, layout change and expand without reconnect; 25/32 grids with visibility pausing. Mandatory tests: cell1→cell8 same session 0 reconnects; grid→expand same session 0 reconnects. Done: 656b5d9.
- [x] LV-6 — Snapshot poster + last frame, offline/unauthorized/error states with Retry, jittered backoff grouped per server; snapshot cache headers. Test: offline shows last snapshot + auto reconnect. Done: 7d91c84.
- [x] LV-7 — Gateway hardening: one audit row per session, ping/deadlines, structured error frame to client, Prometheus counters. Done: 813f791 (worktree feat/live-view-gateway), merged 3a80d80. Error frame {type:error,code,message,value}; knobs LIVE_AUDIT_WINDOW/LIVE_REVALIDATE_INTERVAL/LIVE_PING_INTERVAL/LIVE_PONG_WAIT; metrics openvms_live_*. Checks after merge: go build/test OK, golangci-lint, vitest (see progress). Route: delegated (parallel worktree). Gap: no e2e WS test; dedupe per API process; socket upgraded before auth (PO decision pending).
- [x] LV-8 — Rollout (flag default), `docs/live-view-architecture.md`, changelog. Done: docs/live-view-architecture.md (architecture, config, errors, metrics, troubleshooting, changelog); local compose enables OPENVMS_FEATURES=persistentPlayers,videoSurfaceLayer (code default off). Route: inline (docs + 1 config line). Deploy recorded in progress.
- [x] LV-9 — LIVE/REC toggle in Live view (PO request 2026-09-30): REC switches the current grid (same cameras/positions) to synchronized recorded playback reusing S2-8 sync + drift correction; live sessions SUSPENDED (not closed) and resume instantly on LIVE; day timeline under the grid (recording coverage of visible cameras + event markers), mouse-wheel zoom centered on cursor (24 h → ~2 min), drag to pan; controls play/pause, speed, ±10 s/±1 min, prev/next event, "now"; calendar day picker; clicking a time seeks ALL cameras there and plays; URL state `?mode=rec&t=`; REC plays up to 16 cameras (configurable), others show snapshot + notice. Defaults chosen by orchestrator, PO may adjust. TDD off. Done: a3a3b89 (timeline + scale/zoom math + calendar), faea095 (LIVE/REC integration, N-tile sync, URL state). Route: delegated (writer trigger: 2+ non-trivial files). Checks: web tsc, vitest (273 tests), eslint, vite build all green; Go untouched. Gaps: not verified in a real browser; no per-day recording highlight in the calendar; no per-camera VOD offset when a camera starts recording mid-window. Review: medium risk, declined by PO. Deployed (web rebuild, no migrations).
- [x] LV-10 — Top-bar LIVE/REC toggle + viewport-fit grid (PO request 2026-09-30): segmented EN VIVO (red, pulsing dot, motion-safe) / GRABACIÓN (amber) control portaled into the top bar next to the breadcrumb via a new AppShell top-bar actions slot (inline fallback outside the shell); breadcrumb reads OPERACIONES / GRABACIÓN in REC; grid gets a red/amber ring (no layout cost); sidebar is full-height with its own scroll; /live only (`fitViewport`): shell h-dvh, main overflow-hidden, grid rows split remaining height (`--grid-rows`), tiles fill cell with video object-contain; REC dock is in the flex height budget; <md falls back to page scroll. TDD off. Done: cd03ecc. Route: delegated (writer trigger: 2+ non-trivial files). Checks: web tsc, vitest (273 tests), eslint, vite build all green. Gaps: not verified in a real browser (video overlay alignment, 32-tile fit, 1024x600).

## Acceptance
Brief §"Core acceptance principle" and mandatory UI tests; reconnect counter shows 0 for
DnD/layout/expand in tests.

## Delivery
Strategy: ask-on-risk (forecast ~2.6k lines > budget; chain strategy to be chosen with the user before opening PRs). Push/PR are user decisions.

## Progress
- 2026-09-30: Pending items closed: d73b5db (401 before WS upgrade), 84d131b (delivery/notification pruning), 2b73454 (SMTP TLS tests), 3fbddc5 (server delete cleans notifications + disables rules instead of widening; migration 00019), c0547d8 playback drift correction (merge f0448ff). Review: high risk, declined by PO. Deployed after backup (pre-pending-*): migration 00019 applied, /health/ready ok, unauthenticated /live returns 401. Remaining gaps: HLS secondary does not recover when recording coverage returns; secondary native controls are overridden by sync.
- 2026-09-29: P0 complete (LV-1..LV-8). Review: medium risk, declined by PO (candidate-scoped). Deployed @5db5842 with make up after pg_dumpall backup (openvms-backups/pre-live-p0-*); /health/ready ok; OPENVMS_FEATURES=persistentPlayers,videoSurfaceLayer active. Pending: manual browser verification (see docs/live-view-architecture.md Known limitations).
TDD: off (user decision 2026-09-29). Started strict (RED observed for Go config), then the user turned it off; no
further tests were added beyond those already written. Ordinary checks run per task.
Route for LV-1..3: delegated (writer trigger: 2+ non-trivial files).
Checks per task (all green): go build, go vet, go test ./..., go test -tags integration ./internal/api/ (LV-1),
golangci-lint 0 issues, web tsc, vitest (227 tests), eslint, vite build.

- LV-1 (a0f4a62): flags via OPENVMS_FEATURES (comma-separated, case-insensitive; config.ParseFeatures) exposed at
  GET /api/v1/features (authenticated); web useFeatures() with all-off defaults and a DEV-only localStorage override
  (openvms.features.override); lib/live/playerMetrics.ts (connect attempts, reconnectCount, TTFF, transitions,
  window.__openvmsPlayerMetrics in DEV). Characterization tests for cell 1->8 / expand were NOT written (TDD off).
- LV-2 (b3d0624): lib/live/playerState.ts (10 states, allowed-transition table, cause+timestamp, metrics emit,
  debug via localStorage openvms.live.debug=1), backoff.ts (jittered, capped), PlayerSession.ts (owns <video>, MSE
  pipeline, reconnect, attach/detach/close); MsePlayer is a thin adapter (legacy path unchanged in behaviour).
- LV-3 (4e84007): PlayerSessionManager (acquire/release refcount, WARM TTL 30 s, maxWarmPlayers 8 LRU, soft
  maxConcurrentPlayers 32, clear on logout/user change/unmount), PlayerSessionProvider mounted in Layout,
  usePlayerSession; MsePlayer persistent prop driven by the persistentPlayers flag in Live GridTile.

- LV-6 (7d91c84), LV-4 (1f632e0), LV-5 (656b5d9). Route: delegated (writer trigger: 2+ non-trivial files). Committed in the
  order 6, 4, 5 because 4 renders the LV-6 status overlay and 5 uses the session suspend/WARM API from 6; each commit compiles alone.
  Checks (all green after LV-5): web tsc, eslint, vitest (252 tests), vite build; go build, go vet, go test ./..., golangci-lint 0 issues.
  * LV-6: streamErrors.ts (code -> label + retryable), PlayerSession `resilient` mode (error frames, 1008 close = unauthorized, stop until
    retryNow, in-memory last frame via canvas -> blob URL on close/suspend/error and every 20 s while ACTIVE, suspend on hidden tab,
    resume <video> after DOM-move pause), serverBackoff.ts (one shared timer + single probe per Frigate server, staggered release),
    PlayerStatusOverlay (last frame else cold `/media/v1/cameras/{id}/snapshot.jpg?h=360`, spinner only without image, Reintentar for
    stopped errors), snapshot `Cache-Control: private, max-age=5` on 200 (no-store otherwise). markWarm/markActive now only move
    ACTIVE<->WARM (they used to force ACTIVE on a still-connecting session, hiding the "Conectando" overlay).
  * LV-4: surfaceLayer.ts controller + SurfaceLayer.tsx (VideoSurfaceLayer in Layout, SurfaceSlot). Layer is fixed, z-1, pointer-events none;
    tile controls use z-[2]/z-[3]. Duplicate camera policy: one <video> per session, so the grid never mounts a second player for a
    camera already shown; a repeat (only possible from saved views) renders a snapshot + "Ya visible en otra celda", and placing a camera
    already on the grid moves it (swap) instead of duplicating.
  * LV-5: tiles keyed `camera:<id>` (flag on), swapTiles/placeCameraUnique/duplicateTileIndexes in liveGrid (reorderTiles kept for flag off),
    rectSwappingStrategy, expand keeps all tiles mounted (others `hidden`, session WARM) and never switches quality (flag on: tile quality
    always used, including 1x1), 5x5 and 8x4 (32) walls, IntersectionObserver suspends off-screen tiles (not tiles hidden by expand),
    page-hidden suspends every session. Acceptance tests in routes/Live.test.tsx (cell 1->8 via keyboard dnd, grid->expand->grid).
  * Gaps: not verified in a real browser (video overlay geometry/clipping, z-order over the app shell, suspend/resume, MSE poster capture);
    flags default off; WARM sessions of an expanded view still count against the 30 s TTL only after the Live page unmounts.

- 2026-09-30 LV-9: LIVE/REC toggle (a3a3b89, faea095). Master = selected tile if it has coverage else first tile with coverage; REC players capped at 16
  (`REC_MAX_PLAYERS`), extra tiles show snapshot + notice; live sessions suspended with reason "rec" (kept, last frame); canvas timeline with pixel-bucket
  event clusters; URL `?mode=rec&t=<ISO>`. See docs/live-view-architecture.md "LV-9".
