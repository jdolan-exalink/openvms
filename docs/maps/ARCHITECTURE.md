# Maps — Architecture and Implementation Plan

Status: proposal for PO review (2026-09-30). Input: [`docs/specs/maps-brief.md`](../specs/maps-brief.md).
Branch: `feat/maps`. No code exists yet; every "reuse" below cites the current code.

**In one paragraph.** Maps is a new lazy-loaded web route (`/maps`) built on MapLibre GL JS.
It reads map data from a new Go package `internal/maps` behind contract-first `/api/v1/maps/*`
endpoints, and gets live updates from the existing `/ws` feed. Maps does not
open a second socket or talk to Frigate. The backend gains six map tables in one migration (`00022`). It keeps plain
`lat/lng` columns and GeoJSON `jsonb` and does **not** switch to PostGIS in the MVP. It also gains a
normalized realtime envelope with a resumable `id`, a `camera.status_changed` publisher, and
a backward-compatible extension of the alarm state machine. Video previews reuse the existing
`PlayerSessionManager`. Floor plans render in the same MapLibre component on a synthetic local
extent, so layers, clustering and interaction code are written once.

---

## Contents

1. [General architecture](#1-general-architecture)
2. [React component structure](#2-react-component-structure)
3. [Data model](#3-data-model)
4. [API schema](#4-api-schema)
5. [WebSocket protocol](#5-websocket-protocol)
6. [TypeScript entities](#6-typescript-entities)
7. [Database design](#7-database-design)
8. [MapLibre strategy](#8-maplibre-strategy)
9. [Clustering](#9-clustering)
10. [Realtime strategy](#10-realtime-strategy)
11. [Scaling to thousands of cameras](#11-scaling-to-thousands-of-cameras)
12. [Video previews](#12-video-previews)
13. [Map editor](#13-map-editor)
14. [Zones](#14-zones)
15. [Floor plans](#15-floor-plans)
16. [Heatmaps](#16-heatmaps)
17. [Historical timeline and replay](#17-historical-timeline-and-replay)
18. [Permissions](#18-permissions)
19. [Phased implementation plan](#19-phased-implementation-plan)
20. [Directory structure](#20-directory-structure)
21. [Tests](#21-tests)
22. [Technical risks](#22-technical-risks)
23. [Key architectural decisions](#23-key-architectural-decisions)
24. [Open questions for the PO](#open-questions-for-the-po)

---

## 1. General architecture

### 1.1 Control and data flow

```text
                        ┌──────────────── apps/web (/maps, lazy chunk) ────────────────┐
                        │ MapShell ─ MapCanvas (MapLibre) ─ layers ─ panels ─ editor     │
                        │   ▲ persistent state: TanStack Query (/api/v1/maps/*)          │
                        │   ▲ realtime state:   mapRealtimeStore  ◄── lib/realtime.ts    │
                        │   ▲ UI state:         useMapUi (zustand-free reducer)          │
                        │   ▲ video:            PlayerSessionManager (lib/live)          │
                        └───────┬──────────────────────────┬──────────────────┬──────────┘
                     REST /api/v1/maps/*           WS /ws (existing)    /media/v1 (existing)
                                │                          │                  │
 apps/api ── internal/api (oapi strict) ── internal/maps ──┤ internal/realtime│ internal/media
                                │   uses access.Checker,   │ Hub + Feed       │ gateway
                                │   store (RLS), audit     │ (JetStream)      │
                                ▼                          ▲                  ▼
                           Postgres 17  ◄── worker ── NATS JetStream (FRIGATE, PLATFORM)
                           (maps tables)    events.Syncer / HealthPoller / rules / alarms
                                                   │ (polls Frigate over HTTP)
                                                   ▼
                                               Frigate servers
```

Hard rule from the brief: the map talks only to OpenVMS. The worker already isolates Frigate:
`events.Syncer` polls review items (`internal/events/syncer.go:38-58`), `HealthPoller` polls
stats (`internal/inventory/health.go:84`), and both publish to NATS
(`apps/worker/main.go:117-123`, `:159-165`).

### 1.2 What we reuse as-is

| Capability | Reuse point |
|---|---|
| Tenant isolation (RLS) | `app_tenant_visible()` + `store.ScopeFor(actor)` (`migrations/00002_inventory_and_authz.sql:190-198`) |
| Scope checks | `access.Checker.CameraIDs/SiteIDs/ServerIDs` (`internal/access/access.go:76-88`), `Require` (`:69`) |
| Audit | append-only `audit_log` (`migrations/00002…:166`), per-service `audit()` helpers (`internal/alarms/service.go:482`) |
| Realtime fan-out | `realtime.Hub` tenant filter + per-scope `Authorizer` (`internal/realtime/hub.go:102-122`, `authorizer.go:84`) |
| Alarms | `alarms.Service.Acknowledge/Assign/Resolve/Bulk` (`internal/alarms/service.go:171,224,290,340`) |
| Rules | `rules.Conditions.MatchesEvent` (`internal/rules/rule.go:103`), offline detector (`internal/rules/offline.go:37`) |
| Snapshot | `GET /media/v1/cameras/{id}/snapshot.jpg` (`internal/media/gateway.go:52,82`) |
| Live sessions | `PlayerSessionManager.acquire/release`, WARM 30 s, max 8 warm, 32 live (`apps/web/src/lib/live/PlayerSessionManager.ts:20-22,62,79`) |
| Deep links | `/live?mode=rec&t=` (LV-9, `docs/live-view-architecture.md` §LV-9), `/alarms`, `/events` |
| Search | omnibox `search.Service.Search` result kinds (`internal/search/service.go:68`) |
| Object storage | `Blobs.Put` interface pattern (`internal/branding/service.go:77`), SeaweedFS S3 |
| Rollout flags | `config.Features` / `useFeatures` (`internal/platform/config/features.go:7`, `apps/web/src/lib/features.ts`) |
| Theme | CSS tokens `:root` dark / `:root.light` (`apps/web/src/index.css:24-60`), toggle `Layout.tsx:304` |

### 1.3 What is missing today (gaps) and the compatibility layer for each

| # | Gap (verified) | Compatibility layer (additive, no destructive refactor) |
|---|---|---|
| G1 | No geo data anywhere: `sites` has only `address` (`00002…:62`), `cameras` only free-text `location` (`00013…`) | New map tables (§7). `sites` gets nullable `lat/lng/default_zoom` (same "VMS-owned columns" pattern as `00013`). |
| G2 | Realtime envelope is `{type, tenant_id, data}` only (`internal/realtime/routes.go:23-27`); no id, ts or top-level site/camera | Envelope v2 adds `id, ts, site_id, camera_id, server_id` **next to** existing fields; old client keeps working (it reads only `type`, `lib/realtime.ts:14-19`). |
| G3 | No resume: JetStream ordered consumer with `DeliverNewPolicy` (`internal/realtime/feed.go:119-121`); client frames ignored (`handler.go:149`, read limit 512 B) | Per-API-instance 5-min ring buffer keyed by JetStream stream sequence; client `hello{last_event_id}` frame; `resync` frame when the gap is outside the ring (§5). |
| G4 | Camera status changes are **not published**: HealthPoller writes `UpdateCameraHealth` (`health.go:124`) but only server transitions go to NATS (`apps/worker/main.go:159-165`) | HealthPoller computes a per-camera diff and publishes `camera.status.<tenant>` (subject already covered by PLATFORM stream `camera.>`, `internal/platform/natsx/nats.go:28`). |
| G5 | Alarm lifecycle has 3 states (`00014_alarms.sql:13`); no comments, no history table | Widen the CHECK constraint, keep `open` as the stored value for NEW, add `alarm_transitions` (§3.4, §7). Inbox filters keep working. |
| G6 | "AI detection" latency = event sync poll interval (`cfg.EventSyncInterval`, `apps/worker/main.go:116`) | Accept for MVP (document as "seconds, not sub-second"); Phase 3 adds a worker-side Frigate MQTT/WS ingest. Maps is unaffected because it only consumes the bus. |
| G7 | No hierarchy above site (Region) or below (Area/Building/Floor) | `map_regions` (optional, tenant-scoped tree), `map_buildings`, `map_floors`; Area = zone of `kind='area'`. |
| G8 | `streamPrewarming` flag exists (`features.ts`) but the manager has no prewarm API | Map uses `acquire` + deferred `release` as prewarm (§12); a dedicated `prewarm()` lands with Live P1. |
| G9 | No generic "device" inventory (sensors, doors) | `map_devices`: map-owned entities with manual status; an adapter seam (`maps.DeviceStatusSource`) for future access-control/IoT integrations. |

### 1.4 Normalized event layer

The brief's event vocabulary is mapped onto today's subjects by **decoders in
`internal/realtime/routes.go`**. No new publisher is needed except G4:

| Normalized `type` | Source subject (stream) | Decoder today | Scope permission |
|---|---|---|---|
| `object.detected` | `frigate.event.new.<tenant>` (FRIGATE) | `decodeEventCreated` (`routes.go:103`) → emit both `event.created` and the new type | `events.view` / camera |
| `lpr.detected` | same, when `plates` non-empty (Phase 2 adds `lpr.read.<tenant>`) | new | `lpr.view` / camera |
| `alarm.created` / `alarm.updated` / `alarm.acknowledged` | `alarm.<action>.<tenant>` (PLATFORM) (`alarms/service.go:474`, `rules/service.go:473`) | `decodeAlarmUpdated` (`routes.go:168`); `action` token → type | `alarms.view` / camera |
| `server.status_changed` | `server.<status>` (`apps/worker/main.go:163`) | `decodeServerStatus` (`routes.go:135`) | `servers.view` / server |
| `camera.status_changed` | **new** `camera.status.<tenant>` | new | `cameras.view` / camera |
| `camera.recording_changed`, `ptz.position_changed`, `device.health_changed`, `sensor.triggered`, `door.opened`, `object.entered_zone/left_zone`, `face.detected` | reserved (Phase 2/3) | — | per kind |

The existing type strings (`event.created`, `server.status`, `alarm.updated`,
`notification.created`) keep being sent unchanged. The new names are extra frames, sent only to
connections that opt in with `hello.topics` (§5). Old tabs see no change.

---

## 2. React component structure

The components follow the repo's conventions: pages in `routes/`, feature components in `components/<feature>/`
(like `components/zones/`), and non-visual logic in `lib/<feature>/` (like `lib/live/`).

```text
routes/Maps.tsx                         route entry; parses search params (mode, site, camera, alarm, floor, view)
components/maps/
  MapShell.tsx                          layout: toolbar, breadcrumb, canvas, side panels, bottom dock (container)
  MapToolbar.tsx                        mode switch LIVE/INVESTIGATE/ANALYTICS/EDIT, search box, saved views
  HierarchyBreadcrumb.tsx               Org › Region › Site › Building › Floor
  canvas/
    MapCanvas.tsx                       owns the maplibregl.Map instance (one per page); imperative bridge
    MapStyleController.ts               builds style JSON from theme tokens + provider; swaps on theme change
    layers/cameraLayers.ts              source + cluster/unclustered/status/label layers (pure specs)
    layers/fovLayer.ts                  FOV cones source (computed polygons) + selected emphasis
    layers/fxLayers.ts                  detection ripple / alarm pulse (small separate source)
    layers/zoneLayers.ts                zone fill/line/label
    layers/floorPlanLayer.ts            image source for floor plans (local CRS mode)
    layers/heatmapLayer.ts              Phase 2
    interactions.ts                     hover/click/dblclick/contextmenu → typed intents
  panels/
    CameraPanel.tsx                     floating preview + details; actions (Live, Playback, Events, Add to Live View)
    CameraPreview.tsx                   snapshot → prewarm → live (uses usePlayerSession)
    AlarmPanel.tsx                      alarm detail + lifecycle actions (reuses alarms API)
    SiteHealthPanel.tsx                 site aggregates, grouped root causes
    LayersPanel.tsx                     layer toggles (persisted prefs)
    FiltersPanel.tsx                    combinable filters
    NearbyCamerasList.tsx               distance/zone/building/floor
  editor/
    EditorToolbar.tsx                   add camera/device/zone/building/floor, snap, rotation step
    PlacementTool.ts                    drag & drop, rotate by mouse, FOV handles (MapLibre events, no React per feature)
    ZoneDrawTool.ts                     polygon draw/edit (reuses lib/zoneGeometry.ts validation)
    UnplacedTray.tsx                    authorized cameras without a placement → drag onto map
    FloorPlanUpload.tsx                 Phase 2
  timeline/                             Phase 2: GeoTimeline.tsx, ReplayController.ts
  CameraContextMenu.tsx                 reuses components/ContextMenu.tsx
lib/maps/
  api.ts                                TanStack Query options (mapsConfigQuery, siteEntitiesQuery…)
  types.ts                              TS entities (§6)
  geo.ts                                bearing/FOV polygon/haversine/normalized↔local CRS
  mapRealtimeStore.ts                   ring buffer, dedup, rAF batching, subscriptions (§10)
  entityIndex.ts                        id → feature index, status reducer, cluster property encoding
  animationBudget.ts                    priority queue for fx, prefers-reduced-motion
  hoverIntent.ts                        0/150/400/700 ms timers
  prefs.ts                              layer prefs + auto-focus policy (server-persisted)
  metrics.ts                            FPS, entities visible, events/s, WS lag, TTFR, time-to-preview
```

Rules:

- **No React component per camera or event.** Features live in MapLibre sources. React renders
  only panels, and they subscribe to one selected id.
- `MapCanvas` is the only component that touches `maplibregl`. Layer modules export pure
  functions `(ctx) => LayerSpecification[]`, which are unit-testable without WebGL.
- Container/presentational split: panels receive data through props from `MapShell` hooks, so
  they are testable with fixtures.

---

## 3. Data model

### 3.1 Hierarchy mapping

| Brief level | OpenVMS entity | Notes |
|---|---|---|
| Organization | `tenants` | existing |
| Region | `map_regions` (new, optional tree) | `sites.region_id` nullable FK |
| Site | `sites` (+ `lat, lng, default_zoom`) | existing, extended |
| Area | `map_zones` with `kind='area'` | no table of its own; polygons already model it |
| Building | `map_buildings` (new) | footprint polygon + site |
| Floor | `map_floors` (new) | ordinal, plan image in object storage |
| Zone | `map_zones` (new) | geo (`floor_id IS NULL`) or floor-local |
| Device | cameras / servers (existing) + `map_devices` (new) | all placed through `map_placements` |

### 3.2 Positions

`MapPosition` is a tagged union:

- **geo**: `lat, lng` in WGS84.
- **floor**: `floor_id, x, y`, with x and y normalized to `[0,1]` of the plan image. The origin is at the top-left, the same
  convention as Frigate zone coordinates in `lib/zoneGeometry.ts`.

A camera may have **one geo placement and one placement per floor**. For example, an indoor
camera sits on the floor plan and also has a building-level geo point.

### 3.3 Camera map fields

Most camera map fields are stored on `map_placements`, not on `cameras`. The Frigate inventory
sync must never touch them. That is the same rule as `00013`.

| Field | Storage |
|---|---|
| position | placement `lat/lng` or `floor_id/x/y` |
| orientation (bearing°) | `map_placements.bearing_deg` |
| fov_angle | `map_placements.fov_deg` (default by `camera_type`) |
| estimated_range (m) | `map_placements.range_m` |
| camera_type (fixed/dome/ptz/fisheye/lpr) | `map_placements.props.camera_type` |
| ptz_capabilities | derived: `live.ptz` grant + Frigate capabilities (`frigate_servers.capabilities`) — read-only |
| status | derived from `cameras.status` (`unknown/online/degraded/offline`, `00002…:115`) + server status |
| recording_status | reserved (Phase 2, from Frigate recordings stats) |
| analytics_capabilities | derived from camera `lpr` flag + tracked labels (read-only) |

The brief's **display state** is computed on the client from those inputs. Worst state wins:
`SELECTED > ALARM > OFFLINE | NO_SIGNAL | RECORDING_ERROR > DEGRADED > ONLINE`.
`NO_SIGNAL` means `cameras.status='unknown'` while its server is online. When the server is offline,
the camera is shown as **unreachable via server**, which is the root-cause grouping in §10.4.

### 3.4 Alarm lifecycle extension

| Brief state | Stored `alarms.status` | Transition (API) | Permission |
|---|---|---|---|
| NEW | `open` (unchanged value) | created by syncer/rules | — |
| ACKNOWLEDGED | `acknowledged` | `POST …/acknowledge` (existing) | `alarms.manage` |
| ASSIGNED | `assigned` (new) | `POST …/assign` (existing; now also sets status when `open|acknowledged`) | `alarms.manage` |
| INVESTIGATING | `investigating` (new) | `POST …/investigate` (new) | `alarms.manage` |
| RESOLVED | `resolved` (unchanged) | `POST …/resolve` (existing) | `alarms.manage` |
| CLOSED | `closed` (new) | `POST …/close` (new) | `alarms.manage` |

Every transition writes an `alarm_transitions` row (from, to, actor, comment, at) in the same
transaction as the existing audit row. Comments: `POST /alarms/{id}/comments`.

Inbox compatibility:

- The web `Alarms.tsx` status map (`routes/Alarms.tsx:15-16`) gets labels for the three new
  values.
- The "open" filter becomes "active" (`open|acknowledged|assigned|investigating`) with a
  server-side `status_group=active` parameter. The single-value filter keeps working.
- The OpenAPI enum (`packages/api-contract/openapi.yaml:3050`) is extended. Generated clients
  must tolerate unknown values, so the change ships in the same work unit as the UI labels.

---

## 4. API schema

The API is contract-first, like every endpoint today. The schema lives in `packages/api-contract/openapi.yaml`,
oapi-codegen strict handlers are mounted by `gen.HandlerFromMux` (`internal/api/router.go:69-89`),
and the web types come from `apps/web/src/api/schema.d.ts`. Errors use the existing `gen.Error` shape.

| Method | Path | Purpose | Permission (server-enforced) |
|---|---|---|---|
| GET | `/api/v1/maps/config` | tile provider(s), style URLs, attribution, offline flag, limits | any authenticated |
| GET | `/api/v1/maps/overview` | per-site geo point + health aggregates (country/city zoom) | `maps.view` (site set) |
| GET | `/api/v1/maps/sites/{siteId}` | site map: bounds, buildings, floors, zones | `maps.view` on site |
| GET | `/api/v1/maps/sites/{siteId}/entities?bbox=&types=&floor_id=` | compact placed entities (cameras, servers, devices) + status | `maps.view` + per-entity (§18) |
| GET | `/api/v1/maps/entities?bbox=&zoom=` | cross-site viewport query (multi-city) | idem |
| GET | `/api/v1/maps/unplaced?site_id=` | authorized cameras without a placement | `maps.edit` |
| PUT | `/api/v1/maps/placements/{entityType}/{entityId}` | upsert placement (geo or floor), with `If-Match` version | `maps.edit_device` |
| DELETE | `/api/v1/maps/placements/{placementId}` | remove placement | `maps.edit_device` |
| POST/PATCH/DELETE | `/api/v1/maps/devices[/{id}]` | map-owned devices (sensor, door, label, custom) | `maps.edit_device` |
| GET/POST | `/api/v1/maps/sites/{siteId}/zones` | list/create zones | view: `maps.view`; create: `maps.create_zone` |
| PATCH/DELETE | `/api/v1/maps/zones/{zoneId}` | edit zone | `maps.create_zone` |
| PATCH | `/api/v1/sites/{siteId}/geo` | set site lat/lng/zoom/region | `maps.edit` on site |
| POST/PATCH/DELETE | `/api/v1/maps/buildings[/{id}]`, `/api/v1/maps/floors[/{id}]` | hierarchy | `maps.edit` |
| PUT | `/api/v1/maps/floors/{floorId}/plan` | multipart upload (Phase 2) | `maps.edit` |
| GET | `/api/v1/maps/floors/{floorId}/plan` | plan image (proxied from object storage, `private, max-age`) | `maps.view` |
| GET/POST/PATCH/DELETE | `/api/v1/maps/views[/{id}]` | saved map views | `views.create_private` / `views.create_shared` / `views.manage_shared` |
| GET/PUT | `/api/v1/me/map-prefs` | layers, filters, auto-focus policy | self |
| GET | `/api/v1/maps/analytics?site_id=&zone_id=&start=&end=&object_type=&camera_id=&metric=` | heatmap weighted points (Phase 2) | `maps.view` + `events.view` per camera |
| GET | `/api/v1/maps/timeline?site_id=&start=&end=&cursor=` | windowed events for replay (Phase 2) | `events.view` per camera |
| GET | `/api/v1/maps/lpr-track?plate=&start=&end=` | ordered plate reads with positions (Phase 2) | `lpr.search` per camera |
| GET | `/api/v1/maps/cameras/{cameraId}/nearby?radius_m=&limit=` | nearby cameras | `maps.view` + `cameras.view` |
| POST | `/api/v1/alarms/{id}/investigate`, `/close`, `/comments` | lifecycle extension | `alarms.manage` |

Compact entity payload. This is the hot path, so fields are short but still named, which keeps the OpenAPI types readable:

```json
{
  "revision": 1842,
  "entities": [
    {"id":"c9f…","t":"camera","site":"a1…","srv":"b2…","name":"Acceso Norte",
     "pos":{"k":"geo","lat":-31.4201,"lng":-64.1888},
     "cam":{"bearing":135,"fov":90,"range":40,"type":"dome","ptz":false,"lpr":true},
     "st":"online","alarms":0}
  ]
}
```

`revision` is a per-tenant monotonically increasing number (the `map_revision` sequence in §7). Clients refetch
only when an `entity.changed` frame carries a higher revision.

---

## 5. WebSocket protocol

### 5.1 Today

- The handshake is GET `/ws`, authenticated by the router middleware (`internal/api/auth.go:90`).
- The server only pushes `{type, tenant_id, data}` frames.
- There are per-user connection caps of 5 (`hub.go:37`), a slow-consumer close (`hub.go:113-121`), and credential revalidation every 30 s
  (`handler.go:106-137`).
- There is no replay. The client catches up by invalidating queries on reconnect (`lib/realtime.ts:65-70`).

### 5.2 Envelope v2 (backward compatible)

```json
{
  "v": 2,
  "id": "PLATFORM:918273",            // "<stream>:<jetstream stream sequence>" — globally unique, ordered per stream
  "type": "camera.status_changed",
  "ts": "2026-09-30T14:05:11.204Z",   // event time (payload time, not delivery time)
  "tenant_id": "…",
  "site_id": "…", "camera_id": "…", "server_id": "…",   // present when known
  "data": { "from": "online", "to": "offline" }
}
```

- `id` comes from the JetStream message metadata. `Source.Consume` passes it through by extending the
  handler signature with `seq uint64` (`feed.go:17`). The sequence is shared by every API
  instance because they read the same stream, so resume works across a load balancer.
- `Envelope` (`routes.go:23`) gets the new fields with `omitempty`. The existing JSON keys stay the same.

### 5.3 Client → server frames (new)

The read limit (`handler.go:149`) is raised from 512 B to 4 KiB, and at most one control frame per second is accepted.

```json
{"op":"hello","v":2,"topics":["camera","alarm","object","server"],"last_event_id":{"PLATFORM":918200,"FRIGATE":55012}}
{"op":"filter","site_ids":["…","…"]}     // optional narrowing; empty = all authorized
```

- `topics` opt the connection into v2 frames. A connection with no `hello` receives exactly today's frames.
- `site_ids` narrows server-side by `site_id`. It is cheap: a map lookup in `Subscription`, applied after the
  tenant check and before the `Authorizer`. **Viewport filtering stays client-side.** The server
  does not know positions. A site filter plus client-side bbox culling is enough up to 5k cameras,
  as §11 shows.

### 5.4 Resume

Each API instance keeps a **ring buffer of decoded `Message`s for the last 5 minutes**, capped at
50k messages per stream.

On `hello.last_event_id`:

1. For each stream, if `last_seq+1` is still in the ring, the hub replays the messages after it. Each one goes through the
   same tenant, site and `Authorizer` checks, and then the connection switches to live.
2. Otherwise the hub sends `{"op":"resync","streams":["FRIGATE"]}`. The client refetches REST, which is the current
   behaviour, and continues live.

Why not a JetStream consumer per connection? Hundreds of operators would create hundreds of ephemeral
consumers on the NATS server. The ring buffer costs about 50k × ~400 B ≈ 20 MB per instance at most,
and it matches the brief's 5-minute client buffer.

### 5.5 Server-side aggregation and flood control

- **Coalescing**: per connection, `camera.status_changed` frames for the same camera within 500 ms
  collapse to the latest one.
- **Root cause**: when `server.status_changed` goes to `offline`, the hub suppresses per-camera status
  frames for that server for 30 s. The client derives "unreachable via server" from the server frame (§10.4).
  The data is already consistent because HealthPoller sets the server's cameras to `unknown` (`health.go:97-99`).
- **Batch frame**: when more than 50 frames for one connection are queued within 100 ms, they are sent as
  `{"op":"batch","frames":[…]}`. This reduces per-frame JSON overhead during alarm floods, and the
  `ErrSlowConsumer` close stays as the last resort.

---

## 6. TypeScript entities

```ts
// lib/maps/types.ts
export type EntityType = "camera" | "server" | "sensor" | "door" | "alarm_point" | "lpr" | "label" | "building" | "custom";

export type GeoPosition = { kind: "geo"; lat: number; lng: number };
export type FloorPosition = { kind: "floor"; floorId: string; x: number; y: number }; // x,y ∈ [0,1]
export type MapPosition = GeoPosition | FloorPosition;

export type CameraDisplayState =
  | "ONLINE" | "DEGRADED" | "OFFLINE" | "NO_SIGNAL" | "RECORDING_ERROR" | "UNREACHABLE" | "ALARM";

export interface MapEntity {
  id: string;            // entity id (camera id for cameras)
  type: EntityType;
  siteId: string;
  serverId?: string;
  name: string;
  position: MapPosition;
  status: "unknown" | "online" | "degraded" | "offline";
  metadata: Record<string, unknown>;
}

export interface CameraMapProps {
  bearingDeg: number | null;   // null = direction unknown → no cone
  fovDeg: number;
  rangeM: number;
  cameraType: "fixed" | "dome" | "ptz" | "fisheye" | "lpr";
  ptz: boolean;
  lpr: boolean;
}
export interface CameraEntity extends MapEntity { type: "camera"; camera: CameraMapProps; activeAlarms: number }

export interface Site { id: string; name: string; regionId?: string; center?: GeoPosition; defaultZoom?: number; health?: SiteHealth }
export interface SiteHealth { online: number; offline: number; degraded: number; activeAlarms: number; severity: "OK" | "WARNING" | "CRITICAL" }
export interface Building { id: string; siteId: string; name: string; footprint?: GeoJSON.Polygon; floors: Floor[] }
export interface Floor { id: string; buildingId: string; name: string; ordinal: number; plan?: FloorPlan }
export interface FloorPlan { url: string; widthPx: number; heightPx: number; metersPerPx?: number; georef?: FloorGeoreference }
export interface FloorGeoreference { origin: GeoPosition; rotationDeg: number; metersPerPx: number }

export type ZoneKind = "area" | "restricted" | "perimeter" | "parking" | "entrance" | "custom";
export interface Zone {
  id: string; siteId: string; floorId?: string; name: string; kind: ZoneKind;
  geometry: GeoJSON.Polygon;           // geo: [lng,lat]; floor: [x,y] normalized
  style: { color?: string; opacity?: number; pattern?: "solid" | "hatched" };
  metadata: Record<string, unknown>;
  ruleIds: string[];                   // read-only link, see §14
}

export interface SavedMapView {
  id: string; name: string; shared: boolean; ownerId: string;
  state: { center: [number, number]; zoom: number; bearing: number; siteId?: string; floorId?: string;
           layers: LayerPreference; filters: MapFilters; zoneIds?: string[] };
}
export interface LayerPreference {
  cameras: boolean; coverage: boolean; ptzDirection: boolean;
  aiPerson: boolean; aiVehicle: boolean; lpr: boolean; faces: boolean;
  eventsAlarm: boolean; eventsMotion: boolean; eventsAudio: boolean;
  infraServers: boolean; infraNetwork: boolean; infraAccess: boolean; infraSensors: boolean;
  heatmap: boolean; traffic: boolean;
}
export interface MapFilters {
  siteIds?: string[]; cameraIds?: string[]; serverIds?: string[]; tags?: string[];
  objects?: string[]; priority?: ("alert" | "detection")[]; eventTypes?: string[];
  status?: CameraDisplayState[]; timeRange?: { start: string; end: string };
}
export type AutoFocusPolicy = "none" | "highlight" | "center" | "center_zoom" | "center_preview" | "incident_mode";

export interface RealtimeFrame<T = unknown> {
  v: 2; id: string; type: string; ts: string; tenantId: string;
  siteId?: string; cameraId?: string; serverId?: string; data: T;
}
```

The wire DTOs come from `schema.d.ts`, and `lib/maps/api.ts` maps them to these types in one place.

---

## 7. Database design

### 7.1 PostGIS: not in the MVP

The stack runs `postgres:17-alpine` (`docker-compose.yml:27`) with only `pgcrypto` and `pg_trgm`
(`migrations/00001_extensions.sql`).

| Option | Pros | Cons |
|---|---|---|
| **A. lat/lng `double precision` + GeoJSON `jsonb` (chosen)** | No image change; works in any managed Postgres; the queries are simple bbox ranges | Point-in-polygon and nearest-neighbour run in Go; no spatial index for polygons |
| B. PostGIS image (`postgis/postgis:17-3.5-alpine`) | GiST indexes, `ST_Contains`, `ST_DWithin`, clustering in SQL | Image and operations change (backups, upgrades); an extension on customers' existing DBs |

Scale check: 5k cameras per tenant is below 1 MB of positions, so bbox filtering on a btree index
`(tenant_id, lat, lng)` plus Go geometry (`orb` or ~150 lines of our own) is cheap. **Revisit B** when geo-rules run
per event at high rate (Phase 3), or when an entity count above 50k per tenant is a real requirement.
All geometry is stored as GeoJSON, so migrating to PostGIS later is a generated-column
addition (`geometry GENERATED ALWAYS AS (ST_GeomFromGeoJSON(geometry)) STORED`), not a rewrite.

### 7.2 Migration `00022_maps_core.sql`

```sql
-- +goose Up
-- Maps MVP. All map data is VMS-owned; the Frigate inventory sync never writes these tables.
ALTER TABLE sites
    ADD COLUMN lat double precision CHECK (lat BETWEEN -90 AND 90),
    ADD COLUMN lng double precision CHECK (lng BETWEEN -180 AND 180),
    ADD COLUMN default_zoom real,
    ADD COLUMN region_id uuid;

CREATE SEQUENCE map_revision;   -- bumped on every map write; drives client refetch

CREATE TABLE map_regions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    parent_id uuid REFERENCES map_regions (id),
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE sites ADD CONSTRAINT sites_region_fk FOREIGN KEY (region_id) REFERENCES map_regions (id);

CREATE TABLE map_buildings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    site_id uuid NOT NULL REFERENCES sites (id),
    name text NOT NULL,
    footprint jsonb,                                  -- GeoJSON Polygon, [lng,lat]
    lat double precision, lng double precision,       -- label/anchor point
    revision bigint NOT NULL DEFAULT nextval('map_revision'),
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE TABLE map_floors (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    building_id uuid NOT NULL REFERENCES map_buildings (id),
    name text NOT NULL,
    ordinal int NOT NULL DEFAULT 0,
    plan_key text NOT NULL DEFAULT '',                -- object storage key; '' = no plan
    plan_content_type text NOT NULL DEFAULT '',
    plan_width_px int, plan_height_px int,
    georef jsonb,                                     -- {origin:{lat,lng}, rotation_deg, meters_per_px}
    revision bigint NOT NULL DEFAULT nextval('map_revision'),
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    UNIQUE (building_id, ordinal)
);

CREATE TABLE map_devices (                            -- non-inventory entities (sensor, door, label, custom…)
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    site_id uuid NOT NULL REFERENCES sites (id),
    kind text NOT NULL CHECK (kind IN ('sensor','door','alarm_point','lpr','label','custom')),
    name text NOT NULL,
    status text NOT NULL DEFAULT 'unknown',
    props jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE TABLE map_placements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    site_id uuid NOT NULL REFERENCES sites (id),
    entity_type text NOT NULL CHECK (entity_type IN ('camera','server','device')),
    entity_id uuid NOT NULL,                          -- cameras.id | frigate_servers.id | map_devices.id
    floor_id uuid REFERENCES map_floors (id),         -- NULL = geo placement
    lat double precision, lng double precision,
    x real, y real,                                   -- normalized [0,1] on the floor plan
    bearing_deg real CHECK (bearing_deg >= 0 AND bearing_deg < 360),
    fov_deg real CHECK (fov_deg > 0 AND fov_deg <= 360),
    range_m real CHECK (range_m > 0),
    props jsonb NOT NULL DEFAULT '{}'::jsonb,         -- camera_type, height_m, icon…
    revision bigint NOT NULL DEFAULT nextval('map_revision'),
    created_by uuid, updated_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((floor_id IS NULL AND lat IS NOT NULL AND lng IS NOT NULL AND x IS NULL)
        OR (floor_id IS NOT NULL AND x BETWEEN 0 AND 1 AND y BETWEEN 0 AND 1))
);
-- One geo placement and one placement per floor per entity.
CREATE UNIQUE INDEX map_placements_geo_key ON map_placements (entity_type, entity_id) WHERE floor_id IS NULL;
CREATE UNIQUE INDEX map_placements_floor_key ON map_placements (entity_type, entity_id, floor_id) WHERE floor_id IS NOT NULL;
CREATE INDEX map_placements_site_idx ON map_placements (site_id);
CREATE INDEX map_placements_bbox_idx ON map_placements (tenant_id, lat, lng) WHERE floor_id IS NULL;
CREATE INDEX map_placements_floor_idx ON map_placements (floor_id) WHERE floor_id IS NOT NULL;

CREATE TABLE map_zones (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    site_id uuid NOT NULL REFERENCES sites (id),
    floor_id uuid REFERENCES map_floors (id),
    name text NOT NULL,
    kind text NOT NULL DEFAULT 'custom',
    geometry jsonb NOT NULL,                          -- GeoJSON Polygon (validated in Go)
    min_lat double precision, min_lng double precision, max_lat double precision, max_lng double precision,
    style jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    revision bigint NOT NULL DEFAULT nextval('map_revision'),
    created_by uuid, updated_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE INDEX map_zones_site_idx ON map_zones (site_id) WHERE deleted_at IS NULL;

CREATE TABLE map_views (                              -- same ownership model as `views` (00005)
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    owner_id uuid NOT NULL REFERENCES users (id),
    name text NOT NULL,
    shared boolean NOT NULL DEFAULT false,
    state jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE TABLE map_user_prefs (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    tenant_id uuid REFERENCES tenants (id),
    prefs jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- RLS: identical policy to every tenant table (00002…:201-219).
ALTER TABLE map_regions ENABLE ROW LEVEL SECURITY; ALTER TABLE map_regions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON map_regions USING (app_tenant_visible(tenant_id)) WITH CHECK (app_tenant_visible(tenant_id));
-- … same three statements for map_buildings, map_floors, map_devices, map_placements, map_zones, map_views, map_user_prefs.
```

Notes:

- `map_placements.entity_id` is polymorphic, so it has no FK. The rule "a placement dies with its camera"
  is enforced by the service. Soft-deleted cameras are filtered with a join (`cameras.deleted_at IS NULL`),
  so a camera that returns keeps its placement.
- The bbox index is range-on-lat plus filter-on-lng. At the target scale the planner returns under 10k rows
  per tenant, which is fine. PostGIS is the upgrade path (§7.1).
- RLS cost: every query already runs under `ScopeFor(actor)`. The policy function is `STABLE`,
  and there is at most one tenant per query, so it adds nothing beyond existing tables.

### 7.3 Migration `00023_alarm_lifecycle.sql`

```sql
ALTER TABLE alarms DROP CONSTRAINT alarms_status_check;
ALTER TABLE alarms ADD CONSTRAINT alarms_status_check
    CHECK (status IN ('open','acknowledged','assigned','investigating','resolved','closed'));
ALTER TABLE alarms ADD COLUMN closed_by uuid REFERENCES users (id), ADD COLUMN closed_at timestamptz;
CREATE TABLE alarm_transitions (
    id bigserial PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    alarm_id uuid NOT NULL REFERENCES alarms (id),
    from_status text, to_status text,        -- NULL,NULL = comment only
    actor_id uuid REFERENCES users (id),
    comment text NOT NULL DEFAULT '',
    at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX alarm_transitions_alarm_idx ON alarm_transitions (alarm_id, at);
-- + RLS as above. Existing index alarms_inbox_idx (tenant_id, status, created_at DESC) still serves filters.
```

The Down migration maps `assigned|investigating` back to `acknowledged` and `closed` back to `resolved` before
restoring the old CHECK, so it is reversible without losing any rows.

### 7.4 Phase 2 migration `00024_event_rollups.sql`

`event_counts_hourly(tenant_id, camera_id, hour, label, severity, n)` has PK
`(camera_id, hour, label, severity)`. The worker maintains it with an upsert on event insert
(syncer, after commit). It feeds heatmaps over 7 or 30 days (§16).

---

## 8. MapLibre strategy

### 8.1 Provider abstraction

```ts
interface MapProviderConfig {
  id: string;                          // "protomaps-local" | "maptiler" | "osm-raster" | "custom"
  kind: "vector-style" | "pmtiles" | "raster";
  styleUrl?: { light: string; dark: string };   // vector: full style JSON URL
  tiles?: string[];                    // raster or pmtiles:// URL template
  attribution: string;                 // always rendered (AttributionControl, compact)
  maxZoom: number;
  offline: boolean;                    // true = no call leaves the network
}
```

The server returns this from `GET /maps/config`, sourced from env
(`OPENVMS_MAP_PROVIDER`, `OPENVMS_MAP_TILE_URL`, `OPENVMS_MAP_STYLE_URL_LIGHT|DARK`,
`OPENVMS_MAP_ATTRIBUTION`). Later it can be a per-tenant override. API keys for commercial providers stay
server-side where possible. When a key must reach the browser, it is a domain-restricted key.

**Recommended default: self-hosted PMTiles.** This is a single `.pmtiles` file (a Protomaps
basemap built from OSM data), served with HTTP range requests from SeaweedFS or the web container.

- It needs no tile server process, it works fully offline, and one file per region is a simple
  deployment artifact.
- It uses the `pmtiles` protocol plugin (~15 KB).
- The attribution is "© OpenStreetMap contributors" (ODbL).

### 8.2 Light/dark styles from theme tokens

`MapStyleController` builds the vector style at runtime from CSS variables
(`getComputedStyle(document.documentElement)`):

| Style layer | Dark token | Light token |
|---|---|---|
| background / land | `--bg-app` (#0e1523) | `--bg-app` (#f4f6f5) |
| water | `--bg-nav` darkened 20 % | tinted `--accent` 12 % |
| roads (minor/major) | `--bg-panel-hover` / `--border-default` | `--border-default` / `--bg-panel` |
| buildings | `--bg-panel` @ 0.8 | `--bg-panel-elevated` |
| labels | `--text-muted` + halo `--bg-app` | `--text-secondary` + halo `--bg-panel` |

A `MutationObserver` on `<html class>` (the toggle at `Layout.tsx:304-310`) triggers
`map.setStyle(next, {diff: true})`. Our own sources and layers are re-added in the `style.load` handler,
so the theme switch does not reload the camera data.

Raster providers cannot be recolored. For those, the dark mode fallback applies
`raster-brightness-max: 0.35`, `raster-saturation: -0.6`, `raster-contrast: -0.1` and a dark
`background`, which keeps the map dim ("dark map must not be bright").

### 8.3 Sources and layers (top → bottom)

| Source (type) | Layers |
|---|---|
| `fx` (geojson, ≤ 200 features) | `fx-ripple` (circle, animated radius/opacity), `fx-alarm-pulse` |
| `cameras` (geojson, `cluster: true`, `promoteId: "id"`) | `cam-cluster`, `cam-cluster-count`, `cam-cluster-badges`, `cam-point` (symbol, status icon), `cam-label` (zoom ≥ 17) |
| `fov` (geojson, polygons, no cluster) | `fov-fill`, `fov-outline` (feature-state `selected`) |
| `devices` (geojson, cluster) | `dev-*` |
| `zones` (geojson) | `zone-fill`, `zone-line`, `zone-label` |
| `sites` (geojson) | `site-point` + health ring (zoom < 10) |
| `floorplan` (image) | `floorplan-raster` (floor mode only) |
| basemap | provider layers |

Status icons come from a runtime-built SDF sprite of Font Awesome glyphs (camera, video-slash,
triangle-exclamation, bell), so status is carried by **shape and color**, not color alone.
Selection is a halo ring. Offline cameras get a dashed outline pattern.

### 8.4 Semantic zoom

| Zoom | Shows |
|---|---|
| < 6 (country) | `sites` points with health ring; cameras hidden |
| 6–12 (city) | camera clusters with alarm/offline badges; zones as outlines; site labels |
| 13–17 (street) | individual cameras, FOV when "Coverage" is on, AI fx |
| ≥ 17 / building selected | floor selector; entering a floor switches to floor mode |

### 8.5 Floor plans: same MapLibre instance, local CRS mode (decision)

A floor view uses the same `MapCanvas` with an **empty base style** and an `image` source placed on
a synthetic extent near (0°, 0°), where Mercator distortion is negligible. The mapping is
`lng = (x − 0.5) · W`, `lat = (0.5 − y) · H`, with `W, H` in degrees chosen from the plan aspect
ratio (max side 0.01°). Floor placements are converted with `geo.ts:floorToLocal`.

Alternatives considered:

- **(a) A separate canvas or SVG renderer.** It would duplicate clustering, feature-state,
  hit-testing, FOV and fx code.
- **(b) Drawing the plan only as a georeferenced overlay on the geo map.** It needs georeference data that most sites won't have on day one, and
  it rotates with the city.

Local mode gives one code path. When a floor has `georef`, Phase 2 *also* offers
the plan as a georeferenced overlay in geo mode.

---

## 9. Clustering

- The client uses MapLibre GeoJSON clustering with `clusterRadius: 50`, `clusterMaxZoom: 16`, and cluster properties:

```ts
clusterProperties: {
  alarms:   ["+", ["case", [">", ["get", "alarms"], 0], 1, 0]],
  offline:  ["+", ["case", ["in", ["get", "st"], ["literal", ["offline", "unreachable"]]], 1, 0]],
  warnings: ["+", ["case", ["in", ["get", "st"], ["literal", ["degraded", "no_signal"]]], 1, 0]],
}
```

- The cluster circle's color is driven by the worst child: alarms > offline > warnings > ok. The badges are
  separate symbol layers offset around the circle, each with an icon and a number (not color-only).
- **Constraint**: `clusterProperties` read feature *properties*, not feature-state. So status
  changes update properties through `setData`, while pulses and ripples use feature-state on the unclustered layer or the separate `fx`
  source. `setData` is throttled to at most 1 per second, because status changes are rare (§10.3).
- **Server-side clustering** (supercluster in Go, per zoom and tile) is deferred. It is only needed when a
  tenant view exceeds ~20k points, or on mobile. The `/maps/entities?bbox&zoom` endpoint is shaped
  so it can return clusters later without changing the client contract (a `t: "cluster"` entity).

---

## 10. Realtime strategy

### 10.1 State separation

| Store | Holds | Tech |
|---|---|---|
| Persistent | placements, zones, buildings, prefs, views | TanStack Query (`lib/maps/api.ts`), invalidated by `entity.changed` / revision |
| Realtime | status overrides, active alarms per camera, 5-min event ring, fx queue | `mapRealtimeStore` (plain TS, `useSyncExternalStore` for panels) |
| UI | mode, selection, hover, open panels, draft edits | `useReducer` in `MapShell` + URL search params |

### 10.2 Feed integration

`lib/realtime.ts` stays the single `/ws` owner. It gains:

- A tiny listener API (`subscribeFrames(fn)`).
- A `hello` sent on open, with the per-stream `last_event_id` it has seen.

Its existing invalidation map stays unchanged, so other pages keep working. Maps registers a
listener while mounted and adds its topics to the `hello`. No second socket is opened; the per-user
limit is 5 (`hub.go:37`).

### 10.3 Pipeline (client)

```text
frame → dedup (Set of ids, 5-min TTL) → ring buffer (5 min, max 20k)
      → pending batch (Map cameraId → latest state)
      → requestAnimationFrame flush:
           • status/alarm-count changes → entityIndex patch → setData("cameras") at most 1/s
           • selected/hover             → setFeatureState (cheap, every frame OK)
           • fx (ripple/pulse)          → animationBudget.enqueue(priority)
```

**Animation budget**:

| Constraint | Limit |
|---|---|
| Concurrent ripples | max 30; lowest priority dropped first (detection < LPR < alarm) |
| Alarm pulses | max 50, one shared animation clock (a single `setPaintProperty` per frame, not per feature) |
| Ripple duration | 2.5 s, then removed |
| Pulse after acknowledge | becomes a static ring |
| `prefers-reduced-motion` | no animation; static badge + list highlight instead |
| Hidden tab | fx paused; `document.hidden` stops the rAF loop, and the state still updates |

### 10.4 Root-cause grouping

- **Server goes offline**: cameras of that server render as `UNREACHABLE` (grey, server glyph),
  not as N offline alarms. The site panel shows one grouped item, "Server X offline — 42 cameras
  affected".
- **Alerts**: rules already debounce per resource (`rule_firings`, `00017`). Maps never creates alerts of its own.

### 10.5 Incident auto-focus

The per-user policy lives in `map_user_prefs.prefs.autoFocus`, default `none`. The map never moves by
default. `center*` modes are throttled: at most one move per 10 s, and never while the user has interacted with
the map in the last 15 s.

---

## 11. Scaling to thousands of cameras

| Cameras | Initial payload (compact JSON) | gzip | Strategy |
|---|---|---|---|
| 100 | ~25 KB | ~5 KB | one site query |
| 1 000 | ~250 KB | ~40 KB | one query, client cluster |
| 5 000 | ~1.2 MB | ~180 KB | one query per visible site set; client cluster; FOV computed lazily for the viewport only |
| > 20 000 | — | — | `bbox + zoom` query with server-side clusters (Phase 3) |

- **Culling FOV**: cone polygons (16 segments) are generated only for cameras inside the
  viewport and at zoom ≥ 13. They are recomputed on `moveend`, debounced by 150 ms.
- **Updates**: never re-send the full list for status. Status patches are applied to the in-memory
  FeatureCollection and pushed with a throttled `setData`. MapLibre's worker handles 5k
  points in about 20–40 ms off the main thread. Hover and selection use feature-state only.
- **Server**: `GET /maps/sites/{id}/entities` joins placements with
  `cameras.status`, the authorized camera ID set (`access.CameraIDs(cameras.view)`), and active-alarm counts
  (`alarms_camera_idx`) in one query. It has an ETag on `revision + max(cameras.updated_at)`, so repeat loads return 304.
- **Metrics**, in dev as `window.__openvmsMapMetrics` and optionally sent to Prometheus through a beacon later:

  | Metric | Target |
  |---|---|
  | FPS (rAF delta) | ≥ 50 at 5k |
  | entities visible | — |
  | events/s applied | — |
  | WS lag (`now − frame.ts`) | — |
  | time to first render | < 1.5 s at 1k |
  | time to preview | < 500 ms warm |

---

## 12. Video previews

The hover strategy follows the brief exactly, and it only works on top of the existing manager:

| t (hover) | Action | Reuse |
|---|---|---|
| 0 ms | tooltip: name, status, site, last event (from the entity + realtime store) | — |
| 150 ms | `<img src=/media/v1/cameras/{id}/snapshot.jpg?h=240>` | gateway snapshot (`gateway.go:172`), `private, max-age=5` |
| 400 ms | **prewarm**: `manager.acquire(id,"sub")` (not attached) | `PlayerSessionManager.acquire` (`:62`) |
| 700 ms | if the "Live on hover" pref is on: attach the session `<video>` over the snapshot | `usePlayerSession` |
| leave | `release(id,"sub")` → WARM 30 s → evicted | `release` (`:79`), WARM TTL 30 s |

Rules:

- **Single hover preview**: a new hover target releases the previous one immediately.
- **Pinned previews** (click → `CameraPanel`): max 4. A 5th pin replaces the oldest.
- **The map never auto-opens streams** for visible cameras, and it never exceeds the manager's soft cap of 32. The
  manager already evicts WARM sessions at the cap.
- **Progression** is icon → snapshot → sub → main. Main is used only in the expanded panel, and it
  uses the P1 seamless switch when available (`seamlessQualitySwitch` flag).
- **Snapshot as poster**: the snapshot stays visible until the first frame, reusing the Live "never
  black" poster order (`docs/live-view-architecture.md` §Never black).
- **Touch** (tablet): there is no hover. Tap → panel with snapshot → "Live" button.
- **Double click** → `/live?camera=<id>`. "Add to current Live View" writes to the Live grid state
  (`lib/liveGrid.ts` `placeCameraUnique`).
- **PTZ**: controls appear in the panel when the actor has `live.ptz` on the camera. They go through the
  existing gateway, never directly to the camera.

---

## 13. Map editor

- **EDIT mode** is visible only with `maps.edit` or `maps.edit_device` on the current site. The
  server re-checks every write.
- **Tools**: add camera (from `UnplacedTray` drag and drop), device, zone, building, floor, and label.
- **Rotate**: drag a handle at the end of the cone. `bearing = atan2`, with a 15° step and Shift for free rotation.
- **FOV and range**: drag the arc edge and radius.
- **Snap**: on the floor plan, the grid is in normalized units (1 %). On the geo map, snapping to other entities within 8 px.
- **Implementation**:
  - Tools are plain TS classes bound to MapLibre mouse events, writing into a **draft
    FeatureCollection** source (`edit-draft`).
  - React shows only the properties form.
  - "Save" sends one `PUT` per changed placement, with `If-Match: <revision>` for optimistic concurrency.
    A 409 re-bases the draft.
- **Audit** (existing `audit_log` pattern):

  | Action | Details |
  |---|---|
  | `maps.placement.upsert`, `maps.placement.delete` | before/after |
  | `maps.zone.create`, `maps.zone.update`, `maps.zone.delete` | — |
  | `maps.floor.plan_upload`, `maps.site.geo_update` | — |

- **Undo/redo**: a client-side stack of draft operations until save.
- **Bulk entry for existing cameras** (answers G1):
  - Unplaced tray.
  - "Place all at site center" (then drag).
  - CSV import `camera_id|remote_name,lat,lng,bearing,fov,range` (MVP-late unit; validates
    authorization per row, dry-run preview).

---

## 14. Zones

There are **two distinct concepts**, and they are never merged:

| | Map zone (`map_zones`) | Frigate zone (`cameras.zones`, FC-4 editor) |
|---|---|---|
| Space | world (geo) or floor plan | camera image pixels |
| Owner | OpenVMS | Frigate config |
| Used for | situational awareness, rules by location, analytics | Frigate object filtering, `events.zones` |

Relation: an **optional mapping** in `map_zones.metadata.frigate_zones: [{camera_id, zone}]`. It lets the backend say "event X in
Frigate zone `door` of camera C" ⇒ "object entered map zone Z" without projecting geometry. This is
how `object.entered_zone` is produced in Phase 2 (from events' `zones` array).

**Rules integration.** The interface is added now; the engine comes later.

- `rules.Conditions` (`internal/rules/rule.go:18`) gains `MapZoneIDs []uuid.UUID`
  `json:"map_zone_ids,omitempty"`.
- `EventContext` gains `MapZoneIDs`, filled by a `maps.ZoneResolver` interface:

  ```go
  type ZoneResolver interface {
      ZonesForEvent(ctx context.Context, cameraID uuid.UUID, frigateZones []string) ([]uuid.UUID, error)
  }
  ```

- `MatchesEvent` treats an empty list as "any", like every other condition.
- The resolver is nil in the MVP, so rules behave exactly as before.
- The zone panel lists the rules that reference it (read-only query on `rules.conditions`).

**Validation** reuses `lib/zoneGeometry.ts` (`polygonArea`, `hasSelfIntersection`) on the client,
with a Go mirror on the server. The server enforces: max 500 vertices, closed ring, no self-intersection, and the bbox columns
are computed on write.

---

## 15. Floor plans (Phase 2)

1. **Upload**: `PUT /maps/floors/{id}/plan` (multipart, ≤ 20 MB, PNG/JPG/SVG).
   - SVG is sanitized server-side: scripts, foreign objects and external refs are stripped.
   - The server records `plan_width_px/height_px` and stores the file under
     `maps/<tenant>/floors/<floor>/<sha256>.<ext>` via the `Blobs.Put` pattern
     (`internal/branding/service.go:77`).
   - PDF/DXF/DWG are rasterized in the worker later (Phase 3).
2. **Serve**: `GET /maps/floors/{id}/plan` goes through the API (auth + `maps.view`), with `ETag` = sha.
3. **Calibrate** (optional `georef`):
   - The editor sets origin (a pixel ↔ lat/lng pair), scale (two clicked points plus a known distance
     → `meters_per_px`) and rotation.
   - This enables range and FOV in meters on the plan, and the georeferenced overlay in geo mode.
4. **Coordinates**: placements on floors are always normalized `x,y`, so re-uploading a plan at a
   different resolution keeps every placement.

---

## 16. Heatmaps (Phase 2)

- **Endpoint**: `GET /maps/analytics?metric=object|person|vehicle|motion|alarm|lpr&start&end&site_id&zone_id&camera_id&object_type`
  returns `[{camera_id, lat, lng, weight}]` plus totals.
- **Query** (≤ 24 h, raw events):

  ```sql
  SELECT camera_id, count(*) AS n
  FROM events
  WHERE tenant_id = $1 AND start_time >= $2 AND start_time < $3
    AND camera_id = ANY($4)                  -- authorized (events.view) ∩ filter
    AND ($5::text[] IS NULL OR labels && $5) -- events_labels_idx (gin)
  GROUP BY camera_id;
  ```

  This is served by `events_tenant_time_idx` / `events_camera_time_idx` (`00003_events.sql`).
- **Longer ranges** (> 24 h): the query reads `event_counts_hourly` (§7.4), so a 30-day query touches at most
  720 rows per camera.
- **Other metrics**: `alarm` counts `alarms`. `lpr` counts `lpr_reads` (`lpr_reads_camera_time_idx`).
- **Placement of weight**: at the camera point by default. With "coverage" weighting, the weight is spread over 3 points
  along the FOV axis. A heatmap never implies an exact object location.
- **Rendering**: a MapLibre `heatmap` layer, with weight normalized by `max(n)` and intensity by zoom.
  ANALYTICS mode hides fx and dims the camera layer.
- **Caching**: the server keeps results for 60 s per `(tenant, params)` in Valkey (already in the stack).

---

## 17. Historical timeline and replay (Phase 2)

- **Windowed loading**: `GET /maps/timeline?site_id&start&end&cursor` returns events in
  **15-minute windows** (at most 2 000 per page, keyset on `(start_time, id)`, matching
  `events_tenant_time_idx`). The client prefetches the next window when the playhead reaches 70 %.
- **Replay**: `ReplayController` drives the same fx pipeline from stored events at 1×–64×,
  with the realtime feed paused. Camera statuses during replay come from `camera_outages` history.
  Phase 2 adds `camera_status_log` if needed; the MVP shows current status with a "historical status
  unavailable" note.
- **Observed vs correlated**:
  - **Observed events** are points at camera positions.
  - A **correlated track** is only drawn when the backend returns an explicit correlation, and it is always a
    **dotted temporal sequence** between camera positions, labelled with times.
  - **No interpolation, no inferred path geometry.**
- **LPR sequences**: `GET /maps/lpr-track?plate=` reads `lpr_reads` by `plate_normalized` (trigram
  index `lpr_reads_plate_trgm_idx`), ordered by `seen_at`, and keeps only strong matches
  (exact normalized plate, `score ≥` a configurable threshold). Each hop shows camera, time and score.
- **Playback**: clicking an event opens `/live?mode=rec&t=<ts>&camera=<id>` (LV-9 deep link).

---

## 18. Permissions

New catalog entries (`internal/authz/catalog.go`, same `Definition` shape as `:81-109`):

| Permission | Description | Narrowest scope |
|---|---|---|
| `maps.view` | View maps | `site` |
| `maps.edit` | Edit map structure (site geo, buildings, floors, plans) | `site` |
| `maps.create_zone` | Create/edit map zones | `site` |
| `maps.edit_device` | Place/move/rotate cameras and devices | `site` |

The brief's names map to existing permissions. Nothing is duplicated:

| Brief name | Existing permission |
|---|---|
| `alarm.acknowledge`, `alarm.assign` | `alarms.manage` (camera scope) |
| `camera.ptz` | `live.ptz` |

Server-side filtering (never send unauthorized entities):

| Entity | Visible iff |
|---|---|
| Site | `maps.view` on site (`SiteIDs(maps.view)`) |
| Camera marker | site visible **and** `cameras.view` on the camera (same set as the camera list, `internal/inventory/cameras.go:29`) |
| Camera snapshot / preview | + `live.view` (enforced by the media gateway already) |
| Server marker | site visible and `servers.view` |
| Device / zone / building / floor | site visible |
| Event fx, heatmap, timeline | + `events.view` per camera |
| Alarm fx / panel | + `alarms.view`; actions require `alarms.manage` |
| LPR track | + `lpr.search` per camera |

- Realtime uses the same checks. The `Authorizer` already checks the camera scope per frame (`authorizer.go:84`).
  Frames for cameras whose site lacks `maps.view` are filtered client-side only for display purposes,
  because the frames are already authorized for the camera itself.
- Denied writes are audited automatically by the router (`internal/api/router.go:78-83`).
- The nav item is added to `navGroups` (`components/nav.ts`) with `permission: "maps.view"`, behind the `maps`
  rollout flag (`features.go` / `features.ts`, default off).

---

## 19. Phased implementation plan

Conventions:

- ODD work units, each ≤ ~400 authored lines. This is advisory, not a hard gate.
- Every unit carries its own tests and docs.
- Contract-first: every backend unit that adds endpoints edits `openapi.yaml`, and the web
  unit that consumes them regenerates `schema.d.ts`.

### 19.1 MVP work units

| ID | Unit | Side | Depends on | ~Lines |
|---|---|---|---|---|
| **M-B1** | Migration `00022_maps_core` + sqlc queries + `maps.*` permissions in catalog + `maps` feature flag (Go + web) | BE | — | 380 |
| **M-B2** | `internal/maps` read path: `GET /maps/config`, `/maps/overview`, `/maps/sites/{id}`, `/maps/sites/{id}/entities` (RBAC filter, ETag) + OpenAPI | BE | B1 | 400 |
| **M-B3** | Write path: placements upsert/delete (If-Match), `PATCH /sites/{id}/geo`, `GET /maps/unplaced`, audit | BE | B2 | 350 |
| **M-B4** | Zones CRUD + geometry validation + bbox columns + `rules.Conditions.MapZoneIDs` / `ZoneResolver` seam (nil) | BE | B1 | 350 |
| **M-B5** | Realtime v2 envelope (`id/ts/site_id/camera_id`), `seq` plumbing in `Source`, HealthPoller per-camera diff → `camera.status.<tenant>` + decoder | BE | — | 350 |
| **M-B6** | Hub: `hello`/`filter` client frames, 5-min ring buffer resume + `resync`, coalescing + server-offline suppression, batch frame | BE | B5 | 400 |
| **M-B7** | Alarm lifecycle: `00023`, `investigate/close/comments`, `assign` sets status, `alarm_transitions`, OpenAPI enum, Alarms inbox labels + `status_group=active` | BE+web | — | 400 |
| **M-W1** | `maplibre-gl` + `pmtiles` deps, lazy `/maps` route + nav (flagged), `MapCanvas`, `MapStyleController` (tokens, theme switch, raster fallback), provider config | Web | B2 contract | 380 |
| **M-W2** | Camera layers: compact entity → GeoJSON, clustering + clusterProperties badges, SDF status sprite, labels, semantic zoom, sites layer | Web | W1 | 400 |
| **M-W3** | FOV layer (viewport-culled cones, "Coverage" toggle), selection feature-state, breadcrumb/site navigation, deep links `/maps/site/:id[/camera/:id]` | Web | W2 | 350 |
| **M-W4** | `lib/realtime.ts` listener API + `hello`; `mapRealtimeStore` (dedup, 5-min ring, rAF batch, throttled setData), `animationBudget` + fx layers (ripple, alarm pulse, reduced motion) | Web | W2, B5 (B6 for resume) | 400 |
| **M-W5** | `CameraPanel` + `CameraPreview` (hover intent 0/150/400/700, PlayerSessionManager), context menu, dblclick → Live, "Add to Live View", nearby cameras (client haversine) | Web | W2 | 400 |
| **M-W6** | `AlarmPanel` (ack/assign/investigate/resolve/close via alarms API), `SiteHealthPanel` with root-cause grouping, auto-focus policy | Web | W4, B7 | 380 |
| **M-W7** | `LayersPanel` + `FiltersPanel` + `/me/map-prefs` (BE endpoint included) | Web+BE | W2 | 350 |
| **M-W8** | Editor A: EDIT mode, UnplacedTray drag & drop, move/rotate/FOV handles, draft source, save with If-Match, undo | Web | W3, B3 | 400 |
| **M-W9** | Editor B: zone draw/edit (zoneGeometry reuse), zone layers, zone panel | Web | W8, B4 | 350 |
| **M-W10** | Perf harness: synthetic 5k fixture generator, metrics overlay, Playwright perf smoke | Web | W4 | 250 |
| **M-B8** *(MVP-late)* | CSV placement import (dry-run + apply) | BE+web | B3 | 300 |

The total is about 6.6k authored lines across 18 units.

**Parallel tracks.** B1→B2→B3 runs alongside B5→B6 and alongside B7. W1 can start as soon as B2's
OpenAPI section is merged, using fixtures before the handlers exist. W5 and W7 are independent after W2.
With one BE and one web developer, the critical path is **B1 → B2 → W1 → W2 → W4 → W6**,
about 5–6 weeks. Single-writer rule: no two units edit the same file at once. The `openapi.yaml` edits
are serialized by the contract-first order.

### 19.2 Phase 2 (≈ 5–7 weeks)

- Floor plans: upload, serve and calibrate, local CRS mode, georef overlay (≈ 3 units).
- Heatmaps: `00024` rollups, worker upsert, `/maps/analytics`, heatmap layer (≈ 3 units).
- Timeline and replay: `/maps/timeline`, `GeoTimeline`, `ReplayController` (≈ 3 units).
- LPR visualization: `/maps/lpr-track`, omnibox "Show on Map" for plates (≈ 2 units).
- PTZ direction: rate-limited ONVIF/Frigate PTZ position → `ptz.position_changed` (≈ 2 units).
- Saved map views: `map_views` CRUD and UI (≈ 1 unit).
- `object.entered_zone` via Frigate-zone mapping (≈ 1 unit).

### 19.3 Phase 3 (≈ 8–12 weeks, needs discovery)

- Multi-camera correlation (direct / probable / confirmed) and route reconstruction.
- Blind-spot analysis (FOV union vs zone polygons; likely the PostGIS trigger).
- Access control and IoT device adapters (`DeviceStatusSource`).
- Incident mode.
- Server-side clustering for more than 20k entities.
- Low-latency event ingest in the worker (Frigate MQTT/WS).
- Natural-language integration.
- PDF/DXF/DWG plans.

---

## 20. Directory structure

```text
internal/maps/                     # new bounded context (screaming: "maps")
  doc.go
  model.go                         # MapEntity, Placement, Zone, Building, Floor, Position (domain types)
  service.go                       # Service{Store, Blobs, Pub, Log}: read/write use cases, RBAC, audit
  entities.go                      # site/viewport entity query + status/alarm join + ETag
  placements.go
  zones.go                         # CRUD + geometry validation
  geometry.go                      # polygon validity, bbox, point-in-polygon, haversine (pure)
  hierarchy.go                     # regions/buildings/floors
  floorplans.go                    # Phase 2
  analytics.go / timeline.go       # Phase 2
  zone_resolver.go                 # implements rules.ZoneResolver (Phase 2)
internal/api/maps_handlers.go      # strict handlers → maps.Service (pattern of alarms_handlers.go)
internal/realtime/                 # routes.go (+decoders), hub.go (+filters, ring), resume.go (new), protocol.go (client frames)
internal/inventory/health.go       # + per-camera diff callback (OnCameraChange)
internal/alarms/service.go         # + lifecycle transitions
internal/authz/catalog.go          # + maps.* permissions
internal/store/queries/maps.sql    # sqlc
migrations/00022_maps_core.sql, 00023_alarm_lifecycle.sql, 00024_event_rollups.sql (P2)
packages/api-contract/openapi.yaml # + /maps/* paths, schemas under components/schemas/Map*
apps/web/src/routes/Maps.tsx
apps/web/src/components/maps/...   # §2
apps/web/src/lib/maps/...          # §2
docs/maps/ARCHITECTURE.md          # this file; later docs/maps/OPERATIONS.md (tiles, offline)
```

This follows existing conventions (`components/zones`, `lib/live`) rather than introducing a
`features/` tree that nothing else in `apps/web/src` uses.

---

## 21. Tests

| Level | What | Tooling |
|---|---|---|
| Go unit | geometry validation, bbox, haversine; display-state derivation; envelope v2 decoding (allow-list, no leaked fields — same style as `routes.go:114`); ring buffer resume/resync edges; coalescing; alarm transition table (all from→to pairs) | `go test`, table-driven (`go-testing` skill) |
| Go integration | RBAC: an operator with one camera grant gets exactly that marker; tenant isolation via RLS; If-Match 409; audit rows written; migration up/down/up (incl. alarm status mapping on Down) | existing `testutil` Postgres harness, `*_integration_test.go` pattern |
| Contract | OpenAPI validates; generated types compile; handlers return schema-conformant payloads | existing contract checks |
| Web unit | layer spec builders (pure), `entityIndex` patching, `mapRealtimeStore` dedup/ring/batch, `animationBudget` priority + reduced motion, `hoverIntent` timers (fake timers), style builder from tokens | vitest |
| Web component | panels with fixtures (container/presentational), editor forms, filters | vitest + Testing Library |
| E2E | open /maps, place a camera, see status change pushed, acknowledge alarm from map, deep link | Playwright against demo bootstrap (`internal/bootstrap/demo.go`) |
| Performance | synthetic 1k / 5k cameras + 50 events/s + alarm flood 500 in 10 s: FPS ≥ 50 (desktop), no main-thread task > 50 ms after first render, WS stays connected (no slow-consumer close) | W10 harness, Playwright trace, `window.__openvmsMapMetrics` |
| WebGL | MapLibre mocked in jsdom (canvas unit tests don't need GL); real GL only in Playwright | — |

TDD mode follows the project/session configuration at implementation time. The Live View feature ran
with TDD off by PO decision (`docs/roadmap/live-view-platform.md` §4). Confirm this for Maps (see the open questions).

---

## 22. Technical risks

| Risk | Impact | Mitigation |
|---|---|---|
| Tile licensing/usage policy: public `tile.openstreetmap.org` forbids heavy/commercial use and has no SLA | blocked or throttled maps in production | Self-hosted PMTiles default; OSM raster only for dev; attribution always rendered |
| Offline/air-gapped networks | blank basemap | PMTiles file shipped as a deploy artifact; `offline: true` config disables every external URL (fonts/glyphs/sprites served locally too) |
| WebGL unavailable or weak (low-end clients, VDI) | map unusable | `maplibregl.supported()` check → fallback list/table view of the same entities; reduced layer set on low FPS (auto-disable FOV/fx) |
| Stream explosion from previews | gateway/Frigate overload | hover single-session, pin cap 4, manager soft cap 32, never auto-open; snapshots first |
| Alarm/event floods | UI jank, slow-consumer disconnects | server coalescing + batch frames; client rAF batching; fx budget; root-cause suppression |
| Event latency (polling) | "realtime" AI pulse is seconds late | documented; Phase 3 low-latency ingest; never marketed as sub-second |
| Coordinate data entry for existing installs | empty map at launch | unplaced tray, site-center bulk place, CSV import, site default center |
| No PostGIS | complex spatial queries in Go | scale-bounded; GeoJSON storage makes the upgrade a generated column |
| RLS + scope filtering cost on large entity lists | slow first load | one joined query per site, ETag/304, `AuthorizedCameraIDs` already cached per request; realtime uses the existing 30 s `CachedAuthorizer` |
| MapLibre bundle (~230 KB gzip) | slower first visit to /maps | lazy route chunk only; not in the main bundle |
| Style/theme drift | bright map in dark mode | style derived from tokens at runtime; visual snapshot test per theme |
| Ring buffer memory per API instance | memory growth under floods | hard cap per stream (50k) → overflow means `resync`, never unbounded |
| Polymorphic `entity_id` without FK | orphan placements | service-level cleanup on camera delete + nightly orphan check query |

---

## 23. Key architectural decisions

| # | Decision | Alternatives | Why / tradeoff |
|---|---|---|---|
| AD-1 | New bounded context `internal/maps`; inventory and alarms stay the sources of truth | extend `internal/inventory` | isolates map-owned data; inventory sync can never overwrite placements; costs one more package |
| AD-2 | lat/lng + GeoJSON jsonb, no PostGIS in MVP | PostGIS now | no infrastructure change; enough for 5k/tenant; upgrade path is additive |
| AD-3 | Reuse the single `/ws` with envelope v2 + opt-in `hello` | separate `/ws/maps` or SSE | one socket per tab, one auth path, old clients unaffected; the hub gains a small protocol |
| AD-4 | Resume via per-instance 5-min ring keyed by JetStream sequence, `resync` fallback | per-connection JetStream consumers | bounded memory, no NATS consumer churn; the gap beyond 5 min falls back to REST refetch (today's behaviour) |
| AD-5 | Viewport filtering client-side, site filtering server-side | server-side bbox per connection | the hub doesn't need positions; fine to 5k; revisit with server clustering |
| AD-6 | Floor plans in the same MapLibre instance on a synthetic local extent | separate canvas/SVG renderer | one code path for layers, clustering, fx, editor; slight coordinate trickery isolated in `geo.ts` |
| AD-7 | Status via throttled `setData`; hover/selection/pulse via feature-state + separate fx source | re-setting data per event | clusterProperties need properties; fx need 60 fps without touching 5k features |
| AD-8 | Alarm lifecycle extended in place (`open` stays = NEW), transitions table | new `incidents` entity | inbox, rules and syncer keep working; the Down migration is lossless-mappable |
| AD-9 | Map zones ≠ Frigate zones; optional explicit mapping | project Frigate pixel zones to geo | projection needs camera calibration we don't have; the mapping is honest and cheap |
| AD-10 | Self-hosted PMTiles as the default provider behind a provider abstraction | public OSM tiles / MapTiler | offline-capable, no usage-policy risk; needs a regional tile file per deployment |
| AD-11 | Previews ride `PlayerSessionManager` (acquire as prewarm) | Maps-owned players | one session budget across Live and Maps; WARM reuse when jumping to Live |
| AD-12 | Rollout behind `OPENVMS_FEATURES=maps` | always on | matches the Live platform rollout practice; zero impact until enabled |

---

## Open questions for the PO

1. **Tile source for this deployment.** Should the default be public OSM tiles, a commercial provider (MapTiler), or self-hosted/offline?
   *Recommended default:* self-hosted PMTiles (Protomaps basemap for the customer's
   country/region) served by OpenVMS, fully offline. Public OSM tiles only for development, since the OSM
   tile usage policy disallows production traffic at this scale.
2. **Alarm lifecycle scope in the MVP.** Should the full NEW/ACK/ASSIGNED/INVESTIGATING/RESOLVED/CLOSED model ship
   with Maps?
   *Recommended default:* yes, as unit M-B7. It is additive (`open` = NEW), it keeps the
   Alarms inbox working, and it is independent of the map, so it can run in parallel. If it is descoped, the map panel
   works with the current 3 states and the assignee.
3. **How existing cameras get coordinates.**
   *Recommended default:* manual placement from the "Unplaced" tray, plus
   "place all at site center", in the MVP. CSV import comes as the MVP-late unit M-B8. Sites need a
   center point, which the editor sets.
4. **PostGIS.** Adopt it now (Postgres image change) or keep lat/lng + GeoJSON jsonb?
   *Recommended default:* keep lat/lng + jsonb for MVP and Phase 2. Revisit for Phase 3
   blind-spot analysis or when a tenant needs more than 50k entities.
5. **MapLibre dependency and TDD mode.** Is a ~230 KB gzip lazy-loaded chunk for `/maps` acceptable, and
   is TDD on or off for this feature?
   *Recommended default:* accept the chunk, since it is lazy and never in the main bundle.
   Keep TDD off, as for Live View, but require the unit, integration and perf tests listed in §21 for each work unit.
