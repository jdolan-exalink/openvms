# Live View Platform — Product Brief

Source: product owner master prompt (2026-09-29), condensed. This brief is the
requirements reference for the Live View / Media Session Platform stage.

## Goal

Evolve OpenVMS from "a frontend that shows streams" into a media session platform
whose Live View feels immediate, stable and professional (reference UX: Ava Aware,
Avigilon, Milestone, Scrypted NVR), while staying open, multi-site, edge-first,
vendor-neutral and backward compatible.

Core acceptance principle: **UI changes must not change the media transport unless
strictly necessary.** Moving a camera, changing layout (camera stays visible),
entering/leaving fullscreen: zero reconnects. Changing quality: the old stream stays
visible until the new one is rendering (zero black frames).

## Working rules

1. Audit first; write `docs/ARCHITECTURE_AUDIT.md` (current state, problems,
   reusable parts, parts to change, proposed architecture, migration strategy).
2. Reuse/extend existing code; add abstractions only for concrete problems.
3. Keep existing installations working; degrade gracefully (single-stream cameras,
   no ONVIF → generic RTSP, WebRTC failure → alternative transport).
4. No hardcoded IPs, URLs, server names, cameras, codecs, vendors or stream counts;
   everything configurable, multi-server and multi-site ready.
5. Incremental migration, feature flags for gradual rollout
   (adaptiveStreaming, persistentPlayers, streamPrewarming, seamlessQualitySwitch,
   aiSearch, aiSummaries, unifiedTimeline, sensorApi).
6. Do NOT build: own encoder, RTSP server, WebRTC server, separate vector DB (use
   pgvector if needed), all vendors, native mobile apps, new face recognition,
   a Frigate replacement.

## Architecture target

- Control plane (central): users, permissions, sites, servers, cameras, config,
  layouts, events, search, metadata, audit, alerts, federation.
- Data/media plane (edge): RTSP ingest, go2rtc, Frigate, streaming, recordings,
  snapshots, detection. Edge nodes keep recording/detecting/serving local video
  when the central server is unreachable.
- Media gateway: browsers never connect to cameras directly; go2rtc fans out.
- Edge-direct streaming: central authorizes, video takes the shortest path
  (browser → edge go2rtc) using short-lived signed tokens/URLs, never a bare
  secret URL. Never expose RTSP/ONVIF credentials or permanent edge tokens.

## Priorities and phases

- **P0**: Persistent Player Sessions; drag & drop without reconnect; fullscreen
  without reconnect; Video Surface Layer; go2rtc/media gateway; snapshot fallback;
  player state machine.
- **P1**: Adaptive stream selection; seamless LOW/MEDIUM/HIGH switching;
  prewarming; player pool; decoder budget; transport policy engine.
- **P2**: ONVIF capability discovery; camera validation; camera health.
- **P3**: Unified event bus; unified timeline; MQTT/webhook ingestion.
- **P4**: AI event summaries; natural-language video search; semantic search.

Suggested phase order: 1 persistent session → 2 DnD/fullscreen without reconnect →
3 player pool + WARM → 4 LOW/MEDIUM/HIGH selection → 5 seamless handoff →
6 transport policy → 7 camera discovery → 8 event bus → 9 unified timeline →
10 AI enrichment/search.

## Live View requirements (P0/P1)

- Decouple `Camera → PlayerSession → PlayerSurface → grid position`; the session
  belongs to the camera, not to the cell.
- `PlayerSessionManager`: create/reuse sessions per camera, keep them across layout
  changes, own state, stream + transport selection, metrics, prewarm, suspend,
  close idle, error handling, snapshot fallback.
- `VideoSurfaceLayer`: persistent `<video>` elements outside the grid, positioned
  over cell rectangles (ResizeObserver, IntersectionObserver, translate3d/GPU
  compositing). Fullscreen = same session/video/decoder animated to a larger rect.
- Player state machine: UNINITIALIZED, CONNECTING, BUFFERING, ACTIVE, WARM, IDLE,
  SUSPENDED, RECONNECTING, ERROR, EVICTED; each transition records cause,
  timestamp, metrics, debug log.
- Player pool (LRU): configurable warmSessionTTL, idleSessionTTL, maxWarmPlayers,
  maxConcurrentPlayers, maxHighQualityStreams.
- `StreamSelectionEngine`: inputs = capabilities, tile size, DPR, grid size,
  viewport, network/decoder metrics, mode (fullscreen, digital zoom, PTZ, audio),
  priority/alarm/selection/visibility; output = profile, transport, reason.
  Configurable policy (not a hardcoded table), hysteresis (upgradeDebounceMs,
  downgradeDelayMs). Profiles come from ONVIF/manual/Frigate/go2rtc/vendor plugin.
- Seamless switching: target stream starts hidden, swap after first decodable frame
  (`requestVideoFrameCallback` or equivalent), then close the old one.
- Prewarming on hover, focus, selection, drag start, alarm, context menu, expand.
- Snapshot → LOW live → higher quality; keep last frame/snapshot while connecting,
  reconnecting or switching; spinner only when nothing visual exists.
- `TransportPolicyEngine`: MSE for grid/normal single view; WebRTC for PTZ,
  two-way audio, low-latency mode; prefer MSE on unstable networks; automatic
  fallback both ways; configurable.
- `DecoderBudgetManager`: degrade by priority (selected > alarms > PTZ > visible >
  background) when decoder/CPU/GPU/memory load is excessive.
- Visibility policies per layout: CONTINUOUS, SMART, ECO; IntersectionObserver +
  Page Visibility API.
- Grids 1/2/4/6/9/12/16/25/32; never open 32 main streams needlessly.
- Preserve selected camera, volume, mute, zoom, PTZ and stream state across layout
  changes. Digital zoom requests HIGH while LOW stays visible. PTZ requests low
  latency (HIGH + WebRTC when suitable) and returns gradually. Audio decoupled from
  video; two-way audio may force a compatible transport.
- Error states: Connecting, Reconnecting, Camera Offline, Unauthorized, Codec
  Unsupported, Stream Error, Edge Offline; keep last snapshot; Retry button;
  admin diagnostics. Reconnect with exponential backoff + jitter, no reconnect
  storms, group failures of the same edge.
- Cache: last snapshot/frame, metadata, capabilities, health (with TTLs).
- Observability per session: timeToFirstFrame, timeToLive, reconnectCount,
  bufferingCount, currentProfile, currentTransport, bitrate, fps, droppedFrames,
  decodeErrors, packetLoss, jitter, bufferDepth, playerState; global counts by
  state/transport/quality. Admin "Live View Diagnostics" page.
- Targets (LAN): cached snapshot < 200 ms; LOW live < 1 s when GOP allows; warm
  selection feels instant; DnD and grid→fullscreen 0 reconnects; LOW→HIGH 0
  visible black frames.
- Mandatory UI tests: camera moved cell 1 → 8 keeps same session, 0 reconnects;
  grid → fullscreen same session, 0 reconnects; LOW → HIGH keeps LOW visible,
  preloads HIGH, swaps after first frame; camera offline shows last snapshot,
  offline indication, automatic reconnect.

## Camera discovery and health (P2)

- Wizard: IP/host + username + password → WS-Discovery + ONVIF probing (Device,
  Media/Media2, Events, PTZ, Imaging, analytics). Store manufacturer, model,
  firmware, serial, hardwareId, profiles, services; per stream profileId, name,
  resolution, fps, codec, bitrate, GOP, audio, RTSP URI, snapshot URI; PTZ
  capabilities/presets; audio in/out, two-way audio, motion, tamper, I/O, IR, WDR,
  focus, exposure, analytics, metadata, edge storage.
- Store declared vs verified capability; verify with non-destructive tests.
- `CameraHealthService`: connectivity, RTSP, main/sub streams, snapshot, codec,
  resolution, fps, GOP, timestamps, audio, ONVIF events, PTZ, packet loss,
  interruptions. GOP validation warns (e.g. FPS 20, GOP 100 → slow startup);
  optional "Optimize Camera" only via safe vendor API with confirmation, diff,
  rollback and audit.
- `CameraAdapter` interface (GenericONVIF, Hikvision, Dahua, Reolink, UniFi,
  GenericRTSP) — design the interface, implement only what is needed now.

## Events, timeline, search (P3/P4)

- Unified internal event bus with a versioned schema (schemaVersion, id,
  timestamp, siteId, sourceId, sourceType, type, severity, cameraIds, attributes)
  and namespaced taxonomy (video.*, lpr.*, face.*, access.*, sensor.*,
  analytics.*, system.*). Sources: Frigate, ONVIF events (deduplicated against
  Frigate), generic MQTT with topic + JSONPath mapping, `POST /api/v1/events`
  webhook with auth, rate limiting, schema validation, idempotency, audit.
- `EventCorrelationEngine`: merge related detections into incidents (camera,
  proximity, tracks, temporal gap, zone, relationships).
- Unified timeline: video, motion, detections, alerts, LPR, face, access, sensors,
  analytics, PTZ, bookmarks, annotations, system events; level of detail
  (histogram → clusters → groups → individual → tracks) with backend buckets and
  virtualization/canvas, never thousands of DOM nodes.
- AI: representative frames (start/best/end) → VLM → structured JSON → embedding
  (pgvector) → index. Query planner extracts time, site, camera, zone, object,
  attributes, plate, identity, action, relation, direction, speed. Hybrid search:
  hard SQL filters → FTS/semantic retrieval → optional rerank; explainable results
  without fake percentages. Structured AI summaries (data, title, summary,
  confidence, model version).

## Cross-cutting

- RBAC integration for new capabilities (live.view, playback.view, search.ai,
  camera.ptz, camera.audio, camera.talk, camera.configure, camera.optimize,
  timeline.view, sensors.view, system.camera_health …) at site/server/camera-group/
  camera scope, following the existing permission catalog conventions.
- Mobile-ready policies (lower bitrate, fewer decoders, SMART mode).
- Docs to produce: docs/live-view-architecture.md, adaptive-streaming.md,
  camera-discovery.md, event-bus.md, unified-timeline.md, ai-video-search.md;
  changelog of files, modules, endpoints, migrations, env vars, dependencies,
  feature flags; README only links.
