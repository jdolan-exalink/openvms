# Feature: On-Demand Forensic Detail Enhancement (Mejorar detalle)

## Objective
Implement an on-demand forensic detail enhancement pipeline ("Mejorar detalle" / "Mejora IA") in the OpenVMS export player and playback interfaces. When playback is paused, operators can trigger enhancement on the full frame or digitally zoomed region of interest (ROI). An interactive before/after split comparator modal displays the original versus enhanced image, complete with forensic metadata and download options for both images. Original video and evidence remain strictly untouched.

## Problem & Motivation
CCTV operators frequently pause recorded evidence on critical moments (license plates, faces, vehicles, clothing) to inspect identifying details. Often these frames suffer from motion blur, camera sensor noise, or optical compression. Operators need an immediate, trustworthy enhancement tool with zero risk of forensic evidence contamination, allowing side-by-side verification and export of both the pristine original and the digitally processed frame.

## Scope & Constraints
- **Forensic Integrity**: Non-destructive. Never modify original video streams, recordings, or export archives.
- **Adaptive Modular Pipeline**: `ForensicEnhancementEngine` with `FrameAnalyzer`, `DeblurProvider`, `DenoiseProvider`, `SuperResolutionProvider`, and `PostProcessor`.
- **ROI Support**: Respects active digital zoom coordinates so the operator can enhance a license plate or face at high detail without processing an entire 4K canvas unnecessarily.
- **Interactive UI**: Before/after split slider modal with pan/zoom, fullscreen, download original, download enhanced, and copy snapshot.
- **TDD Mode**: Strict TDD Mode enabled (RED -> GREEN -> REFACTOR). Runner: `pnpm --filter @openvms/web test`.

## Acceptance Criteria
- [x] Modular `ForensicEnhancementEngine` with pluggable analyzers and filters (Deblur, Denoise, Super-Resolution, Tone Mapping).
- [x] Quality analyzer assessing Laplacian blur variance and noise levels to apply only necessary processing stages.
- [x] Interactive `ForensicComparisonModal` with before/after split slider, side-by-side mode, zoom/pan, downloads, and forensic label.
- [x] "Mejorar captura" / "Mejorar detalle" button appears on video streams when playback is paused.
- [x] Integrated into `EvidencePlayerModal` with camera and timestamp metadata attached.
- [x] 100% test pass rate and clean build.

## Actionable Tasks

- [x] `TASK-1`: Core Forensic Pipeline & Adaptive Providers
  - Route: Direct inline / delegated writer.
  - Implement `ForensicEnhancementEngine`, `FrameAnalyzer`, `DeblurProvider`, `DenoiseProvider`, `SuperResolutionProvider`, and `PostProcessor` in `apps/web/src/lib/enhancement/forensic.ts`.
  - TDD: RED test in `apps/web/src/lib/enhancement/forensic.test.ts` verifying blur detection, adaptive pipeline execution, and ROI processing -> GREEN -> REFACTOR.
  - Verification: 6 vitest tests passing.

- [x] `TASK-2`: Interactive Split Before/After Comparator Component
  - Route: Direct inline / delegated writer.
  - Implement `ForensicComparisonModal.tsx` with interactive split comparison slider, side-by-side view, zoom/pan controls, original & enhanced download buttons, and forensic metadata banner.
  - TDD: RED test in `apps/web/src/components/ForensicComparisonModal.test.tsx` verifying slider interaction, downloads, and modal controls -> GREEN -> REFACTOR.
  - Verification: 8 vitest tests passing.

- [x] `TASK-3`: Integration into Export Player (`EvidencePlayerModal`)
  - Route: Delegated writer / direct inline.
  - Add "Mejorar captura" action button when stream is paused in `EvidencePlayerModal.tsx`.
  - Wire frame capture, ROI extraction, enhancement execution, and comparison modal presentation.
  - TDD: Tests in `apps/web/src/components/EvidencePlayerModal.test.tsx` -> GREEN -> REFACTOR.
  - Verification: 3 vitest tests passing in `EvidencePlayerModal.test.tsx`.

- [x] `TASK-4`: Final Verification, Build & Docker Deployment
  - Route: Direct inline.
  - Verify full test suite (138 test files, 1013 tests passing 100%), strict TypeScript compilation (`tsc --noEmit`), Vite production build, and deploy to Docker container (`openvms-web-1` running healthy on port 8000).

## Progress & Verification Evidence
- `TASK-1`: `forensic.ts` and `forensic.test.ts` completed with 6 passing tests. Committed in `6108405`.
- `TASK-2`: `ForensicComparisonModal.tsx` and `ForensicComparisonModal.test.tsx` completed with 8 passing tests. Committed in `b61c36c`.
- `TASK-3`: Integrated into `EvidencePlayerModal.tsx` with paused button on video tiles and transport bar. Committed in `d4eb8fd`.
- `TASK-4`: Verified strict TypeScript typed-array indexing (`129c865`), passed all 1,013 tests, Vite build clean, container `openvms-web-1` deployed and serving HTTP 200 on port 8000.

## Next Step
Feature complete. Ready for user inspection or PR creation.
