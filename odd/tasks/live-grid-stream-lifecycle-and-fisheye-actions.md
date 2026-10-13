# Live Grid Stream Lifecycle and Fisheye Actions

## Objective

Make Live grid stream closure/replacement reliable without changing the user's grid layout, improve fisheye defaults, and consolidate the fisheye live-tile actions.

## Problem and rationale

Closing the final camera stream can leave an empty grid cell with no useful close action, and replacing one camera can allow the old camera to reappear when the new one is removed. Live currently has no explicit close-all-streams action. Fisheye defaults and live-tile affordances should be consistent and recognizable.

## Scope and constraints

- Add a Live action beside the grid editor and maximize control to close all camera streams.
- Close-all clears every stream/selection while preserving the current grid dimensions/layout.
- Fix individual close and camera replacement so tile state and persisted selection do not resurrect the replaced camera; closing the final stream must leave that stream closed.
- Default newly configured fisheye cameras to Virtual PTZ while respecting an explicitly saved per-camera mode.
- Consolidate the live-tile fisheye dewarping and view-configuration entry points behind one fish-icon affordance matching the sidebar fish icon. The icon opens an accessible menu with the two existing actions, preserving their distinct modal flows and avoiding an unrelated modal redesign.
- Preserve unrelated dirty work; do not reset the worktree.
- Strict TDD is enabled; test runner: `cd apps/web && pnpm test`.
- Current branch: `feat/fisheye-grid-camera-indicators`; existing dirty changes must be preserved.
- No Docker deployment/restart, PR, merge, or remote operation is authorized by this request. ODD work-unit commits remain required only when the new changes can be staged without including the pre-existing dirty edits.

## Tasks

- [x] LGS-01 — Add close-all-streams action; clear all camera selections and stop/unmount their players while preserving the current grid dimensions. Add regression coverage.
- [x] LGS-02 — Fix single-stream close and replace-then-close state/persistence behavior so the old camera cannot reappear and the final stream remains closed. Add regression coverage.
- [x] LGS-03 — Make Virtual PTZ the default fisheye mode for cameras without an explicit saved choice; retain explicit user choices. Add storage/viewer tests. Reopened for viewer-level evidence and closed after adding a regression that proves an explicitly saved `defaultMode: "360"` opens selected.
- [x] LGS-04 — Replace separate live-tile dewarping/configuration actions with one fish-icon affordance matching the sidebar and preserve both capabilities in the consolidated flow. Add Live regressions.

## Acceptance criteria and checks

- Close all removes every selected camera/virtual stream, unmounts the video players, and preserves rows/columns/grid shape.
- Closing the final populated tile clears its stream. Replacing camera A with B and then closing B never restores A, including after state persistence/restoration.
- An unconfigured fisheye camera opens in Virtual PTZ by default; a saved non-default mode remains honored.
- A fisheye live tile exposes one fish icon instead of separate dewarping and view-settings icons; activating it opens an accessible menu with both existing actions, preserving their respective capabilities and distinct flows.
- Focused regressions and `cd apps/web && pnpm test`, `pnpm typecheck`, changed-file lint, and `git diff --check` pass.
- No unrelated regressions are weakened or skipped.

## Progress and evidence
- Committed as `feat(web): add live grid stream lifecycle controls and fisheye tile actions` on 2026-10-13.

- Route: delegated direct; behavior spans Live route, grid-state utilities/storage, fisheye config/viewer, and tests (mapping trigger: 4+ files; writer trigger: multiple non-trivial files).
- Effective TDD mode: enabled by project instructions; runner `pnpm test`.
- User clarified close-all behavior: empty every stream while leaving the grid layout exactly as-is.
- Resume reconciliation (2026-10-13): the actual task file and full Engram observation #942 agree on LGS-01 through LGS-04, the preserved-grid close-all semantics, and the two distinct fisheye flows. Existing dirty FGI edits in `Live.tsx`, fisheye storage/types/viewer/section, and related tests are pre-existing inputs and must be preserved; implementation will be additive to that work.
- Estimated authored diff: approximately 250 lines, additions plus deletions; heuristic only, not a cap.
- Delivery preference: independent PRs, stacked to main, as previously stated; no PR action authorized here.
- Read-only mapping confirmed the dewarping button launches `FisheyeViewer` while the view-settings button launches `FisheyeSection`; they are distinct flows. Chosen low-impact consolidation: one Fish icon with an accessible action menu, preserving both flows.
- LGS-01: Added close-all beside layout/fullscreen; clears tile refs and state while retaining columns, rows, and pane geometry. The regression verifies persisted empty cells, detached video elements, unchanged geometry, and socket closure.
- LGS-02: Removal updates `tilesRef` synchronously; replacement/removal discards a shared player session only after its last consumer releases it. Regressions verify the final stream stays empty and replacing A with B then closing B persists no camera and does not restore A after reload.
- LGS-03: Unconfigured mode now defaults to `virtual_ptz`; per-camera `defaultMode` and a saved transient mode were previously reported honored. Viewer cancel restores the mode the viewer initially opened with (including explicit `initialMode`) rather than the new global default. **Evidence gap closed (2026-10-13):** independent verification found that storage retrieval and viewer default were tested separately. Added a viewer-level regression that saves per-camera `defaultMode: "360"`, mounts the viewer without `initialMode`, and asserts `360°` is selected while Virtual PTZ is not. The regression passed on the first run, so no precedence defect was demonstrated and no source behavior was changed; the explicit `initialMode` path remains untouched.
- LGS-04: Replaced the two tile buttons with a Fish icon opening the accessible existing `ContextMenu`; its two actions still open the distinct dewarping viewer and configuration section.
- Strict-TDD RED evidence observed: before implementation, the close-all test could not find its action, storage expected `virtual_ptz` but received `original`, and the explicit stream-stop assertion observed 0 socket closures after removing rendered tiles. The focused regressions pass after implementation. RED was not separately observed for every new LGS-02/LGS-04 assertion before coding; do not represent those as pre-implementation RED.
- Verification: focused `pnpm exec vitest run src/routes/Live.test.tsx src/lib/liveGrid.test.ts src/lib/fisheye/storage.test.ts src/components/fisheye/FisheyeViewer.test.tsx src/components/fisheye/FisheyeSection.test.tsx --reporter=dot` — 5 files, 115 tests passed. Full `pnpm test` — 145 files, 1071 tests passed. `pnpm typecheck` passed. `git diff --check` passed. Targeted ESLint passed for Live, its tests, fisheye types/storage test/viewer test, and player manager/provider files; including `FisheyeViewer.tsx` reports 5 `react-hooks/refs` and `react-hooks/set-state-in-effect` findings in dirty viewer code (no unrelated refactor made).
- Changed files for this feature: `apps/web/src/routes/Live.tsx`, `apps/web/src/routes/Live.test.tsx`, `apps/web/src/lib/fisheye/types.ts`, `apps/web/src/lib/fisheye/storage.test.ts`, `apps/web/src/components/fisheye/FisheyeViewer.tsx`, `apps/web/src/components/fisheye/FisheyeViewer.test.tsx`, `apps/web/src/lib/live/PlayerSessionManager.ts`, `apps/web/src/lib/live/PlayerSessionManager.test.ts`, `apps/web/src/lib/live/PlayerSessionProvider.tsx`.
- No files were staged or committed. Pre-existing dirty FGI work remains in the shared worktree.
- LGS-03 follow-up (2026-10-13): strict test-first added the viewer-level saved-360 regression; observed result was pass on first run (no RED failure), confirming existing explicit `defaultMode` precedence. No implementation change was needed. Focused `pnpm exec vitest run src/components/fisheye/FisheyeViewer.test.tsx src/lib/fisheye/storage.test.ts --reporter=dot`: 2 files, 15 tests passed. Full `pnpm test`: 145 files, 1072 tests passed. `pnpm typecheck` passed. `pnpm exec eslint src/components/fisheye/FisheyeViewer.test.tsx` passed; linting `FisheyeViewer.tsx` still reports the 5 previously recorded React refs/set-state findings (at lines 161, 189, 196, 208, 571), with no source change in this follow-up. `git diff --check` passed.

## Relevant Files

- `apps/web/src/routes/Live.tsx` — grid action bar, tile state, close/replace handlers, live-tile affordances.
- `apps/web/src/lib/liveGrid.ts` — grid placement and selection persistence helpers.
- `apps/web/src/lib/fisheye/types.ts`, `storage.ts`, `FisheyeViewer.tsx` — fisheye mode defaults and persistence.
- `apps/web/src/components/LiveExplorer.tsx` — sidebar fisheye icon reference.
- `apps/web/src/routes/Live.test.tsx`, `apps/web/src/lib/liveGrid.test.ts`, `apps/web/src/lib/fisheye/storage.test.ts`, `apps/web/src/components/fisheye/FisheyeViewer.test.tsx` — relevant regressions.
- `apps/web/src/lib/live/PlayerSessionManager.ts`, `PlayerSessionProvider.tsx` — explicit user removal discards sessions after the last active consumer releases them; tested in `PlayerSessionManager.test.ts`.
