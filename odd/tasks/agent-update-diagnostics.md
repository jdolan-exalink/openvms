# Safe diagnostics for existing-agent updates

Expose enough failure context to troubleshoot an existing-agent SSH update without recording or returning raw SSH, remote-command, or transport errors. This work does not retry an update and does not perform any SSH operation.

## Scope and security boundary

- Emit structured API-service logs with only `server_id`, `job_id`, and fixed diagnostic enums: `stage`, `code`, and `rollback`.
- Never log or return `err.Error()`, nested errors, SSH stdout/stderr, host/IP, SSH username/password, agent token, SSH key fingerprint, certificate/private key, environment values, or command text.
- Runner errors expose only fixed safe text and typed enums. Specific SSH host-key mismatch diagnosis is allowed only for the known internal mismatch sentinel; every unknown nested error maps to a generic code.
- Preserve the existing job/API schema. Existing `ServerAgentInstallJob.Message` is sufficient for a static safe failure message; do not add a raw cause field.
- No remote SSH retry, camera access, DB migration, or API/web deployment. Deployment is a separately authorized operation.

## Work units

| ID | Acceptance | Verification |
|---|---|---|
| AGENT-DIAG-01 — typed runner failures | `RunAgentUpdate` returns safe structured stage/code/rollback details for validation, SSH connection, preflight, staging, transfer, activation, health, cleanup, and rollback outcomes. Preserve generic safe `Error()` output; do not retain or unwrap nested SSH/remote errors. Unknown failures remain generic. | Strict TDD in `internal/provision/agent_update_test.go`: cover known mismatch vs unknown dial error, each runner boundary, restored/uncertain rollback, cleanup failure, and marker-secret absence from error/format/JSON representations. |
| AGENT-DIAG-02 — safe service logs and job message | `runServerAgentUpdate` logs a fixed event with server/job IDs and allowlisted enums only. Use a safe generic fallback for non-typed injected/runtime errors. Set an actionable static `Message` from an allowlisted code, not from the underlying error. | Strict TDD in `internal/provision/agent_update_service_test.go`: capture `slog` and final job for typed and unknown failures; assert IDs/codes/stage/rollback appear as appropriate and injected secret, raw error marker, host, token, and remote output never appear. |
| AGENT-DIAG-03 — show safe failure message | Existing update UI renders the server-provided static safe job message on failure while retaining localized generic fallback behavior. Do not add a raw-error field or alter install behavior. | Strict TDD in `apps/web/src/routes/Servers.test.tsx`: mocked failed update job shows its safe message; generic fallback remains when absent; no credential/error details leak. Run focused test, typecheck, and full web tests. |

## Route, TDD, and checks

- Route: delegated direct; runner, service/log tests, and UI are separate non-trivial files and the source preparation spans Go and React boundaries.
- Strict TDD: enabled by `AGENTS.md`; Go runner is `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision`, UI runner is `(cd apps/web && pnpm exec vitest run src/routes/Servers.test.tsx)`. Observe RED before production changes, then GREEN and REFACTOR.
- Backend verification: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision`; same command with `-race`; `GOCACHE=/tmp/openvms-go-build-cache go test ./...`.
- UI verification: focused Servers test, `(cd apps/web && pnpm typecheck)`, `(cd apps/web && pnpm test)`, scoped ESLint, and `git diff --check`.
- Forecast: approximately 300–380 authored changed lines, advisory only; do not omit coverage to hit it. Delivery strategy `ask-on-risk`, chain strategy `stacked-to-main`; no push/PR authorized.

## Out of scope and rollback

No source/runtime change may launch, retry, or deploy an SSH job. Existing failed jobs cannot be retroactively diagnosed; tests must use injected failures only. Rollback removes the typed diagnostic mapping, safe service logging/message, UI display/test, and this task document without changing agent install/update security behavior.

## Progress

- [x] AGENT-DIAG-01 — added private typed failure values containing only fixed `stage`, `code`, and `rollback` enums. Known host-key mismatch is specific only through `errors.Is`; unknown nested failures remain generic. Rollback is returned as a finite result, not parsed from error text.
- [x] AGENT-DIAG-02 — background service now logs only server/job IDs and validated enum fields. Failures set a fixed safe message; panic and persistence failures also receive safe fallback diagnostics.
- [x] AGENT-DIAG-03 — failed update UI maps only exact allowlisted backend messages to localized English/Spanish/Portuguese copy; absent or unrecognized messages use the existing generic localized fallback.

### TDD and verification evidence

- RED runner/service: after adding regressions, `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision` failed to compile because `agentUpdateFailure` did not exist.
- RED UI: `(cd apps/web && pnpm exec vitest run src/routes/Servers.test.tsx)` failed the new health/rollback diagnostic test because the UI still showed only generic failure copy (1 failed, 44 passed).
- GREEN: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision` passed; same command with `-race` passed; `GOCACHE=/tmp/openvms-go-build-cache go test ./...` passed.
- GREEN: focused Servers Vitest passed (45 tests); web typecheck passed; full web suite passed (120 files, 882 tests); scoped ESLint and `git diff --check` passed.
- Actual source/test diff: 419 additions and 27 deletions (446 changed lines), above the advisory forecast because safe typed failure coverage and localized, allowlisted UI messages were kept intact.
- No agent connection, SSH retry, remote operation, database access, or deployment was performed. Prior failed jobs remain undiagnosed; this change only improves future failures.
- Runtime harness: `N/A` for this source-only diagnostic change; failure behavior is tested with injected errors and fake runner/HTTP data.

### Follow-up regression: preserve the primary update failure

- [x] `RunAgentUpdate` cleanup no longer replaces an earlier typed transfer/health failure. Cleanup failure is reported as `cleanup_failed` only when the update otherwise succeeded; successful rollback metadata remains attached to the original health failure.
- RED observed: after adding two injected-failure regressions, `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision` failed because both surfaced `("cleanup", "cleanup_failed", "not_attempted")` instead of the primary `transfer_failed` and `health_check_failed/restored` results.
- GREEN observed: focused provision tests, provision race tests, `go test ./...`, focused Servers Vitest (45 tests), web typecheck, and `git diff --check` all passed after the minimal deferred-cleanup fix.
- No SSH, deployment, database mutation, or camera access was performed. Cleanup failure accompanying a primary failure is intentionally secondary and not separately surfaced in this API contract.
