# Versioning, release history and existing-agent detection

## Objective
(1) The SSH agent installer detects an agent that is already installed on the target, reports its version and whether it is outdated, and every installer failure shows a clear reason instead of "remote output was not retained". (2) OpenVMS and the edge agent get a versioning system with a change history; the version is shown on the login page; the first release under this system is cut at the end.

## Problem and why (2026-10-19)
- Installing the agent on Frigate `Casa` (10.1.1.252) from a fresh OpenVMS (10.1.1.60) failed in ~1 s with "Agent installation failed; remote output was not retained". Cause: the host already runs `openvms-agent` 0.2.1 managed by the production OpenVMS; the preflight refuses (exit 27) but `runAgentInstallStep` drops the exit status (`internal/provision/agent_install.go:212`) and the service never logs the error (`internal/provision/agent_install_service.go:291`).
- There is no single source of version truth: `Makefile` derives `VERSION` from `git describe`, the agent version is `0.2.1` in `deploy/docker/go.Dockerfile` and `0.2.2` in `Makefile`/`docker-compose.yml`; a GitHub release `v0.3.0` (2026-10-15) already exists. No CHANGELOG.

## Scope / tasks
- [x] VER-01 — Installer diagnostics: map preflight exit codes (21 not root, 22 unsupported OS, 23/24 architecture, 25 agent files present, 26 no systemd, 27 agent already installed) and transfer/activation failures to clear job messages; always log the underlying error server-side without secrets. Route: delegated direct.
- [x] VER-02 — Existing-agent detection: the preflight reports the installed agent's version (when present) and the job/UI says "already installed, version X (bundled Y): up to date / outdated"; if it is registered on this OpenVMS, point to the existing update flow; never overwrite an agent the operator did not choose to replace. Route: delegated direct.
- [x] VER-03 — Versioning system: single source files for the OpenVMS and agent versions, CHANGELOG with semantic-versioning rules, builds read them, public version endpoint/file, version shown on the login page. Route: delegated direct. Version number pending user decision (existing `v0.3.0`). Evidence: PR #104 merged (`e56111f`, release bump `137243f`): `VERSION` (0.4.0) and `deploy/agent/VERSION` are the single sources read by `Makefile`, `deploy/docker/go.Dockerfile`, `web.Dockerfile`, `docker-compose.yml`; `CHANGELOG.md` with SemVer rules in `docs/VERSIONING.md`; public `/version.json` written by `web.Dockerfile` (`VERSION`/`COMMIT`/`BUILD_TIME`); `AppVersion` shown on `Login.tsx` (test in `Login.test.tsx`); `internal/platform/buildinfo/release_consistency_test.go` keeps files in sync. Version decided: 0.4.0 (tag `v0.4.0`).
- [ ] VER-04 — Cut the first release under the new system (tag + GitHub release from the CHANGELOG) after the PRs merge. Route: inline (parent), with user OK on the number.

## Constraints and checks
- Strict TDD (user global config). Go: `GOPROXY=off GOTOOLCHAIN=local GOCACHE=/tmp/openvms-go-build-cache go test ./internal/provision/...`, integration where relevant, `golangci-lint run ./...`; web: `cd apps/web && pnpm exec vitest run <file>`, `pnpm typecheck`.
- Never contact real hosts from tests; the parent runs live checks on the authorized test host 10.1.1.60 only.

## Progress
- 2026-10-19: branch `feat/agent-install-detection` from `main`.

- 2026-10-19: VER-01 done (commit e970260): typed `AgentInstallFailure` keeps the remote exit status, codes 21-27 + transfer/activation/SSH mapped to operator messages, job `code` in OpenAPI, service always logs host/server/stage/code/exit status; web renders localized text by code. Evidence: RED = compile failure (undefined AgentInstallFailure/Code*) and 6 failing vitest cases; GREEN = `go test -race ./internal/provision/... ./internal/api/... ./apps/edge-agent/...`, vitest AgentJobStatusRegion/Servers/agentJobs/i18n (106 passed), golangci-lint 0 issues.
- 2026-10-19: VER-02 done (commit below in git log): preflight prints `openvms_install_preflight=agent_present version=X` (exit 27, read-only), edge-agent `--version`, semver comparison (up_to_date/outdated/unknown), job fields installed_version/bundled_version/version_state/registered_here, message points to "Actualizar agente" when registered here; web shows localized text. Route: delegated direct (one writer).

## Next step
VER-03 (versioning system) and VER-04 (release). Live check of the new preflight on 10.1.1.60 by the parent.
