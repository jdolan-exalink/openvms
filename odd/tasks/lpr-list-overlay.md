# LPR list overlay (Live View + Vision AI) and totem sources

## Goal

On LPR-flagged cameras in Live View, show recent plate reads as a futuristic glass list on the
left of the video. The overlay is optional (per-user toggle) and also available in Vision AI.
The LPR/face totem channels stay reachable as sources in the camera list, with clearer naming.

## Decisions

- Totem stays a source in the Live explorer ("Fuentes Virtuales"), not a per-camera button.
- Overlay data: per-camera plate reads (`/api/v1/lpr/reads`, `camera_id` filter), refreshed by
  the existing realtime `["plates"]` invalidation plus a short poll.
- Toggle is a client-side preference (localStorage), default on for LPR cameras.
- Presentation reuses `ArPlate` and `plateCropUrl`; no new API or contract change.

## Slices

- [x] 1. `useCameraPlates` hook + `LprListOverlay` component (futuristic glass list, entry animation, empty state) with tests
- [x] 2. Live grid tile: render overlay on cameras with `lpr=true`, per-tile toggle persisted in localStorage
- [ ] 3. Vision AI: same overlay on the active slot camera and the LPR layout, same toggle
- [ ] 4. Explorer: rename channel sources to "Tótem LPR" / "Tótem Rostros", add a TÓTEM badge and icon

## Verification

- `pnpm --dir apps/web typecheck`
- `pnpm --dir apps/web test -- --run <touched test files>`
