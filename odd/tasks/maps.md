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
- [ ] M-W4 — realtime store, pulse/ripple, animation budget
- [ ] M-W5 — camera panel, hover preview, context menu, nearby cameras
- [ ] M-W6 — alarm panel, site health, auto-focus
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



