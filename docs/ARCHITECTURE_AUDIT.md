# Live View Platform: Architecture Audit

Scope: brief rule #1 of `docs/specs/live-view-platform-brief.md`. Audit only, no code changed.
Branch `fix/playback-purity-lint`, audited 2026-09-29. Evidence is `file:line` from the working tree.
Legend: **VERIFIED** = read in code; **SUSPECTED** = inferred, needs a runtime check.
Complements `docs/UI_REFACTOR_AUDIT.md` (UI shell audit); this one covers media transport and player lifecycle.

## 1. Current state

### 1.1 Stream transport (end to end)

```
Browser <video>  --MSE fMP4-->  WebSocket  /media/v1/cameras/{id}/live?quality=sub|main   (MsePlayer.tsx:65)
   Caddy (deploy/docker/Caddyfile: /media/* -> api:8080)
     -> openvms API  media.Gateway.live (gateway.go:571-622)   authz live.view + audit LIVE_VIEWED
          -> WebSocket dial  <frigate base_url>/live/mse/api/ws?src=<go2rtc stream>   (frigate/client.go:262-283, Frigate session cookie)
               -> Frigate's embedded go2rtc (MSE producer)  -> camera RTSP
```

| Aspect | Finding | Evidence |
|---|---|---|
| Live transport | **MSE only**, over a WebSocket proxied by the central API. The browser speaks the go2rtc MSE protocol (`{"type":"mse","value":codecs}` then binary fMP4). The gateway is a dumb frame pipe. | `MsePlayer.tsx:85-87,89-145`; `gateway.go:571-622` |
| WebRTC | **Absent** (client and gateway). No `RTCPeerConnection`, no `/live/webrtc`. | rg over `apps/web/src`, `internal/media` |
| HLS live | Absent. HLS is used only for recordings (`hls.js`, native on Safari). | `HlsPlayer.tsx:29-50` |
| go2rtc access | **Never directly.** OpenVMS talks only to Frigate's HTTP API: `GET /api/go2rtc/streams` (name discovery), `/live/mse/api/ws?src=` (live), `/api/{cam}/latest.jpg` (snapshot), `/vod/...` (HLS). go2rtc's own ports (1984/8555) are never used. | `frigate/v017.go:117-130`; `gateway.go:74-80,164-186,581` |
| Stream choice | `LiveStream` (sub) or `HQStream` (main), stored in `cameras.live_stream/hq_stream` by discovery `pickStreams`. Fallback: `quality=main` uses HQ; empty sub falls to HQ; both empty gives 422 `no_stream`. | `gateway.go:558-567`; `v017.go:144-158`; `migrations/00002:111-112` |
| Auth on stream URL | Browser session cookie (same-origin WS upgrade) or bearer via `Authenticate` middleware; origin check `httpx.OriginAllowed`. **No per-stream/short-lived token.** Frigate credentials stay server-side (sealed password, cookie jar). | `router.go:53-67`; `gateway.go:550-552`; `frigate/client.go:262` |
| Authorization | `Service.Authorize` per request, cached 15 s per (user, camera, perm); WS is **not** revalidated after upgrade (unlike `/ws`). | `media/access.go:29-37,59-99`; `gateway.go:606-622` |
| Snapshot endpoint | `GET /media/v1/cameras/{id}/snapshot.jpg?h=` exists (`live.view`, proxies `latest.jpg`, `Cache-Control: private, no-store`). **Not used by any live tile.** Web only uses event/LPR snapshots. | `gateway.go:74,164-186`; rg `snapshot.jpg` in `apps/web/src` |
| Playback | `HlsPlayer` per camera over `/media/v1/cameras/{id}/vod/{start}/{end}/master.m3u8` (`recordings.view`). | `HlsPlayer.tsx:29`; `gateway.go:76,188-` |
| Synchronized playback (S2-8) | `Playback.tsx` renders up to N `HlsPlayer`, key `${camId}-${winStart}`. **No master clock, drift correction or `playbackRate` logic exists in `apps/web/src`** (rg `drift`/`playbackRate`: 0 hits). Commit `f0e3dbe` only added the multi-camera grid + selection. | `Playback.tsx:312-321`; `git show --stat f0e3dbe` |

### 1.2 Web Live View structure

| Piece | Where | Notes |
|---|---|---|
| Screen | `routes/Live.tsx` (511 lines), lazy route (`router.tsx:11-19`) | All state is component-local `useState`: `columns, rows, tiles, selected, focus, viewId...` (`Live.tsx:39-50`). No zustand/redux; server data via React Query; `AppShell` only has a sidebar-portal context. |
| Tile | `GridTile` (`Live.tsx:308-399`), renders `<MsePlayer cameraId quality>` (`:352`) | Tile = `{camera_id, quality}` or `null` (`liveGrid.ts:9-10`). Identity = **array index**. |
| Player | `components/MsePlayer.tsx` (193 lines) | Owns its own `<video>`, WebSocket, MediaSource, retry. States: `connecting|playing|error` only (`:22`). |
| Grid logic | `lib/liveGrid.ts` | Pure: `resizeTiles, placeCameraAt, reorderTiles, resolveDragEnd`, localStorage `openvms.live.selection.v1:<tenant>:<user>` (`:69-76`). |
| Layouts | `GRID_LAYOUTS` 1x1, 2x1, 2x2, 3x2, 3x3, 4x3, 4x4 (`Live.tsx:22-25`) | Brief wants 1/2/4/6/9/12/16/**25/32**: 25 and 32 missing. |
| DnD | `@dnd-kit/core` + `sortable`: one `DndContext` wraps grid + sidebar (`:245`), `PointerSensor` distance 4 + `KeyboardSensor` (`:110-113`), `SortableContext` `rectSortingStrategy` (`:266`), `useSortable({id:"tile:<index>"})` per tile (`:333`), `useDraggable("camera:<id>")` in tree (`:473`). No `DragOverlay`: the real tile (with its `<video>`) is CSS-transformed while dragging. | `liveGrid.ts:41-52` |
| "Fullscreen" | Not the Fullscreen API (rg `requestFullscreen`: 0). It is in-page **focus**: `focus` index, `shown=[focus]`, 1 column (`Live.tsx:164-165`), toggled by double click (`:341`) or Maximize button (`:367-378`). | |
| Saved views | Local last-used (localStorage) + server views `/api/v1/views`; table `views.layout jsonb {columns, cells:[{camera_id, quality}]}` (`migrations/00005:10`). `rows` is derived `ceil(cells/columns)` (`Live.tsx:127`); no positions, spans, per-cell policy. | `Live.tsx:122-162` |
| `default_live_quality` (S2-5) | `cameras.default_live_quality sub|main` (`migrations/00013:7`), used only when a camera is placed: `columns===1 ? "main" : default` (`Live.tsx:99-100`). Editable in `CameraSettingsDrawer.tsx`. | |
| Realtime | One `/ws` (`lib/realtime.ts`, mounted `Layout.tsx:18`); frames only invalidate React Query keys (`event.created, server.status, alarm.updated, notification.created`). Backoff has jitter here (`realtime.ts:32-35`). No stream/camera-health frames. | `internal/realtime/routes.go:59-63` |
| Camera status | `cameras.status` from Frigate stats fps (`inventory/health.go:120-124`), polled 15 s (`queries.ts`). Shown as a dot; **does not drive the player**. | |
| Tests | `Live.test.tsx` (DnD via keyboard, persistence, views), `MsePlayer.test.tsx` (2 tests: handshake reject, drop retry). No reconnect-count or same-session assertions. | |

### 1.3 Backend model relevant to streams

| Area | Finding | Evidence |
|---|---|---|
| Hierarchy | `tenants > sites > frigate_servers > cameras`; groups via `camera_groups`. `frigate_servers.site_id` exists. | `migrations/00002:62-123` |
| Camera columns | `remote_name, display_name, live_stream, hq_stream, status, fps, missing_since, default_live_quality, description, location, tags`. **No profiles/codec/resolution/GOP/ONVIF/capabilities columns.** Server has a `capabilities jsonb` (Frigate feature flags: review, LPR, PTZ, audio...). | `00002:100-123`; `frigate/adapter.go:62-71` |
| Edge concept | **None.** `base_url` is reached by the central API; **all media is proxied by the central API process** (2 goroutines + 2 sockets per tile). A "server" (Frigate host) is the nearest thing to an edge node. Frigate is polled over HTTP (events by pull, not MQTT: `events/syncer.go:45`). | `gateway.go:571-622` |
| RBAC catalog | `live.view, live.audio, live.talk, live.ptz` (camera scope) already declared but **unenforced** (no audio/PTZ endpoint); `recordings.view/seek`, `snapshots.*`, `views.*`, `health.view`, `servers.config`. Catalog is a Go slice `Definition{Permission, Description, Narrowest}`. | `authz/catalog.go:5-10,76-` |
| Audit | Each live WS upgrade inserts `LIVE_VIEWED` (`gateway.go:603`), so every reconnect writes an audit row. | |
| Feature flags | **No mechanism.** Config is env-only (`platform/config/config.go:64-99`); the UI has no runtime-config endpoint. | |
| Messaging | NATS JetStream streams FRIGATE/PLATFORM, ephemeral consumers feed `/ws`; Mosquitto in compose (used by the mock); `paho.mqtt.golang` already in `go.mod`. | `realtime/feed.go`; `go.mod:17` |
| Postgres | `postgres:17-alpine`, extensions `pgcrypto, pg_trgm` only (`docker-compose.yml:24`, `migrations/00001`). No pgvector. | |
| Metrics | Prometheus `/metrics` mounted (`router.go:60`), Grafana/Prometheus in compose. | |

### 1.4 React lifecycle: exactly what mounts/unmounts/reconnects

`MsePlayer` connects in one `useEffect` with deps `[cameraId, quality]` (`MsePlayer.tsx:181`); its cleanup closes the WS, clears `<video>` src and revokes the object URL (`:171-179`). **Any change of either prop, or an unmount, is a full teardown + new WebSocket + new MediaSource.** `GridTile` uses `key={i}` (`Live.tsx:274`).

| Action | What happens | Reconnects | Status |
|---|---|---|---|
| Drag tile 1 -> 8 | `reorderTiles` does an array *move*, shifting tiles 2..8 by one index (`liveGrid.ts:26-33`). Keys are indexes, so each `MsePlayer` at index 0..7 receives a **different `cameraId`** and re-runs its effect. | Every tile between `from` and `to` inclusive (8 for cell 1 to 8; 2 for adjacent cells) | VERIFIED |
| Drop camera from tree onto a tile | `place` sets that index's tile; only that index changes. Also mid-drag `rectSortingStrategy` transforms tiles visually only. | 1 (expected) | VERIFIED |
| Grid to focus (double click / Maximize) | `shown=[focus]` (`Live.tsx:164`), the `.map` renders only that tile, so every other `GridTile` **unmounts**: N-1 sockets closed. The focused tile's `quality` becomes `"main"` (`:281`), so its effect re-runs: new WS to the HQ stream. | N total (all others closed, focused one swapped to HQ) | VERIFIED |
| Focus back to grid | Others **remount** with fresh `<video>` + WS; the focused one flips main->sub: reconnect. | N | VERIFIED |
| Layout change 2x2 to 3x3 | `resizeTiles` keeps indexes, same keys, same props: existing tiles keep streaming; new cells mount empty. | 0 for kept tiles | VERIFIED |
| Layout change to 1x1 | `quality` forced to `"main"` when `columns===1` (`:281`): reconnect at HQ; cells past index 0 unmounted. Shrinking any grid unmounts truncated tiles. | 1 + truncated | VERIFIED |
| Click-place while `columns===1` | Stores `quality:"main"` in the tile (`:99-100`); after growing the grid the tile **stays main** in a 4x4 (extra HQ decoders). | n/a | VERIFIED |
| Load saved view | `setTiles` wholesale; same camera at same index keeps its player, any other index reconnects. | up to N | VERIFIED |
| Leave `/live` and return | Lazy route component unmounts; all sockets close; returning reconnects everything. No session outlives the route. | N | VERIFIED |
| Dev / StrictMode | `StrictMode` wraps the app (`main.tsx:26`): every effect runs twice in dev, so each tile opens 2 WS in development. | +N in dev only | VERIFIED |
| Sidebar portal | Camera tree is portaled into the shell (`Live.tsx:235-237`); test asserts it does not remount media (`Live.test.tsx:84`). Not a cause. | 0 | VERIFIED |
| Camera list refetch (15 s) / `/ws` invalidation | New `camById` Map but same values keys; `GridTile` props by value; no effect deps affected. Not a cause. | 0 | VERIFIED |

## 2. Problems detected (vs. brief acceptance principle)

| # | Problem | Status | Evidence | Brief requirement violated |
|---|---|---|---|---|
| P-1 | Session identity = grid index, not camera: moving a camera reconnects every tile in the range. | VERIFIED | `Live.tsx:274`, `liveGrid.ts:26-33`, `MsePlayer.tsx:181` | DnD 0 reconnects |
| P-2 | Focus unmounts all other tiles and swaps the focused one sub->main; return path repeats. There is no Fullscreen API and no shared decoder. | VERIFIED | `Live.tsx:164-165,268,281` | Fullscreen 0 reconnects |
| P-3 | `<video>`, WS and MediaSource are owned by the tile component; no `PlayerSessionManager`; sessions die with the route. | VERIFIED | `MsePlayer.tsx:41-181` | Persistent sessions |
| P-4 | State machine is 3 states; no transition log, cause, metrics; `playing` is set on every binary frame. | VERIFIED | `MsePlayer.tsx:22,143` | State machine + observability |
| P-5 | No snapshot fallback or poster: snapshot endpoint exists but is unused for live; a fresh tile is a black `<video>` with a text overlay. | VERIFIED (endpoint unused); black frames on reconnect SUSPECTED | `gateway.go:164`; `MsePlayer.tsx:186-190` | Snapshot fallback, no black frames |
| P-6 | Quality change = teardown then reconnect (effect dep `quality`): black gap, no seamless handoff, no `requestVideoFrameCallback`. | VERIFIED | `MsePlayer.tsx:181` | LOW->HIGH keeps LOW visible |
| P-7 | Reconnect: exponential `500*2^n` capped 32 s, **no jitter**, per tile, not grouped by server: a Frigate restart makes N tiles retry in lockstep. `retry` resets only after a binary frame. | VERIFIED | `MsePlayer.tsx:158-159,144` | No reconnect storms |
| P-8 | Every reconnect writes a `LIVE_VIEWED` audit row and re-runs auth + Frigate login check: a flapping edge floods `audit_log`. | VERIFIED | `gateway.go:603` | Robustness |
| P-9 | Browser cannot read the WS handshake status, so errors are guessed from `opened` (`never opened` = "no stream"); Unauthorized/Offline/Edge Offline/Codec states are indistinguishable. | VERIFIED | `MsePlayer.tsx:70-73,146-155` | Error states |
| P-10 | No visibility policy: hidden tab / off-screen tiles keep streaming; only reconnect-on-visible exists. No IntersectionObserver, no decoder budget, no cap on concurrent streams (1x1 forces `main`; 4x4 = 16 sub streams). | VERIFIED | `MsePlayer.tsx:164-170`; `Live.tsx:281` | Visibility policies, decoder budget |
| P-11 | Grids 25 and 32 not offered; layouts are fixed cols x rows with no per-cell config. | VERIFIED | `Live.tsx:22-25` | Grid sizes |
| P-12 | Live WS is not revalidated after upgrade (logout/revocation keeps the stream until it drops); no server ping/read deadline, so half-open sockets linger. | VERIFIED (no revalidation code); impact SUSPECTED | `gateway.go:606-622` vs `realtime/handler.go` | Security hardening |
| P-13 | Every byte goes through the central API process: no edge-direct path, no signed short-lived stream URL, no transport policy (MSE only). WebRTC is impossible without exposing go2rtc's UDP port or a TURN/ICE-TCP relay (Frigate exposes go2rtc WebRTC signaling but media is direct UDP to the go2rtc host: platform knowledge, not exercised in this repo). | VERIFIED (topology); WebRTC constraints SUSPECTED | `gateway.go:581-622` | Edge-direct, transport policy |
| P-14 | MSE buffering is ad hoc: keeps ~10 s, jumps to live edge if >3 s behind, drops backlog on `QuotaExceeded`; no metrics (TTFF, dropped frames, stalls). | VERIFIED | `MsePlayer.tsx:75-83,107-125` | Observability |
| P-15 | Synchronized playback lacks the S2-8 decided master clock/drift logic in code (only the grid landed). Not P0, tracked for P3 timeline. | VERIFIED | rg drift/playbackRate: 0 | n/a (S2-8 gap) |
| P-16 | `live.audio/talk/ptz` permissions and `capabilities.ptz/audio` exist but nothing consumes them (no PTZ/audio UI or endpoint). | VERIFIED | `catalog.go:8-10`; `adapter.go:69-70` | P1 modes |
| P-17 | No feature-flag mechanism, so rollout of `persistentPlayers` etc. needs new plumbing. | VERIFIED | `config.go` | Rule 5 |

## 3. Reusable components vs components to modify

| Reuse as is | Why |
|---|---|
| `lib/liveGrid.ts` (pure grid logic, storage, drag resolution) | Well tested seam; extend with camera-keyed ops, keep serialization compatible (`v1`, add optional fields). |
| `@dnd-kit` setup (sensors, `useSortable`, `useDraggable`) | Keep; only the ids/keys and what is rendered inside tiles change. |
| `media.Gateway` authorization, origin check, `Service.Authorize`, audit helpers | Extend, do not replace. |
| `frigate.Media` adapter (`Open`, `WebSocket`), `pickStreams`, `/api/go2rtc/streams` discovery | Source of stream names; becomes the "profile provider: Frigate/go2rtc". |
| `/ws` hub + `Route` registry (`realtime/routes.go`) | Add `camera.stream_status`/health frames later without new transport. |
| `views` API and `default_live_quality` | Layout persistence and initial profile hints. |
| Prometheus `/metrics`, `HlsPlayer` (recordings), MSE codec probe (`supportedCodecs`) | Metrics sink; playback unchanged; codec list reused by the MSE transport. |
| `backoffDelay` with jitter (`lib/realtime.ts:32-35`) | Lift into a shared util for player reconnect. |

| Modify | Change |
|---|---|
| `MsePlayer.tsx` | Extract transport/logic into a framework-free `PlayerSession`; component becomes a thin adapter (keeps current API; tests keep passing). |
| `Live.tsx` `GridTile` / `Live` | Stop rendering players inside tiles; tiles register a rect for the surface layer; key sessions by camera; focus becomes a rect change. |
| `liveGrid.ts` | Camera-keyed identity for DnD and reorder (positions map camera to cell), 25/32 layouts. |
| `gateway.go` `live()` | Dedupe audit, ping/deadlines, structured error frame, revalidation, session token, metrics counters. |
| `snapshot()` | Add cacheable short TTL/ETag variant for warm posters. |
| `cameras`/`frigate_servers` schema | Later: stream profiles, media mode/edge URL (P1/P2). |
| `platform/config` + new `GET /api/v1/features` (or field in `/me`) | Feature flags. |

## 4. Proposed architecture (fits this codebase)

### 4.1 Frontend (P0/P1)

```
App (Layout)                                  <- mounted once per authenticated shell, outlives routes
 └ PlayerSessionProvider  (singleton PlayerSessionManager)
     ├ Map<cameraId, PlayerSession>           session = camera-owned: WS + MediaSource + <video> + metrics + state
     ├ pool (LRU): warmTTL, idleTTL, maxWarm, maxConcurrent, maxHigh   (from policy config)
     └ StreamSelectionEngine / TransportPolicyEngine / DecoderBudgetManager (pure, injectable)
 Routes
 └ Live: Grid (SurfaceSlot per cell: only a rect + metadata)  ──registers rect──▶ VideoSurfaceLayer
 └ VideoSurfaceLayer (portal, position:fixed, pointer-events:none)
        renders each active session's persistent <video> at translate3d(rect); ResizeObserver on slots,
        IntersectionObserver for visibility policy; fullscreen = animate the same element to a larger rect.
```

| Component | Responsibility in this repo | Notes |
|---|---|---|
| `lib/live/playerState.ts` | Pure reducer: `UNINITIALIZED, CONNECTING, BUFFERING, ACTIVE, WARM, IDLE, SUSPENDED, RECONNECTING, ERROR, EVICTED`; each transition `{from,to,cause,ts,metrics}` into a ring buffer. | Unit-testable without DOM. |
| `lib/live/PlayerSession.ts` | Wraps today's MSE logic (`MsePlayer.tsx:45-181`) behind a `Transport` interface (`MseTransport` now, `WebRtcTransport` later); owns `<video>`; exposes `subscribe()`; metrics (`ttff, reconnectCount, bufferingCount, stalls, droppedFrames` from `getVideoPlaybackQuality()`); reconnect with jitter. | Same URL contract, so backend unchanged for P0. |
| `PlayerSessionManager` | `acquire(cameraId, {profile, transport, priority})` / `release` with refcount; keeps sessions across layout/DnD/focus; TTL eviction; snapshot cache; exposes global counts. | React context + `useSyncExternalStore`; no new state library (repo has none). |
| `VideoSurfaceLayer` | Persistent `<video>` host outside the grid; positions by rect. The grid never contains a `<video>`. | Because `<video>` moves by CSS only, no DOM reparenting, so no MSE reset. |
| Snapshot layer | Poster `<img>` under the video; sources: last captured frame (`canvas`/`ImageBitmap` from the video, in memory) then `GET /media/v1/cameras/{id}/snapshot.jpg`. | Must be dropped on logout (RBAC). |
| `StreamSelectionEngine` (P1) | Pure `select(ctx) -> {profile, transport, reason}` where ctx = tile size, DPR, grid size, viewport, mode, priority, metrics; policy is data (JSON), with hysteresis `upgradeDebounceMs/downgradeDelayMs`. | Profiles: today only `sub`/`main`, later per-camera `stream_profiles`. |
| Seamless swap (P1) | Second session/`MediaSource` for target profile, hidden; swap on first frame via `requestVideoFrameCallback`, then close old. | Requires two logical sessions per camera during the handoff. |
| `TransportPolicyEngine` (P1) | Chooses MSE/WebRTC; falls back both ways. | WebRTC gated on P-13 topology answer. |

### 4.2 Backend / media plane

| Topic | Proposal |
|---|---|
| Where is the media gateway? | Keep **go2rtc inside each Frigate** (already there, no new service, matches "do not build a WebRTC server / Frigate replacement"). Central `media.Gateway` remains the default path for existing installs. |
| Session token (fits current proxy) | New `POST /media/v1/cameras/{id}/sessions {profile, transport}`: runs `Service.Authorize(live.view)` (`access.go:59`), returns `{url, transport, expires_at, token}`. Token = short-lived (30-60 s, single-use `jti`) HMAC/JWT bound to camera, stream, quality, user. In central mode the URL is today's `/media/v1/cameras/{id}/live?...&t=<token>`; `live()` accepts the token instead of re-authorizing per reconnect (reconnects under one session do not re-audit; audit once per session). |
| Edge-direct | Add `frigate_servers.media_mode (central\|edge)` and `public_media_url`. For `edge`, the session response carries the edge URL; the edge verifies the token with the shared/public key (small Go `edge-gateway` sidecar or reverse-proxy JWT check in front of Frigate's `/live/mse`), holds Frigate creds locally, and central never exposes RTSP/ONVIF creds or permanent tokens. **Needs infrastructure that does not exist today**; see section 5. |
| Structured live errors | After WS upgrade, send `{type:"error", code:"unauthorized|no_stream|camera_offline|edge_offline|codec"}` before closing, or a pre-flight `GET .../live/status`, so the client leaves the `opened` heuristic (`MsePlayer.tsx:70-73`). |
| Hardening | Ping/pong + read deadline, revalidate session periodically like `realtime/handler.go`, cap concurrent live sockets per user (config). |
| Metrics | Prometheus: `openvms_live_sessions_opened_total{reason}`, `openvms_live_sessions_active{quality}`, upgrade duration histogram. Client beacon optional (P1) for TTFF/reconnect diagnostics. |
| Schema (P1+) | `camera_stream_profiles(camera_id, profile_id, name, source, resolution, fps, codec, bitrate, gop, audio, rtsp_uri_sealed, snapshot_uri, declared jsonb, verified jsonb)`; `cameras.capabilities jsonb`; `frigate_servers.media_mode/public_media_url`. Reuse `secrets.Sealer` for URIs. Migrations continue at `00019`. |
| Feature flags | New `Features` block in `platform/config` (`FEATURE_ADAPTIVE_STREAMING`, `FEATURE_PERSISTENT_PLAYERS`, `FEATURE_STREAM_PREWARMING`, `FEATURE_SEAMLESS_QUALITY_SWITCH`, `FEATURE_AI_SEARCH`, `FEATURE_AI_SUMMARIES`, `FEATURE_UNIFIED_TIMELINE`, `FEATURE_SENSOR_API`), exposed via `GET /api/v1/features` (OpenAPI + `make generate`), read once in the web app into a context; localStorage override for dev. Default off; legacy path stays until the flag is removed. |
| RBAC | Reuse `live.view/audio/talk/ptz`; add `playback.view` only if the existing `recordings.view` naming is not accepted (open question, default: keep existing names, map brief's `playback.view` to `recordings.view`). New: `camera.configure`, `camera.optimize`, `system.camera_health` (P2), `timeline.view`, `sensors.view`, `search.ai` (P3/P4), each with `Narrowest` scope per `catalog.go` convention. |

## 5. Feasibility and gaps per priority

| Priority | Buildable now | Needs new infrastructure / decision |
|---|---|---|
| **P0** Persistent sessions, DnD/fullscreen 0 reconnect, surface layer, snapshot fallback, state machine | **Yes, fully frontend + small gateway hardening.** Zero change to the media URL contract. Snapshot endpoint exists. go2rtc "gateway" already = Frigate's go2rtc. | Flag endpoint (small). Decision: fullscreen keeps sub stream in P0 (no reconnect) until P1 seamless swap. |
| **P1** Adaptive selection, seamless LOW/HIGH, prewarm, pool, decoder budget, transport policy | Selection/pool/budget/seamless swap: yes with `sub`/`main` profiles (2 profiles per camera is what discovery provides). Prewarm: yes. | **WebRTC**: not reachable via central proxy (UDP media); needs edge-direct or TURN/ICE-TCP or go2rtc port exposure. PTZ/audio/talk transport rules need PTZ/audio endpoints that do not exist (permissions unused). Multi-profile (LOW/MEDIUM/HIGH) needs `camera_stream_profiles` and ONVIF/manual source. |
| **P2** ONVIF discovery, validation, health | Design + schema now. Go library options: `github.com/use-go/onvif` (SOAP client, WS-Discovery), `IceWhaleTech/onvif`, or a thin in-house SOAP client for Device/Media/Media2/PTZ (fewer deps; recommended if only GetProfiles/GetStreamUri/GetSnapshotUri/GetServiceCapabilities are needed). WS-Discovery needs UDP multicast from the API host, which fails inside a bridged Docker network (needs `network_mode: host` or an edge probe). | Discovery must run **near the cameras**, i.e. on the edge/site, not in the central container. Camera credentials handling (sealed). Vendor adapters: implement `GenericRTSP` + `GenericONVIF` only. Camera write-back ("Optimize Camera") out of v1. |
| **P3** Event bus, MQTT/webhook, unified timeline | NATS JetStream + `/ws` route registry + `paho.mqtt.golang` already in repo; webhook = new authenticated endpoint. Timeline on `events`, `alarms`, `recordings` with backend buckets. | Versioned event schema/taxonomy design, dedupe rules (ONVIF vs Frigate), `EventCorrelationEngine` semantics. |
| **P4** AI summaries + NL/semantic search | FTS/`pg_trgm` already used (`00015`). Frigate has semantic search capability flag (`Capabilities.SemanticSearch`). | **pgvector is not in `postgres:17-alpine`**: needs image change (e.g. `pgvector/pgvector:pg17`) + `CREATE EXTENSION vector` migration + data volume compatibility check. **VLM provider not chosen** (cloud vs local, cost, residency). |

### Open questions for the product owner (most important first)

1. **Edge reachability**: can browsers reach Frigate hosts directly (LAN/VPN/public), and is a small edge verifier sidecar acceptable per site? This decides edge-direct scope and whether WebRTC is realistic; default proposal: P0/P1 stay on the central proxy, edge-direct as a later flag.
2. **Fullscreen in P0**: accept staying on the current (sub) stream when expanding until P1 seamless HQ swap lands (0 reconnects, lower resolution), or must expansion show HQ from the start?
3. **Session lifetime**: should sessions survive navigation between routes (Live -> Events -> Live) as WARM, and what default TTLs/limits (`warmSessionTTL`, `maxConcurrentPlayers`, `maxHighQualityStreams`) fit your typical hardware (and mobile)?
4. **Grids 25/32** and low-end behavior: OK to auto-degrade or pause tiles (SMART/ECO) by default in large grids?
5. **Snapshot fallback source and privacy**: OK to keep the last decoded frame in memory only (cleared on logout), fetching `snapshot.jpg` for cold tiles, with no on-disk/IndexedDB cache?
6. **P4 AI**: VLM provider (cloud vs local) and approval to switch the Postgres image to one with pgvector; also confirm mapping the brief's `playback.view` onto the existing `recordings.view`.

## 6. Migration strategy

Principles: legacy path stays behind `persistentPlayers=false`; each work unit (WU) is a commit-sized slice with tests, about 400 authored lines or less (advisory), Strict TDD (RED first). Web tests: Vitest + Testing Library with a fake `WebSocket` counting constructions (pattern already in `MsePlayer.test.tsx`).

### 6.1 Measuring reconnects and TTFF

| Metric | How |
|---|---|
| Reconnects (tests) | Fake `WebSocket` records `new WebSocket(url)` per camera+quality; assertion: count unchanged after action. Also assert the same `PlayerSession` object id and same `<video>` element (`toBe`). |
| Reconnects (runtime) | `PlayerSession.metrics.reconnectCount` (client), plus server counter `openvms_live_sessions_opened_total{camera}` incremented in `live()` after `Upgrade` (`gateway.go:598`). A DnD/fullscreen action must leave both unchanged. |
| TTFF / time to live | `t0` = `session.connect()`, `t1` = first `requestVideoFrameCallback` (fallback `loadeddata`); stored per session, shown in a debug overlay (`?liveDebug=1`) and the P1 diagnostics page. Snapshot time = `img.decode()` end minus mount time. |
| Black frames | Test: the poster/last frame `<img>` remains in the DOM until the new session's first frame event; runtime: count `waiting`/`emptied` events on the visible `<video>`. |
| Baseline first | WU-1 adds counters to the **existing** `MsePlayer` so the before/after numbers are recorded (expected baseline: 8 reconnects for cell1->cell8 with 8 tiles, N for focus). |

### 6.2 P0 work units (ordered)

| WU | Objective | Files likely touched | Tests | ~Lines |
|---|---|---|---|---|
| **P0-1** Flags + baseline metrics | `Features` config + `GET /api/v1/features` (OpenAPI, generate), web `useFeatures`; instrument current `MsePlayer` with reconnect/TTFF counters and failing baseline tests that document P-1/P-2. | `platform/config/config.go`, `packages/api-contract/openapi.yaml`, `internal/api` handler, `apps/web/src/api/queries.ts`, `lib/features.ts`, `MsePlayer.tsx`, `Live.test.tsx` | Go: config parsing + handler; web: features hook; baseline test proving today's reconnect counts (marked as characterization). | 300 |
| **P0-2** State machine + `PlayerSession` core | Pure reducer + transition log; extract MSE logic from `MsePlayer` into `PlayerSession` with `MseTransport`, jittered backoff shared with `realtime.ts`; `MsePlayer` becomes an adapter (same behavior). | `lib/live/playerState.ts`, `lib/live/PlayerSession.ts`, `lib/live/backoff.ts`, `MsePlayer.tsx` | Reducer table tests; session tests with fake WS/MediaSource (connect, drop, backoff, error); existing `MsePlayer.test.tsx` stays green. | 400 |
| **P0-3** `PlayerSessionManager` + provider | Camera-keyed acquire/release with refcount, WARM/IDLE TTL, LRU, mounted once in `Layout`; session owns its `<video>`. Behind `persistentPlayers`. | `lib/live/PlayerSessionManager.ts`, `PlayerSessionProvider.tsx`, `Layout.tsx` | Same camera acquired twice yields the same session; release then re-acquire within TTL reuses; TTL eviction; unmount of route keeps WARM. | 300 |
| **P0-4** `VideoSurfaceLayer` + `SurfaceSlot` | Persistent video host positioned over cell rects (ResizeObserver, translate3d); `GridTile` renders a slot instead of `MsePlayer` when the flag is on; legacy path untouched otherwise. | `components/VideoSurfaceLayer.tsx`, `Live.tsx` (GridTile), `index.css` | Layer positions video at slot rect and follows resize (mock RO); flag off renders `MsePlayer` as before. | 380 |
| **P0-5** DnD, layout and focus without reconnect | Camera-keyed tile identity (session by camera; DnD only changes slot rect), focus/expand = same session to a larger rect, no sub->main swap in P0, remove unmount of other tiles, fix the stored `main` on 1x1 place, add 25/32 layouts. | `Live.tsx`, `lib/liveGrid.ts`, `VideoSurfaceLayer.tsx` | **Mandatory:** cell 1->8 keeps same session, 0 new WS; grid->focus same session, 0 new WS; grid resize keeps kept tiles' sessions; liveGrid unit tests for keyed ops. | 350 |
| **P0-6** Snapshot fallback + poster | Poster layer (last frame kept in memory, else `snapshot.jpg`); states CONNECTING/RECONNECTING/ERROR keep it visible; spinner only when nothing visual; offline UI + Retry; gateway `snapshot` short private cache/ETag. | `PlayerSession.ts`, `VideoSurfaceLayer.tsx`, `lib/live/snapshotCache.ts`, `gateway.go` `snapshot()` | **Mandatory:** offline shows last snapshot + offline indication + automatic reconnect (fake timers); cache cleared on logout; Go: snapshot cache headers. | 330 |
| **P0-7** Gateway hardening + structured errors | Audit once per session (not per reconnect), ping/read deadline, periodic revalidation, `{type:"error", code}` frame, Prometheus live counters; client maps codes to Unauthorized/Offline/No stream/Codec. | `internal/media/gateway.go`, `media/metrics.go`, `PlayerSession.ts`, `MsePlayer.tsx` | Go: audit dedupe, deadline/close on revoked session, error frame; web: code-to-state mapping. Reconnect-storm test: N sessions to one server get jittered delays and grouped backoff. | 380 |
| **P0-8** Rollout | Default `persistentPlayers` on in dev, docs (`docs/live-view-architecture.md`), changelog, remove dead code paths after a soak period. | docs, `.env.example` | Full web + Go suites; manual: run app and observe zero reconnects. | 150 |

Notes: P0-3..5 are the risk core; land P0-2 first so the legacy component stays the safety net. Per ODD, each WU ends with a Conventional Commit and native review assessment against the last reviewed boundary (`src` files under `internal/media` and auth are likely `high_risk`).

### 6.3 P1 to P4 outline

| Phase | Slices (coarse) |
|---|---|
| **P1** | (1) `StreamSelectionEngine` + policy config + hysteresis; (2) seamless LOW->HIGH swap with `requestVideoFrameCallback` (flag `seamlessQualitySwitch`); (3) prewarm triggers (hover, focus, selection, drag start, alarm); (4) pool limits + `DecoderBudgetManager` + IntersectionObserver/Page Visibility, CONTINUOUS/SMART/ECO per layout; (5) `TransportPolicyEngine` + WebRTC transport (only after open question 1); (6) Live View Diagnostics admin page + session metrics beacon; (7) session-token endpoint + edge-direct verifier. |
| **P2** | Schema `camera_stream_profiles`/capabilities; `CameraAdapter` interface with `GenericRTSP`/`GenericONVIF`; discovery wizard (site-side probe); `CameraHealthService` reusing `inventory/health.go`; GOP/FPS warnings; RBAC additions. |
| **P3** | Event schema v1 + taxonomy on NATS; MQTT and `POST /api/v1/events` ingestion (auth, rate limit, idempotency, audit); ONVIF-Frigate dedupe; correlation engine; unified timeline API with buckets (histogram to individual) + canvas timeline; master-clock/drift for synchronized playback (S2-8 gap). |
| **P4** | pgvector image + migration; frame extraction + VLM adapter (provider TBD); embeddings + hybrid query planner (SQL filters, FTS, semantic, optional rerank); explainable results; structured summaries; flags `aiSearch`, `aiSummaries`. |

## 7. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Moving `<video>` out of the grid breaks a11y/focus/drag hit-testing | Slot keeps focusable role, labels and dnd handlers; layer is `pointer-events:none` except controls; a11y tests exist in `Live.test.tsx` and stay green. |
| MSE `SourceBuffer` state after long sessions | Keep existing trim logic; add stall/quota metrics; session recycle on repeated decode errors. |
| Central proxy CPU/socket load from persistent sessions | Server-side cap and metrics (P0-7); prefer fewer, warmer sessions; edge-direct later. |
| Regression of existing installs | Flags default off, `MsePlayer` adapter kept, media URL contract unchanged in P0. |
| Session data leakage across users | Snapshot/frames in memory only; manager cleared on logout and on tenant/user change. |
