# Web lint baseline cleanup

## Objective

Make `pnpm lint` in `apps/web` pass so the CI "Web (lint, typecheck, tests, build)" job runs its typecheck, test, and build steps again.

## Problem

CI runs `pnpm lint` first; 13 pre-existing errors and 1 warning make it fail, so typecheck, tests, and build are skipped on every push (failing on `main` since at least `748fe6a`).

## Why

Without those steps CI verifies nothing for the web app; #32 and #33 were only tested locally.

## Scope and constraints

- Fix the 14 current diagnostics with real code changes; no `eslint-disable` comments and no rule configuration changes unless a rule is demonstrably wrong for the code (report before doing so).
- No behavior changes. Hook-rule fixes must preserve existing behavior and tests (FisheyeViewer FGI-09/LSQ-06/RF-04/RF-05 target-view and cancel behavior; EvidencePlayerModal and ForensicComparisonModal behavior).
- Effective TDD mode: enabled by project instructions; runner `pnpm test` (vitest) in `apps/web`. Pure lint fixes with unchanged behavior are covered by existing tests; add a regression only where a hook refactor changes control flow without existing coverage.
- No push, PR, or remote operations without explicit user authorization.

## Diagnostics (from `pnpm exec eslint .` on `1e0b1ec`)

- `src/lib/fisheye/math.ts:174` prefer-const (`phi`).
- `src/lib/fisheye/storage.ts:153,164` no-empty.
- `src/components/EvidencePlayerModal.tsx:667` no-empty; `:785` react-hooks/refs (ref read during render).
- `src/components/ForensicComparisonModal.tsx:47` no-unused-vars (`_onReprocess`); `:75,:84` react-compiler preserve-manual-memoization; `:196` impure function during render.
- `src/components/fisheye/FisheyeViewer.tsx:189,615` react-hooks/refs; `:217,:224` set-state-in-effect.
- `src/components/LiveExplorer.tsx:743` warning: unnecessary `useMemo` dependency `fisheyeVersion`.

## Tasks

- [x] WL-01 — Mechanical fixes: math.ts, storage.ts, EvidencePlayerModal no-empty, ForensicComparisonModal unused var, LiveExplorer useMemo dependency (verify `fisheyeVersion` is really unnecessary before removing it; if it is used to force recomputation, restructure instead).
- [ ] WL-02 — FisheyeViewer react-hooks fixes (refs in render, set-state-in-effect).
- [ ] WL-03 — EvidencePlayerModal ref read during render.
- [ ] WL-04 — ForensicComparisonModal memoization and impure call during render.

## Acceptance criteria

- `pnpm lint` exits 0 with 0 errors and 0 warnings.
- `pnpm test`, `pnpm typecheck`, `pnpm build` pass.
- One Conventional Commit per task on `fix/web-lint-baseline`.

## Route

- Delegated direct: 6 files, 3 with non-trivial hook refactors → one bounded writer.

## Progress and evidence

- 2026-10-13: document created; branch `fix/web-lint-baseline` from `origin/main` (`1e0b1ec`).
- WL-01 done: 8 diagnostics fixed (math prefer-const; storage 2x and EvidencePlayerModal:667 no-empty via explanatory comments inside catch, since swallow semantics are intended; ForensicComparisonModal unused `_onReprocess` destructure removed; LiveExplorer `fisheyeVersion` was an intentional refresh trigger, so the stale useMemo was replaced by a per-render read so the context change still re-reads localStorage). Tests (existing, pass-first): src/lib/fisheye, LiveExplorer, EvidencePlayerModal, ForensicComparison, routes/Live: 6 files / 98 tests pass; typecheck passes. No characterization tests needed.

Repository-relative file: `odd/tasks/web-lint-baseline.md`
