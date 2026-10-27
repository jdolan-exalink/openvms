# Feature: Dynamic Visual Enhancement Engine (ImageEnhancementEngine)

## Objective
Implement an ultra-low-overhead, non-destructive client-side visual enhancement engine for Live View, Playback, Digital Zoom, and Export players in OpenVMS. The engine dynamically analyzes the full scene or the zoomed region of interest (ROI) and smoothly enhances contrast, dynamic range, white balance, and sharpness without modifying original video or stored recordings.

## Problem & Motivation
Operators monitoring security cameras frequently encounter suboptimal lighting conditions: backlighting, underexposed shadows, washed-out highlights, or color cast. When digitally zooming into a specific region (e.g., a license plate in shadow or a person under bright glare), standard whole-frame adjustments fail to optimize the specific area of interest. A client-side, hardware-accelerated enhancement engine that computes ROI-aware corrections with smooth transitions dramatically increases operational clarity with zero server load and zero forensic tampering risk.

## Scope & Constraints
- **Client-Side Only**: 100% computed in the browser via WebGL shaders with Canvas/CSS filter fallback. Never send frames to the backend for enhancement.
- **Strictly Non-Destructive**: Presentation layer only. Never alter source media, recordings, snapshots, or evidence files.
- **ROI & Digital Zoom Awareness**: When zooming, visible viewport coordinates define the analysis ROI; recalculate adjustments with smooth interpolation (lerp) and throttling.
- **Modes**: Simple `OFF` / `AUTO` toggles in Player UI, with optional manual fine-tuning (brightness, contrast, gamma, saturation, sharpness) and immediate reset.
- **Multicamera Scalability**: Optimize for 1, 4, 9, 16 camera grids by prioritizing selected/maximized/zoomed tiles and throttling idle tiles.
- **TDD Mode**: Strict TDD Mode enabled (RED -> GREEN -> REFACTOR). Runner: `pnpm --filter @openvms/web test`.

## Acceptance Criteria
- [ ] Decoupled `ImageEnhancementEngine` interface (`analyze`, `calculateAdjustments`, `render`, `reset`).
- [x] Image statistics analysis (histogram, min/max percentile levels, luminance, color cast) for full-frame and ROI bounding boxes.
- [x] Automatic adjustment calculations (black/white levels, gamma, contrast, white balance, saturation, light sharpening) avoiding clipping and halos.
- [x] Smooth temporal transitions (lerp smoothing across frames) preventing flicker or harsh jumps during zoom/pan.
- [x] WebGL shader pipeline applying non-destructive hardware adjustments with fallback for unsupported environments.
- [x] UI controls (`OFF` / `AUTO` badge, manual slider drawer/popover, reset button) embedded cleanly into Player/ZoomFrame.
- [x] Integrated into `MsePlayer` (Live), `HlsPlayer` (Playback), and `EvidencePlayerModal` (Exports).
- [x] Unit & integration tests passing with 100% success rate.

## Actionable Tasks

- [x] `TASK-1`: Core Enhancement Engine & ROI Analysis Math
  - Route: Direct inline.
  - Implement `ImageEnhancementEngine` interface, types (`EnhancementAdjustments`, `EnhancementMode`, `RoiRect`), histogram/luminance/white-balance analysis, and lerp smoother in `apps/web/src/lib/enhancement/`.
  - TDD: RED test in `apps/web/src/lib/enhancement/engine.test.ts` verifying analysis and smooth adjustments -> GREEN -> REFACTOR (8 passed).

- [x] `TASK-2`: WebGL & Fallback Rendering Pipeline
  - Route: Direct inline.
  - Implement WebGL shader renderer (`WebGLVisualEnhancer`) with shaders for levels, gamma, contrast, saturation, and unsharp mask, plus Canvas 2D / CSS filter fallback.
  - TDD: RED test in `apps/web/src/lib/enhancement/webgl.test.ts` verifying shader setup, texture binding, and fallback degradation -> GREEN -> REFACTOR (3 passed).

- [x] `TASK-3`: Player UI Controls (`EnhancementControl`)
  - Route: Direct inline.
  - Implement compact `EnhancementControl` component with `OFF` / `AUTO` quick toggle, popover with manual sliders (brightness, contrast, gamma, saturation, sharpness), and "Restablecer imagen" button.
  - TDD: RED test in `apps/web/src/components/EnhancementControl.test.tsx` verifying toggle states, accessibility, and reset actions -> GREEN -> REFACTOR (4 passed).

- [x] `TASK-4`: Integration with Digital Zoom & Players
  - Route: Delegated writer (subagent).
  - Connect `ZoomFrame` visible ROI to `ImageEnhancementEngine`.
  - Wire into `MsePlayer` (Live View), `HlsPlayer` (Playback), and `EvidencePlayerModal` (Exports).
  - TDD: Tests in `apps/web/src/components/DigitalZoomEnhancement.test.tsx` and `apps/web/src/components/EvidencePlayerModal.test.tsx` (all passed).

- [x] `TASK-5`: Grid Multicamera Throttling & Final Verification
  - Route: Direct inline.
  - Implement grid throttling policy (active/zoomed camera priority at 150ms, throttled sampling for background grid tiles at 600ms, skip redundant re-renders via equality check).
  - Full test suite verified (26/26 tests passed) and production build clean (0 errors).

## Progress & Verification Evidence
- `TASK-1`: Complete. 8/8 tests passed in `src/lib/enhancement/engine.test.ts`. Verified ROI extraction from digital zoom, auto contrast/gamma/black-level adjustments, and lerp interpolation.
- `TASK-2`: Complete. 3/3 tests passed in `src/lib/enhancement/webgl.test.ts`. WebGL hardware shader pipeline (Laplacian unsharp mask, levels stretch, gamma, contrast, saturation, white balance) and CSS filter fallback implemented.
- `TASK-3`: Complete. 4/4 tests passed in `src/components/EnhancementControl.test.tsx`. Compact quick AUTO/OFF toggle badge, manual slider drawer (brightness, contrast, gamma, saturation, sharpness), and image reset action.
- `TASK-4`: Complete. `ZoomFrame` (powering Live View `MsePlayer` and Playback `HlsPlayer`) and `EvidencePlayerModal` (Exports) integrated with dynamic ROI tracking and hardware-accelerated CSS filter rendering.
- `TASK-5`: Complete. Multicamera performance optimized for 1, 4, 9, 16 camera grids. Verified clean build and full vitest suite passing (26/26 tests).

## Next Step
Feature complete and deployed. Ready for user verification.
