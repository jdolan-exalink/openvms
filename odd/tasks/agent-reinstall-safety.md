# Agent detection and safe reinstall

## Objective
Detect an installed OpenVMS edge agent over SSH even when it is disconnected from this control plane or its version cannot be read, and make replacement explicit, safe, and operable with the local self-signed HTTPS deployment.

## Problem and why
The existing SSH preflight can notice the service but reconciliation rejects unknown versions unless mTLS enrollment is configured. Status is primarily based on this control plane's local registration, and an offline registered agent cannot reach the install path. Replacing an existing agent also clears its prior mTLS identity, stopping synchronization with the OpenVMS that previously managed it, without a clear user confirmation.

## Scope and constraints
- Detect remote service/file presence independently from agent health or version output; represent unknown version/ownership explicitly.
- Before changing an existing installation or resetting its identity, require an explicit user confirmation that explains synchronization with the previous OpenVMS will stop.
- Permit an explicitly confirmed replacement of a supported existing installation with unknown version. Preserve the refusal to downgrade a known newer binary and fail closed for unsupported layouts.
- Keep SSH passwords transient; do not store them, log them, or include them in job/error output. Cancelled confirmation must cause no remote writes.
- Keep TLS certificate validation enabled. Support the operator's local self-signed HTTPS certificate through explicit trust configuration; never add an insecure/skip-verification mode.
- Keep server/tenant scoping, permissions, audit behavior, and post-install fresh-enrollment/health checks. Do not persist agent registration until remote replacement, fresh enrollment, and health verification succeed.
- Verification uses fakes only. No SSH, camera, endpoint probing, production deployment, or remote host mutation is in scope.
- Preserve unrelated untracked `data/` contents; never add or inspect them as part of this feature.

## Route, configuration, and delivery
- Route: delegated direct. Mapping spans SSH preflight, API/job state, generated API contract, Compose TLS wiring, and Servers UI; one bounded writer at a time.
- TDD: strict mode enabled by `AGENTS.md`; require observed RED before implementation, then GREEN and REFACTOR.
- Go runner: `GOCACHE=/tmp/openvms-go-build-cache go test ./...`.
- Web checks: `(cd apps/web && pnpm exec vitest run src/routes/Servers.test.tsx)`, `(cd apps/web && pnpm typecheck)`, plus scoped lint for edited paths.
- Compose/docs checks: `docker compose config --quiet`, focused config contract tests, and `git diff --check`.
- Delivery strategy: `ask-on-risk`; agent work previously used `stacked-to-main`, retained as the current chain strategy. The user explicitly authorized PR/merge/deployment for this continuation. For ARS-02 only, the user approved `size:exception` because generated OpenAPI output expands the small schema change into an inseparable embedded swagger blob diff.
- Forecast: cross-layer implementation likely exceeds 400 authored changed lines; use work-unit commits and honest slices rather than reducing tests/docs.

## Tasks
- [x] `ARS-01` — Integrate a read-only SSH inspection into the user-facing server agent workflow, using the transient password supplied by the operator. Report remote service presence independently from connection health and version; unknown version/ownership is explicit. Inspection must make no remote changes, persist no SSH credential, and expose no raw host output. Add fake-SSH/API/UI tests for present-known, present-unknown, absent, and connection failure. Route: delegated direct. Evidence: endpoint/UI integrated; writer reports Go provision/API tests, Servers tests (77/77), web typecheck, and `git diff --check` passing; parent re-ran `git diff --check` successfully.
- [x] `ARS-02` — Add an explicit, user-visible replacement confirmation for both a detected remote agent and a locally registered but disconnected agent. Explain that replacement removes the old agent identity and stops sync to its former OpenVMS. Require server-side confirmation before any identity reset or remote write; allow supported unknown-version replacement only after confirmation; preserve credentials and local registration until successful fresh enrollment/health. Add backend/API/UI/job tests. Route: delegated direct. Evidence: fake-SSH tests prove unconfirmed existing installs write no files, confirmed unknown-version replacement resets identity, newer versions remain refused, unsupported layouts fail closed, and existing registration survives enrollment/health failures; UI verifies warning copy, cancellation without install, and explicit confirmed request. `GOCACHE=/tmp/openvms-go-build-cache go test ./...`, `(cd apps/web && pnpm exec vitest run src/routes/Servers.test.tsx)` (79/79), `(cd apps/web && pnpm typecheck)`, and `git diff --check` passed. Implementation commit: `bddb2f5`.
- [x] `ARS-03` — Make Compose-based mTLS enrollment operable with the configured self-signed HTTPS endpoint, including trusted API CA, the agent listener's server TLS certificate, configurable public/bind addresses and published port, and safe defaults derived from the HTTPS host where possible. Keep verification enabled and test the effective Compose contract. Route: delegated direct. Implementation commit: `b4282ad`.
  - [x] Record current live evidence and safety boundary before source changes.
  - [x] Add failing Compose contract and certificate-bootstrap tests for the selected mTLS wiring. RED observed: `GOCACHE=/tmp/openvms-go-build-cache go test ./deploy/agent/compose` failed because API TLS/enrollment env, mounts and port were absent and the dedicated gRPC certificate bootstrap script did not exist. A second RED after making container port configurable showed the Compose port target/bind address were not yet aligned.
  - [x] Implement secure Compose gRPC mTLS settings, public address derivation, dedicated cert/trust material, and documentation.
  - [x] Verify focused and full Go tests, Compose rendering, and diff; commit this work unit.

## Acceptance criteria
- An SSH-connected host with a running/installed agent is reported as present even if the agent is offline from OpenVMS or its version cannot be obtained.
- The UI does not describe unknown version/ownership as definitely outdated or absent.
- No overwrite or identity reset occurs without an explicit confirmation after the existing-agent condition is known; cancelling leaves the remote installation untouched.
- Confirmation copy clearly states the loss of synchronization with the previous OpenVMS.
- A known newer agent is not silently downgraded; unsupported layouts fail before mutation.
- Successful replacement enrolls to this OpenVMS, passes authenticated health verification, and only then updates the local registration.
- Self-signed HTTPS trust is distributed through configured CA material; TLS verification is never bypassed. The listener address, certificate paths, and port remain configurable in Compose.
- SSH credentials, enrollment tokens, private keys, and raw remote output never appear in logs or user-visible job data.

## Progress and next step
- Exploration confirmed: the SSH preflight recognizes service presence with unknown version, but its Go gate rejects that state; the service precheck rejects any existing local DB registration; `DiscoverRemoteAgent` has no production caller; the UI only offers install when no registration exists; identity reset occurs during confirmed replacement and breaks the prior controller relationship.
- Exploration confirmed: Compose lacks agent-enrollment/gRPC listener wiring. The supplied HTTPS base is `https://10.1.1.24:8443` (omit `/servers`); the installer appends `/api/v1/agent/enroll`. The repo supports a configurable self-signed certificate, and mTLS must retain trust validation.
- Completed `ARS-01`: added the HTTPS-only SSH inspection API and Servers dialog action. It reports presence, version (including unknown), local registration ownership (including unknown), or safe connection failure; inspection is read-only and uses the supplied SSH password transiently. Focused writer checks passed, and parent spot-check `git diff --check` passed. No real SSH or host probe occurred.
- Completed `ARS-02`: replacement is opt-in in the UI and request contract; the backend carries that confirmation into the read-only fresh SSH preflight and refuses existing installs before any writes unless confirmed. Unknown-version replacement is accepted only for the supported installation layout, known newer binaries remain refused, and confirmed replacement clears the previous identity only after staging; local registration is updated only after fresh enrollment and authenticated health succeed.
- ARS-03 live diagnosis (read-only): HTTPS login succeeded using the mounted public certificate. SSH discovery for `10.1.1.252` returned present, unknown ownership, and unknown version. A confirmed reinstall attempt returned HTTP 400; the exact response body was not captured, and it must not be retried yet. The API container had no environment-variable names matching `AGENT`, `GRPC`, `TLS`, or `HTTPS`. The user authorized reading the active public TLS certificate and live read-only discovery/install diagnosis, but not host mutation or deployment; no remote mutation/deploy is in scope.
- ARS-03 safety and design constraints: do not read `.env` secret values; preserve untracked `data/`; keep TLS verification enabled; do not reuse the web certificate private key if avoidable. The local web/Caddy HTTPS certificate is distinct from the API gRPC listener and the edge-agent ONVIF TLS certificate. The API's existing install code only accepts an existing agent when explicit replacement is paired with mTLS enrollment configuration.
- Completed `ARS-03`: Compose configures the API TLS cert/key, requires agent client certificates on a separate listener, and publishes only that listener on configurable host/container bind values. Public gRPC host and HTTPS enrollment URL derive from the HTTPS site host/port unless explicitly overridden. The API mounts dedicated gRPC certificate/key and public HTTPS CA read-only; `bootstrap-agent-grpc-tls.sh` creates a separate self-signed gRPC certificate/key with SAN validation, restrictive private-key permissions, trust verification, and no-overwrite behavior. Docs explain bootstrap, certificate trust, configuration and the impact on the general gRPC listener. No remote install retry, SSH, or deployment occurred.
- ARS-03 proof: `GOCACHE=/tmp/openvms-go-build-cache go test ./deploy/agent/compose ./internal/platform/config` passed; `GOCACHE=/tmp/openvms-go-build-cache go test ./...` passed; `docker compose config --quiet`, `sh -n deploy/docker/bootstrap-agent-grpc-tls.sh`, and `git diff --check` passed. The script integration test generated and validated certificates under `t.TempDir()` only.
- Next: any host rollout or install retry requires separate authorization.

## Relevant Files
- `internal/provision/agent_install.go` — SSH preflight, version gate, replacement steps, identity reset.
- `internal/provision/agent_install_service.go` — install job preconditions and enrollment/health persistence.
- `internal/provision/agent_discovery.go` — current read-only SSH discovery helper.
- `internal/api/provision_handlers.go` and API contract source — install request and job boundary.
- `apps/web/src/routes/Servers.tsx` — install/reinstall action and confirmation UI.
- `docker-compose.yml`, `deploy/docker/prepare-web-tls.sh`, `docs/grpc-tls.md` — HTTPS certificate and agent-listener configuration.
