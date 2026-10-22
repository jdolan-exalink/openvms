# ONVIF Mutation Audit and Ambiguous Outcomes

## Goal

Make every existing ONVIF camera mutation auditable before and after crossing the API-to-agent-to-camera boundary. Keep durable records append-only and do not automatically replay operations whose device-side outcome may be unknown.

## Scope

- Existing mutating operations only: time sync, PTZ move/preset commands, video configuration writes, and imaging writes.
- Use the current `audit_log` and `store.InsertAudit`; do not add a migration unless the existing row fields cannot support a bounded correlation record.
- Require `cameras.manage` on the persisted camera resource in addition to the existing server permissions before recording an attempt or contacting the edge agent.
- Persist stable camera/server identifiers, operation ID, operation kind, start/end timestamps, result state, and a sanitized error category. Never persist camera credentials, endpoint or snapshot/stream URIs, SOAP bodies, or unsanitized remote errors.
- An operation with no terminal audit event after the remote call is treated as outcome unknown and requires manual reconciliation. Do not retry automatically.

## Work Unit

- [ ] ONA-01 — Append an intent audit event before each remote mutation and a correlated terminal event after it. Fail closed if intent cannot be stored. Verify permission checks precede audit and outbound calls; cover success, local denial, device error, timeout/unknown outcome, terminal-audit failure, correlation, and secret/URI redaction. Use unit tests with fake agent transport and a disposable PostgreSQL integration test for persisted rows.

## Verification

Strict TDD: demonstrate focused authorization/audit regressions first, implement the smallest shared audit path, then refactor without weakening assertions.

```bash
GOCACHE=/tmp/openvms-go-build-cache go test ./internal/provision ./internal/api ./internal/agent/onvifdiscover ./internal/onvif
GOCACHE=/tmp/openvms-go-build-cache go test -tags integration ./internal/api -run TestOnvifMutationAuditLifecycle
git diff --check
```

No camera, remote host, deployment, or real edge agent is part of this work unit. Physical interoperability remains separate acceptance.
