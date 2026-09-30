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

## LV-9: LIVE / REC toggle

A segmented **En vivo / Grabación** toggle in the Live toolbar (hidden without any
`recordings.view` grant). REC keeps the grid (cameras, positions, layout) and covers each tile
with recorded HLS playback synchronized to one clock; a docked day timeline and transport sit
below the grid.

- **Live is suspended, not closed.** With `persistentPlayers`, each tile's session gets the
  suspend reason `rec` (socket closed, last frame kept) and reconnects when switching back.
  Without the flag the live player is unmounted while in REC (it remounts on return).
- **Shared clock.** `useRecPlayback` opens one hour-long VOD window (`vodWindowForInstant`) for
  every tile; seeks inside it only move `currentTime`, outside it a new window is opened.
  The master tile (selected tile if it has recordings, else the first that has) is the clock;
  `useSyncedPlayback` (S2-8 drift correction) follows it for all N tiles.
- **Cap.** At most `REC_MAX_PLAYERS` (16, `lib/liveRec.ts`) recorded players; extra tiles show
  the camera snapshot and "Reproducción limitada a 16 cámaras". Tiles without permission
  (recordings API 403) show "Sin permiso de grabaciones"; cameras with no recording that day
  show "Sin grabaciones este día".
- **Timeline** (`DayTimeline`, canvas): union coverage, per-camera rows up to 8 cameras, event
  markers clustered by 7 px bucket (`clusterMarkers`), playhead. Wheel zooms around the cursor
  (24 h to 2 min), ctrl+wheel/pinch too, drag pans, click seeks all cameras and plays,
  shift+drag or dragging the playhead scrubs. Math lives in `lib/timeScale.ts`.
- **Controls:** play/pause, 0.5x-8x, +/-10 s and +/-1 min, previous/next event, "Ahora",
  calendar (`DayCalendar`), keyboard: Space, Left/Right (10 s), Shift+Left/Right (1 min).
- **URL:** `?mode=rec&t=<ISO>`; kept in step with the clock (every 15 s while playing, shortly
  after seeks); switching to LIVE clears both. Entering defaults to now - 30 s, or to the
  latest recording if the cameras stopped earlier.
- **Known limitations:** not browser-verified; the calendar does not mark days with
  recordings (no cheap API); all tiles assume the same VOD offset, so a camera whose
  recording starts mid-window is corrected only by the drift loop; the window is clamped to
  "now", so playback at the live edge re-opens a window about every minute; REC "snapshot"
  for capped tiles is the latest one, not the one at the shared time.

## LV-13: Explorador sidebar and shared camera folders

The Live sidebar is one **Explorador** (`components/LiveExplorer.tsx`): search, an accordion
**Cámaras** (Site > Server > Folder > Camera, counts, status dots) and **Vistas guardadas**
(list, load, save current view). It is portaled into the shell sidebar (LV-10) and collapses to
zero width (`setContextSidebarCollapsed`, `md:w-0` with a 200 ms width transition, none with
reduced motion); a reopen button sits at the left of the grid toolbar. Collapse state is
`openvms.live.sidebar.collapsed`, tree expansion `openvms.live.explorer.v1` (localStorage).
SurfaceLayer follows the grid growing through its ResizeObserver plus settle period.

- **Data.** Folders and camera order are shared per tenant (`camera_folders`, `cameras.folder_id`,
  `cameras.sort_order`, migration 00020). Folders belong to one server, one level. Everyone sees
  the same tree filtered by camera visibility; a folder shows when it holds a visible camera or
  the caller can manage its server (`manageable_server_ids`).
- **Permissions.** Only `cameras.manage` (on the server for folders, on each camera for moves)
  may create, rename, delete and reorder. Audit: FOLDER_CREATED/RENAMED/DELETED, CAMERAS_REORDERED.
- **Same-server rule.** A camera only moves to a folder of its own server or that server's root.
  The tree disables and dims other servers while dragging; the API rejects the batch with 400
  (atomic: nothing is applied).
- **Drag and drop.** One DndContext: grid uses `camera:<id>` (draggable) and `tile:<n>`; the tree
  adds droppables `tcam:`, `tfolder:`, `troot:` and folder handles `tfolder:` (draggable).
  `lib/explorer.ts` turns (active, over) into a reorder batch; `useCameraFolders` applies it
  optimistically and rolls back on error.
- **Not browser-verified:** drop indicators, drag overlay, collapse transition, keyboard DnD in the tree.

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
- LV-9: `a3a3b89`, `faea095` (no API/config changes).
- Commits: LV-1 `a0f4a62`, LV-2 `b3d0624`, LV-3 `4e84007`, LV-7 `813f791` (merge `3a80d80`),
  LV-6 `7d91c84`, LV-4 `1f632e0`, LV-5 `656b5d9`.
