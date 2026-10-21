# Vision AI: birdseye behavior and LPR/face channels

## Objective and authorization
User request (2026-10-21): Vision AI should start with every smart detection enabled and keep remembering the operator's selection; cameras should refresh and reorder by priority; show a seconds counter since the detection; drop a camera after a period without detections (Frigate birdseye style); expose LPR and face detections as fixed "channels" that behave like a camera, usable as Live grid tiles (saved views) and as a fullscreen LPR/face totem. Implementation and PRs requested; merging and local deploy need explicit user confirmation per slice.

## Product decisions
- Channel layout: latest detection large (plate or face crop, camera, time, "hace Ns") with a strip of the previous detections underneath. Same component in the Live tile and the totem.
- Priority reordering happens client side with CSS `order`, so players are never re-mounted; the backend allocator keeps stable slot indexes.
- Inactivity timeout default 30 s (Frigate `inactivity_threshold` default), operator-adjustable (10 s, 30 s, 1 min, 2 min; no "never" option because 0 still releases at max display).
- Face channel shows every face detection (events label=face): recognized name from sub_labels, else "Desconocido" (user choice 2026-10-21).
- Tile counter: "EN VIVO" while any detection on the camera runs, then "hace Ns" since the last activity (user choice 2026-10-21).
- Saved user preferences keep winning over the new "all detections on" default.

## Tasks
- [x] V1 Default every object filter (person, car, motorcycle, truck, license_plate, face) to enabled; saved prefs unchanged. Web only.
- [x] V2 Order matrix and dynamic tiles by score with CSS `order` (highest first) without re-mounting players. Web only.
- [x] V3 Backend + contract: `detected_at` (latest detection time of the slot's event) on `VisionAISlot`; release an unpinned slot once its camera has had no detection for the inactivity timeout; timeout configurable per request. Web: live "hace Ns" counter and timeout selector.
- [x] V4 LPR and face channels: shared `DetectionChannel` component (latest large + history strip) fed by the plate-read and face-event APIs; LiveExplorer virtual sources `visionai:lpr-channel` / `visionai:faces-channel` placeable in the Live grid and saved views.
- [x] V5 Totem routes `/totem/lpr` and `/totem/faces`: fullscreen kiosk rendering of the same channel component.

## Acceptance and checks
Strict TDD per slice (RED -> GREEN). Web: `pnpm --dir apps/web test -- --run <paths>` and `pnpm --dir apps/web typecheck`. Go: `go test -race ./internal/visionai/... ./internal/api/...`. Contract changes edit `packages/api-contract/openapi.yaml` and regenerate. One PR per slice (V1+V2 share one web-only PR); CI green before merge; local `make deploy` after the last merge.

## Progress
- Tracker created.
- V1+V2 (2026-10-21): default filters all enabled (`visionAiPrefs.ts`); `priorityOrder` in `visionAiGrid.ts` drives CSS `order` in matrix and dynamic modes (pinned first, then score, empty last). RED->GREEN in `visionAiPrefs.test.ts`, `visionAiGrid.test.ts`, `VisionAI.test.tsx` (player identity kept across a reorder). Next: V3.
- V3 (2026-10-21): `VisionConfig.InactivitySeconds` (default 30, `WithInactivity`, 0..600) drops candidates whose last detection (running = now, else end) is older than the timeout and releases camera-less slots after min display; `SlotAssignment.DetectedAt` = latest event start. Contract: `inactivity` query param, `detected_at` slot field. Web: `inactivitySeconds` pref + "Cerrar tras inactividad" selector, `DetectionAge` "hace Ns" counter. Legacy max-display tests pinned to inactivity 0. Note: the 30s default also applies to LPR/alarm/focus sources and Live vision tiles. Next: V4.
- V3b (2026-10-21): `SlotAssignment.Active`/`LastSeenAt` aggregated over every candidate event of the camera, cleared when the camera has no candidate (pinned stays, not live); contract `active`, `last_seen_at`; web `DetectionAge` shows EN VIVO or "hace Ns"; "Nunca" option removed. Next: V4.
- V4 (2026-10-21): discovered saved views only persisted camera cells. V4a: `ViewCell.vision` (`ViewVisionRef` id `^visionai:[a-z0-9-]{1,40}$`, name <= 100), `media.Cell.Vision`, `hiddenFrom` keeps virtual tiles out of camera redaction/permission checks; unit + Postgres integration test. V4b: `tilesToViewCells`/`viewCellsToTiles` replace 4 lossy conversions in Live.tsx; `detectionChannels.ts` (ids, LPR/face mappers, polling 3 s, LPR under ["plates"] for realtime); `DetectionChannel` component (latest large + history strip, header age); `LiveVisionTile` dispatches channel ids; LiveExplorer offers "Canal LPR" and "Canal Rostros". Next: V5.
- V5 (2026-10-21): `/totem/$kind` route outside the app shell, behind the shared `requireSession` guard; `Totem` renders `DetectionChannel` fullscreen, unknown kinds show "Tótem desconocido". Router tests cover lpr/faces, unknown and signed-out redirect. Full web suite 1736/1736, eslint clean. Pending: user confirmation to merge #133/#134 and this branch, then local deploy.
