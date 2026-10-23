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
- [x] LP-03 — Pinch-zoom/pan in the single-camera view. Route: delegated direct. Commit `744a5f4`; rollback boundary: revert it (DigitalZoom `touch`/`doubleTapZoom` props, LivePhone SingleCamera, tests).
- [x] LP-04 — Multi-camera phone layout: snapshots + selector (user decision 2026-10-17). Route: delegated direct. Commit `28aba65`; rollback boundary: revert it (LivePhone split into PhoneGrid/PhoneFocus/PhoneLayoutToggle/PhoneSingleCamera, `lib/phoneLayoutPrefs|phoneSnapshot|useHorizontalSwipe|useVisibleSnapshot`, live i18n, tests).
- [x] LP-05 — Review, PR, CI, merge. PR #73 merged 2026-10-17 as `a578c6b8c18fc162036fad3630ee37fcc6e094f9`.
  Status (2026-10-22): PR #73 is closed and merged; review, CI, and merge are complete. Web deployment remains pending separate user authorization. The manual device checklist below remains unrun and requires real iOS and Android standalone PWAs.

## Acceptance
- Standalone PWA on iOS/Android: no content under notch/status bar/home indicator in portrait or landscape.
- Single camera: landscape fills the screen; fullscreen toggle; pinch-zoom/pan with reset.
- Existing phone tests stay green; new behavior covered by component tests.

## Progress
- 2026-10-17: branch `feat/live-phone-pwa` from `origin/main` (`7c1d074`); exploration done (map of PWA config, LivePhone, chrome, tests).

- LP-01 evidence: RED 4 failed (AppShell insets/compact, MobileNav insets/compact) before implementation. GREEN: AppShell + MobileNav + Layout + LivePhone 47/47. `pnpm typecheck` clean, eslint on changed files clean, `git diff --check` clean. Behavior: shell root pads `env(safe-area-inset-top|left|right)`; bottom nav pads left/right and its More sheet pads sides; on /live the bottom bar is `h-14` (was `h-20`) with main reserving `3.5rem`+inset, and the page header is `min-h-11` with tighter margins on phones (desktop unchanged).
- LP-02 evidence: RED 8 failed (new immersive describe in LivePhone.test) before implementation. GREEN: LivePhone 31/31, AppShell + i18n 44/44 together. typecheck clean, eslint clean (after fixing react-hooks/set-state-in-effect), `git diff --check` clean. Behavior: `useImmersive` (Fullscreen API with webkit prefix, CSS fixed inset-0 fallback, refused request keeps CSS mode, system exit returns to previous state, closing the camera exits), landscape auto-fills via `(orientation: landscape)`; section is `fixed inset-0 z-[60]` with all four safe-area insets, header becomes a gradient overlay, picture uses object-fit contain. No orientation lock requested (optional; skipped). Known limitation: in immersive/landscape the REC timeline dock is covered (mode toggle stays in the overlay).
- LP-03 evidence: RED 6 failed (2 ZoomFrame opt-in/double-tap, 4 LivePhone pinch/pan/double tap/reset-on-change) before implementation. GREEN: LivePhone + DigitalZoom 48/48. Final: `pnpm typecheck` clean, eslint on changed files clean, full suite 180 files / 1579 tests passed, `git diff --check` clean. Why zoom was off on phones: not a technical bug; `ZoomFrame` gated touch gestures with `!useIsPhoneDevice()` because phones had their own Live and the multi view swipes between pages with one finger. Now `ZoomFrame` takes `touch` (force on) and `doubleTapZoom` (2.5x toward the tap, second double tap resets); only the single camera view uses them, so the multi list is untouched. Zoom resets on camera change (`resetKey`) and on exit (unmount). Not covered: GRABADO single view (RecTile) has no zoom yet.
- LP-04 decision: research (docs.frigate.video/usage/live, Reolink support "How to Live View Multiple Cameras", UniFi Protect App Store notes, Ring support multi-cam) shows modern apps avoid many simultaneous live streams on phones: snapshot/thumbnail grids, one live camera at a time, swipe between cameras. Chosen: grid (default, 2 cols, 4 in landscape) of snapshots refreshed about every 10 s only while visible (IntersectionObserver; paused when the document is hidden), focus (one live main-quality player plus a snapshot strip; strip beside the picture in landscape), and list (the previous 4-card pages). Header toggle list/grid/focus; layout remembered per tenant+user in localStorage (`openvms.livephone.layout.v1:<tenant>:<user>`, same approach as Vision AI `visionAiPrefs`; there is no server-side preference). Site chips (grid/focus) are a quick switcher; the scope sheet stays. Single view swipes left/right to the next/previous camera in scope (history entry replaced, so back still returns to the multi view); not while zoomed or pinching. GRABADO keeps the list's recorded tiles whatever the layout (toggle hidden). Stream budget: grid 0 live, focus 1 (sub warm under main, as the single view), list <= 4 sub, single 1.
- LP-04 evidence: RED 19 failed (+ `phoneLayoutPrefs.test.ts` import failure) before implementation; the sub-picture `contain` check already passed (see below). GREEN: LivePhone + prefs 67/67. `pnpm typecheck` clean, eslint on changed files clean, full suite 181 files / 1609 tests passed, `git diff --check` clean.
- Review follow-ups folded in: inline sub player object-fit was already `contain` when the picture fills (immersive/landscape) and `fill` inline; regression test added. Immersive/landscape GRABADO: the page dock was covered by the fixed section; the transport now lives in an on-demand overlay opened from a header button ("Mostrar controles de grabación"), LIVE immersive unchanged.
- Known limitation: in phone landscape outside the single view the app shell still shows its desktop rail (AppShell `md:` breakpoint, not LivePhone); LivePhone layouts adapt (4 columns, side strip). The swipe guard for a zoomed picture is belt and braces: ZoomFrame already swallows pointer events while zoomed.

## Manual device checklist (to be run by the parent on real devices; NOT run yet)
- [ ] iOS standalone PWA, portrait: no content under the status bar/notch or home indicator on Live, Events and More sheet.
- [ ] iOS standalone PWA, landscape: nothing under the notch/side insets; single camera fills the screen.
- [ ] Android standalone PWA (portrait and landscape): same checks; system bars not overlapped.
- [ ] Compact Live chrome: bottom bar and header targets remain comfortable to tap (>= 44px) and all nav destinations reachable (including More).
- [ ] Fullscreen toggle in the single camera: enters and exits (Android native fullscreen; iPhone immersive fallback); back button still closes the camera.
- [ ] Pinch-zoom, pan when zoomed, double-tap toggle/reset in the single camera; page swiping in the multi view is unaffected.
- [ ] Grid: snapshots refresh about every 10 s only for tiles on screen; background the app and confirm no requests (DevTools/network); scroll a long grid; tap a tile opens the camera.
- [ ] Layout toggle list/grid/focus persists after reload and per user; grid is the default for a new user.
- [ ] Focus: one live stream only; tapping strip thumbnails switches it; landscape puts the strip beside the picture.
- [ ] Single view: swipe left/right changes camera within the selected site; pinch/pan when zoomed does not change camera; back returns to the multi view.
- [ ] Landscape GRABADO: the controls button reveals the timeline over the picture; LIVE landscape shows no such button.

## Next step
Run the manual device checklist below on real iOS and Android standalone PWAs and record device/OS plus observed outcomes. Web deployment remains pending separate user authorization.
