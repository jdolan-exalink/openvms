# Export Marks Stability and Grabado View Cleanup

## Objective
Fix the timeline bounce/reset during export marks placement, hide non-recording/synthetic cameras (BirdEye) and virtual vision sources (Totem LPR, Totem Rostros, Vision AI) from Grabado and Playback, and guarantee host volume permissions on exports.

## Problem and why
1. **Host export volume permissions**: The `/mnt/cctv/openvms/exports` host bind mount was owned by `root:root 0755`, preventing the non-root worker container (`uid 10001, gid 999`) from creating clip export folders (`permission denied`).
2. **Export mark Fin bounce/reset**: When placing the Fin export mark, scrubbing or clicking on the timeline caused the playhead to bounce back to earlier times or start playing involuntarily. This was caused by:
   - `<video>` elements emitting stale `timeupdate` events while `seeking === true`.
   - `seek` in `useRecPlayback` defaulting `play` to `true` instead of preserving current playback state.
   - `DayTimeline` auto-centering resetting the view while marks were being placed.
   - `DayTimeline` treating ruler drag as pan instead of scrub when placing marks, and not updating `effectiveSelection` or the moving mark badge in real time.
3. **Non-recording views in Grabado**: BirdEye synthetic cameras and virtual vision sources (Totem LPR, Totem Rostros, Vision AI) have no recordings. Users requested they not appear in Grabado mode or Playback.

## Scope
- `deploy/deploy.sh`: Ensure `/mnt/cctv/openvms/exports` is owned by `10001:999` with mode `775`.
- `apps/web/src/lib/recordings.ts`: Add `isRecordableCamera` helper to identify cameras with recordings vs synthetic/virtual views (e.g. BirdEye).
- `apps/web/src/routes/Playback.tsx`: Filter camera selection with `isRecordableCamera`.
- `apps/web/src/components/LivePhone.tsx`: Filter `all` cameras in GRABADO mode with `isRecordableCamera`.
- `apps/web/src/components/LiveExplorer.tsx`: Accept `rec` prop; hide "Fuentes Virtuales" and filter `displayCameras` when `rec` is true.
- `apps/web/src/routes/Live.tsx`: Pass `rec` prop to `LiveExplorer`.
- `apps/web/src/lib/useRecSession.ts`: Filter `timelineCameras` with `isRecordableCamera`.
- `apps/web/src/components/HlsPlayer.tsx` & `apps/web/src/components/ReplayPlayer.tsx`: Ignore `onTimeUpdate` while `seeking === true`.
- `apps/web/src/lib/useRecPlayback.ts`: Preserve playback state on seek (`play = options?.play ?? cur.playing`); add cooldown guard to `onMasterTime`.
- `apps/web/src/components/DayTimeline.tsx`: Scrub on ruler click/drag and while placing marks; live update moving mark badge and `effectiveSelection`; disable auto-centering while placing marks.
- `apps/web/src/components/LiveRecDock.tsx`: Pass `{ play: playing }` on seek.

## Tasks
- [x] REM-01 — Permissions: Fix host exports volume permissions and add automated guard in `deploy/deploy.sh`.
- [x] REM-02 — Non-recording camera filter: Implement `isRecordableCamera`, apply to `Playback`, `LivePhone`, `LiveExplorer`, and `useRecSession`.
- [x] REM-03 — Export marks stability: Guard `onTimeUpdate` during video seek, preserve play state on seek, enable live scrub & selection in `DayTimeline`.
- [x] REM-04 — Tests: Comprehensive vitest suites for `recordings`, `LivePhone`, `Playback`, `LiveRecDock`, and `exportMarks`.
