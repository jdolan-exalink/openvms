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

### Authorized local diagnostic deployment attempt (blocked at preflight)

- User authorization covered the local API/web deployment only; remote SSH, agent retry, and camera access remained excluded.
- `docker compose config --quiet` passed. Before any mutation, the deployment preflight showed `openvms-postgres-1` exited (health state `starting`), while API and worker were already restarting. The current API/web image IDs were read as `960c5877a646…` and `62845cd9016c…` respectively.
- Stopped before backup, image tagging/build, API/web restart, or any database operation because the requested protected backup could not be taken and validated. No service was changed. No API/HTTPS verification was attempted.
- A follow-up authorized attempt started the existing stopped dependencies: Postgres, NATS, Valkey, SeaweedFS, and Mosquitto became healthy. Starting the stopped web service also caused Compose to attempt its API dependency; API start failed because host `127.0.0.1:8080` is already allocated to a `docker-proxy` listener. That listener was not stopped or altered.
- Protected backup completed after dependencies were healthy: `.atl/onvif-deploy/diagnostics-f5a0b7b/postgres.dump`, mode `0600`, 22,831,166 bytes, SHA-256 `6594694ef2542cfb2cc33c2e36937e70afa167350da5935917e2b2e21ad1144b`. Archive TOC was verified using an isolated `postgres:17-alpine` helper container. No backup contents were printed.
- Stopped before rollback tagging, build, API/web restart, or database mutation because the API bind conflict prevents a safe deployment without changing an unrelated listener. API and web remain exited; worker remains restarting. The dependency services that were started remain running. No API/HTTPS verification was attempted.
- Next step requires resolving the host port 8080 owner and then deciding whether to retry the API/web deployment. The earlier API/worker restart state and current worker restart loop are not established as caused by this diagnostics change.

### Authorized continuation: stale proxy removed; Docker allocation still blocks startup

- Revalidated PID 7940 immediately before signaling: `/usr/sbin/docker-proxy`, start time `Wed Oct 7 09:35:42 2026`, command mapped `127.0.0.1:8080` to `172.18.0.3:8080`, and it owned the only 8080 listener. Full active-container inspection showed no container publishing host port 8080. Sent SIGTERM to that exact PID only; the host socket became free. No container was stopped.
- Tagged the previous API and web images as `openvms-api/web:rollback-pre-diagnostics-f5a0b7b` (image IDs `960c5877a646…` and `62845cd9016c…`). `docker compose build --pull=false api web` passed; candidate image IDs are recorded in Docker locally.
- `docker compose up -d --no-deps api web` recreated candidate containers but API startup still failed with Docker's `Bind for 127.0.0.1:8080 failed: port is already allocated`. Candidate image IDs are API `356e1053d1050aceacd60de66fe5f76e9b4442d90901181983e195f80c356b6a` and web `39b861232ed7b003383d20ea966d2a89a1b607d822cb4619e502aab95b53aebe`. A follow-up check found no listening socket or docker-proxy process on 8080; this appears to be stale Docker port-allocation state, not a live host listener. Per scope, no Docker daemon restart or unrelated container change was attempted.
- Current API/web containers are in `created` state and are not serving; no HTTP/HTTPS health, authentication, UI asset, or log verification could be performed. Dependencies remain running; Postgres remains healthy. Backup above remains intact. No migration, database restore/change, SSH retry, or camera access occurred.
- Next step requires explicit authorization to resolve stale Docker port-allocation state (likely a Docker daemon restart with effects beyond this Compose project); do not retry API/web startup blindly. The existing rollback tags and protected database dump are available if needed.

### Authorized local restoration: keep API private behind web

- The operator explicitly authorized removing the API's unnecessary host loopback publication to restore OpenVMS without a Docker daemon restart. Added `TestAPIIsInternalOnlyAndUsesNoHostPortPublication`; it also asserts the existing fixed private Caddy upstream is unchanged.
- Strict-TDD RED observed: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./deploy/agent/compose` failed because the API service had `ports` configured. Removed only API's `127.0.0.1:8080:8080` publication from `docker-compose.yml`; SeaweedFS and web publications were unchanged. Focused test passed afterward and `docker compose config --quiet` passed.
- With the specifically approved stale proxy removed and Compose allocation recovered by the internal-only API config, `docker compose up -d --no-deps api web` succeeded using the already-built candidate images. No DB migration/restore, volume operation, unrelated container restart, SSH, or camera access occurred.
- API image `356e1053d105…` is running with no host port mapping at internal fixed address `172.29.240.2`; web image `39b861232ed7…` is running at `172.29.240.4`, publishing HTTP 8000 and HTTPS 8443. Previous images remain tagged `openvms-api/web:rollback-pre-diagnostics-f5a0b7b`; the protected DB backup above remains intact.
- Runtime verification: both `http://10.1.1.24:8000/health/live` and verified-certificate `https://10.1.1.24:8443/health/live` returned 200; both HTTPS-port hints returned `8443`; anonymous POST to the update route returned HTTP 403 over HTTP and 401 over HTTPS using only a nonsecret sentinel body, with no job/SSH action. The served Servers JS asset contains the static allowlisted transfer-failure diagnostic copy. Bounded API/web tail scans found zero lines matching error/fatal/panic.
- After 30 seconds, API and web remained running with restart count 0; worker remained running at its previously observed restart count 28. Postgres, NATS, Valkey, and SeaweedFS were healthy; Mosquitto was running. No actual authenticated update submission occurred; diagnosis logging is verified by source tests, not a remote job.
- Verification: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./deploy/agent/compose`, same with `-race`, `GOCACHE=/tmp/openvms-go-build-cache go test ./...`, and `git diff --check` passed. Rollback requires restoring the removed API host publication and using the preserved image tags; DB restore is not needed or authorized.
