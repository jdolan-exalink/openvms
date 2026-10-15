# Vision AI dynamic matrix (birdseye style)

## Objective
A "Dinámica" layout mode in `/vision-ai` that shows only cameras with activity and builds the grid around
them, growing and shrinking as events come and go (like Frigate birdseye).

## Problem and rationale
Fixed layouts (1/4/9/16) either waste space with "Sin evento asignado" placeholders or cut active cameras.

## Scope and constraints
- Web only. Request the existing API with `layout=16` (server-side ranking/stability unchanged) and render
  only filled slots.
- Grid shape from the active count n: 1→1×1, 2→2×1, 3–4→2×2, 5–6→3×2, 7–9→3×3, 10–12→4×3, 13–16→4×4.
  Pure helper with unit tests.
- Stable order: slot index order, so a camera does not jump when another one leaves; players keyed by camera.
- Existing pins, object chip filters, debug and playback keep working; empty state when n = 0.
- Mode persisted like other modes (if they persist); default mode unchanged.

## TDD
- Mode: strict (source: user global CLAUDE.md). Runner: `cd apps/web && pnpm vitest run <file>`.

## Tasks
- [x] VDM-01 — `dynamicGridShape(n)` helper + "Dinámica" mode in VisionAI.tsx rendering only filled slots.
      Route: delegated writer.

## Acceptance criteria and checks
- `pnpm typecheck`, `pnpm lint`, VisionAI tests, full `pnpm test`.
- Live check on the running stack: grid size follows active cameras.

## Delivery
- Forecast ~200 lines → single PR. RDD on (global).

## Progress and evidence
- Branch `feat/vision-ai-dynamic-matrix` from main.
- VDM-01 `4b4dba0`: `lib/visionAiGrid.ts` `dynamicGridShape` (+ tests for every boundary); "Dinámica" mode
  (layout=16, size picker hidden, only filled slots in slot order, players keyed by camera, empty state).
  RED: import unresolved + 5 page tests failing on "Dinámica" button → GREEN 42/42; full `pnpm test` 1508.
  Modes are not persisted (plain useState), so neither is Dinámica; pins share key with the 4×4 matrix.
- Live (rebuilt web): with >16 active cameras the grid fills 4×4 with 16 live tiles; no console errors.
  Shrinking (fewer cameras) not observable live at current activity — covered by tests.
- Native review (lineage review-45c3fb2af6afc5da): APPROVED, acknowledged. Addressed: duplicate camera keys
  (pinned camera also in a ranked slot) → dynamic tiles deduped by camera; cap at 16 tiles; page test for
  shrinking with a middle slot emptied (order kept, first player node not remounted). RED: dedupe and cap
  tests failed; shrink test passed first (behavior already correct, coverage added). 45/45 GREEN.

## Next step
Push + PR (user decision).
