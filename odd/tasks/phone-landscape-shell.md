# Phone landscape shell and swipe pointer leak

## Objective
Keep the mobile app shell on phones in landscape, and fix the stuck-swipe pointer leak in the phone single-camera view.

## Problem and why
- Shell layout is chosen only by viewport width (`md:` in `AppShell.tsx`, `MobileNav.tsx`, `Layout.tsx`). A phone in landscape is wider than `md` (768px), so it gets the desktop nav rail and context sidebar instead of the bottom nav (follow-up from PR #73).
- `useHorizontalSwipe` tracks active pointers in a Set cleared only on `pointerup`/`pointercancel`. A pointer released outside the element (no capture) or a lost event leaves a stale id, so every later gesture is treated as multi-touch and swipe navigation stops (review advisory R3-swipe-pointer-leak / R3-002 on PR #73).
- User asked (2026-10-17) to fix both.

## Scope
- Shell regions only (primary nav rail, context sidebar frame, bottom nav, main workspace sheet, page header): on phone devices (`useIsPhoneDevice`, stable across rotation) always use the mobile shell; desktop/tablet unchanged. Page content keeps its own breakpoints.
- `useHorizontalSwipe`: no stale pointers (pointer capture and/or leave/lost-capture cleanup, self-heal on a fresh primary pointer).

## Constraints
Strict TDD (user global config); runner `cd apps/web && pnpm exec vitest run <file>`, `pnpm typecheck`, eslint. Route: delegated direct (writer trigger: 2+ non-trivial files). Delivery: one PR, `stacked-to-main`; deploy authorized by the user after merge.

## Tasks
- [x] PLS-01 — Swipe pointer leak fix. Commit `8a62920`; rollback boundary: revert that commit. Route: delegated direct.
- [ ] PLS-02 — Phone shell stays mobile in landscape.
- [ ] PLS-03 — Review, PR, CI, merge, deploy web.

## Progress
- 2026-10-17: branch `fix/phone-landscape-shell` from `origin/main` (`a578c6b`).
- PLS-01 evidence: RED `vitest run src/lib/useHorizontalSwipe.test.tsx` failed (stale pointer 1 made the next swipe a pinch); fix resets the tracked set on a primary pointerdown (no pointer capture, because ZoomFrame captures pointers itself and outer capture would steal its events). GREEN: that file + LivePhone.test.tsx 66 passed; `pnpm typecheck` and eslint clean.
