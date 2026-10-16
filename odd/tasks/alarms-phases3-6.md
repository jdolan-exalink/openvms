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
- Phase 4 wizard implementation is blocked pending a focused product decision on step/copy/form grouping. Existing Phase 2 group coverage is complete; do not invent wizard flow.
- Zone writes must preserve foreign Frigate zones, use `ovms_` ownership, patch and verify, tolerate drift/offline retry, and rebind only within tenant with audit.
- Durable rule evaluation must survive crashes and replicas; event publication must be after commit/outbox-backed.
- HTTPS variable migration and prior Alarms aggregate review are unrelated and out of scope.

## Tasks and acceptance criteria
- [x] **P3-01 — Finish priority inbox UX.** Existing route exposed status/camera filters and actions but did not request the backend's supported priority ordering or expose its cursor pagination. The inbox now defaults to priority order, allows activity-time sorting, and loads subsequent cursor pages. Acceptance: covered by the focused regression test; no duplicate backend or invented requirements.
- [ ] **P3-02 — Contextual alarm video.** Connect alarm context/time to existing camera playback without duplicating snapshot/playback foundations. Acceptance: selected alarm opens the relevant camera/time context; focused web tests.
- [ ] **P3-03 — Real-time ordering and pagination.** Ensure incoming alarm updates coexist correctly with cursor paging. Acceptance: no duplicate/lost/reordered visible items under tested updates; focused tests.
- [ ] **P3-04 — Optional VisionAI alarm pin.** Implement only if source inspection confirms an actual missing pin behavior. Acceptance: evidence-based gap and tests; otherwise record not needed.
- [ ] **P4-01 — Alarm rule wizard.** Pending product decision for wizard steps, copy, and form grouping; existing P2-04 group coverage is done. Acceptance after decision: approved flow implemented and tested.
- [ ] **P5-01 — Persisted camera zones.** Add tenant-safe persistence/API for normalized zones and stable IDs. Acceptance: forward-only migration, RLS, contract/client generation, integration tests.
- [ ] **P5-02 — Safe zone sync.** Implement `ovms_` ownership, per-zone patch/verify, preserve foreign zones, drift detection and offline retry queue. Acceptance: tests cover preservation, partial failures, and convergence; no full-section replacement.
- [ ] **P5-03 — Audited camera rebind.** Support same-tenant camera rebind with an audit record. Acceptance: tenant boundary enforced and history preserved; integration tests.
- [ ] **P6-01 — Durable rule evaluation.** Make event evaluation crash-safe and multi-replica safe/idempotent. Acceptance: committed events eventually evaluate once semantically under retries; integration tests.
- [ ] **P6-02 — Publish-after-commit and hardening.** Move event/rule notifications behind commit/outbox; add end-to-end and latency coverage. Acceptance: rollback cannot emit events, multi-replica behavior tested, measured latency documented.

## Route and estimate
- Route: delegated direct implementation was assigned as one bounded writer; no parallel writers. P3-01 stays one cohesive work unit.
- Trigger evidence: this roadmap spans multiple phases/files; parent performed read-only mapping. This agent has a single delegated P3-01 scope.
- Forecast: approximately 2,000 authored changed lines across phases 3–6, excluding generated files; sliced by work unit. Revisit actual counts as commits accumulate. No PR strategy/chain selected or PR authorized here.

## Progress and evidence
- [x] Tracker created before source changes; full Engram mirror and readback required before code.
- [x] P3-01 strict TDD: RED observed (new test expected `sort=priority`, received no sort); GREEN after implementation; refactor kept the prior array query API intact for maps consumers and isolated paging under a separate infinite-query key. Focused test: `cd apps/web && pnpm exec vitest run src/routes/Alarms.test.tsx --reporter=dot` — 9/9 passed. Typecheck: `cd apps/web && pnpm typecheck` — passed.
- [x] P3-01 authored source diff: 84 lines (73 additions, 6 deletions in source/test files; tracker is separate documentation).
- [x] Work-unit runtime scenario: N/A (browser UI-only behavior; no runtime/hardware boundary exercised).
- [x] Rollback boundary: revert `apps/web/src/api/queries.ts`, `apps/web/src/routes/Alarms.tsx`, and `apps/web/src/routes/Alarms.test.tsx` to remove this behavior; no API/database changes.
- [x] Work-unit commit: `fb4b75e` (`feat(alarms): add priority inbox pagination`) on `feat/https-agent-ingress`.
- Known blocker: Phase 4 wizard needs a product decision. P3-04 is conditional on confirmed gap.

## Next step
Proceed to P3-02 contextual alarm video; the remaining Phase 3 tasks and Phase 4 product-decision gate are unchanged.
