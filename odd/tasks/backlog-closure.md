# Close the Verified Product Backlog

Reconcile and complete the real remaining backlog as independently reviewable work units and ordered PRs. This recovery document is the source of truth for sequencing; existing trackers remain historical evidence until reconciled against the current branch.

## Why

The user authorized liquidating the actual pending backlog, but prior task trackers contain stale checkboxes and next-step notes. Start by validating the current mTLS/provisioning branch, then establish the real remaining work without treating old claims or hardware/remote checks as proof.

## Scope and constraints

- Authorized outcome: implement all genuinely pending backlog and deliver independent PRs merged in order.
- Initial authorization was limited to creating this recovery document and locally verifying the then-current branch; that was a historical boundary, not a restriction on later authorized backlog work. The user has since authorized all pending backlog implementation and independent PRs. Local source/documentation changes and work-unit commits are within scope; push, PR creation, merge, and any remote execution remain pending explicit credential/destination/operation confirmation as applicable.
- Future implementation proceeds task-by-task; strict TDD is enabled by the user-provided AGENTS.md. Each source task must observe RED, then GREEN, then REFACTOR with its exact runner.
- Route: delegated direct for substantial implementation; mapping/preparation and multi-file writers are delegated because those triggers apply. This unit itself is documentation-only; no model delegation in the initial bounded unit.
- Delivery: `auto-chain`, `stacked-to-main`, based on the user's request for independent PRs merged in order. Make coherent feature boundaries, not artificial tiny slices. Existing mTLS remainder's prior size exception applies only to that remainder.
- Forecast: at least several thousand authored changed lines across the full roadmap (ONVIF alone spans tasks 02–19); refine per task before implementation. No size exception is implied for other features.
- External camera, Frigate, hardware, and remote-host acceptance remains blocked until destination/operation/credentials are explicitly authorized and the relevant hardware is available. Never mark those checks complete without evidence.

## Verification of current branch (initial task)

Run locally, in foreground, in this order. Do not run integration tests here because they may mutate a running database.

1. `GOCACHE=/tmp/openvms-go-build-cache go build ./...`
2. `GOCACHE=/tmp/openvms-go-build-cache go vet ./...`
3. `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/agent/... ./internal/agentstatus ./internal/control ./internal/provision ./internal/api ./apps/edge-agent`
4. `cd apps/web && pnpm typecheck`

Record exact observed outcomes below. Failures are evidence, not authorization to fix source in this unit.

## Work units

The list is an initial inventory; reconcile each tracker and current code before checking off or starting implementation.

- [x] BC-01 — Verify current mTLS/provisioning branch; capture baseline checks. Acceptance: outcomes recorded. Evidence: current branch verification at 5893d2e is recorded below; the initial baseline remains historical evidence.
- [ ] BC-02 — Reconcile stale backlog trackers and SSH provisioning/install/update checks. Acceptance: stale P2/alarm claims and mTLS next-step text are reconciled; T2 build/deployment verification is either evidenced locally or explicitly blocked; no remote execution without authorization.
- [ ] BC-03 — Maps acceptance and performance. Acceptance: resolve M-W27 performance and stale M-W20/alarm S2-3b–d states against current implementation and tests; record any remaining manual/browser acceptance separately.
- [ ] BC-04 — ONVIF camera management, work units 02–19. Acceptance: derive cohesive independent PR slices from `odd/tasks/onvif-camera-management.md`, preserve its requirements and per-unit verification; do not assume readiness from roadmap checkboxes.
- [ ] BC-05 — Frigate configuration editor FC-5. Acceptance: reconcile FC-5 against current code/tests, implement and verify only actual remaining requirements; keep real Frigate/hardware acceptance pending until authorized and available.
- [ ] BC-06 — Remaining device lifecycle and PWA acceptance. Acceptance: reconcile device/agent trackers and test the outstanding local PWA behavior; report unavailable browser/device/hardware proof honestly.
- [ ] BC-07 — Final backlog reconciliation. Acceptance: every source tracker is synchronized with observed outcomes, blocked external acceptance is explicit, and PR slices/dependencies are documented in merge order.

## Progress and evidence

- Baseline: clean worktree, branch `feat/agent-mtls-agent`, HEAD `3cccae7` (observed before verification).
- Initial task route: direct documentation-only edit; no source modifications authorized.
- Verification results (2026-10-11):
  - `GOCACHE=/tmp/openvms-go-build-cache go build ./...` — PASS (exit 0; required escalation because the sandbox initially denied writes to `/root/go/pkg/mod/cache`).
  - `GOCACHE=/tmp/openvms-go-build-cache go vet ./...` — PASS (exit 0).
  - `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/agent/... ./internal/agentstatus ./internal/control ./internal/provision ./internal/api ./apps/edge-agent` — PASS (all eight packages; the first sandboxed run failed because loopback sockets and route netlink were denied, then passed on the approved rerun).
  - From `apps/web`: `pnpm typecheck` — PASS (`tsc --noEmit`, exit 0).
  - No integration tests or remote/hardware checks were run.
- PR order: stacked-to-main; define coherent boundaries after reconciliation. No PR/remote actions authorized in this initial unit.

### Current verification (HEAD `5893d2e`, 2026-10-11)

- Worktree was clean before this documentation update.
- `GOCACHE=/tmp/openvms-go-build-cache go build ./...` — PASS.
- `GOCACHE=/tmp/openvms-go-build-cache go test -race ./...` — PASS after an approved rerun; the initial sandboxed run was blocked by socket/netlink restrictions.
- From `apps/web`, `pnpm test` — PASS (130 files, 964 tests).
- From `apps/web`, `pnpm build` — PASS (TypeScript and Vite/PWA build); Vite reported a chunk larger than 500 KB.
- Scoped Go vet and typecheck — PASS (per parent verification report; no new full `go vet ./...` or `pnpm typecheck` run recorded here).
- No integration, remote, hardware, or deployment acceptance was established. The remaining mTLS branch lint correction (BC01-L) is complete: candidate-diff Go lint passes; full Go lint before correction reported 130 findings total, and web lint reports unrelated existing failures (details in `odd/tasks/agent-mtls.md`).
- BC-01 commit `5893d2e`: risk `passive`, reason `non_executable_only`; reviewed boundary advances to `5893d2e`.
- Rollback boundary: this documentation-only reconciliation, removable by reverting the changes to `odd/tasks/backlog-closure.md` and `odd/tasks/agent-mtls.md`; no source behavior is involved.

## Next step

Continue BC-02 by reconciling remaining trackers and SSH install/update checks; assess isolated-database integration feasibility for mTLS. PR creation/push/merge remain pending explicit authorization of destination, operation, and credential/session; BC-02 remains open while the wider tracker reconciliation is unfinished.
