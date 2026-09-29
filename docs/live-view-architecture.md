# Live View Architecture (P0)

Status: P0 implemented behind feature flags (2026-09-29). Requirements:
`docs/specs/live-view-platform-brief.md`. Pre-change audit: `docs/ARCHITECTURE_AUDIT.md`.
Roadmap/handoff: `docs/roadmap/live-view-platform.md`.

## Principle

UI changes must not change the media transport. Moving a camera, changing layout,
expanding a tile or navigating away and back (within the WARM TTL) reuse the same
session: same WebSocket, same MediaSource, same `<video>`.

## Media path (unchanged in P0)

```text
<video> (MSE) ─WS─► API /media/v1/cameras/{id}/live?quality=sub|main
                    (internal/media/gateway.go)
                     ─WS─► Frigate /live/mse/api/ws?src=… ─► Frigate's go2rtc ─► camera
```

MSE only; no WebRTC or edge-direct streaming yet (P1, see roadmap).

## Web building blocks (`apps/web/src/lib/live/`)

| Module | Responsibility |
|---|---|
| `playerState.ts` | State machine: UNINITIALIZED, CONNECTING, BUFFERING, ACTIVE, WARM, IDLE, SUSPENDED, RECONNECTING, ERROR, EVICTED. Allowed-transition table; each transition records cause + timestamp and feeds metrics. Debug: `localStorage["openvms.live.debug"]="1"`. |
| `PlayerSession.ts` | Owns one imperatively created `<video>` and the MSE pipeline; `attach`/`detach`/`close`; suspend on hidden tab; last-frame capture; reconnect with jittered exponential backoff (cap 32 s). |
| `PlayerSessionManager.ts` + `PlayerSessionProvider.tsx` | Sessions keyed by camera + quality, refcounted `acquire`/`release`; released sessions go WARM for 30 s then are evicted; `maxWarmPlayers` (8, LRU) and soft `maxConcurrentPlayers` (32); `clear()` on logout/user change. Mounted in `Layout`. Hook: `usePlayerSession(cameraId, quality)`. |
| `surfaceLayer.ts` + `SurfaceLayer.tsx` | `VideoSurfaceLayer`: fixed overlay (`z-[1]`, `pointer-events: none`) hosting session videos; grid cells render a `SurfaceSlot` whose rect (ResizeObserver + scroll/resize) positions the video with `translate3d`. Tile controls sit at `z-[2]`/`z-[3]`. |
| `streamErrors.ts` | Maps gateway error frames to UI states (below). |
| `serverBackoff.ts` | Groups reconnects per Frigate server: one shared timer and one probe session, others released staggered after the probe gets media (no reconnect storms). |
| `playerMetrics.ts` | Per camera/quality: connect attempts, reconnect count, TTFF (`requestVideoFrameCallback`, fallback `loadeddata`), state log. Dev: `window.__openvmsPlayerMetrics`. |

Grid (`routes/Live.tsx`, `lib/liveGrid.ts`) with `persistentPlayers` on:
tiles keyed by camera id; drag & drop **swaps** cells (`swapTiles`); expand keeps every
tile mounted and hides the others (their sessions stay alive); tiles outside the
viewport are SUSPENDED showing their last frame (IntersectionObserver); grids 5×5 (25)
and 8×4 (32) are offered. A camera is shown in at most one tile (`placeCameraUnique`);
a duplicate coming from a saved view renders its snapshot with "Ya visible en otra celda".
With the flag on, a tile keeps its own quality in 1×1 as well (expand keeps sub in P0).

## Never black

Poster order: last in-memory frame (canvas → JPEG blob URL, max 640 px, captured on
suspend/close/error and every 20 s while active) → cold `snapshot.jpg?h=360`
(`Cache-Control: private, max-age=5`) → spinner only when no image exists. Frames are
memory-only and revoked on close/logout.

## Errors (gateway → UI)

Gateway sends `{"type":"error","code","message","value"}` before closing (1008 for
auth, 1011 otherwise). A 1008 close without a frame is treated as `unauthorized`.

| code | UI | Retry |
|---|---|---|
| `camera_offline` | Cámara sin conexión | yes (backoff) |
| `upstream_unreachable` | Servidor no disponible | yes (grouped per server) |
| `server_error` | Error de stream | yes |
| `unauthorized` | Sesión expirada | no — "Reintentar" button |
| `forbidden` | Sin permiso | no — "Reintentar" button |
| `codec_unsupported` | Códec no soportado | no — "Reintentar" button |

## Gateway (`internal/media`)

One `LIVE_VIEWED` audit row per user+camera per sliding window; ping/pong keepalive and
deadlines on both legs; bounded messages (browser 64 KB, Frigate 8 MB, 10 s write
deadline); periodic revalidation of session and camera permission.

## Configuration

| Setting | Where | Default |
|---|---|---|
| `OPENVMS_FEATURES` | API env, comma-separated, case-insensitive: `persistentPlayers`, `videoSurfaceLayer`, reserved `adaptiveStreaming`, `streamPrewarming`, `seamlessQualitySwitch` | all off in code; local compose enables `persistentPlayers,videoSurfaceLayer` |
| `LIVE_AUDIT_WINDOW` | API env | 5m |
| `LIVE_REVALIDATE_INTERVAL` | API env | 30s |
| `LIVE_PING_INTERVAL` / `LIVE_PONG_WAIT` | API env | 20s / 60s |
| Dev flag override | browser `localStorage["openvms.features.override"]` | — |

Flags are served at authenticated `GET /api/v1/features`; the web `useFeatures()` hook
falls back to all-off on error.

## Metrics (Prometheus)

`openvms_live_sessions_active{quality}`, `openvms_live_sessions_opened_total{quality}`,
`openvms_live_reconnects_total`, `openvms_live_errors_total{code}`,
`openvms_live_proxied_bytes_total{direction}`.

## Troubleshooting

- Video misplaced/clipped over the grid: check scroll containers and stacking contexts
  (a dragged tile is its own stacking context); the layer tracks slot rects via
  ResizeObserver + scroll/resize listeners.
- Unexpected reconnects: enable `openvms.live.debug` and read
  `window.__openvmsPlayerMetrics` (connect attempts, transition causes).
- Tile stuck in "Sesión expirada"/"Sin permiso": expected, no auto-retry; log in again
  or fix the grant, then "Reintentar".
- Roll back: unset the flags (`OPENVMS_FEATURES=`) — the grid returns to the pre-P0 path.

## Known limitations

Not yet browser-verified: overlay geometry/z-order during drag, IntersectionObserver
suspend/resume on 25/32 grids, hidden-tab suspend, canvas capture from MSE video, 1008
close delivery. Expanded view hides other tiles, so their sessions stay alive while Live
is open. No WebSocket end-to-end test with a fake Frigate; audit dedupe is per API process.

## Changelog (P0)

- Endpoint: `GET /api/v1/features`.
- Env vars: `OPENVMS_FEATURES`, `LIVE_AUDIT_WINDOW`, `LIVE_REVALIDATE_INTERVAL`,
  `LIVE_PING_INTERVAL`, `LIVE_PONG_WAIT`.
- Migrations: none. Dependencies: none added.
- Commits: LV-1 `a0f4a62`, LV-2 `b3d0624`, LV-3 `4e84007`, LV-7 `813f791` (merge `3a80d80`),
  LV-6 `7d91c84`, LV-4 `1f632e0`, LV-5 `656b5d9`.
