# UI/UX Refactor Audit

## Objective
Incrementally refactor OpenVMS frontend UX around a three-region enterprise shell while preserving existing routes, APIs, media flows, authorization, and OpenVMS identity.

## Problem and why
The requested redesign spans operational video workflows, administration, navigation, and responsive behavior. The completed baseline audit identifies reusable frontend services, backend contracts, and missing capabilities; phase 1 now establishes the design tokens and shell foundation before later route-specific migrations.

## Scope and constraints
- Authorized scope for this continuation: implement phase 4 only — Video Grid + VideoTile — as a separate work unit with regression tests and tracker evidence. Do not start phases 5–14.
- Preserve all existing APIs and operational behavior (Live/MSE, Playback/HLS, Events, Frigate, LPR, auth/RBAC, views, and settings). Do not add fake controls or unimplemented product capabilities.
- No backend changes, remote access, asset generation, deployment, push, PR, or merge. Preserve `bin/*` and unrelated pending changes; stage only the active phase's files.
- Preserve unrelated pending work in `apps/web/src/routes/Playback.test.tsx` and `odd/tasks/resolve-build-typecheck-errors.md`.
- Keep all claims grounded in repository evidence; distinguish implementation from PRD plans and mark gaps/uncertainties.
- Strict TDD: enabled by current active project instructions; phase 4 code must follow RED → GREEN → REFACTOR.
- Each phase should include focused tests, the web test suite, `pnpm typecheck`, web lint, `git diff --check`, and an observed diff estimate as applicable.
- Delivery strategy: `ask-on-risk` (default); phase 4 is a separate work unit and its estimate will be updated from the observed diff.
- Shell concept labels from the user: `Primary Nav Rail`, `Context Sidebar`, `Main Workspace`.

## Route and acceptance
- Route: delegated direct documentation; multi-file writing requires a bounded writer.
- Acceptance: tracker mirrored in Engram topic `odd/ui-ux-refactor/tasks`; audit document is concise, scan-friendly, and cites exact repository paths; both files read back; `git diff --check` passes; report mirror/check outcomes honestly.

## Tasks
- [x] UIUX-1 — Map current frontend, video/media, API/auth/RBAC, events, saved views, tests, and PRD evidence. Evidence: delegated read-only architecture mapping; no code changed.
- [x] UIUX-2 — Write this tracker and persist its full text to `odd/ui-ux-refactor/tasks`. Evidence: Engram save confirmed on 2026-09-28.
- [x] UIUX-3 — Write and read back `docs/UI_REFACTOR_AUDIT.md` with the current-state summary, invariants, dependencies, phase map, and acceptance checks. Evidence: file read back; all 14 phases present.
- [x] UIUX-4 — Run documentation-only structural checks and report results. Evidence: both paths exist and are non-empty; required headings and tracker items read back; `git diff --check` returned clean. No tests/builds run.
- [x] UIUX-5 — Implement phase 1 (Design tokens + AppShell) with strict TDD, preserving current routes, actions, theme preference, and page rendering; run focused and applicable frontend checks. Commit: `aa7c90a` (`feat(web): add semantic app shell foundation`); implementation, 2 focused tests, full web suite (16 files/71 tests), typecheck, lint, and parent focused spot-check passed. Native review was declined for this candidate. Independent verification then identified UIUX-6 findings below.
- [x] UIUX-6 — Resolve independent-verification findings before closing phase 1: hide the primary-nav landmark together with its responsive-hidden contents, and record that the audit and phase-1 files were committed together in `aa7c90a`. Added focused accessibility regression coverage and reran checks. Evidence: `AppShell.test.tsx` class assertion failed before the fix (landmark had no responsive class); focused test passed after adding `hidden md:flex`; web suite (16 files/71 tests), typecheck, web lint, parent focused spot-check, and `git diff --check` passed. Runtime smoke: N/A; this landmark-only accessibility correction has no runtime workflow boundary. Commit: `c1b7013` (`fix(web): hide empty nav landmark on mobile`); assessment: medium / `under_budget`, so no review was due for this small slice.
- [x] UIUX-7 — Implement phase 2 (Primary Navigation + TopBar) with a visibly compact, icon-first rail, active-route styling, accessible tooltips/labels, and a compact context header; preserve existing route destinations and account/theme/logout behavior; add focused tests. Route: delegated direct, one bounded writer. RED reproduced missing rail, fixed-width theme text overflow, settings route active-state, and `/settings` landing-label defects. GREEN: focused shell tests (2 files/5 tests), full web suite (17 files/74 tests), `pnpm typecheck`, web lint, `git diff --check`, and independent parent spot-check passed. Runtime smoke N/A: no authenticated browser/runtime harness available. Commit: `df3559f` (`feat(web): add compact navigation rail and context header`); assessment: medium / `under_budget`, review not due. Rollback boundary: `apps/web/src/components/AppShell.tsx`, `apps/web/src/components/Layout.tsx`, and `apps/web/src/components/Layout.test.tsx`.
- [x] UIUX-8 — Implement phase 3 (Video View contextual sidebar) by moving/refactoring the current camera-tree experience into the shell's context-sidebar region; preserve camera filtering/grouping/status, drag/drop, saved-view behavior, and mounted media streams; add focused tests. Route: delegated direct, one bounded writer. RED reproduced the missing Context Sidebar and narrow-screen horizontal overflow. GREEN: Live/AppShell focused tests (2 files/9 tests), full web suite (17 files/75 tests), `pnpm typecheck`, web lint, `git diff --check`, and independent parent review/spot-check passed. Regression coverage confirms camera-group collapse preserves the same video element and WebSocket count; responsive shell stacks on mobile and restores fixed sidebar on desktop. Runtime smoke N/A: no authenticated browser/runtime harness available. Commit: `db89038` (`feat(web): move live camera tree into context sidebar`); assessment: medium / `under_budget`, review not due. Rollback boundary: `apps/web/src/components/AppShell.tsx`, `apps/web/src/components/AppShell.test.tsx`, `apps/web/src/components/Layout.tsx`, `apps/web/src/routes/Live.tsx`, and `apps/web/src/routes/Live.test.tsx`.
- [x] UIUX-9 — Implement phase 4 (Video Grid + VideoTile) as an operational video-first workspace using existing MSE streams and current selection/layout/drag behavior; preserve media lifecycle, camera actions, permissions, accessibility, and existing saved-view semantics. Route: delegated direct with prep reading included in the writer task. Strict TDD. RED: new 3×2 layout assertion failed before the control existed. GREEN: focused Live/grid/MSE tests — 31 passed; full web suite — 78 passed; `pnpm typecheck`, web lint, `git diff --check`, and independent parent review/spot-check passed. Rectangular layouts use existing columns+cells saved-view representation; switching to 3×2 keeps the same mounted video element and WebSocket count. Unsupported asymmetric layouts, custom editor, timeline/playback/rotation controls were not added. Runtime smoke N/A: no authenticated browser harness available. Commit: `be8c795` (`feat(web): expand live video grid layouts`); 145 changed lines in assessment, medium / `under_budget`, review not due. Rollback boundary: `apps/web/src/routes/Live.tsx`, `apps/web/src/routes/Live.test.tsx`, `apps/web/src/lib/liveGrid.ts`, and `apps/web/src/lib/liveGrid.test.ts`.

## Phase map from the requested scope
1. Design tokens + AppShell
2. Primary Navigation + TopBar
3. Video View sidebar
4. Video Grid + VideoTile
5. Timeline + playback
6. Saved views + layouts
7. Search
8. Events + Alarms
9. Devices
10. Camera settings drawer
11. Rules
12. Users/RBAC
13. System
14. Polish, performance, and responsive behavior

## Progress and next step
Audit and phase-1 implementation are committed together in `aa7c90a`; the audit is not pending as a separate commit. The user explicitly authorized phase 4 on 2026-09-29 after phases 2–3 were already complete. Phases 1–4 are complete: phase 2 commit `df3559f`, phase 3 commit `db89038`, phase 4 commit `be8c795`. Each phase was assessed medium / `under_budget`, so no native review was due. Preserve unrelated pending Playback changes; next is phase 5 (Timeline + playback) after authorization.

## Phase 1 forecast and commit evidence
- Forecast: fewer than 400 authored changed lines; phase boundary is tokens + shared shell only. Re-estimate honestly from the diff.
- Runtime smoke: not run; suggested scenario is sign in, visit `/live`, `/playback`, and `/settings`, verify route content/nav remains visible, and toggle light/dark theme twice to confirm persistence.
- Rollback boundary: `apps/web/src/components/AppShell.tsx`, `apps/web/src/components/AppShell.test.tsx`, `apps/web/src/components/Layout.tsx`, and `apps/web/src/index.css` only.
- TDD evidence: focused test was RED before implementation because `./AppShell` did not exist (Vitest failed to resolve the import); `pnpm --filter web exec vitest run src/components/AppShell.test.tsx` GREEN: 1 focused file, 2 tests passed.
- Checks: `pnpm --filter web test` — 16 files / 71 tests passed; `pnpm typecheck` — passed; `pnpm --filter web lint` — passed; `git diff --check` — passed.
- Observed authored line estimate: 136 changed lines (additions plus deletions across phase paths), below 400.
- Phase-1 commit: `aa7c90a`.
- UIUX-6 follow-up commit: `c1b7013`.
