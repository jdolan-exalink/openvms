# Existing edge-agent detection and reconciliation

## Objective
When the operator uses the existing agent-install flow and supplies the server's SSH password, detect an already-installed OpenVMS edge agent, register it with this OpenVMS, and update it if its version is behind the bundled agent.

## Problem and why
On 2026-10-19, the user added the home server at `10.1.1.60`. OpenVMS did not show its existing `0.2.1` agent; a manual install attempt then refused because the installed agent was older than the bundled `0.3.0` and might belong to another OpenVMS. Current status only checks this control plane's `server_agents` record, while install preflight refuses any existing service. The user authorized reconciling an existing agent when discovered, using the SSH password already provided in the agent-install flow. SSH passwords remain transient; this work must not add credential persistence or change the user's server password.

## Scope and constraints
- Run discovery through the existing agent-install flow using the SSH password already supplied by the operator; do not add persistent SSH credential storage or change the password.
- Report presence, installed version, and whether the agent is registered to this OpenVMS.
- When this flow discovers an existing unregistered agent, reconfigure and freshly enroll it to this OpenVMS; when its version is older than the bundled agent, update it as part of that reconciliation. This behavior is explicitly authorized by the user; tests must never connect to a remote host.
- Do not rotate/reinstall an agent already registered and healthy here unless it is outdated or its credentials require renewal.
- Persist the local `server_agents` record only after remote configuration and fresh mTLS enrollment succeed and the agent is healthy; partial failures must remain retryable and must not claim registration.
- Keep credentials and remote command output out of API/UI responses and logs.
- Server add/background status refresh cannot SSH without credentials and is out of scope; keep the current authentication interaction unchanged.
- No live remote SSH/deployment is authorized by this task; implementation and tests are local only. A later live verification requires explicit destination, operation, and access authorization.
- Strict TDD is enabled globally. Go test runner: `GOPROXY=off GOTOOLCHAIN=local GOCACHE=/tmp/openvms-go-build-cache go test ./internal/provision/... ./internal/api/...`; web: `cd apps/web && pnpm exec vitest run <relevant test>` and `pnpm typecheck`.

## Tasks
- [x] AGD-01 — Add read-only remote-agent discovery and a typed status result that distinguishes absent, present/unregistered, registered-here, and unknown/error states. Add focused tests for parsing, transport failures, and safety (no upload/service mutation). Route: delegated direct.
- [x] AGD-02 — Integrate discovery into the existing agent-install flow, using its transient SSH password. Automatically reconfigure/re-enroll a found unregistered agent and upgrade it when outdated; expose accurate post-operation version/registration status. Persist registration only after fresh enrollment and healthy verification. Add API/provision/UI tests for successful reconciliation, already-managed agents, unsupported versions, and failure/retry safety. Route: delegated direct.

## Acceptance criteria
- The existing agent-install flow detects the installed agent before attempting a fresh install and uses the already-entered SSH password; the password is not persisted or changed.
- An unregistered agent is visibly distinguished from an absent agent and an agent managed by this OpenVMS.
- An unregistered agent is freshly enrolled to this OpenVMS; an outdated agent is upgraded to the bundled compatible version during that flow.
- Already-managed healthy agents are not unnecessarily re-enrolled.
- Registration is not persisted until remote enrollment and health verification succeed; failure is visible and retryable.
- Discovery/reconciliation does not leak SSH secrets, one-time enrollment tokens, private keys, or arbitrary remote output.

## Progress
- 2026-10-19: Created on `feat/automatic-agent-discovery`; mapping confirmed that `/api/v1/servers/{id}/agent` only recognizes this OpenVMS's `server_agents` row, while install preflight refuses any existing service (exit 27). No remote host was contacted.
- 2026-10-19: AGD-01 implemented in `internal/provision/agent_discovery.go`; focused tests cover strict output parsing, transport-error redaction, and a read-only SSH command that excludes upload/service mutation. Verified with provision/API tests, repository golangci-lint, and web typecheck. Work-unit commit: `cf2f49e14b0c1871f8c6cfac53826a655be240e3` (`feat(provision): discover remote agent status read-only`).
- 2026-10-19: The user clarified the credential boundary: keep the existing SSH-password interaction unchanged; use that transient password in the current install flow to reconcile/re-register an existing agent and update it if outdated. Do not store or change the SSH password. Local implementation is authorized; no remote host operation is authorized.
- 2026-10-19: AGD-02 implemented on `feat/automatic-agent-discovery`. Existing services are classified by strict version/OS/architecture output; outdated binaries are upgraded, newer/unknown versions fail without writes, and same-version binaries are not retransferred. Reconciliation stages the new controller settings, clears only the prior mTLS state before restart, waits for a fresh server-bound certificate to authenticate, verifies HTTPS health, then atomically persists bearer/TLS metadata. Already-registered agents remain protected by the existing DB-row precheck. Passwords, one-time tokens, private keys, and remote output are not included in jobs/logs/UI. Tests: `GOPROXY=off GOTOOLCHAIN=local GOCACHE=/tmp/openvms-go-build-cache go test ./internal/provision/... ./internal/api/...` passed; `cd apps/web && pnpm exec vitest run src/components/AgentJobStatusRegion.test.tsx --reporter=dot` passed (25 tests); `cd apps/web && pnpm typecheck` passed. No live host was contacted. Work-unit commit: `d23808f` (`feat(provision): reconcile installed edge agents`).

- 2026-10-19: PR #110 CI found a revive lint error because a local `comparable` variable shadowed the Go predeclared type. Renamed it to `versionComparable` without changing behavior. RED: `GOCACHE=/tmp/openvms-go-build-cache make lint` failed at that diagnostic; GREEN: the same command passed (go vet, web lint, and typecheck), `GOPROXY=off GOTOOLCHAIN=local GOCACHE=/tmp/openvms-go-build-cache go test ./internal/provision/... ./internal/api/...` passed, and `git diff --check` passed. Fix commit: `904d5b0` (`fix(provision): avoid comparable builtin shadowing`).

## Next step
AGD-02 is implemented and committed locally. Native review remains pending because the review tool requires an exact intended-untracked-selection JSON schema that is unavailable locally; do not guess or inspect/select `data/`. Live remote verification also remains pending explicit destination/operation/access authorization; do not connect to a host.
