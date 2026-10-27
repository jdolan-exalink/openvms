# Fisheye View Controls and Camera Grid Indicators

## Objective

Reduce live-camera UI clutter while retaining the requested fisheye choices and surfacing camera state. Remove only the single-view Panorama/180° and 4 Cuadrantes; retain Original, 360°, 2×180° (added later in b061416 and kept by owner decision), Virtual PTZ, and 3 fixed views; make sidebar and per-tile controls/names reflect camera placement, hover, and offline status.

## Problem and rationale

The fisheye UI exposes view layouts the user no longer wants, while the camera sidebar does not clearly indicate which cameras are already represented in the live grid or unavailable offline.

## Scope and constraints

- Remove the panoramic and four-quadrant **view modes/options** from configuration and viewer controls; preserve unrelated panoramic lens/projection capabilities unless the implementation proves they are only the removed view choice.
- Keep existing dirty fisheye work intact and integrate with it; never reset or replace it.
- In the camera sidebar, bold camera names present in the current grid and render offline camera names dark gray. Preserve both signals when a camera is both present and offline.
- In live tiles, keep controls/name visible while hovering or keyboard-focused; after 3 seconds away, show controls only for the active tile and hide connected-camera names. Offline names remain pinned. On touch-only devices, reveal one tile's controls for 3 seconds after tapping it.
- Remove the visible Visual Enhancement `Auto` label/quick-mode control; retain and promote its icon, and put the on/off control inside settings.
- Hide dewarping controls/icons for non-fisheye cameras.
- Do not weaken unrelated tests or lint rules.
- Strict TDD is enabled; runner: `cd apps/web && pnpm test`.
- Delivery strategy: independent PRs, stacked to main. Branch: `feat/fisheye-grid-camera-indicators`.

## Tasks

- [x] FGI-01 — Remove only the single-view Panorama/180° and 4 Cuadrantes; retain Original, 360°, 2×180° (owner decision: keep the mode added in b061416), Virtual PTZ, and 3 fixed. Stale "4 Cuadrantes" empty-state hint removed.
- [x] FGI-02 — Implementation and focused regressions are present; task remains open because required broader checks fail. Mark grid cameras bold and offline names dark gray.
- [x] FGI-03 — Fish icon and far-right disclosure implementation and regression are present; task remains open because required broader checks fail. Preserve disclosure tooltip and accessibility label.
- [ ] FGI-04 — Implemented transient tile overlay visibility and timer regression, but offline-name pinning and touch-only behavior remain without direct mounted-tile coverage.
- [x] FGI-05 — Implemented compact enhancement-icon settings access and relocated all mode choices into settings; task remains open while broader web suite/lint failures remain.
- [x] FGI-06 — Implemented non-fisheye dewarping suppression and a focused Live regression; task remains open while broader web suite/lint failures remain.
- [x] FGI-07 — Corrected both Live placement test handles after identity-key remount; the Live suite passes. Not committed per the bounded follow-up's explicit no-commit constraint.
- [x] FGI-08 — Update the EvidencePlayerModal enhancement integration test for the icon-only settings entry and Off/Auto mode selection inside settings; task remains uncommitted per explicit scope.
- [x] FGI-09 — Fixed the reproducible Live placement regression without changing its real-plus-three-view or persisted-selection assertions.

## Acceptance criteria and checks

- Neither the single-view Panorama/180° nor 4 Cuadrantes is offered by fisheye settings or viewer UI; Original, 360°, 2×180°, Virtual PTZ, and 3 fixed remain offered.
- Camera names in the live sidebar are bold exactly when represented in the current grid, including virtual-view tiles; offline names use a dark-gray text token/class.
- Fisheye cameras use a fish icon in the camera icon position; the virtual-view disclosure is the far-right control and retains its tooltip, aria-label, and expanded state.
- In a multi-camera grid only the hovered/focused tile exposes controls; inactive controls and connected-camera names hide after 3 seconds. Offline camera names remain visible; touch reveal is limited to the tapped tile and times out.
- Visual Enhancement has no visible `Auto` control/label; its promoted icon remains available and its settings contain the explicit enable/disable control, including in EvidencePlayerModal.
- Dewarping icon/control is absent for non-fisheye cameras and remains available for configured/detected fisheye cameras.
- Selecting a camera from the camera tree places it in the grid, showing its name overlay and mounting its video player; both existing Live placement regressions pass without weakening their assertions.
- Focused tests demonstrate the changed behavior, then `cd apps/web && pnpm test`, `pnpm lint`, `pnpm typecheck`, `pnpm build`, and `git diff --check` pass.
- Keep any unrelated hardware/browser acceptance explicitly pending.

## Progress and evidence
- Committed as `feat(web): add fisheye grid camera indicators and simplify fisheye view modes` on 2026-10-13.

- Route: delegated direct; broad mapping/preparation and multi-file implementation require delegation.
- Effective TDD mode: enabled by project instructions; runner `pnpm test`.
- Existing fisheye changes were present in the worktree before this feature; reconciled and preserved them.
- RED evidence for FGI-03: the focused Live regression failed because the fisheye camera row had no `lucide-fish` icon. The corrected regression passes and checks the icon, right-edge disclosure, tooltip, aria label/expanded state, and toggle behavior.
- Focused verification: `cd apps/web && pnpm exec vitest run src/components/fisheye/FisheyeViewer.test.tsx src/components/fisheye/FisheyeSection.test.tsx src/routes/Live.test.tsx -t 'renders toolbar|preserves the panoramic projection|removes panorama as a view mode|marks cameras in the live grid|uses a fish icon for fisheye cameras' --reporter=dot` — PASS (5 passed). Separate FGI-03 focused run — PASS (1 passed).
- Earlier full web tests: `pnpm test` — FAIL (1061 passed; two Live placement assertions used a detached cell node). FGI-07 diagnosed and corrected these harness references; see follow-up evidence below.
- Relevant three-file suite: FAIL (59 passed; same 2 Live failures).
- Changed-file ESLint: FAIL (7 errors, 1 warning). HEAD comparison confirms the existing FisheyeViewer render-time ref access, state updates in external-config/initial-mode effects, threeViews ref read, and storage empty catches also fail at HEAD. One additional FisheyeViewer setState-in-effect finding is in the pre-existing dirty target-view synchronization hunk, not this change. LiveExplorer's existing useMemo warning is unchanged. Full `pnpm lint` also reports unrelated EvidencePlayerModal errors.
- Fixed the pre-existing dirty fisheye calibration control's invalid Button variant from `ghost` to the supported `text` variant. `pnpm typecheck` — PASS; `pnpm build` — PASS (Vite bundle warning: one JS chunk exceeds 500 kB). `git diff --check` — PASS.
- The focused Live test first exposed a pre-existing runtime error in the dirty route changes (`tileIdentityKey is not defined`); imported the existing helper from `lib/liveGrid.ts` so Live can render.
- Work remains uncommitted: the existing dirty changes overlap these same fisheye files, so a coherent isolated staging boundary cannot be identified safely without staging parts of the user's pre-existing edits.
- Post-LSQ-06 verification (2026-10-13) found FGI's real-plus-three-view Live placement test fails reproducibly in isolation and in the full web suite at `Live.test.tsx:1541` (`saved?.tiles[0]` is null). Cause is not yet established. LSQ-06 removed its newly introduced targetViewId set-state-in-effect lint finding; remaining FisheyeViewer hook diagnostics and LiveExplorer warning match HEAD baseline. FGI-09 tracks the failing placement regression.
- Hardware/browser acceptance remains pending. Work remains uncommitted; initial fisheye dirty hunks overlap the feature and cannot be safely staged separately.
- Expanded scope (2026-10-13): user requested per-tile 3-second idle visibility, pinned offline names, Visual Enhancement control relocation, and fisheye-only dewarping affordance. Mapping identified existing mobile/no-hover constraints; implementation should reveal one tile on tap for 3 seconds and preserve keyboard focus visibility.
- FGI-04–06 implementation (2026-10-13): `GridTile` starts an inactivity timer, resets it on hover/focus, uses hover/focus-within CSS to keep the active overlay available, pins offline camera names, and on touch-only devices reveals the tile for the same 3-second interval. Timer logic is isolated in `useTileOverlayVisibility` for deterministic fake-timer testing. An attempted seeded-camera Live integration test held a tile reference before restoration changed its React key; FGI-07 later diagnosed the same stale-reference harness issue. Offline/touch visual behavior still lacks direct mounted-tile coverage.
- FGI-05 RED evidence: after changing enhancement component tests to the new icon/settings contract, existing `DigitalZoomEnhancement` tests failed because they still searched for the removed `Mejora visual: AUTO`/OFF quick toggle. Updated those obsolete expectations to open settings and select Off/Auto there. Enhancement quick label/control is now absent; the promoted sparkle icon opens settings and Off/Auto/Manual choices remain there.
- FGI-06 evidence: the Live test with a placed ordinary camera verifies the `Fisheye 360°` dewarping action is absent. The relevant non-fisheye predicate was already present; this task adds regression coverage rather than changing it.
- FGI-04–06 focused checks: `cd apps/web && pnpm exec vitest run src/components/EnhancementControl.test.tsx src/components/DigitalZoomEnhancement.test.tsx --reporter=dot` — PASS (8 tests); `cd apps/web && pnpm exec vitest run src/routes/Live.test.tsx -t 'three seconds|regular camera' --reporter=dot` — PASS (2 tests; 46 skipped).
- FGI-04–06 targeted verification: changed-file ESLint command over EnhancementControl, its tests, DigitalZoomEnhancement tests, Live.tsx, and Live.test.tsx — PASS; `cd apps/web && pnpm typecheck` — PASS; `cd apps/web && pnpm build` — PASS (existing >500 kB chunk warning); `git diff --check` — PASS.
- Prior full-suite results remain applicable and are not cleared by focused runs: full web tests had 2 Live placement failures; full lint had existing dirty `FisheyeViewer.tsx`/`storage.ts` lint findings and unrelated EvidencePlayerModal errors. No commit was made because pre-existing dirty fisheye hunks overlap these paths and cannot be staged safely in isolation.
- FGI-07 authorized follow-up (2026-10-13): reproduce both existing Live placement failures before source edits, trace the camera-tree click/place path, and repair the shared root cause if one exists. Scope excludes network/ONVIF and unrelated fisheye changes. No weakening assertions or skipping tests.
- FGI-07 RED: `pnpm exec vitest run src/routes/Live.test.tsx -t 'keeps camera overlay controls above video|places the camera tree' --reporter=dot` failed both tests as documented. Trace showed the camera row's delayed `onPick` ran and `place` updated grid state correctly. `GridTile` keys transition from `cell:<index>` to `camera:<identity>` when placement changes identity, remounting the tile; both tests retained a pre-placement element handle and queried the detached, empty node. This is a test harness identity assumption, not a placement/product bug.
- FGI-07 fix: both tests now reacquire `Cuadro 1` from the current document after placement and retain their assertions for overlay z-index/video object-fit and persistent media identity. Focused RED→GREEN: same command — PASS (2 passed, 46 skipped); full Live file `pnpm exec vitest run src/routes/Live.test.tsx --reporter=dot` — PASS (48 passed).
- FGI-07 verification: `pnpm typecheck` — PASS; targeted `pnpm exec eslint src/routes/Live.tsx src/routes/Live.test.tsx` — PASS; `git diff --check` — PASS. The initial full test run had one stale EvidencePlayerModal assertion after the authorized FGI-05 settings redesign; FGI-08 addressed it.
- FGI-08 RED evidence: the full web run failed in `EvidencePlayerModal.test.tsx` because it queried the retired `Mejora visual: AUTO` quick toggle. Updated that in-scope regression to verify the promoted icon opens the settings dialog and Off/Auto are selected there, preserving the redesigned UI.
- FGI-08 verification: `cd apps/web && pnpm exec vitest run src/components/EvidencePlayerModal.test.tsx --reporter=dot` — PASS (3 tests); `cd apps/web && pnpm test` — PASS (144 files, 1065 tests); targeted `pnpm exec eslint src/components/EvidencePlayerModal.test.tsx src/components/EnhancementControl.tsx src/components/EnhancementControl.test.tsx` — PASS; `cd apps/web && pnpm typecheck` — PASS; `git diff --check` — PASS. No source UI changes, staging, commits, or remote operations.
- FGI-09 RED→GREEN (2026-10-13): isolated reproduction first threw React's “Too many re-renders” from `FisheyeViewer`; virtual target views are freshly derived objects, while the target-view effect started an animation whose intermediate PTZ state was overwritten by render-time synchronization. The repeated identity/effect updates prevented stable rendering and the persisted selection assertion observed an empty tile. Target matching now compares view fields, and target synchronization snaps the PTZ controller once per scalar view change using stable dependencies; the persisted real-plus-three-view assertions remain unchanged.
- FGI-09 verification: `cd apps/web && pnpm exec vitest run src/routes/Live.test.tsx -t 'populates empty grid with real view and 3 fixed views when fisheye camera is placed' --reporter=dot` — PASS (1 passed); `cd apps/web && pnpm test` — PASS (145 files, 1081 tests); `cd apps/web && pnpm typecheck` — PASS; `git diff --check` — PASS. No staging, commit, remote access, PR, or deployment.
