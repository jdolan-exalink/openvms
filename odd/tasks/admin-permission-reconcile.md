# Reconcile catalog permissions for platform admin

## Objective
Ensure the explicitly bootstrapped platform administrator can receive every permission in the current catalog after upgrades, without creating extra long-lived API tokens.

## Problem
`bootstrap.PlatformAdmin` fills missing catalog grants only when the CLI bootstrap path is invoked. API startup does not reconcile grants. Re-running `vmsctl bootstrap` also issues and prints an additional non-expiring token, which is an avoidable credential side effect for permission repair.

## Why
The user reports that the admin cannot see the Maps Editor after deployment and explicitly wants the admin to have all permissions. Maps edit grants were added after some local databases may already have been bootstrapped.

## Scope and constraints
- Add an explicit grant-only reconciliation path for a named platform user (defaulting to the existing CLI bootstrap username only if consistent with current command conventions).
- Reconcile only missing ALLOW grants for catalog permissions at platform scope. Do not create users, mint tokens, set passwords, revoke tokens, delete DENY grants, or grant all tenant users.
- Reject tenant-owned users; do not infer administrator identity from username alone.
- Preserve normal `vmsctl bootstrap` behavior and existing tenant/platform authorization boundaries.
- No remote operations, credential/session inspection, or deployment until implementation and checks pass.
- Strict TDD is ON based on the user-confirmed Maps work mode: observe RED, then GREEN, then REFACTOR.
- Implementation route: delegated direct; trigger: new Go behavior across bootstrap and CLI with integration/unit tests.
- Delivery strategy: `ask-on-risk`; feature chain strategy previously selected: `feature-branch-chain`.

## Tasks
- [x] ADM-1 — Add idempotent grant-only catalog reconciliation for an existing platform admin, expose it through an explicit `vmsctl` command, test the no-token/no-user-creation behavior, and document usage.

## Acceptance criteria
- Existing platform admin receives any newly added catalog ALLOW grants at platform scope.
- Repeating reconciliation makes no duplicate grants and creates no API token.
- Tenant-owned target is rejected without mutation.
- Existing DENY grants are preserved; authorization's DENY precedence remains intact.
- Regular bootstrap still creates/reuses the platform account, grants the catalog, and issues its one-time-displayed token as before.
- Tests demonstrate RED → GREEN → REFACTOR; final functional checks pass.
- Admin permissions are not silently broadened for tenant users or on API startup.

## Applicable checks
- Focused Go tests for the bootstrap grant reconciler and CLI command (resolve exact packages from repository tests).
- Relevant authz/inventory integration tests for tenant isolation and DENY precedence.
- Full applicable Go tests, `go test ./...`, and `git diff --check` at task closure.

## Progress
- Exploration verified `PlatformAdmin` already adds missing catalog grants, but only during explicit CLI bootstrap; API startup does not call it. Re-running bootstrap adds a fresh non-expiring token. A grant-only admin sync is the narrow safe correction.
- 2026-10-01: Task document created before source edits.
- 2026-10-01: ADM-1 implemented via delegated direct. Added `bootstrap.SyncPlatformAdmin` for existing platform users only; it adds missing platform ALLOW catalog grants, rejects missing and tenant-owned accounts, and creates no token. `PlatformAdmin` reuses the same catalog grant reconciler, preserving bootstrap's existing user/token behavior. Added `vmsctl sync-admin-permissions -user NAME`; it requires an explicit username and reports only count/status, never credentials. No startup wiring or live-account sync was performed.

## Verification evidence
- RED: `GOCACHE=/tmp/openvms-go-build-cache go test -tags integration ./internal/bootstrap` failed to compile before implementation because `bootstrap.SyncPlatformAdmin` did not exist (the initial test also had a fixture field mismatch, corrected before implementation).
- GREEN: `GOCACHE=/tmp/openvms-go-build-cache go test -tags integration ./internal/bootstrap` — PASS; disposable PostgreSQL integration tests verify existing platform user only, catalog grant count, idempotency, no API token, missing-user rejection, tenant-user rejection/no grants, and DENY preservation.
- `GOCACHE=/tmp/openvms-go-build-cache go test ./internal/bootstrap ./apps/vmsctl` — PASS.
- `GOCACHE=/tmp/openvms-go-build-cache go test ./...` — PASS.
- `git diff --check` — PASS.
- Runtime CLI against an actual configured deployment: N/A; no deployment or live DB access authorized/performed.
- Commit identity: pending parent; worker was instructed not to commit.

## Next step
- Parent to inspect and commit this work unit; do not execute sync against an unverified live account or deploy absent explicit authorization.

## Relevant files
- `internal/bootstrap/bootstrap.go` — current platform admin grant seeding and token issuance.
- `apps/vmsctl/main.go` — explicit bootstrap CLI path.
- `internal/authz/catalog.go` — complete permission catalog and scope definitions.
- `internal/inventory/grants.go` — target validation and tenant/platform grant boundaries.
- `apps/web/src/routes/Maps.tsx` — Editor tab derives visibility from effective Maps edit grants.
