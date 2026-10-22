# ONVIF Mutation Audit and Ambiguous Outcomes

## Goal

Make every existing ONVIF camera mutation auditable before and after crossing the API-to-agent-to-camera boundary. Keep durable records append-only and do not automatically replay operations whose device-side outcome may be unknown.

## Scope

- Existing mutating operations only: time sync, PTZ move/preset commands, video configuration/encoding writes, and imaging writes.
- Use the current `audit_log` and `store.InsertAudit`; do not add a migration unless the existing row fields cannot support a bounded correlation record.
- Require `cameras.manage` on the persisted camera resource in addition to the existing server permissions before recording an attempt or contacting the edge agent.
- Persist stable camera/server identifiers, operation ID, operation kind, start/end timestamps, result state, and a sanitized error category. Never persist camera credentials, endpoint or snapshot/stream URIs, SOAP bodies, or unsanitized remote errors.
- An operation with no terminal audit event after the remote call is treated as outcome unknown and requires manual reconciliation. Do not retry automatically.

## Work Unit

- [x] ONA-01 — Append an intent audit event before each remote mutation and a correlated terminal event after it. Fail closed if intent cannot be stored. Verify permission checks precede audit and outbound calls; cover success, local denial, device error, timeout/unknown outcome, terminal-audit failure, correlation, and secret/URI redaction. Use unit tests with fake operation/audit callbacks and a disposable PostgreSQL integration test for persisted rows.
  Status (2026-10-22): implementation slice complete for time sync, PTZ commands, video configuration/encoding writes, and imaging writes. Each lifecycle pair uses a shared operation UUID; immutable `audit_log.occurred_at` values supply event timestamps. Remote errors are reduced to `remote_outcome_unknown`, and callers receive a manual-reconciliation message. No automatic retry is introduced. ONVIF-09 remains broader than this slice because readback/request-result evidence and physical-device acceptance remain separate work.

## Verification

Strict TDD: demonstrate focused authorization/audit regressions first, implement the smallest shared audit path, then refactor without weakening assertions.

```bash
GOCACHE=/tmp/openvms-go-build-cache go test ./internal/provision ./internal/api ./internal/agent/onvifdiscover ./internal/onvif
GOCACHE=/tmp/openvms-go-build-cache go test ./internal/api -run TestExecuteAuditedOnvifMutation
GOCACHE=/tmp/openvms-go-build-cache go test -tags integration ./internal/inventory -run TestAuditCameraOnvifMutation
git diff --check
```

Results (2026-10-22): package tests PASS; focused PostgreSQL integration PASS; `golangci-lint run ./internal/api ./internal/inventory` reports 0 issues; `go vet ./...` PASS; `git diff --check` PASS. The full `make lint` reaches the web lint but cannot run it in this worktree because `apps/web/node_modules` is absent (`eslint: not found`).

Runtime harness: N/A. The change is verified with fake callbacks and disposable PostgreSQL; no real edge agent or camera is needed or authorized for this unit.

Rollback boundary: revert the ONA-01 stacked commits to remove only the mutation audit API hooks, inventory writer/tests, and corresponding ONVIF/backlog tracker evidence. No schema migration or persisted-data cleanup is required.

No camera, remote host, deployment, or real edge agent is part of this work unit. Physical interoperability remains separate acceptance.
