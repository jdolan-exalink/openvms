# Live and fisheye review follow-ups

## Objective

Fix six non-blocking reliability findings from the approved native reviews of commits `bb81c6f` (FGI) and `26b6856`+`1e05a29` (LGS+LSQ) before opening the PR.

## Problem

The reviewer reported inferential defects in quality overrides, player session reuse, and fisheye viewer state. They were not verified when reported.

## Why

Two of them leave streams open indefinitely or force reconnects; the others break saved fisheye state or user interaction.

## Scope and constraints

- Only the six findings below. Each fix starts with a failing regression (strict TDD); if the regression cannot be made to fail, the finding is recorded as not reproduced and no source change is made.
- Preserve existing behavior and assertions of FGI, LGS, and LSQ.
- Legacy `4_quadrants` migration default: treat it as the current default mode (`virtual_ptz`) and drop grid tiles that reference fisheye views that no longer exist.
- No push, PR, or remote operations.
- Effective TDD mode: enabled by project instructions; runner `pnpm test` (vitest) in `apps/web`.

## Tasks

- [x] RF-01 — Clear a camera's `manual` quality override when all of its grid identities are removed, so that a later maximize still promotes to main (`apps/web/src/routes/Live.tsx` ~769, ~738).
- [ ] RF-02 — On unmaximize, restore each identity's own saved quality instead of installing a permanent `sub` focus-restore override; no lingering dual session (`apps/web/src/routes/Live.tsx` ~725).
- [x] RF-03 — Clear the sticky `discardWhenReleased` flag when the same camera:quality session is acquired again, so the next full release moves it to WARM (`apps/web/src/lib/live/PlayerSessionManager.ts` ~96).
- [ ] RF-04 — Update the fisheye Cancel baseline after "Guardar vista" so Cancel returns to the saved mode (`apps/web/src/components/fisheye/FisheyeViewer.tsx` ~69).
- [ ] RF-05 — Do not snap user pan/zoom on targeted virtual-view tiles back on every render; resync only when the target view itself changes (`apps/web/src/components/fisheye/FisheyeViewer.tsx` ~210).
- [ ] RF-06 — Migrate legacy `4_quadrants` configs to a supported mode and handle stale quadrant view references in saved grids (`apps/web/src/lib/fisheye/storage.ts` ~78, `types.ts`, Live grid restore).

## Acceptance criteria

- Each task has a regression that failed before the fix (or a recorded non-reproduction) and passes after.
- `pnpm test`, `pnpm typecheck` pass; `pnpm lint` adds no diagnostics over the baseline (13 errors, 1 warning).
- One Conventional Commit per task on `feat/fisheye-grid-camera-indicators`.

## Route

- Delegated direct: fixes span 4+ non-trivial files (Live.tsx, PlayerSessionManager.ts, FisheyeViewer.tsx, storage.ts) → one bounded writer.

## Progress and evidence

- 2026-10-13: document created from review findings R3-stale-manual-override, R3-focus-restore-forces-sub, R3-discard-flag-sticky, R3-001, R3-002, R3-003.
- RF-03: RED `expected close not to be called, got ["user-removed"]` (new test in PlayerSessionManager.test.ts). Fix: acquire() clears `discardWhenReleased`. GREEN: `pnpm vitest run src/lib/live/PlayerSessionManager.test.ts` 2/2; `pnpm typecheck` ok.
- RF-01: RED `expected <video> to have a length of 2 but got 1` (Live.test.tsx, remove+replace manually overridden camera, maximize >5s). Fix: tiles effect deletes the override entry when no identity remains. GREEN: Live.test.tsx 62/62; typecheck ok; lint still 13 errors/1 warning.

Repository-relative file: `odd/tasks/live-fisheye-review-followups.md`
