# UI/UX Refactor Audit

## Objective
Incrementally refactor OpenVMS frontend UX around a three-region enterprise shell while preserving existing routes, APIs, media flows, authorization, and OpenVMS identity.

## Problem and why
The requested redesign spans operational video workflows, administration, navigation, and responsive behavior. The completed baseline audit identifies reusable frontend services, backend contracts, and missing capabilities; phase 1 now establishes the design tokens and shell foundation before later route-specific migrations.

## Scope and constraints
- Authorized scope: implement phase 1 only — Design tokens + AppShell — and its required regression tests and tracker evidence. Do not start phases 2–14 without routing them as separate work units.
- Preserve all existing APIs and operational behavior (Live/MSE, Playback/HLS, Events, Frigate, LPR, auth/RBAC, views, and settings). Do not add fake controls or unimplemented product capabilities.
- No backend changes, remote access, asset generation, deployment, push, PR, or merge. Preserve `bin/*` and unrelated pending changes; stage only phase-1 files.
- Preserve unrelated pending work in `apps/web/src/routes/Playback.test.tsx` and `odd/tasks/resolve-build-typecheck-errors.md`.
- Keep all claims grounded in repository evidence; distinguish implementation from PRD plans and mark gaps/uncertainties.
- Strict TDD: enabled by current active project instructions; phase-1 code must follow RED → GREEN → REFACTOR.
- Phase-1 verification should include focused shell/layout tests, the web test suite, `pnpm typecheck`, and web lint as applicable.
- Delivery strategy: `ask-on-risk` (default); forecast phase 1 below 400 authored changed lines, update from observed diff.
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
Audit and phase-1 implementation are committed together in `aa7c90a`; the audit is not pending as a separate commit. The user explicitly authorized phase 1 on 2026-09-29. Native review consent for that candidate was declined; independent verification findings were addressed in `c1b7013`. Phase 1 is now complete. Preserve unrelated pending Playback changes; they remain outside both UIUX commits.

## Phase 1 forecast and commit evidence
- Forecast: fewer than 400 authored changed lines; phase boundary is tokens + shared shell only. Re-estimate honestly from the diff.
- Runtime smoke: not run; suggested scenario is sign in, visit `/live`, `/playback`, and `/settings`, verify route content/nav remains visible, and toggle light/dark theme twice to confirm persistence.
- Rollback boundary: `apps/web/src/components/AppShell.tsx`, `apps/web/src/components/AppShell.test.tsx`, `apps/web/src/components/Layout.tsx`, and `apps/web/src/index.css` only.
- TDD evidence: focused test was RED before implementation because `./AppShell` did not exist (Vitest failed to resolve the import); `pnpm --filter web exec vitest run src/components/AppShell.test.tsx` GREEN: 1 focused file, 2 tests passed.
- Checks: `pnpm --filter web test` — 16 files / 71 tests passed; `pnpm typecheck` — passed; `pnpm --filter web lint` — passed; `git diff --check` — passed.
- Observed authored line estimate: 136 changed lines (additions plus deletions across phase paths), below 400.
- Phase-1 commit: `aa7c90a`.
- UIUX-6 follow-up commit: `c1b7013`.
