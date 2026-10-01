# Maps (Geospatial Security Operations)

Locator: `odd/tasks/maps.md` · Engram mirror: `odd/maps/tasks`
Branch: `feat/maps` (from `feat/frigate-config-editor` @31233de)
Brief: `docs/specs/maps-brief.md` · Design: `docs/maps/ARCHITECTURE.md` (be2789a)

## Objective
New top-level Maps page per the brief; MVP first (design §19), then Phase 2/3.

## PO decisions (2026-09-30)
1. Tiles: **self-hosted offline PMTiles** behind a provider abstraction (public OSM only for dev).
2. **Full alarm lifecycle in MVP** (NEW/ACKNOWLEDGED/ASSIGNED/INVESTIGATING/RESOLVED/CLOSED, comments, history) — additive, Alarms inbox keeps working (M-B7).
3. Camera coordinates: **"Sin ubicar" tray + "ubicar todas en el centro del sitio" in MVP; CSV import as M-B8**.
4. **No PostGIS** for now (lat/lng + GeoJSON jsonb).
5. (Default taken by orchestrator) MapLibre chunk accepted (lazy on /maps only). TDD OFF as in Live View,
   but the tests required by design §21 (RBAC filtering, realtime protocol, performance harness) are mandatory.

## Constraints
Feature flag `maps` (OPENVMS_FEATURES); no hardcoded sites/vendors; Maps consumes only OpenVMS
normalized APIs/event bus; RBAC server-side filtering; Conventional Commits, no AI attribution;
~400 authored lines per unit (advisory).

## Tasks (design §19; parallel tracks A/B/C for backend)
- [x] M-B1 — migration 00022 maps core, sqlc, permissions maps.*, feature flag (track A) (`eb53c22`)
- [x] M-B2 — read endpoints: config, overview, site, entities (RBAC filtering) (track A) (`2dc97be`)
- [x] M-B3 — placement writes, site coords, unplaced cameras, audit (track A) (`2307705`)
- [x] M-B4 — zones CRUD, geometry validation, rules ZoneResolver hook (track A) (`6a981cd`)
- [x] M-B5 — normalized realtime envelope, camera status publisher (track B) (`23fea3e`)
- [x] M-B6 — client hello/filters, resume buffer, coalescing, batching (track B) (`84c17c4`)
- [x] M-B7 — alarm lifecycle migration 00023 + Alarms inbox labels (track C) (`a3ca385`)
- [x] M-W1 — MapLibre route, canvas, theme styles, tile provider (PMTiles) (`12b6ce0`)
- [x] M-W2 — camera layers, clustering w/ badges, status icons, semantic zoom (`3cf166c`)
- [x] M-W3 — FOV cones, selection, breadcrumb, deep links (`71f3ed0`)
- [x] M-W4 — realtime store, pulse/ripple, animation budget (`a494a46`)
- [x] M-W5 — camera panel, hover preview, context menu, nearby cameras
- [x] M-W6 — alarm panel, site health, auto-focus (verified; commit pending parent)
- [ ] M-W7 — layers & filters panels, user prefs
- [ ] M-W8 — placement editor + Sin ubicar tray
- [ ] M-W9 — zone editor
- [ ] M-W10 — 5k-camera performance harness
- [ ] M-B8 — CSV import of camera positions

## Delivery
ask-on-risk; push/PR are PO decisions.

## Progress
- 2026-09-30: M-B1 completed (`eb53c22`).
- 2026-09-30: M-B2 completed (`2dc97be`).
- 2026-09-30: M-B3 completed (`2307705`: placement write endpoints, optimistic locking via If-Match, site geo PATCH, unplaced cameras tray endpoint, audit logging, and integration tests).
- 2026-09-30: M-B4 completed (`6a981cd`: zones CRUD endpoints, polygon geometry validation with bowtie/closed-ring checks, rules engine ZoneResolver seam, and integration tests).
- 2026-09-30: M-B5 completed (`23fea3e`: realtime v2 envelope with ID/TS/site_id/camera_id/server_id, JetStream sequence plumbing in Source/Feed, camera.status_changed decoder, HealthPoller per-camera diff computation, and NATS publisher).
- 2026-09-30: M-B6 completed (`84c17c4`: client control frames hello/filter, 5-min ring buffer resume + resync, camera status coalescing 500ms, server-offline 30s suppression, batch frame delivery >50 queued, and unit tests).
- 2026-09-30: M-B7 completed (`a3ca385`: migration 00023 alarm lifecycle transitions table + 6 status enum, OpenAPI transitions & active filter group, backend service validation & audit logging, web Alarms inbox badges & filter tabs).
- 2026-09-30: M-W1 completed (`12b6ce0`: maplibre-gl and pmtiles dependencies, lazy /maps route with search params, MapShell with HierarchyBreadcrumb and MapToolbar, MapCanvas wrapper, MapStyleController theme tokens with raster dark fallback and PMTiles protocol, navigation item behind maps feature flag, and full unit test coverage).
- 2026-09-30: M-W2 completed (`3cf166c`: camera layers, clustering with worst-child circle color and count/alarm badges, SDF status glyphs sprite generator, semantic zoom with country-level site health rings, EntityIndex with display-state priority engine, and full test suite).
- 2026-10-01: M-W3 completed (`71f3ed0`: FOV cones geometry with haversine destination point, FOV fill/outline layers, selection sync, 150ms viewport culling debouncing, deep-linking URL search params synchronization).
- 2026-10-01: M-W4 completed (`a494a46`: realtime store with 5-min ring buffer, dedup Set, throttled camera status flush 1/s, animation budget with concurrent ripple/pulse caps, reduced-motion fallback, and fx layers).

## Resumed scope (2026-10-01)
Explicit user confirmation today enables **STRICT TDD ON**, superseding historical OFF above.
Source: current user authorization; observe RED → GREEN → REFACTOR with sequential checks.
Runners: `pnpm --filter @openvms/web test`, `typecheck`, `lint`, `build`.
Reconciled observation 120 as historical planning; this file preserves completed B1–W4 evidence.
Resumed review boundary: `3a4cd96`. Delivery: `ask-on-risk`; chain_strategy: `feature-branch-chain` selected by user; ask-on-risk resolved.
At resumption, seven units remained: M-W5, M-W6, M-W7, M-W8, M-W9, M-W10, M-B8.
Forecast: 3000–4200 authored lines, advisory only; no code-golf.

### M-W5a — runtime and storage regression repairs
- [x] Loading-to-ready MapShell keeps hook order; hover manager lifecycle avoids render-time refs.
- [x] Nullable tenant identity cannot write a Live selection.
- [x] CameraPanel narrows geographic positions before accessing coordinates.
- [x] Live grid preserves existing stored cameras, dimensions, duplicate quality and full capacity;
      storage failures return false. Live restoration remains the authorization filter.
Route: delegated; trigger: multifile regression repairs. Scope excludes player/live handoff and W6–B8.
Required sequential checks: focused Maps suite including Maps.test.tsx, full web tests,
typecheck, lint, build, and `git diff --check`.
Status at W5a handoff: regression repairs verified; W5a commit pending (parent delivery); M-W5 was incomplete.
Mirror: synchronized to observation 120; full file and locator read back.



#### W5a verification
- RED: `pnpm --filter @openvms/web test src/lib/maps/liveGridHelper.test.ts src/routes/Maps.test.tsx`:
  5 failed / 4 passed (2 files failed); observed loading-to-ready hook-order error and storage regressions.
- GREEN: same command: 9 passed (2 files). Intermediate repair run: 2 failed / 7 passed,
  exposing duplicate quality preservation in placeCameraUnique; corrected locally in helper.
- Refactor: geographic flatMap narrowing and lifecycle ownership via state; no formatter configured.
- Final sequential checks: focused Maps suite 61 passed (13 files); full web suite 394 passed
  (65 files); typecheck PASS; lint PASS; build PASS (existing large-chunk warning); diff check PASS.
- Runtime harness: real Maps route loading-to-ready test with only WebGL canvas mocked.
- Authorization: helper preserves storage IDs, not grants; Live's parseSelection still filters against
  current authorized cameras on restoration.
- Rollback: remove only W5a hook/lifecycle/tenant guard, nearby-coordinate narrowing,
  grid preservation changes and regression tests; preserve preceding W5 scaffolding.
- W5 follow-ups identified at W5a (now completed below): nullable/empty-ID player acquisition, prewarm wiring, shared surface ownership,
  camera deep-link consumption in Live, and broader integration acceptance.
- Next: parent reviews this bounded slice and creates its work-unit commit; commit pending.

### M-W5 — remaining integration
Route: delegated single writer; trigger: shared player ownership and multifile Live handoff.
Strict TDD ON (explicit user confirmation 2026-10-01); failing tests before source implementation.
- [x] Hover stages 0/150/400/700ms, live-on-hover optional and default false, balanced prewarm release.
- [x] No empty-ID acquisition; one hover preview and at most four pins reuse shared manager/surface.
- [x] Context menu, double-click and Open Live handoff validate and consume authorized camera.
- [x] Nearby geographic cameras work; no nonfunctional PTZ controls.
Acceptance proof: real provider/component integration plus Live route/storage restoration.
Sequential runner: `pnpm --filter @openvms/web test src/lib/maps src/components/maps src/routes/Maps.test.tsx src/routes/Live.test.tsx src/lib/live`;
then full web test, typecheck, lint, build, and `git diff --check`.
Scope: M-W5 only; preserve W5a. No staging, commits, review, remote/PR/merge or deployment.
Status: outcome verified and committed as `eb592e6`; native review approved; exact acknowledgement burned authority (parent-confirmed).


#### M-W5 verification and delivery handoff
- RED: `pnpm --filter @openvms/web test src/components/maps/panel/CameraPreview.test.tsx src/lib/live/surfaceLayer.test.ts src/routes/Live.test.tsx`:
  7 failed / 29 passed (3 failed files). GREEN: same command, 36 passed (3 files).
- Live-hover opt-in RED: `pnpm --filter @openvms/web test src/components/maps/MapShell.test.tsx`:
  1 failed / 5 passed. Integration GREEN with preview/surface/Live suites: 42 passed (4 files).
- Final normalized sequential checks: focused command above PASS, 153 tests (22 files);
  full `pnpm --filter @openvms/web test` PASS, 408 tests (67 files);
  typecheck PASS; lint PASS; build PASS (large-chunk warning); `git diff --check` PASS.
- Initial lint caught synchronous set-state-in-effect in Live handoff; refactored to the existing
  render-time restoration pattern, then reran every required check sequentially.
- Runtime proof: real Maps and Live router/query/provider integration; only WebGL canvas
  and media transport boundaries mocked. Context menu, double-click and panel Open Live all
  retain prior authorized grid selections. Unit tests also cover unauthorized handoff rejection.
- Ownership: 400ms prewarm acquires without attaching; 700ms opt-in uses exclusive shared
  SurfaceSlot; empty ID never acquires. Four-pin cap suppresses duplicate hover surfaces.
- PTZ directional scaffold removed rather than shipping nonfunctional actionable buttons.
- Size: combined uncommitted W5/W5a scaffold source/tests versus HEAD: 1273 additions + 16 deletions = 1289 authored lines; task documentation separate.
- Clean possible slice boundaries: W5a regression repairs; shared preview ownership and Maps
  component integration; authorized Live handoff and router/storage integration. Preserve tests
  and docs with each behavior; no cosmetic shrinking. Parent selects delivery slices.
- Rollback: remove this continuation's optional acquisition guard, preserveOwner surface API,
  preview/panel shared-slot integration, hover opt-in, Live camera search/consumption and tests;
  retain W5a repairs and original scaffold. No source edits outside M-W5.
- No staging, commits, review, PR/merge, remote operation or deployment performed.
- Remaining implementation: M-W6, M-W7, M-W8, M-W9, M-W10 and M-B8.
- Parent-reported deployment blocker: actual local PMTiles asset is missing;
  ranged `/tiles/world.pmtiles` returns SPA HTML. Deployment stays pending until assets are
  provisioned and ALL remaining units are ready. This worker did not probe or change deployment.
- Work-unit commit: `eb592e6` (`feat(maps): complete camera previews and authorized live handoff`).
- Parent spot check: Maps shell, preview and Live suites PASS (39 tests, 3 files).
- Running resumed authored count: 1372 lines including task documentation (no generated code).
- This coherent local commit exceeds the advisory per-task size; future PR slices remain subject
  to the selected feature-branch-chain policy. No PR is created or authorized here.
- Native outcome: approved and exactly acknowledged; authority burned. Reviewed boundary: `17644d8`.
- No re-review of the consumed W5 target. Later fixes are separate work units.

## Later W5 reliability follow-ups (R3)
Parent-confirmed W5 review: approved; exact acknowledgement burned authority.
Current boundary: `17644d8`. Completed W5 acceptance/receipt remains unchanged.
Route: delegated single bounded writer; no native correction or re-review of consumed target.
Strict TDD ON; sequential checks; `feature-branch-chain` selected.
- [x] R3-hover-actions — retain controls across marker-to-preview pointer transition; cancel
  stale leave on preview/new-camera entry; delayed leave and cleanup release sessions.
- [x] R3-snapshot-recovery — failed camera A must not hide camera B's successful snapshot.
Runner: `pnpm --filter @openvms/web test src/lib/maps src/components/maps src/routes/Maps.test.tsx`;
then full web test, typecheck, lint, build, and `git diff --check`.
Status: both bug outcomes verified and committed: snapshot `10443f1`, hover `dac2054`.
Native assessment (parent): medium, 119 authored lines, review_due=false / under_budget.
Current accumulated-slice review boundary remains `17644d8`.
Scope excludes W6, staging, commits, review, remote operations and deployment.

### R3 verification (later bugs only)
- RED: `pnpm --filter @openvms/web test src/components/maps/MapShell.test.tsx src/components/maps/panel/CameraPreview.test.tsx`:
  3 failed / 11 passed (2 files failed). GREEN: identical command, 14 passed (2 files).
- Final sequential foreground checks after source normalization:
  focused Maps command PASS (75 tests, 15 files); full web test PASS (411 tests, 67 files);
  typecheck PASS; lint PASS; build PASS (large-chunk warning); `git diff --check` PASS.
- R3-hover-actions root cause: immediate marker leave unmounted the card before controls
  could be reached. A cancellable 150ms grace period bridges marker and preview; preview
  pointer/focus entry and new camera entry cancel stale leave. Cleanup remains immediate.
- R3-snapshot-recovery root cause: React reused the image DOM node with camera A's error
  display:none style. URL-keyed images isolate camera changes; onLoad clears visibility errors.
- Acquisition/release unchanged: tests observe delayed release, target replacement and unmount.
  Existing shared-manager/surface and accessibility control behavior remains covered.
- Independent rollback/commit boundaries: hover task includes manager delay, shell wiring,
  preview boundary callbacks and MapShell regression tests; snapshot task includes only the
  image key/onLoad hunk and failed-A to successful-B component regression.
- No native correction, re-review, staging, commits, remote operations or deployment performed.
  W5 approved/acknowledged authority remains burned; parent commits these later fixes before W6.

## M-W6 — alarm operations, health and incident focus
Route: delegated single writer; trigger: multifile new logic and realtime reconciliation.
Strict TDD ON; source explicit user confirmation 2026-10-01. Chain: feature-branch-chain.
Reviewed boundary: `17644d8`; W5 approved authority stays burned.
- [x] Permission-aware alarm acknowledge/assign/investigate/resolve/close, comments and history
  reuse normalized authorized alarm APIs; errors remain visible.
- [x] Realtime counts seeded from REST; terminal updates idempotent; server-only outage patches;
  reconnect/resync refetch maps; route-owned identity-scoped state prevents cross-user leakage.
- [x] Site health groups server root causes and provides site/camera navigation.
- [x] Incident focus defaults NONE; explicit opt-in respects site, manual navigation (15s),
  movement throttle (10s) and reduced motion; integrate real MapShell query data.
Focused runner: `pnpm --filter @openvms/web test src/lib/maps src/components/maps src/routes/Maps.test.tsx src/routes/Alarms.test.tsx src/lib/realtime.test.ts`;
then full web test, typecheck, lint, build and `git diff --check`, sequentially.
Scope excludes W7 preferences/backend editor/import/performance work. Existing contracts first;
missing contracts are reported rather than invented. No staging/commits/review/remote/deploy.
Status: verified; commit pending parent delivery. Native assessment/review remains parent-owned.


### M-W6 evidence and handoff (2026-10-01)
- AlarmPanel uses existing acknowledge/assign/investigate/resolve/close/comments/transitions/
  assignees endpoints. Read-only actors see history, not management buttons; the API remains
  authoritative per camera. Eligible transitions and pending actions disable controls;
  authorization/network errors are visible. Site list is bounded to the latest 100 alarms.
- MapShell owns a tenant-scoped realtime store keyed by existing Me user+tenant identity;
  authenticated Layout/Login already clear query caches at session changes. Default fallback
  singleton no longer subscribes globally. REST camera counts and known alarm states seed it;
  repeated resolved->closed updates decrement once. Server-only patches reach health and
  GeoJSON UNREACHABLE styling. Reconnect/resync clears overrides and invalidates Maps queries.
- Server outages group authorized affected cameras once, with working site/camera navigation.
  Focus defaults NONE; explicit current-site opt-in never selects/opens a stream. Site guard,
  10-second movement throttle, 15-second manual interaction suppression and reduced-motion
  duration zero are covered. Focus stays session-local; persisted preferences belong to M-W7.
- RED (observed commands; failed/passed counts):
  - `pnpm --filter @openvms/web test src/lib/maps/mapRealtimeStore.test.ts src/lib/maps/incidentPolicy.test.ts src/lib/realtime.test.ts`:
    4 failed / 21 passed, plus incidentPolicy missing-module suite failure. GREEN: 28 passed.
  - `pnpm --filter @openvms/web test src/components/maps/panel/AlarmPanel.test.tsx`:
    missing-module suite first, then skeleton 3 failed / 0 passed. GREEN: 3 passed.
  - `pnpm --filter @openvms/web test src/components/maps/MapShell.test.tsx`:
    initial integration 1 failed / 8 passed; GREEN 9 passed. Added focus-site regression
    RED 1 failed / 9 passed; final expanded integration GREEN 13 passed.
  - `pnpm --filter @openvms/web test src/lib/maps/entityIndex.test.ts src/lib/maps/mapRealtimeStore.test.ts`:
    2 failed / 19 passed (metadata UNREACHABLE and cleanup/reconnect flush). Combined GREEN
    with MapShell: 31 passed. Refactor added policy setter and reseeding on dataUpdatedAt.
- Final sequential foreground checks, after all source normalization:
  - `pnpm --filter @openvms/web test src/lib/maps src/components/maps src/routes/Maps.test.tsx src/routes/Alarms.test.tsx src/lib/realtime.test.ts`: PASS, 112 tests / 19 files.
  - `pnpm --filter @openvms/web test`: PASS, 428 tests / 69 files.
  - `pnpm --filter @openvms/web typecheck`: PASS.
  - `pnpm --filter @openvms/web lint`: PASS.
  - `pnpm --filter @openvms/web build`: PASS; existing large-chunk warning remains.
  - `git diff --check`: PASS (also repeated after this documentation update).
  The requested realtime.test.ts selector includes actual realtime.test.tsx. No backend changes
  or Go checks needed; no missing alarm API contract discovered. Earlier iteration typecheck
  found test-array nullability and lint found direct policy mutation; corrected before final run.
- Root causes: scaffold panels had no operational API wiring; realtime counts lacked REST
  baseline and alarm identity; server-only metadata changes were skipped and feature conversion
  ignored outage metadata; singleton lifetime exceeded actor lifetime; cleanup left flush pending.
  Backend normalizes all alarm lifecycle subjects to alarm.updated, so unknown active updates
  reconcile REST rather than assume creation. Structurally shared refetches require observing
  dataUpdatedAt as well as object identity to reseed after resync.
- Natural authored source+test size: 506 additions+deletions (generated files excluded), plus
  tracker evidence. Possible coherent review slices: realtime/render repairs 147; operational
  panels 140; incident policy 63; MapShell integration/tests 156. No artificial shrinking/splits.
- Rollback baseline: HEAD `dac2054`; W6 is an unstaged patch. Revert only W6 changed/new paths
  if needed; preserve W5 scaffolding, followups and approved/burned authority at `17644d8`.
  No staging, commit, native review, remote operation or deployment performed by this writer.
- Next: parent commits/assesses accumulated slice against `17644d8`, then dispatches M-W7.
  Remaining implementation: M-W7, M-W8, M-W9, M-W10, M-B8. Local deployment remains blocked
  by the missing PMTiles asset (ranged /tiles/world.pmtiles returns SPA HTML) and remaining units.
