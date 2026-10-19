# Automatic edge-agent discovery for servers

## Objective
Automatically discover an already installed OpenVMS edge agent when a server is added, report its installed version and registration state, and guide the operator to the safe next action without silently replacing or adopting another OpenVMS agent.

## Problem and why
On 2026-10-19, the user added the home server at `10.1.1.60`. OpenVMS did not show its existing `0.2.1` agent; a manual install attempt then refused because the installed agent was older than the bundled `0.3.0` and might belong to another OpenVMS. Current status only checks this control plane's `server_agents` record, while the install preflight intentionally refuses any existing service. The agent is present but unregistered in this OpenVMS, so current status cannot discover it.

## Scope and constraints
- Automatically perform a read-only discovery using the saved server SSH connection after server registration (or at the next agent-status refresh); do not connect to remote hosts during tests.
- Report presence, version when available, and whether the agent is registered to this OpenVMS.
- Preserve the install safeguard: never overwrite, adopt, or change a detected agent without an explicit operator action.
- Keep credentials and remote command output out of API/UI responses and logs.
- No remote SSH/deployment is authorized by this task; implementation and tests are local only.
- Strict TDD is enabled globally. Go test runner: `GOPROXY=off GOTOOLCHAIN=local GOCACHE=/tmp/openvms-go-build-cache go test ./internal/provision/... ./internal/api/...`; web: `cd apps/web && pnpm exec vitest run <relevant test>` and `pnpm typecheck`.

## Tasks
- [x] AGD-01 — Add read-only remote-agent discovery and a typed status result that distinguishes absent, present/unregistered, registered-here, and unknown/error states. Add focused tests for parsing, transport failures, and safety (no upload/service mutation). Route: delegated direct.
- [ ] AGD-02 — Connect server registration/status refresh to discovery and display the detected state/version with a clear, safe next action. Add API and UI tests. Route: delegated direct.

## Acceptance criteria
- Adding a server with SSH access discovers an existing agent without requiring a manual install attempt.
- An unregistered agent is visibly distinguished from an absent agent and an agent managed by this OpenVMS.
- A detected older version does not result in a silent replacement or adoption; the UI explains that an explicit update/reconciliation action is required.
- Discovery is read-only and does not leak SSH secrets or arbitrary remote output.

## Progress
- 2026-10-19: Created on `feat/automatic-agent-discovery`; mapping confirmed that `/api/v1/servers/{id}/agent` only recognizes this OpenVMS's `server_agents` row, while install preflight refuses any existing service (exit 27). No remote host was contacted.
- 2026-10-19: AGD-01 implemented in `internal/provision/agent_discovery.go`; focused tests cover strict output parsing, transport-error redaction, and a read-only SSH command that excludes upload/service mutation. Verified with provision/API tests, repository golangci-lint, and web typecheck. Work-unit commit: `cf2f49e14b0c1871f8c6cfac53826a655be240e3` (`feat(provision): discover remote agent status read-only`).

## Next step
Implement AGD-02 with strict TDD; then verify and record a local work-unit commit.
