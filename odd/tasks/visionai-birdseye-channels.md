# Vision AI: birdseye behavior and LPR/face channels

## Objective and authorization
User request (2026-10-21): Vision AI should start with every smart detection enabled and keep remembering the operator's selection; cameras should refresh and reorder by priority; show a seconds counter since the detection; drop a camera after a period without detections (Frigate birdseye style); expose LPR and face detections as fixed "channels" that behave like a camera, usable as Live grid tiles (saved views) and as a fullscreen LPR/face totem. Implementation, PRs, merge and local deploy authorized for this feature.

## Product decisions
- Channel layout: latest detection large (plate or face crop, camera, time, "hace Ns") with a strip of the previous detections underneath. Same component in the Live tile and the totem.
- Priority reordering happens client side with CSS `order`, so players are never re-mounted; the backend allocator keeps stable slot indexes.
- Inactivity timeout default 30 s (Frigate `inactivity_threshold` default), operator-adjustable.
- Saved user preferences keep winning over the new "all detections on" default.

## Tasks
- [x] V1 Default every object filter (person, car, motorcycle, truck, license_plate, face) to enabled; saved prefs unchanged. Web only.
- [x] V2 Order matrix and dynamic tiles by score with CSS `order` (highest first) without re-mounting players. Web only.
- [ ] V3 Backend + contract: `detected_at` (latest detection time of the slot's event) on `VisionAISlot`; release an unpinned slot once its camera has had no detection for the inactivity timeout; timeout configurable per request. Web: live "hace Ns" counter and timeout selector.
- [ ] V4 LPR and face channels: shared `DetectionChannel` component (latest large + history strip) fed by the plate-read and face-event APIs; LiveExplorer virtual sources `visionai:lpr-channel` / `visionai:faces-channel` placeable in the Live grid and saved views.
- [ ] V5 Totem routes `/totem/lpr` and `/totem/faces`: fullscreen kiosk rendering of the same channel component.

## Acceptance and checks
Strict TDD per slice (RED -> GREEN). Web: `pnpm --dir apps/web test -- --run <paths>` and `pnpm --dir apps/web typecheck`. Go: `go test -race ./internal/visionai/... ./internal/api/...`. Contract changes edit `packages/api-contract/openapi.yaml` and regenerate. One PR per slice (V1+V2 share one web-only PR); CI green before merge; local `make deploy` after the last merge.

## Progress
- Tracker created.
- V1+V2 (2026-10-21): default filters all enabled (`visionAiPrefs.ts`); `priorityOrder` in `visionAiGrid.ts` drives CSS `order` in matrix and dynamic modes (pinned first, then score, empty last). RED->GREEN in `visionAiPrefs.test.ts`, `visionAiGrid.test.ts`, `VisionAI.test.tsx` (player identity kept across a reorder). Next: V3.
