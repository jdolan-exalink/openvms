# Live view on phones (PWA)

## Objective
Make the phone Live view feel like a modern camera app when installed as a PWA: respect device safe areas, give video more room, support landscape/fullscreen and pinch-zoom on a single camera, and replace the current multi-camera stack with a better pattern.

## Problem and why
Phone Live (`apps/web/src/components/LivePhone.tsx`) is a single-column stack of four 16:9 cards per swipe page. In standalone PWA mode the top/side safe areas are not handled (`viewport-fit=cover` + `black-translucent` status bar), the bottom nav (~80–100px) and in-view controls (~90px) consume a large share of the screen, there is no landscape or fullscreen experience, and the single-camera view has no pinch-zoom. The user asked (2026-10-17) to fix these and to replace the multi-camera stack with a better solution "like modern camera apps".

## Scope
- Safe areas (top/left/right/bottom) for the phone Live view and app chrome in standalone mode; reclaim vertical space on Live (compact/hidden chrome).
- Landscape and fullscreen for the single-camera view (Fullscreen API where available; CSS immersive fallback for iOS).
- Pinch-zoom and pan in the single-camera view (reuse the existing `DigitalZoom` behavior from tablet/desktop).
- Multi-camera phone layout redesign — pattern to be chosen by the user after research.
- Out of scope: desktop/tablet grid, stream-quality policy (phones keep sub-streams for multi-view, main for single).

## Constraints
- Strict TDD (user global config). Runner: `cd apps/web && pnpm exec vitest run <file>`; `pnpm typecheck`; eslint on changed files.
- UI copy in neutral professional Spanish; code/comments in English.
- No real-device test harness exists (BC-06 deferred); verification is component tests plus a manual device checklist recorded here.
- Delivery: `ask-on-risk`, cached `stacked-to-main`.

## Tasks
- [x] LP-01 — Safe areas and space on phone Live. Route: delegated direct. Commit `7058bc6`; rollback boundary: revert it (touches AppShell, MobileNav, Layout and their tests only).
- [x] LP-02 — Landscape and fullscreen single-camera view. Route: delegated direct. Commit `a7cc79b`; rollback boundary: revert it (LivePhone, `lib/useImmersive.ts`, live i18n, LivePhone tests).
- [ ] LP-03 — Pinch-zoom/pan in the single-camera view. Route: delegated direct.
- [ ] LP-04 — Multi-camera phone layout redesign (pending research + user decision).
- [ ] LP-05 — Review, PR, CI, merge, deploy (deploy only with user confirmation).

## Acceptance
- Standalone PWA on iOS/Android: no content under notch/status bar/home indicator in portrait or landscape.
- Single camera: landscape fills the screen; fullscreen toggle; pinch-zoom/pan with reset.
- Existing phone tests stay green; new behavior covered by component tests.

## Progress
- 2026-10-17: branch `feat/live-phone-pwa` from `origin/main` (`7c1d074`); exploration done (map of PWA config, LivePhone, chrome, tests).

- LP-01 evidence: RED 4 failed (AppShell insets/compact, MobileNav insets/compact) before implementation. GREEN: AppShell + MobileNav + Layout + LivePhone 47/47. `pnpm typecheck` clean, eslint on changed files clean, `git diff --check` clean. Behavior: shell root pads `env(safe-area-inset-top|left|right)`; bottom nav pads left/right and its More sheet pads sides; on /live the bottom bar is `h-14` (was `h-20`) with main reserving `3.5rem`+inset, and the page header is `min-h-11` with tighter margins on phones (desktop unchanged).
- LP-02 evidence: RED 8 failed (new immersive describe in LivePhone.test) before implementation. GREEN: LivePhone 31/31, AppShell + i18n 44/44 together. typecheck clean, eslint clean (after fixing react-hooks/set-state-in-effect), `git diff --check` clean. Behavior: `useImmersive` (Fullscreen API with webkit prefix, CSS fixed inset-0 fallback, refused request keeps CSS mode, system exit returns to previous state, closing the camera exits), landscape auto-fills via `(orientation: landscape)`; section is `fixed inset-0 z-[60]` with all four safe-area insets, header becomes a gradient overlay, picture uses object-fit contain. No orientation lock requested (optional; skipped). Known limitation: in immersive/landscape the REC timeline dock is covered (mode toggle stays in the overlay).

## Manual device checklist (to be run by the parent on real devices; NOT run yet)
- [ ] iOS standalone PWA, portrait: no content under the status bar/notch or home indicator on Live, Events and More sheet.
- [ ] iOS standalone PWA, landscape: nothing under the notch/side insets; single camera fills the screen.
- [ ] Android standalone PWA (portrait and landscape): same checks; system bars not overlapped.
- [ ] Compact Live chrome: bottom bar and header targets remain comfortable to tap (>= 44px) and all nav destinations reachable (including More).
- [ ] Fullscreen toggle in the single camera: enters and exits (Android native fullscreen; iPhone immersive fallback); back button still closes the camera.
- [ ] Pinch-zoom, pan when zoomed, double-tap toggle/reset in the single camera; page swiping in the multi view is unaffected.

## Next step
LP-01..LP-03 (writer) in parallel with LP-04 research.
