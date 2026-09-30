# Maps — Product Brief

Source: product owner requirements (2026-09-30), condensed. Reference for the Maps stage.

## Vision
A new top-level page **Maps**: a geospatial security operations surface combining cameras,
devices, AI detections, alarms, zones, floor plans, infrastructure health and historical
analytics — not "a page to place cameras". Must scale from small installs to municipalities,
multi-city, thousands of cameras, many servers/recorders/analytics engines.

## Hard architecture rules
- Maps never talks to Frigate (or any backend) directly: it consumes OpenVMS normalized APIs
  (Camera, Device, Event, Alarm, Analytics, Map) and the OpenVMS event bus over WebSocket/SSE.
- No hardcoded sites/cameras/vendors. Respect RBAC fully: never send unauthorized entities.
- Reuse users/RBAC, cameras, servers, events/alarms, Live View, Playback, Search, themes, UI kit.
  Create compatible abstraction layers where missing; no destructive refactor.
- Production quality: modular, typed, testable, documented, scalable.

## Tech
React + TypeScript + **MapLibre GL JS** (WebGL, GeoJSON sources, clustering, heatmaps, vector
tiles). `MapProvider` abstraction (OSM, MapTiler, custom tile server, **offline tiles** via
configurable Tile Server URL — critical for isolated networks). Light/Dark styles integrated
with OpenVMS themes (dark map must not be bright).

## Model
- Hierarchy: Organization › Region › Site › Area › Building › Floor › Zone › Device; breadcrumb
  navigation in/out of levels.
- Map types: Geographic (GIS) and Floor plans (SVG/PNG/JPG now; PDF/DXF/DWG later). Site has
  buildings; building has floors; each floor has its plan.
- Generic `MapEntity {id, type camera|sensor|door|server|alarm|building|custom, siteId,
  position {type geo(lat,lng) | floor(floorId, x, y normalized)}, status, metadata}`.
- Camera map fields: position, orientation, fov_angle, estimated_range, camera_type,
  ptz_capabilities, status, recording_status, analytics_capabilities.
- Zone: id, site_id, floor_id, name, type, geometry (GeoJSON Polygon), style, metadata, rules.
- Entities: Map, Site, Building, Floor, Zone, MapEntity, MapPosition, SavedMapView,
  MapLayerPreference.

## Modes
LIVE (operational, realtime), INVESTIGATE (timeline + replay), ANALYTICS (heatmaps/density,
cleaner), EDIT (authorized editors).

## LIVE
- Cameras with states ONLINE/OFFLINE/DEGRADED/RECORDING_ERROR/NO_SIGNAL/ALARM/SELECTED; normal
  discreet, critical prominent; not color-only (icons/shapes/labels/patterns).
- Clustering with counts + alarms/offline/warnings badges; semantic zoom (country: sites/health;
  city: clusters/alarms/areas; street: cameras/FOV/AI events; building: floors/doors/sensors/zones).
- FOV cones (toggle "Camera Coverage", soft transparency, emphasis when selected).
- AI detection: single ripple 2–3 s; alarms: repeated subtle pulse until acknowledged then static
  highlight. Animation budget by priority; honor prefers-reduced-motion; avoid fatigue.
- Alarm lifecycle NEW/ACKNOWLEDGED/ASSIGNED/INVESTIGATING/RESOLVED/CLOSED with timestamps,
  actors, comments, audited. Alarm side panel (live video, details, acknowledge/assign/investigate/
  open recording/create incident).
- Incident auto-focus configurable per user (do nothing [default] / highlight / center /
  center+zoom / center+preview / incident mode). Never move the map by default.
- Camera click → floating preview/side panel (don't leave Maps); double click → Live View; "Add
  to current Live View". Hover strategy: 0 ms metadata, 150 ms snapshot, 400 ms prewarm, 700 ms
  optional live. Progressive: icon → snapshot → substream → main. Never auto-open streams for all
  visible cameras. Snapshot shown while video loads; target < 500 ms perceived when prewarmed.
- Layers panel (cameras, coverage, PTZ direction; AI person/vehicles/LPR/faces; events alarm/
  motion/audio; infrastructure recorders/servers/network/access/sensors; analytics heatmap/traffic)
  persisted per user. Combinable filters (sites, cameras, servers, tags, objects, priority, event
  type, status, time range).
- Hot zones (manual polygons, GeoJSON), usable by rules (interfaces now; engine later).
- PTZ: 360° capability, live direction when ONVIF reports (rate-limited); PTZ controls/presets
  from preview.
- Health: site aggregates (cameras online/offline/degraded, active alarms), severity OK/WARNING/
  CRITICAL; root-cause grouping (server offline → its cameras, one grouped alert, no floods).
- Context menu on camera: Live, Playback, Events, Add to Live View, Nearby cameras, Coverage,
  Configuration, Disable notifications (permission-dependent). Nearby cameras by distance/zone/
  building/floor; incident nearby view.

## INVESTIGATE / ANALYTICS
- Bottom geographic timeline with PLAY; replay shows events over time; distinguish Observed
  Events vs Correlated Track; never invent trajectories (dotted temporal sequence only). LPR
  tracking for strong plate matches. Future: multi-camera correlation (direct / probable /
  confirmed).
- Heatmaps (object/person/vehicle/motion/alarm/LPR density) for last hour/today/24 h/7 d/30 d/
  custom, filters, rebuilt from stored events; `GET /maps/analytics` (site, zone, start, end,
  object_type, camera, metric).

## EDIT
Toolbar add camera/sensor/alarm point/door/LPR/server/label/zone/building/floor; drag & drop
placement + properties; rotate camera by mouse; floor plan upload/scale/rotate/origin/reference;
snap to grid and rotation steps; buildings with floor selector. All audited.

## Realtime protocol
Normalized events: camera.status_changed, camera.recording_changed, object.detected,
object.entered_zone/left_zone, alarm.created/updated/acknowledged, lpr.detected, face.detected,
sensor.triggered, door.opened, device.health_changed, server.status_changed, ptz.position_changed.
Position resolved from camera_id. Client keeps ~5 min buffer; backend is source of truth; dedup by
event_id; reconnect with backoff, resume via last_event_id; server-side aggregation.

## Cross-cutting
Deep links (/maps/site/:id, /maps/site/:id/camera/:id, /maps/alarm/:id, /maps/floor/:id);
Notification Center and Global Search integration ("Show on Map", plate search); Playback and
Live View integration; saved map views (zoom, center, layers, filters, zones, site); per-user
preferences; permissions maps.view, maps.edit, maps.create_zone, maps.edit_device,
alarm.acknowledge, alarm.assign, camera.ptz; metrics (FPS, entities visible, events/sec, WS lag,
time to first render, time to preview); tablet usable, mobile simplified; performance targets 100 /
500 / 1000 / 5000 cameras (GeoJSON sources, WebGL layers, batch updates — no React component per
event); separated persistent / UI / realtime state.

## Phases (PO)
- MVP: MapLibre geographic map, site hierarchy, camera placement, status, clustering, FOV, live
  preview, WebSocket events, AI pulse, alarms, layers, filters, zones, basic editor, themes.
- Phase 2: floor plans, heatmaps, timeline, historical replay, PTZ direction, LPR visualization,
  saved views.
- Phase 3: multi-camera correlation, route reconstruction, advanced analytics, blind spots, access
  control, IoT sensors, incident mode, natural-language integration.

## Deliverables requested before implementation
Architecture; React component structure; data model; API schema; WebSocket protocol; TS entities;
DB design; MapLibre strategy; clustering; realtime; scaling to thousands; video previews; map
editor; zones; floor plans; heatmaps; historical timeline; permissions; phased plan; directory
structure; tests; technical risks; key architectural decisions.
