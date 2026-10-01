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
- [x] M-W7 — layers & filters panels, user prefs (`cd56639`)
- [x] M-W8 — placement editor + Sin ubicar tray (`9174a44`)
- [x] M-W9 — zone editor (`b88abac`)
- [x] M-W10 — 5k-camera performance harness
- [x] M-B8 — CSV import of camera positions (`38c5eaf`)

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
- 2026-10-01: M-W7 completed (`cd56639`: /api/v1/me/map-prefs contract and handlers with enum/size validation and per-user isolation, client-owned layer/filter defaults, LayersPanel/FiltersPanel, canvas layer visibility, and render-time preference restoration).
- 2026-10-01: M-W8 completed (`9174a44`: MapEntity exposes the placement revision for If-Match, pure placement draft with undo/redo and 409 rebase, unplaced tray with click-to-place/drag/bulk-at-centre, properties form with 15° rotation and clamped FOV/range, and editor wiring in MapShell).
- 2026-10-01: M-W9 completed (`b88abac`: pure ZoneDraft with point-by-point drawing, close/reopen/undo and backend-mirrored geometry validation, ZonesPanel gated on maps.create_zone, zones fill/outline/label layers under a new zones layer group, MapCanvas zone rendering, and MapShell create/update/delete wiring with visible server refusals).
- 2026-10-01: M-B8 completed (`38c5eaf`: POST /api/v1/maps/placements/import with pure parsePlacementCSV, per-line errors, dry-run validation, atomic apply preserving existing props, maps.placement.import audit on apply, and web importPlacements + CsvImportForm + gated Importar CSV entry).
- 2026-10-01: M-W10 completed (window.__openvmsMapMetrics overlay gated on dev/?perf, deterministic synthetic 5k fixture generator, and the @openvms/test Playwright package: 5k render smoke against a production build with in-page API stubs, plus the perf CI job).

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

## M-W7 — layer/filter panels and user preferences
Route: delegated single writer; trigger: multifile new logic and contract change.
Strict TDD ON; source explicit user confirmation 2026-10-01. Chain: feature-branch-chain.
- [x] GET/PUT /api/v1/me/map-prefs contract, schemas MapUserPrefs/MapLayerPreference/MapFilters,
  backend validation (enums, 16 KiB cap) and per-user isolation behind the maps flag.
- [x] LayersPanel offers only toggles with a real layer (sites, coverage, cameras, events_alarm,
  events_motion); remaining groups are labelled Fase 2. FiltersPanel covers status and
  camera_types; the priority=alert checkbox was dropped as redundant with ALARM state.
- [x] Preferences restore at render time from the query result (no setState-in-effect); overrides
  record only this visit's changes, so a background refetch cannot move an operator's map, and
  the first run after the query settles skips its own save.

### M-W7 evidence (2026-10-01)
- RED/GREEN: backend integration `go test -tags integration -run TestMapUserPrefs ./internal/api/`
  PASS (3 tests: flag off returns 004, whole-blob replacement, admin/operador isolation, invalid
  payloads). Web `prefs.test.ts` + `LayersPanel.test.tsx` + `FiltersPanel.test.tsx` +
  `visibility.test.ts`: 25 tests PASS; MapShell hydration test 14 passed.
- Final sequential checks: focused Maps suite PASS, 117 tests / 21 files; full web PASS, 454 tests
  / 73 files; typecheck PASS; lint PASS; build PASS (existing large-chunk warning);
  `go build ./...` and `go vet ./...` PASS; `git diff --check` PASS; `make generate` idempotent
  (generated schema.d.ts/api.gen.go committed in the same change).
- Rollback baseline: HEAD `c433168`; M-W7 commit `cd56639`. No staging beyond that commit, no
  review, remote operation or deployment performed by this writer.
- Next: dispatch M-W8 (placement editor + Sin ubicar tray), then M-W9, M-W10, M-B8.

## M-W8 — placement editor and unplaced tray
Route: delegated single writer; trigger: multifile new logic plus a contract addition.
Strict TDD ON; source explicit user confirmation 2026-10-01. Chain: feature-branch-chain.
- [x] `MapEntity.rev` (placement revision) so the editor can send `If-Match`; entities read
  is the single source of the token, each save's ETag refreshes it.
- [x] Pure draft (`lib/maps/placementDraft.ts`): stage/undo/redo snapshots, one pending entry
  per camera, per-camera revision memory, conflict marking and explicit rebase.
- [x] UnplacedTray: click-to-arm then click the map, HTML5 drag onto the map, and
  "ubicar todas en el centro del sitio" (PO decision 3) as one undo step.
- [x] Save sends one PUT per change with `If-Match: "<revision>"` only when a revision is
  known; a camera without a placement is created without the header. 409 keeps the server
  message visible and parks the camera behind a "Rebase" action that re-reads entities.
- [x] Properties form rotates in 15° steps (wrapping the compass) and clamps FOV to 1..360
  and range to >= 0, the ranges `UpsertPlacement` validates.

### M-W8 evidence (2026-10-01)
- RED/GREEN backend: `go test -tags integration -run TestMapEntitiesExposePlacementRevision
  ./internal/api/` failed ("expected placement revision (rev) on placed entity") until the
  contract/model/handler change; then PASS. `go test -tags integration -run 'TestMapEntities|TestMaps'
  ./internal/api/` PASS (read/RBAC, write lifecycle, floor validation, geo+audit, zones,
  prefs, new revision test). `go build ./...` and `go vet ./...` PASS.
- RED/GREEN web: four new suites failed to resolve their modules first; then PASS:
  placementDraft 8, placements 4, UnplacedTray 6, PlacementPropsForm 4; MapShell integration
  gained 5 editor tests (stage/save without If-Match, undo before write, 409+rebase retry with
  `If-Match: "1"`, move keeps the camera's revision, bulk place at centre).
- Final sequential checks: focused Maps suite PASS, 144 tests / 25 files; full web PASS,
  481 tests / 77 files; typecheck PASS; lint PASS; build PASS (existing large-chunk warning);
  `git diff --check` PASS; `make generate` idempotent (schema.d.ts/api.gen.go committed with
  the contract change).
- Design deviations, deliberate: rotation/FOV live in the React properties form instead of
  on-map drag handles — the design assigns React only the form, and drag handles cannot be
  verified in this headless environment; floor placements, snapping, buildings/labels and
  zone drawing remain for M-W9/Phase 2. Also corrected maps panel utility classes
  (`border-border`/`bg-card`/`danger` → `border-line`/`surface`/`bad`): the theme defines no
  card/border/danger tokens, so those classes generated no CSS.
- Rollback baseline: HEAD `dee37d7`; M-W8 commit `9174a44`. No staging beyond that commit, no
  review, remote operation or deployment performed by this writer.
- Next: M-W9 (zone editor), then M-W10 (performance harness) and M-B8 (CSV import).

## M-W9 — zone editor
Route: delegated single writer; trigger: multifile new logic (contract and backend already
landed in M-B4). Strict TDD ON; source explicit user confirmation 2026-10-01.
- [x] Pure draft (`lib/maps/zoneDraft.ts`): vertices grow on map clicks until the polygon is
  closed; close refuses fewer than 3 points, undo keeps the draft editable and reopens a
  closed ring, and validation mirrors `internal/maps/geometry.go` with the same messages
  (name required, >= 3 points, closed before save, no self-intersection).
- [x] `zoneDraftToPolygon` closes the GeoJSON ring (first vertex repeated last) and
  `zoneToDraft` round-trips a stored zone without its closing vertex.
- [x] ZonesPanel: lists the site zones with kind labels, every write gated on
  `maps.create_zone`, Guardar disabled until the draft validates, and the server's refusal
  rendered as an alert next to the draft that stays on screen.
- [x] zonesLayer source/fill/outline/label with a new `zones` group in `LAYER_GROUPS` and
  `builtLayerIds()` parity; MapCanvas adds the source under the site layers and re-syncs the
  feature collection when the query changes.
- [x] MapShell wires `siteZonesQuery` for the selected site (the map draws polygons in every
  mode), drafts points from `onMapClick`, and saves via POST/PATCH with `DELETE` for removal;
  zone state is cleared only on success.
- [x] `ZoneKind` corrected to the contract enum (`security|perimeter|warning|custom`);
  `stubApi` accepts additive `"METHOD /path"` keys so two verbs on one resource stub
  different outcomes.

### M-W9 evidence (2026-10-01)
- RED: zoneDraft.test.ts (4), zonesLayer.test.ts (3), ZonesPanel.test.tsx (5) failed on
  missing modules and MapShell's 3 new zone tests failed on the absent panel before any
  implementation existed; the ZonesPanel drawing test also exposed a missing
  `onReopenPolygon` destructure in the test itself, and zoneDraft's first GREEN attempt
  lacked the undo floor (3→2 allowed, 2→2 no-op) the test demands.
- GREEN: focused Maps suite PASS, 159 tests / 28 files (+15). Final sequential checks: full
  web PASS, 496 tests / 80 files; typecheck PASS; lint PASS; build PASS (existing
  large-chunk warning); `git diff --check` PASS; `go build ./...` and `go vet ./...` PASS
  (no Go changes in this unit); `make generate` idempotent (no generated deltas).
- Design deviations, deliberate: zone writes carry no If-Match — the contract's
  update/delete take none (M-B4), so conflicts cannot occur and no rebase flow exists for
  zones, unlike placements. The zones list is fetched for the selected site in every mode so
  the polygons are visible outside the editor; the panel itself only renders in edit mode.
  Zone geometry validation runs client-side first with the backend's exact messages, so the
  400 path only surfaces server-side refusals (auth, feature flag, cross-request changes).
- Rollback baseline: HEAD `55cc513`; M-W9 commit `b88abac`. No staging beyond that commit, no
  review, remote operation or deployment performed by this writer.
- Next: M-W10 (performance harness), then M-B8 (CSV import).

## M-B8 — CSV import of camera positions
Route: delegated single writer; trigger: multifile new logic (contract, backend and web all
change). Strict TDD ON; source explicit user confirmation 2026-10-01.
- [x] Contract `POST /api/v1/maps/placements/import`: `{site_id, csv, dry_run}` answered by
  `MapImportReport {dry_run, rows, upserted, errors[{line,message}]}`; generated into
  `schema.d.ts` and `internal/api/gen/api.gen.go`.
- [x] Pure parser (`internal/maps/import.go`): resolves each row's camera by UUID, display
  name (case-insensitive/trim) or remote name; ambiguous or unknown names, duplicate rows and
  out-of-range values each yield at most one error per physical line (`csv.Reader.FieldPos`),
  checked in the order camera → resolution → duplicate → lat → lng → bearing → fov → range,
  with the same ranges as `UpsertPlacement`. BOM stripped via escape (`"\ufeff"`), a BOM
  literal in Go source breaks compilation.
- [x] Service `ImportPlacements`: Tx, `maps.edit_device` required even for dry runs, cameras
  marked used only after a row validates fully, existing placement props preserved, apply
  skipped entirely when any row is invalid (atomic), audit `maps.placement.import` recorded
  only when applying.
- [x] Handler `ImportMapPlacements` mirrors `UpdateSiteGeo`: 404 with the flag off, nil body
  → `maps.ValidationError`, report errors mapped to the contract struct; static chi route
  wins over `{entityType}/{entityId}`.
- [x] Web `lib/maps/import.ts` (`api.POST` + `unwrap`), `CsvImportForm` with Validar (dry
  run) / Importar / Cancelar, per-line report, and an `Importar CSV` entry in the Sin ubicar
  tray offered only to `maps.edit_device`; only a clean apply refreshes
  `["maps","unplaced",id]` and `["maps","sites",id,"entities"]` and closes the form.

### M-B8 evidence (2026-10-01)
- RED backend: unit test failed to compile on the missing `siteCamera`/`parsePlacementCSV`,
  integration test on the missing `ImportMapPlacements` method — both before any
  implementation existed. One fixture bug found while red: a backtick string kept `\n`
  literal, so the parser correctly saw one line; rewritten as an interpreted string.
- GREEN backend: `go test ./internal/maps/` PASS, `go test -tags integration -run
  'TestMapPlacementsImport' ./internal/api/` PASS (both tests).
- RED web: `CsvImportForm.test.tsx` failed on the missing module, `UnplacedTray.test.tsx` and
  the 3 new MapShell import tests failed on the absent `Importar CSV` button; the permission
  gate test confirmed no entry point exists without `maps.edit_device`.
- GREEN web: editor suites 13 tests PASS, MapShell import tests 3 PASS; focused Maps suite
  PASS, 169 tests / 29 files.
- Final sequential checks: full web PASS, 506 tests / 81 files; typecheck PASS; lint PASS;
  build PASS (existing large-chunk warning); `git diff --check` PASS (including the new
  files); `go build ./...` and `go vet ./...` PASS; `go test -tags integration -run
  'TestMaps' ./internal/api/` PASS (23.9s) plus `./internal/maps/` and `./internal/api/`
  unit PASS; `make generate` idempotent (identical `git diff` hash across two runs).
- Design deviations, deliberate: the web gates the import button on `maps.edit_device`
  rather than inheriting the editor's `maps.edit || maps.edit_device`, because the endpoint
  requires the former and a `maps.edit`-only operator would always get a 403; camera
  resolution matches the single-placement upsert ranges, not looser CSV-friendly ones, so a
  file that validates in the dry run also applies.
- Rollback baseline: M-W9 docs `a0e38b9`; M-B8 commit `38c5eaf`. No staging beyond that
  commit, no review, remote operation or deployment performed by this writer.
- Next: M-W10 (performance harness), then push.

## M-W10 — 5k-camera performance harness
Route: delegated single writer; trigger: new test package + overlay wiring. Strict TDD ON;
source explicit user confirmation 2026-10-01. PO decision for this unit: **render-only
smoke** (no WS event flood), package named `@openvms/test`.
- [x] `lib/maps/perfFixture.ts`: deterministic (mulberry32, fixed seed) wire-format camera
  generator; count/seed/bbox/status-mix are the contract; import-free so the Playwright
  package loads it directly across package boundaries.
- [x] `lib/maps/perfMetrics.ts`: the design's overlay as `window.__openvmsMapMetrics` —
  FPS from a 30-frame rAF-delta window, entities visible, events/s (1 s window), WS lag
  (latest `now − frame.ts`), time to first render, and the long-task budget (≤ 50 ms)
  which starts only when the render loop first goes idle. Collection runs in dev builds
  or with `?perf` in the URL; MapShell installs it and feeds entities/first-paint/frames.
- [x] `packages/test` (`@openvms/test`): Playwright config (chromium + `--enable-unsafe-swiftshader`,
  `vite preview` of the production build), `tests/maps-perf.spec.ts` stubbing every
  `/api/v1` call in-page and serving the 5k fixture through the real fetch path; asserts
  entities = 5000, FPS ≥ 50 (`MAPS_PERF_MIN_FPS` to lower on weak runners), max long task
  ≤ 50 ms, first render < 5 s, and zero uncaught page errors, with WebGL2 as precondition.
- [x] CI: `perf` job (browser cache, `playwright install --with-deps chromium`, smoke run,
  report artifact on failure); workspace gains `packages/test`; root gains `test:perf`.

### M-W10 evidence (2026-10-01)
- RED: perfFixture/perfMetrics suites failed to resolve their modules (14 units unwritten);
  the MapShell overlay test failed with the sampler absent before any wiring existed.
- GREEN: perfFixture 5 + perfMetrics 9 unit tests; MapShell +2 (overlay installed with
  `?perf=1`, absent when DEV is stubbed false and no param). Focused Maps suite PASS,
  185 tests / 31 files; full web PASS, 522 tests / 83 files; web typecheck, lint and build
  PASS; `@openvms/test` typecheck PASS; `git diff --check` PASS; `go build ./...` and
  `go vet ./...` PASS (no Go changes); `make generate` idempotent (identical diff hash).
- Smoke run evidence (chromium headless-shell 153, SwiftShader GL): 5 consecutive green
  runs; snapshots ~FPS 60, time to first render 260–320 ms at 5k, max long task 0.
- The three failures the smoke caught, all real and fixed with tests: (1) a buffered
  PerformanceObserver replays cold-start entries after the mark — the budget now keys on
  the task's own `startTime`, not the delivery time; (2) "first render" marked at
  data-in-state fired before the first paint — the mark waits two rAFs; (3) the init tail
  (first cluster `setData`) still hit 55–63 ms after paint on software GL — the budget now
  starts at MapLibre's first `idle` (`markRenderSettled`), which is the design's steady
  state. A mocked-canvas test also exposed `map.once` being called unguarded on fakes.
- Design deviations, deliberate: the smoke is render-only — the 50 events/s + 500-alarm
  flood and "WS stays connected" checks need a live hub (or a WS stub) and were deferred
  by PO decision; the overlay ships in dev builds or behind `?perf` rather than dev-only,
  so the smoke can profile a production build; CI keeps the design's 50 ms / 50 FPS bars
  (weaker hardware lowers them explicitly via `MAPS_PERF_MIN_FPS`, never silently).
- Rollback baseline: M-B8 docs `b171e12`; M-W10 commits listed above. No staging beyond
  that, no deployment performed by this writer.
- Next: push `feat/maps` to `origin` (PO decision recorded 2026-10-01).

### M-W10 follow-up (2026-10-01, after first Docker deploy)
- The deployed stack rendered a dead map: MapLibre v6 resolves its worker relative to its
  own bundle URL, so production builds asked for /assets/maplibre-gl-worker.mjs, which no
  bundler emits — Caddy answered with the SPA fallback and every worker died ("Worker
  failed to load"; jsdom never exercises worker loading, which is why the unit suite
  missed it). The perf smoke itself was blind too: fps/entities come from the overlay and
  React state, not from GL. Fixed with Vite `?worker&url` (self-contained bundled worker,
  `worker.format: "es"`) wired through `maplibregl.config.WORKER_URL` (`adacbbe`), and the
  smoke now fails on console errors, WebSocket-noise excluded (`3a54ba2`); verified served
  as JavaScript through Caddy and smoke green ×6.
- Deployed basemap: `/tiles/world.pmtiles` (the DefaultConfig path) had no file behind it
  ("Wrong magic number"); a real PMTiles archive now lives in the web container (ephemeral
  until the image is recreated).
- Known pre-existing, out of Maps scope: GET /notifications 403s for the platform admin —
  `ListNotifications` requires a tenant and the platform user has none
  (internal/rules/service.go); and Live media WebSockets cannot open against frigate-mock
  (no real streams).
  [Update 2026-10-01: the bell part shipped in `6a863ba` — the Layout no longer renders
  NotificationBell without a tenant.]

### Basemap decision change (2026-10-01, PO override)
- The sample PMTiles extract could not carry a real basemap (single-city extract), and the
  user decided: **public OSM raster is the shipped default** (`osm-public`, offline false),
  overriding design decision 1 for now — self-hosted offline PMTiles remains the
  production goal and returns via config when an archive is available (mount
  `deploy/tiles/` locally).
- Default view lands on **Latin America** (-14.2, -51.9, zoom 3) instead of null island,
  and at startup the API geolocates the server's public IP (freeipapi, best-effort 4 s,
  `OPENVMS_MAPS_CENTER="lat,lng"` override) to center the default view on the server's
  region (`internal/maps/geoip.go` + wiring in `apps/api/main.go`).
- Caddy CSP gained `https://tile.openstreetmap.org` and `https://*.tile.openstreetmap.org`
  in img-src/connect-src — a CSP wildcard does not match the bare host, which is exactly
  where the tiles come from (first attempt shipped only the wildcard and the browser kept
  blocking every tile).

### Basemap migration to OpenFreeMap (2026-10-01, PO decision)
- Raster OSM was superseded the same day by **OpenFreeMap hosted vector tiles**: provider
  `openfreemap`, kind `vector-style`, `style_url_light = .../styles/liberty`,
  `style_url_dark = .../styles/dark` — the web `vector-style` provider path (M-W1) needed
  zero changes: the style URL is the initial style (light/dark by theme) and theme swaps
  diff on top. Full vector detail, one host for style/tiles/sprites/glyphs, no API key.
- Caddy CSP gained `https://tiles.openfreemap.org` in img-src/connect-src; OSM raster
  fallback hosts stay. `TestDefaultConfig` and `GetMapConfig` pin the new contract; the
  geoip precedence test was made hermetic (it had started passing against the real
  network and would never exercise the fallback).

## New units (2026-10-01, PO request): edit-mode monitoring center + camera editor
- [x] M-W11 — Persistent monitoring center: in edit mode (maps.edit) pin the site's
  monitoring center from the current view (PATCH /sites/{id}/geo: lat/lng/default_zoom);
  an open map re-centers when the selected site (or its center) changes.
- [x] M-W12 — Camera icons: bullet/dome/ptz selectable in the placement properties form,
  persisted through placement `props`, rendered per type in the canvas sprite.
- [x] M-W13 — Drag & drop of placed cameras in edit mode: pointer drag stages a move
  draft (keeping the If-Match revision) that the existing save flow writes.

### M-W11 evidence (2026-10-01)
- [x] M-W11 completed (`875f909`): `Fijar centro de monitoreo aquí` in the edit aside
  (gated on maps.edit, the SiteGeo PATCH permission) saves the framed view
  (position + rounded zoom) through the new `lib/maps/sites.ts`; overview+sites refetch;
  MapShell now follows center/zoom prop changes after map init (easeTo, 0 ms under
  reduced motion) and re-syncs the site selection from the URL, so back/forward and pasted
  deep links land on the right site with its monitoring center.
- RED: the three new MapShell tests failed before wiring (no button, no easeTo on site
  change). Two build findings fixed on the way: the new hooks were first placed below the
  config early-returns ("rendered more hooks than during the previous render") and moved
  above them; the URL-selection sync first used setState-in-effect and was rewritten to
  the adjust-during-render pattern to satisfy the react-hooks lint rule.
- Checks: MapShell 30/30, full web 528/528 (83 files), typecheck/lint PASS, perf smoke
  PASS. The centering behavior inside the real MapCanvas GL component remains
  headless-unverifiable; the canvas harness exercises the MapShell contract instead.

## M-W12 and M-W13 — camera type and placed-camera dragging (2026-10-01)
Route: delegated per task; trigger: five-file mapping and multi-file behavior spanning
placement draft/form/MapShell/canvas. Strict TDD ON (user-confirmed): RED → GREEN → REFACTOR.
Runner: `pnpm --filter @openvms/web test`; required closure checks are focused Maps tests,
full web tests, `typecheck`, `lint`, `build`, and `git diff --check`, all sequential.
Delivery: `ask-on-risk`; chain strategy `feature-branch-chain` (user-selected). M-W12 is
committed locally as `f911c20` and M-W13 as `2dbd499`. The Maps frontend is deployed to the
existing local `openvms` Compose stack. No remote delivery has occurred.

- [x] M-W12 — Add camera type (`bullet`/`fixed`, `dome`, `ptz`) to placement draft/form,
  MapShell and save props; render a per-camera glyph while preserving server alarm/offline/
  warning/unreachable icon precedence. Preserve legacy backend type/flags (including
  `fisheye`, `lpr`, `ptz`) when editing; never silently normalize or erase flags.
  Acceptance: form round-trips supported type, save persists props, canvas chooses type glyph,
  and status precedence remains unchanged. Rollback: W12 source/tests only.
- [x] M-W13 — Pointer-drag already-placed cameras only while edit mode is active; stage one
  move retaining the original knownRevision/If-Match, update draft position and preview it,
  without one undo snapshot per pointer movement. Acceptance: pointer interaction stages the
  final location, preserves revision, produces a single undoable move, and does not move
  cameras in view mode. Rollback: W13 source/tests only.
- Unit boundary: W12 and W13 remain separate rollback/work-unit boundaries, committed as
  `f911c20` and `2dbd499` respectively.

### M-W12 evidence
- RED: supplied focused five-file suite had 8 failures (63 pass) across the absent type UI,
  draft helper, props serialization, camera glyph selection, and MapShell type save/drag paths.
- GREEN: focused Maps suite excluding the distinctly scoped pending M-W13 pointer-drag test:
  63 passed, 1 skipped (5 files). Type selection and legacy PTZ/LPR flags are carried through
  the placement props; online-only type glyph selection leaves alarm/offline/warning/unreachable
  state glyphs authoritative.
- Full web suite was attempted and has one expected pending M-W13 failure; typecheck/build also
  report the test's not-yet-implemented drag callback props. Lint PASS (one existing center
  dependency warning); full final verification is repeated after W13 closes.
- W12 rollback boundary: `placementDraft.ts`, `placements.ts`, `PlacementPropsForm.tsx`,
  `entityIndex.ts`, `sprite.ts`, and the W12 seed in `MapShell.tsx` plus associated W12 tests.
- W12 final checks, repeated at W13 closure: focused suite 64/64; full web 536/536 (83 files);
  typecheck PASS; lint PASS (one center dependency warning); build PASS (existing >500KB
  chunk warning); `git diff --check` PASS.
- W12 commit: `f911c20` (`feat(maps): add camera type icons and placement props`). Its post-commit
  accumulated range assessment was high (91 paths/8086 lines); user declined that candidate.

### M-W13 evidence
- RED: the isolated MapShell drag test failed before implementation because no drag callbacks
  were wired and Save never appeared. Its supplied test also consumed the Request body twice;
  corrected to parse once while retaining both assertions.
- GREEN: focused Maps integration + draft helper, 42/42; full focused Maps command, 64/64.
- Drag is gated by edit-mode callbacks and begins on an already-rendered camera feature;
  pointer movement stages only the latest position with knownRevision, uses a single undo
  snapshot per drag gesture, and the canvas camera collection previews draft coordinates.
- W13 final checks: full web 536/536 (83 files); typecheck PASS; lint PASS (one existing
  center dependency warning); build PASS (existing >500KB chunk warning); `git diff --check`
  PASS. Runtime Maps canvas pointer wiring is covered through MapShell callback integration;
  no standalone WebGL interaction harness was added.
- W13 rollback boundary: MapCanvas drag callback wiring, MapShell drag staging/preview,
  updateStagedPosition, and the drag integration test adjustment.
- W13 candidate assessment: medium risk, `review_due=false` / `under_budget`; user declined
  this candidate, so no review receipt or approval exists. Ordinary delivery is unmanaged by
  that candidate choice.
- W13 commit: `2dbd499` (`feat(maps): add edit-mode placed-camera drag`). Its committed
  accumulated-range candidate was high risk due to process/shell evidence in
  `.github/workflows/ci.yml`; the user declined review for that candidate. No review receipt
  or approval exists.
- Local deployment: rebuilt and recreated only the `web` service in Compose project `openvms`;
  API liveness, readiness, and system-info endpoints returned HTTP 200, and `/maps` returned
  HTTP 200. Full `nurby` deployment was not used because its ports conflict with the already
  running `openvms` stack. No remote operation occurred.

## Operational completion follow-up (2026-10-01)

Objective: make Maps discoverable and operational when opening `/maps?mode=live`,
`investigate`, `analytics`, or `edit`, and reconcile current operational documentation.
The reported deployment shows only the world basemap and changing mode buttons; this
report is not proof of its deployed version, permissions, inventory, or placements.
Working baseline: `de66c1c`. Preserve all prior completion and verification evidence above.

### Current authorization and verification policy

- Authorized: local Maps implementation, supporting regression coverage, and documentation.
- Effective TDD for this follow-up: **OFF / tests deferred**, from the user's explicit
  instruction `no hagas los test hasta que terminemos todo`. Historical STRICT TDD and
  RED/GREEN evidence above remain historical and are not rewritten by this override.
- Do not execute tests, typecheck, lint, build, or deployment during implementation.
  Run the final checks only after all authorized implementation/documentation is complete.
- Do not mark the units complete or create work-unit commits before observing final check
  proof. Record failures and unavailable/manual checks honestly, without claiming closure.
- Excluded until separate target/session approval: remote probes, remote deployment,
  credential/session discovery, and live-account permission/grant changes. The reported
  `http://10.1.1.24:8000/maps` URL identifies the symptom, not remote execution authority.
- No push, pull request creation, or merge is authorized. Reuse delivery strategy
  `ask-on-risk` and the existing user-selected `feature-branch-chain` chain strategy.
- Initial authored-change forecast: approximately 350–650 additions plus deletions across
  navigation/data-state behavior, operational mode content, regression coverage, and docs.
  This is an estimate, not a gate or justification for code-golf. Measure actual work-unit
  diffs after implementation; preserve cohesive rollback boundaries and plan focused slices
  if delivery exceeds the existing approximately 400-line PR budget.

### Findings and scope

Local code confirms that site entities load only for a selected site (or the sole site),
while the global breadcrumb hides its selector until a site is already selected. Sites
without coordinates have no clickable map marker. Camera inventory and map placements
are separate: overview counts inventory, but entities returns placements. Unplaced cameras
are fetched only in active editing. Non-edit modes currently share the same operational
content, and overview/entities/unplaced/zones failures are not explained in the shell.
These facts explain a local navigation dead end; deployed state remains unverified.

| ID | Work unit and acceptance | Route and trigger evidence |
| --- | --- | --- |
| M-W14 | Discoverability, navigation, and data/error states: expose site selection from global view, including coordinate-less sites; synchronize URL mode/selection; explain loading, failed requests with recovery, empty inventory, unplaced cameras, and filtered/hidden markers; guide authorized users into the existing editor without inventing positions or granting access. | Delegated direct: mapping required 4+ files; implementation spans 2+ non-trivial route/shell/breadcrumb/state files. |
| M-W15 | Useful live/investigate/analytics behavior using existing APIs only: live camera availability/preview actions; camera-scoped Events/Playback investigation entry points with permission checks; factual inventory/placement/health analytics. Preserve existing placement, zones, FOV, filters, layers, and monitoring-center behavior. | Delegated direct: existing API/permission/route mapping and 2+ non-trivial shell/panel/API integration files. |
| M-W16 | Operational docs and stale README/status cleanup: explain commissioning, inventory versus placement, site selection, permissions, each mode, failure recovery, and the pending final acceptance/deployment checklist. Preserve historic evidence and distinguish implemented from operationally verified. | Delegated direct: analytical documentation preparation across feature document and existing operational docs/README. |
| M-W17 | Final validation and deployment: execute applicable functional/static/build checks after M-W14–16 implementation is finished; perform authorized runtime acceptance; obtain explicit destination/operation/credential-or-session approval before remote work; deploy complete authorized solution only after final proof. | Delegated verification: execution checks require a fresh verifier; deployment remains blocked on operational authorization, not granted by local implementation. |

- [x] M-W14 — Local discoverability/navigation/error states implemented and automated checks observed.
- [x] M-W15 — Local distinct mode workflows implemented and automated checks observed.
- [x] M-W16 — Operational docs and stale status reconciled and structurally reviewed.
- [ ] M-W17 — Final checks, runtime acceptance, and explicitly authorized deployment observed.

### Final acceptance scenarios

1. **Live:** select any authorized site from a global multi-site view, including unlocated
   sites; see placed cameras; understand unplaced inventory; previews respect `live.view`.
2. **Investigate:** a distinct camera-selection workflow opens existing Events/Playback
   with the intended camera context and appropriate permissions; no fabricated history.
3. **Analytics:** display actual authorized site/inventory/placement/availability summaries;
   do not imply unsupported historical analytics or heatmaps.
4. **Edit:** an authorized user selects an unlocated site, places and saves cameras, reloads,
   moves placed markers, edits supported type/FOV, manages allowed zones, and persists the
   monitoring center. Unauthorized users receive clear guidance rather than dead controls.
5. **Monitoring:** revisiting a site restores its center/zoom; availability/alarms reflect
   authorized cameras; hidden layers or filters have explanatory recovery states.
6. **All modes:** expose request failures; deep links and browser navigation synchronize
   state; toolbars/panels remain reachable without depending on geographical markers.

Indoor floor-plan workflows, historical geospatial analytics, fabricated coordinates,
new provider infrastructure, and automatic live-admin grant repair are not implied by this
follow-up. Any newly discovered product requirement is returned to the parent for scope.

### Deferred final runners and proof

Run sequentially after implementation is complete, recording exact outcomes:

```bash
pnpm --filter @openvms/web test
pnpm --filter @openvms/web typecheck
pnpm --filter @openvms/web lint
pnpm --filter @openvms/web build
pnpm test:perf
go test -race ./...
go test -race -tags integration ./...
git diff --check
```

Integration/performance/runtime checks depend on their existing infrastructure and must
be reported pending/unavailable if it cannot safely be used. A generated bundle or HTTP
200 alone does not prove camera placement, editor interaction, permissions, or media.
At initial planning no follow-up checks had run. Final local web, ordinary/integration Go race,
and performance results are recorded below. Work-unit commits/native review receipt and installed-
account runtime acceptance/deployment remain pending; local checks do not certify production.

### Recovery and next step

The local `odd/tasks/maps.md` contains the complete historical record. Parent readback of
Engram observation 120 found an old truncation warning inside the saved content itself;
that incomplete historical mirror was repaired from the local document as multipart historical
observations `odd/maps/tasks/history-1` and `odd/maps/tasks/history-2`, with the current scope
under `odd/maps/tasks`. Tool content limits prevent claiming one full-file observation.
Preserve history; do not reconstruct missing evidence from previews or summaries. The
parent owns persisting/readback of updated current scope and retained historical parts.

Next: parent records local work-unit commits and reconciles delivery policy. M-W14–16 local
implementation/docs and automated verification are complete; M-W17 remains pending real-account
runtime acceptance and explicit deployment destination/operation/session authorization.


### M-W14–16 initial implementation handoff (historical: checks were deferred)

- M-W14 source authored: global selector includes coordinate-less sites; invalid deep links,
  identity/config/data failures and retries are visible; inventory and placements are separated;
  filtered/hidden marker recovery resets only view preferences. Site/mode browser history and
  externally changed URL camera selection synchronize; site changes clear stale unsaved drafts.
- M-W15 source authored: camera search/selection and Live entry, no preview acquisition merely
  for investigation/analytics modes, Events/Playback camera-context actions under existing
  permission checks, and explicitly current-state overview/placement summaries. Editor/zone/
  drag/FOV/type/center paths remain in place. No fabricated coordinates/history or grant changes.
- M-W16 docs authored: `docs/maps/OPERATIONS.md` and README operational status; historical
  evidence retained and mirror multipart limitations corrected. Final closure checkboxes above
  remain unchecked until parent final verification and runtime acceptance are observed.
- Regression coverage authored: global coordinate-less breadcrumb, operational empty/error/
  retry/filter recovery/permission/analytics states, route mode navigation/browser back, and
  camera-scoped Events handoff. These tests have NOT been executed.
- Execution proof: no tests, typecheck, lint, build, performance, Go checks, deployment, commit,
  staging, native review, remote probes or remote delivery executed by this writer.
- API contracts reused: maps overview/entities/zones/config/prefs, existing cameras inventory
  (`cameras.view`), unplaced (`maps.edit`), Live `camera`, Events `camera` + `site`, Playback
  `camera`. A `maps.edit_device` editor can derive unplaced inventory from successful camera
  inventory + placements instead of broadening the unplaced endpoint authorization.
- Rollback boundaries: M-W14 navigation/state panel and shell integration + regression tests;
  M-W15 mode content/actions and preview gating in shell/panel + mode integration tests;
  M-W16 operational guide/README and this handoff only. M-W14 and M-W15 share the operational
  panel; retain navigation/error scaffolding if reverting only mode behavior.
- Future work-unit delivery: navigation/error recovery with tests; mode workflows/permission
  gates with tests; operational documentation alongside those slices. Existing feature-branch-
  chain policy applies, but no commit/PR or approved review outcome is claimed now.
- Next: parent runs final verification only now that implementation/docs are authored, repairs
  any actual failures, and obtains explicit operational authorization before deployment.

### Writer final web verification (2026-10-01)

User-requested test deferral was honored until all M-W14–16 source and documentation were
authored. Parent then authorized sequential foreground verification; no mutating normalizer
is configured in the web package or root. No native review freeze, commit or deployment occurred.

- Initial `pnpm --filter @openvms/web test`: 543 passed / 4 failed, 85 files. Candidate changes
  exposed two stale broad assertions: duplicate site text after adding an operational panel,
  and selecting the first alert rather than the intended zone refusal. Tests now assert the
  selected site control and exact refusal text; successful default zone/unplaced API fixtures
  avoid unrelated missing-route errors. Lazy route setup explicitly allows 5 seconds.
- Focused `pnpm --filter @openvms/web test src/components/maps/MapShell.test.tsx src/routes/Maps.test.tsx src/components/SettingsLayout.test.tsx`:
  PASS, 39 tests / 3 files. Initial unrelated SettingsLayout grant timeout did not reproduce.
- First full rerun: 546 passed / 1 failed (85 files); the unrelated Live unauthorized-camera
  test timed out waiting for North. `pnpm --filter @openvms/web test src/routes/Live.test.tsx`:
  PASS, 28 tests. No unrelated Live or Settings source/test was changed.
- Final exact `pnpm --filter @openvms/web test`, after concurrent Go work exited: **PASS,
  547 tests / 85 files**, 38.72 seconds. Earlier concurrent runs took 135.99–160.69 seconds;
  transient timeout failures are recorded, not erased or presented as deterministic bugs.
- `pnpm --filter @openvms/web typecheck`: PASS (exit 0).
- `pnpm --filter @openvms/web lint`: PASS (exit 0), existing center dependency warning remains.
- `pnpm --filter @openvms/web build`: PASS (exit 0), existing >500 kB chunk warning remains.
- `git diff --check`: PASS (exit 0), repeated after the integration fixture correction below.

Independent Go integration verification found a pre-existing compile error in
`internal/store/maps_core_test.go:61`: its `UpdateSiteGeoParams.TenantID` fixture supplied
`uuid.UUID`, but the generated query accepts nullable `*uuid.UUID`. Parent authorized the
minimal fixture-only fix to `&tenantID`; HEAD confirms the stale fixture predates this Maps
follow-up. Production schema/query/authorization were not changed. The Go verifier owns
rerunning the corrected integration tests. Keep this one-line correction with validation/docs,
not the Maps behavior rollback; rollback replaces `&tenantID` with the prior fixture value.

At this writer handoff the independent Go/performance results were still pending. The final
independent results below close local automated verification only; real-account runtime
interaction/media and explicitly authorized complete deployment remain M-W17.


### Independent final verification and local closure (2026-10-01)

Parent-confirmed independent results (fresh verifier, after all implementation/docs):

| Exact command | Observed outcome |
| --- | --- |
| `go test -race ./...` | PASS, ordinary Go packages. |
| `go test -race -tags integration ./...` | Initial FAIL: pre-existing `maps_core_test.go:61` UUID pointer fixture compile error; final full rerun PASS after the one-line fixture correction described above. |
| `pnpm test:perf` | PASS, 1/1 synthetic 5,000-camera WebGL2 performance smoke; observed harness acceptance of at least 50 FPS, at most 50 ms steady-state long tasks, and first render under 5 seconds. |

The perf reporter supplied PASS rather than numeric snapshots. These performance numbers are
asserted harness thresholds, not independently reported measurements or a claim about installed-
user hardware, production camera traffic, media transport, or a live MQTT feed.
Initial failures remain recorded above; no silent waiver, fabricated approval, or production
schema change occurred. Web final results are 547/547 tests, typecheck/lint/build PASS with
existing warnings, and diff check PASS. Parent structural readback and this doc reconciliation
close M-W14–16 as **locally implemented and verified**, not operationally deployed/100% live.
M-W17 stays unchecked. No native review receipt, commit, remote authorization or deployment is
claimed by this document; parent owns subsequent work-unit delivery.

### Separate discovery: Event Rail (not implemented)

Parallel read-only investigation reported review-item granularity rather than an individual-
object event stream, no production MQTT consumption, and missing lifecycle update/replay paths.
This is a separate feature-scope discovery, not a new Maps task, acceptance criterion, or an
implemented Event Rail. No Event Rail code, ingestion, lifecycle, or replay was changed here.
Any implementation requires separately authorized scope and verification.

### Parent delivery evidence (2026-10-01)

- Local work-unit commit: `0889ec1` (`feat(maps): complete operational navigation and mode workflows`).
- Authored range from baseline `de66c1c`: 655 additions + 52 deletions = 707 lines, including recovery/verification documentation; no generated files. One cohesive operational change with regression tests and supporting docs, not a PR. Existing feature-branch-chain delivery applies to any later PR slices; no push, PR, or merge performed.
- Independent parent-requested focused spot check: 45 tests / 4 files PASS (MapShell, HierarchyBreadcrumb, MapOperationsPanel, Maps route).
- Native committed-range assessment: medium; `review_due=true`, `slice_budget_reached`. Candidate consent/review outcome pending; automated checks are not a review receipt.
- Installed-account acceptance and deployment remain pending explicit operational authorization. Event Rail remains explored only, not implemented.

## Navigation and drag/drop follow-up (2026-10-01)

Authorized scope: restore persistent compact icon navigation; fit Maps inside the shared
workspace; harden camera drag/drop and distribute bulk drafts instead of overlapping markers.
User confirmed provisional distribution of ONLY the 16 unplaced cameras on server `helvecia`
(`55793e53-e7a0-4b3f-9640-b2250cbdc1e9`) around the existing Casa site center
(-31.105609931702922, -60.09134003511838). Do not reassign cameras/servers/sites or change
other placements. Provisional layout is not actual camera geolocation. Preserve configuration.

- [x] M-W18 — Persistent icon rail on all viewports and workspace-fit Maps layout.
- [x] M-W19 — Canvas-contained drag/drop, usable mouse/touch movement and release recovery,
      visible feedback, aesthetic icon-and-name direct-drag camera cards (no selection prerequisite),
      and deterministic spaced provisional bulk drafts; preserve save/undo/revisions.
- [ ] M-W20 — Audited provisional placement of the authorized 16 cameras and configuration-preserving
      local deployment with real interaction acceptance and exact rollback evidence.

Route: delegated direct for M-W18/M-W19 (multiple non-trivial files and preparation for writing).
M-W20 is parent-gated local operational execution, no remote Frigate access. No data mutation or
 deployment before parent inspection of implementation proof. Forecast: 300–650 authored lines;
existing `ask-on-risk` / `feature-branch-chain` delivery strategy applies, no PR/push/merge authorized.
STRICT TDD ON: current AGENTS/session instruction, runner `pnpm --filter @openvms/web test`;
previous deferred/OFF entries are historical. Observe focused RED, GREEN, REFACTOR.
Focused checks: Layout, AppShell, Maps route, MapShell, UnplacedTray and new interaction helpers;
full web test/typecheck/lint/build and `git diff --check`, sequential foreground.
Acceptance: icons reachable desktop/narrow viewport; tray placement and marker mouse/touch movement;
release outside/cancel restores pan; unrelated drops ignored; spaced drafts preserve existing cameras;
save/reload retains revisions/properties. Real browser proof remains explicit, not inferred from callbacks.
Rollback: independent source work-unit commits; operational inserts tracked by exact IDs/revisions and
removed only while unchanged, never overwriting the user's later manual adjustments.
Mirror: current follow-up scope under `odd/maps/tasks`; full historical evidence retained locally and
in existing `odd/maps/tasks/history-1` / `history-2` multipart observations. Next: RED regressions.

- [x] M-W21 — Keep site/camera identity symbols visible through zoom and clustering; reuse
      the existing Live sidebar Font Awesome building/server/video glyphs in actual map/list
      representations; online green and explicit offline X badges, without fabricated server locations.
Route: delegated direct (canvas/layers/sprites/tray plus tests). User additionally requested
5-second per-camera event popups for all available event types; capability inspection is delegated
separately and implementation remains dependent on verified normalized event fields/links.
No event ingestion or fabricated snapshots/plates are implied by this UI unit.
M-W21 acceptance: site glyph remains above zoom 12; clusters retain a camera glyph/count;
individual marker identity remains video while offline X is a separate badge; tray site/server
context uses the same existing sidebar icons. Exact focused layer/sprite/tray tests plus full web checks.

- [x] M-W22 — Automatic 5-second anchored popups for newly indexed, permission-filtered
      camera events: all indexed event categories, real labels/plates/photo only when available,
      latest event replaces per camera; independent cameras; bounded dedup/recency; tenant/site
      cleanup and stale-response guards. Fresh writer follows M-W18/19/21 handoff.
Source contract: existing `event.created` frames carry camera ID and detail ID in `data.id`;
GET `/api/v1/events/{id}` is authoritative. Reviews/alerts/detections are indexed, not every raw
object lifecycle; existing upstream HTTP ingestion latency is not eliminated by this UI.
Do not invent plates/snapshots or implement new ingestion infrastructure in this unit.

### M-W18/19/21 local implementation evidence

- M-W18 navigation RED: focused Layout/AppShell/Maps command returned 2 failures / 12 passes
  (both rail containers hidden). GREEN: 14/14. Shared shell now keeps a fixed narrow-screen
  rail and desktop in-flow rail; Maps uses the workspace-fit shell instead of overflowing
  with a viewport-height child. Browser disappearance cause beyond responsive hiding is not claimed.
- M-W19 helper RED: new editorInteractions suite failed unresolved module before implementation;
  helper GREEN 5/5. Icon-card/bulk editor RED: 3 failures / 41 passes; GREEN 49/49 with helpers.
  Direct-drop integration regression uses real DOM canvas targeting without prior camera selection;
  overlays/out-of-bounds drops are ignored. Marker pointer capture handles mouse/touch/pen,
  movement threshold and cancel/outside release cleanup; last valid coordinate remains a draft.
  Spaced 45-meter provisional grids are deterministic and skip already-staged bulk entries.
- M-W21 layers RED: 2 failures / 4 passes; GREEN with tray 15/15. Site point/ring maxzoom12
  and label maxzoom13 were explicit disappearance rules; site symbols now remain through zoom.
  Clusters keep video identity plus count, individual cameras keep video identity plus offline-X
  badge. Canonical Font Awesome glyphs match Live sidebar; server identity is list context only,
  not a fabricated geographic server marker. All new layers registered in visibility groups.
- Intermediate integration fixture failed `map.once` missing; fake map fixture repaired, no source
  behavior waived. One focused restricted-route async startup timed out at 1 second; wait now
  explicitly permits 5 seconds as the existing lazy-ready route setup does.
- Final full `pnpm --filter @openvms/web test`: PASS 555 tests / 86 files (71.28 seconds).
- Final focused nine-file Layout/AppShell/Maps/MapShell/UnplacedTray/editorInteractions/
  cameraLayers/sitesLayer/visibility command: PASS 74/74 after type-only assertion narrowing.
- `pnpm --filter @openvms/web typecheck`: initial FAIL only new layer-test union narrowing;
  final PASS after discriminating symbol layers. Production source not changed by this correction.
- `pnpm --filter @openvms/web lint`: PASS; existing center-effect dependency warning retained.
- `pnpm --filter @openvms/web build`: PASS; existing >500 kB chunk warning retained.
- Runtime synthetic production-render harness `pnpm test:perf`: FAIL, maxLongTaskMs 54 > 50;
  no real-account/media/deployment acceptance claimed. Data, services/config and existing sessions unchanged.
- M-W20 remains pending parent release. `vmsctl` has no maps operation; HTTP placement API needs
  an explicitly authorized authenticated session/token. Native `maps.Service.UpsertPlacement`
  and `DeletePlacement` provide RBAC and audit without token issuance if invoked through an
  explicitly authorized local administrative service runner with an existing admin identity.
  Nil If-Match does not guarantee create-only: operational code must lock/check the 16 unplaced
  targets and preserve race safety. Rollback records placement IDs/revisions and refuses changed rows.
- Mock-data scope was clarified: remove example records/services only; preserve real servers,
  cameras and events. Operational maintenance evidence follows; no test fixtures deleted.

### M-W20 operational maintenance evidence (parent-confirmed)

- Native audited maintenance inserted 15 new provisional placements, revisions 3–17, around
  current Casa center (-31.107187045294246, -60.09585033385747); prior CEF28 placement preserved.
  All 16 Helvecia cameras are now placed; Frigate-Casa's 10 unplaced cameras remain unchanged.
- Exactly three empty demo sites (Helvecia, Cayasta, SantaRosa) were soft-deleted, and two mock
  profile services stopped. Snapshot preserved two real servers, 26 cameras and 34,172 events.
- Audit request `maps-maintenance-b5a290b0-f009-4fe2-8ec3-6bae920a2625` contains 15 placement
  upserts and three SITE_REMOVED records. No inventory reassignment or real event deletion.
- Existing API image restarted after the bounded maintenance pause; health/ready and web
  returned 200. Initial incorrect healthz probe returned 404, then corrected probe confirmed 200.
- Rollback evidence is mode-0600 `/tmp/openvms-maps-maintenance-evidence.jsonl`; helper removed.
  Rollback must refuse modified placement revisions, preserving later user adjustments.
- New frontend deployment and real browser/media acceptance remain pending parent release.
  External basemap style returned 403; circle-11/wood-pattern provider sprite warnings are not
  custom camera icon references and are not claimed fixed by these UI changes.

### M-W22 event notice implementation and correction evidence

Route: delegated direct (MapShell integration, new component and tests); strict TDD ON, exact
runner `pnpm --filter @openvms/web test`. No backend ingestion, Frigate probes, token issuance,
remote deployment or RDD lifecycle. Existing navigation/editor/icon uncommitted bytes preserved.

- New component tests RED: missing component import before source. Initial GREEN: 8 event
  tests plus 36 MapShell tests passed. Initial full web suite: 563/563, 87 files, 91.84 seconds.
- Independent review found receipt-time expiry shortened visible notices after REST latency;
  correction tests RED: two failures (retry display lifetime, status-rerender preservation).
  Display expiry now starts at first validated detail; separate five-second request deadline,
  one 500 ms retry, abort/replacement/context guards remain bounded. Eligibility signatures
  preserve events across status/name/coordinate updates without retaining hidden cameras.
- Backend frame timestamps are event starts, not publication timestamps. A 30-second-old-start
  regression was observed RED before the two-minute bounded start-recency policy was applied.
  Events indexed after two minutes may not appear; this limitation is documented, not hidden.
- REST uses the generated `/api/v1/events/{eventId}` contract and `data.id`, never transport ID.
  Real labels/plates/severity only; permitted available snapshots otherwise actual thumbnails;
  explicit image-unavailable state. Different cameras have independent replacement/expiry.
- Final source checks are pending below; initial MapShell fixture failures from missing mock
  map.on were repaired by permission-gating the entire new map-move listener, not weakening
  production checks. Initial typecheck failed generated path placeholder/test narrowing and
  was corrected; initial typecheck overlapped the historical focused test before final sequential
  verification. No such historical result is represented as final proof.
- Performance remains unresolved: writer 54 ms, independent isolated baseline 57 ms and
  candidate 74 ms all exceed unchanged 50 ms budget. No retry/threshold waiver or proven
  causal regression/absolution. M-W18/19/21 local functional work is verified; feature-level performance remains partial and M-W20 deployment pending.
- Rollback boundary: remove CameraEventPopups component/tests and only its MapShell import,
  ready-map state/callback and JSX integration; preserve unrelated navigation/drag/icon changes.

#### M-W22 final sequential local checks

- Focused `pnpm --filter @openvms/web test src/components/maps/events/CameraEventPopups.test.tsx src/components/maps/MapShell.test.tsx`: PASS 46/46 (10 event + 36 MapShell tests).
  There is no standalone MapCanvas.test.tsx in this repository; existing MapShell integration
  and canvas/layer tests in the full suite supply existing canvas coverage, not a fabricated runner.
- Final `pnpm --filter @openvms/web test`: PASS 565/565, 87 files, 75.32 seconds; expected
  jsdom HTMLMediaElement.load notices remain. Previous 563 pass was pre-correction evidence.
- Final `pnpm --filter @openvms/web typecheck`: PASS, exit 0.
- Final `pnpm --filter @openvms/web lint`: PASS, exit 0; existing center-effect warning retained.
- Final `pnpm --filter @openvms/web build`: PASS, exit 0; existing >500 kB chunk warning retained.
- M-W22 event behavior is locally implemented and verified, but no real-account/browser/media
  acceptance, source commit or new deployment is claimed. Feature-level status remains partial
  because synthetic performance failed without waiver and deployment remains parent-gated.
- Parent owns final tracker mirror/readback, independent check and delivery decisions. No source
  edits after final focused/full/check batch; only proof/document reconciliation followed.

### Functional work-unit commit closure (2026-10-01)

The checked M-W18/M-W19/M-W21/M-W22 items denote observed **local functional completion**,
not a performance waiver, real-account acceptance, deployment, native receipt or remote delivery.
M-W20 data maintenance is observed above, but its frontend deployment/acceptance remains open;
M-W17 real-account/media acceptance remains open. External basemap sprite errors remain unresolved.

| Work unit | Commit | Authored additions + deletions |
| --- | --- | --- |
| Persistent navigation and workspace fit, including regression tests | `d446ed724bfe5b579ee748f92485f6589ed104d7` | 15 + 10 = 25 |
| Direct camera dragging, spaced drafts and canonical zoom-stable identity icons with tests | `c8423b58ca53a950b932ad5eb8dc4f0ba48bc9cd` | 332 + 78 = 410 |
| Permission-filtered camera event notices, tests and operational docs | `e8a5549f7db9f786ba5d1ac5e55a6350e9eca579` | 228 + 1 = 229 |

Three source commits total 664 authored changed lines, generated outputs excluded. The editor
unit exceeds the advisory 400-line heuristic by 10 because its pointer interactions, tray behavior,
shared canonical glyphs and their tests form one coherent rollback/review unit; no code-golf or
artificial test split. Existing feature-branch-chain policy applies to later PR planning. No PR,
push or merge was requested/performed, and source normalization has no configured mutating formatter.

- Independent event verifier: exact event + MapShell focused command PASS 46/46, 12.10 seconds;
  structural timing/context/media readback PASS with no additional findings.
- Final sequential source checks: full web 565/565, typecheck/lint/build PASS as above; final
  `git diff --check` PASS. Existing center and chunk warnings remain.
- Synthetic performance: FAILED at unchanged 50 ms threshold (writer 54, baseline 57,
  candidate 74 ms); unresolved, not waived, and no attribution/absolution fabricated.
- All commits are local Conventional Commits on feat/maps, without AI/co-author attribution.
- Runtime proof remains synthetic only, with failed performance recorded; real editor/media
  browser acceptance is pending. No additional runtime harness or Frigate probe was executed.
- Rollback boundaries: navigation six files; editor listed fifteen files and editor-only MapShell
  hunks; events four files and only event MapShell integration. Source commits retain tests/docs.
- Native assessment/consent and candidate review remain parent-owned and pending; these commits
  create no receipt or delivery authority. Generated packages/test/test-results artifacts remain
  untouched/uncommitted. Evidence-only tracker commit follows these immutable source units.
- Recovery mirror: full current navigation/drag follow-up under
  `odd/maps/tasks/navigation-drag-followup`, linked to this file and existing main observation 120
  plus retained history topics; parent can reconcile its normal main mirror without losing history.
