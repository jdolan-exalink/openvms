# Alarms Phases 3–6 — Attention Center, UX, zones and hardening

## Objective
Complete the approved Alarms roadmap phases 3–6 in bounded work units, then prepare (but do not publish) reviewable PR slices.

## Problem and rationale
Phase 0 and Phase 2 established the alarm and rules foundation. Remaining work is the operator-facing attention experience, a rule wizard, production-safe Frigate zone ownership/synchronization and camera identity rebind, and durable event/rule processing.

## Scope and constraints
- Authorized: local implementation through phases 3–6, work-unit commits, and preparing PRs. No push, PR creation, merge, deploy, or remote operations.
- Branch: `feat/https-agent-ingress`, starting at `00eeb45`; do not change branches.
- Strict TDD: enabled by user global config; web runner `cd apps/web && pnpm exec vitest run <file> --reporter=dot`; API runner as applicable `GOCACHE=/tmp/openvms-go-build-cache go test ...`.
- Reuse existing alarm priority/cursor/actions, correlated events, basic realtime, maps alarm panel, and camera snapshot/playback. Do not duplicate these foundations.
- Phase 4 wizard: user approved the four-step flow: scope (cameras/groups) → conditions → actions → review/activation. Include an activate/deactivate checkbox, enabled by default. Preserve existing Phase 2 group coverage; do not duplicate it.
- Zone writes must preserve foreign Frigate zones, use `ovms_` ownership, patch and verify, tolerate drift/offline retry, and rebind only within tenant with audit.
- Durable rule evaluation must survive crashes and replicas; event publication must be after commit/outbox-backed.
- HTTPS variable migration and prior Alarms aggregate review are unrelated and out of scope.

## Tasks and acceptance criteria
- [x] **P3-01 — Finish priority inbox UX.** Existing route exposed status/camera filters and actions but did not request the backend's supported priority ordering or expose its cursor pagination. The inbox now defaults to priority order, allows activity-time sorting, and loads subsequent cursor pages. Acceptance: covered by the focused regression test; no duplicate backend or invented requirements.
- [x] **P3-02 — Contextual alarm video.** Added a permission-gated action that opens the existing playback route for the alarm camera at its latest correlated event timestamp (falling back to event start). The action clarifies that an event capture does not guarantee retained video. Acceptance: covered by focused tests; no playback backend or retention behavior duplicated or promised.
- [x] **P3-03 — Real-time ordering and pagination.** Verified the existing app-wide feed invalidates the alarms prefix for `alarm.created`/`alarm.updated` and on reconnect; the priority infinite query refetches pages from a newly-derived cursor while retaining filters/sort. Added regression coverage; no runtime behavior change was needed.
- [x] **P3-04 — Optional VisionAI alarm pin.** No change needed: VisionAI already supports the `alarmas` source and slot pin/unpin controls scoped to source/layout. The focused regression suite confirms those behaviors; avoid duplicating them.
- [x] **P4-01 — Alarm rule wizard.** Implemented the approved four steps: scope (cameras/groups), conditions, actions, and review/activation. Includes an activate/deactivate checkbox enabled by default. Reuses existing CRUD, templates, simulation, validation, and group coverage; no duplicate coverage UI. Acceptance: step navigation, step-level validation, persistence, default activation, and edit-safe behavior covered by focused tests.
- [ ] **P5-01 — Persisted camera zones.** Add tenant-safe persistence/API for normalized zones and stable IDs. Acceptance: forward-only migration, RLS, contract/client generation, integration tests.
- [ ] **P5-02 — Safe zone sync.** Implement `ovms_` ownership, per-zone patch/verify, preserve foreign zones, drift detection and offline retry queue. Acceptance: tests cover preservation, partial failures, and convergence; no full-section replacement.
- [ ] **P5-03 — Audited camera rebind.** Support same-tenant camera rebind with an audit record. Acceptance: tenant boundary enforced and history preserved; integration tests.
- [ ] **P6-01 — Durable rule evaluation.** Make event evaluation crash-safe and multi-replica safe/idempotent. Acceptance: committed events eventually evaluate once semantically under retries; integration tests.
- [ ] **P6-02 — Publish-after-commit and hardening.** Move event/rule notifications behind commit/outbox; add end-to-end and latency coverage. Acceptance: rollback cannot emit events, multi-replica behavior tested, measured latency documented.

## Route and estimate
- Route: delegated direct implementation was assigned as one bounded writer; no parallel writers. P3-01 and P3-02 are separate implementation units; P3-03 closes as a test/evidence unit because existing runtime behavior is sufficient.
- Trigger evidence: this roadmap spans multiple phases/files; parent performed read-only mapping. Each delegated unit stays separately bounded.
- Forecast: approximately 2,000 authored changed lines across phases 3–6, excluding generated files; sliced by work unit. Revisit actual counts as commits accumulate. No PR strategy/chain selected or PR authorized here.

## Progress and evidence
- [x] Tracker created before source changes; full Engram mirror and readback required before code.
- [x] P3-01 strict TDD: RED observed (new test expected `sort=priority`, received no sort); GREEN after implementation; refactor kept the prior array query API intact for maps consumers and isolated paging under a separate infinite-query key. Focused test: `cd apps/web && pnpm exec vitest run src/routes/Alarms.test.tsx --reporter=dot` — 9/9 passed. Typecheck: `cd apps/web && pnpm typecheck` — passed.
- [x] P3-01 authored source diff: 84 lines (73 additions, 6 deletions in source/test files; tracker is separate documentation).
- [x] Work-unit runtime scenario: N/A (browser UI-only behavior; no runtime/hardware boundary exercised).
- [x] Rollback boundary: revert `apps/web/src/api/queries.ts`, `apps/web/src/routes/Alarms.tsx`, and `apps/web/src/routes/Alarms.test.tsx` to remove this behavior; no API/database changes.
- [x] Work-unit commit: `fb4b75e` (`feat(alarms): add priority inbox pagination`) on `feat/https-agent-ingress`.
- [x] P3-02 strict TDD: RED observed (recording-viewer test could not find the playback link); GREEN after implementation. Focused test: `cd apps/web && pnpm exec vitest run src/routes/Alarms.test.tsx --reporter=dot` — 11/11 passed. Typecheck: `cd apps/web && pnpm typecheck` — passed. `git diff --check` — passed.
- [x] P3-02 authored source diff: 74 lines (72 additions, 2 deletions; tracker is separate documentation).
- [x] P3-02 runtime harness: N/A — browser tests verify the playback link's camera/time target and permission gate; no Frigate recording runtime is provided to assert actual retained video availability.
- [x] P3-02 rollback boundary: revert `apps/web/src/routes/Alarms.tsx` and `apps/web/src/routes/Alarms.test.tsx`; no API/database changes.
- [x] P3-02 work-unit commit: `6c15cc5` (`feat(alarms): link alarms to contextual playback`) on `feat/https-agent-ingress`.
- [x] P3-03 validation: test-first behavior check required no product fix. `cd apps/web && pnpm exec vitest run src/lib/realtime.test.tsx --reporter=dot` — 16/16 passed. `cd apps/web && pnpm exec eslint src/lib/realtime.test.tsx` and `cd apps/web && pnpm typecheck` passed; `git diff --check` passed.
- [x] P3-03 authored test diff: 78 lines (74 additions, 4 deletions; tracker is separate documentation).
- [x] P3-03 runtime harness: N/A — controlled WebSocket/query test verifies invalidation and refreshed pages; no external runtime dependency is needed.
- [x] P3-03 rollback boundary: revert `apps/web/src/lib/realtime.test.tsx` and this tracker entry; no production source changed.
- [x] P3-03 evidence commit: `1d326c5` (`test(realtime): verify alarm cursor refresh`) on `feat/https-agent-ingress`.
- [x] P3-04 evidence: `apps/web/src/routes/VisionAI.tsx` already maps the `alarmas` source and exposes slot pin/unpin controls; `apps/web/src/routes/VisionAI.test.tsx` covers alarm source selection and pin/unpin behavior. `cd apps/web && pnpm exec vitest run src/routes/VisionAI.test.tsx --reporter=dot` — 32/32 passed (4 non-fatal React `act(...)` warnings). No production change was needed.
- [x] P3-04 tracker work-unit commit: `7d4d355` (`docs(alarms): close phase 3 pin gap`) on `feat/https-agent-ingress`.
- [x] P4-01 strict TDD: RED observed for the first wizard navigation test (expected step 1 label was absent); RED observed for paired schedule validation (expected blocking alert was absent); RED observed for review summary (expected summary was absent). GREEN after implementation. Focused test: `cd apps/web && pnpm exec vitest run src/routes/Rules.test.tsx --reporter=dot` — 19/19 passed. Typecheck: `cd apps/web && pnpm typecheck` — passed. ESLint: `cd apps/web && pnpm exec eslint src/routes/Rules.tsx src/routes/Rules.test.tsx` — passed. `git diff --check` — passed.
- [x] P4-01 authored work-unit diff: 145 lines (138 additions, 7 deletions across source and tests; tracker evidence is separate).
- [x] P4-01 runtime harness: N/A — browser/UI behavior is covered by focused component tests; no rule-processing runtime/device boundary was exercised.
- [x] P4-01 rollback boundary: revert `apps/web/src/routes/Rules.tsx` and `apps/web/src/routes/Rules.test.tsx` to remove the wizard without affecting API or database behavior.
- [x] P4-01 work-unit commit: `a037cd2` (`feat(alarms): add guided rule activation steps`) on `feat/https-agent-ingress`.
- [x] P4-01 post-commit assessment: risk `medium`, `review_due=true` (`slice_budget_reached`) against `00eeb45`; parent owns any required native review lifecycle.
## Next step
Implement P5-01 through P5-03 in bounded work units.
