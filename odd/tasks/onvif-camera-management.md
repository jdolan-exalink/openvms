# ONVIF Camera Management

Deliver capability-driven ONVIF agent/API management, safe Frigate sync and auditable operations.

## Current implementation status — 2026-10-06

- Authorized unauthenticated GET `/v1/metrics` on `opevms-mimo` (`10.1.1.144:7419`; server `c420319a-7c77-4489-b644-3e212673dd80`): HTTP 401, no `WWW-Authenticate`; body discarded. Reachability only; identity/metrics unverified.

- `712b56d` (relay) and `daafa0d` (UI) connect selected-server read-only probing; success is device/services/time, not saved cameras.
- Probe, registration and SSH install exist; local HTTPS and TLS schema v33 are deployed. Agent registration, remote SSH and device acceptance remain pending; no camera test.
- SSH/Casa acceptance pending.
- UI proof: Cameras 19; Servers 26; typecheck and web 120 files/863 tests PASS; not device acceptance.
- “100%” means declared profiles; unsupported operations remain unavailable.

## Goal and boundaries

- **Outcome:** capability-driven ONVIF discovery/configuration and safe Frigate lifecycle across media, imaging, time/network, PTZ/audio and events.
- **Scope:** agent/API/UI with fakes; SSH/physical operations need separate destination, operation and credential authorization.
- **Safety:** stable IDs/tombstones; encrypted write-only secrets; bounded audited/read-back mutations; preserve recordings, confirm discard, no factory reset.
- **Delivery/TDD:** `ask-on-risk`, approved `stacked-to-main`; no push/PR/merge. Conventional commits include tests/docs. Strict RED/GREEN/REFACTOR must be observed; 400 lines advisory only.

## Baseline and current state

- Feature branch `feat/onvif-camera-management` began at `3d44726ff04f61775115e9cd37dc0c2db8cb5c0a`; do not copy unrelated `feat/maps-phase2` changes.
- Baseline: Frigate owns ONVIF config; existing PATCH edits sections, add/remove needs PUT/agent support. Secrets require `servers.config.secrets`; Frigate <0.16 cannot edit config. No recording DELETE; sync key `(server_id, remote_name)` needs tombstones; preserve remote keys, change display names.
- Checks: `go test -race ./...`, `pnpm test`, `pnpm typecheck`; focused Go and `pnpm exec vitest run <test-file>`. Planning made no source/device changes.

## Work units

Implement in dependency order. Each delegated work unit closes with checks, Conventional Commit/ID, evidence, rollback, and review boundary. First foundation uses mocked protocol responses.

### Phase 1 — Discovery, device/media, lifecycle, and health

- [x] **ONVIF-01 — Mocked ONVIF foundation and bounded transport.** Establish protocol/client abstractions, device endpoint discovery inputs, bounded connection/read/write timeouts and retries, structured errors, and deterministic mocks. No physical camera access. RED/GREEN/REFACTOR evidence required. Rollback: remove the isolated foundation and its tests.
- [ ] **ONVIF-02 — Core discovery and device information.** Discover devices/services and read device information through the agent; expose API results without leaking credentials; map discovered devices to stable VMS identity.
  - [x] Credential-free Cameras-route discovery UI slice: managed-server and explicit-interface selection, API relay request, safe endpoint list, pending/empty/error states; devices are clearly not yet added. Focused UI and full web-suite proof recorded below. This does not complete ONVIF-02.
- [ ] **ONVIF-03 — Media2 with Media1 fallback and profiles.** Discover Media2 first where supported, fall back to Media1, enumerate streams/profiles and preserve profile identity/capability metadata.
- [ ] **ONVIF-04 — Snapshot and stream source management.** Retrieve snapshots and represent stream URIs/options safely; redact credential-bearing URIs and avoid persisting derived secrets in logs or browser state.
- [ ] **ONVIF-05 — Add, edit, delete, manifest, and health.** Implement camera manifest/state reconciliation and CRUD through API/agent; synchronize Frigate additions/edits/removals; retain stable IDs and deletion tombstones to prevent reimport. Health/readiness surfaces device and protocol state. Delete preserves recordings by default; discard requires explicit confirmation and must fail closed if the disposition is missing/invalid. Never factory-reset a device.

### Phase 2 — Physical imaging, video, time, network, and audit

- [ ] **ONVIF-06 — Capability-driven imaging/video reads and writes.** Enumerate operations/options per device, expose only supported settings, validate ranges, and read back every write before reporting success.
- [ ] **ONVIF-07 — Time configuration and verification.** Read/set supported time/NTP/timezone settings through explicit operations; verify by readback and audit before/after state without storing secrets.
- [ ] **ONVIF-08 — Network configuration and safety.** Enumerate supported network controls, validate operation-specific input, require bounded confirmation where connectivity may be disrupted, and never assume the device remains reachable.
- [ ] **ONVIF-09 — Mutation audit and failure recovery.** Persist actor, target stable ID, operation, redacted request/result, readback, timestamps, and outcome; retry only within bounded policy and avoid blind replay of non-idempotent operations.

### Phase 3 — PTZ, presets, tours, focus, and fisheye/dewarp

- [ ] **ONVIF-10 — PTZ capability and operation model.** Discover supported spaces/ranges and map only available continuous, relative, and absolute operations; enforce time-bounded movement and stop behavior.
- [ ] **ONVIF-11 — Presets and tours.** List/create/update/remove presets and tours only where supported; preserve stable IDs and audit mutation/readback outcomes.
- [ ] **ONVIF-12 — Focus and auxiliary PTZ controls.** Surface supported focus/iris/auxiliary controls from advertised capabilities, validate values, and verify readback.
- [ ] **ONVIF-13 — Fisheye/dewarp profiles.** Discover compatible imaging/dewarp modes and expose configuration only when the device advertises the relevant capability; preserve original stream behavior as fallback.

### Phase 4 — Audio, backchannel, and I/O

- [ ] **ONVIF-14 — Audio discovery and configuration.** Enumerate audio sources/encoders/configuration and supported changes; protect any write-only credentials and audit readback.
- [ ] **ONVIF-15 — Backchannel and audio operation safety.** Implement supported backchannel operations with explicit capability checks, bounded sessions, cancellation/stop behavior, and authorization boundaries.
- [ ] **ONVIF-16 — Device I/O.** Discover digital input/output/relay capabilities, represent operation-specific states, require explicit actuation intent, and audit every operation and observed result.

### Phase 5 — Events, Profile M, metadata, rules, and detection fusion

- [ ] **ONVIF-17 — Permanent events and Profile M metadata.** Subscribe/poll with bounded lifetimes, recover safely from interrupted subscriptions, parse supported Profile M metadata, and associate events with stable VMS camera identity.
- [ ] **ONVIF-18 — Rules, detection fusion, and event bus integration.** Normalize device rules/analytics and detections; fuse ONVIF metadata with existing sources without duplicate identity or event loss; publish through the existing event bus with auditable provenance.
- [ ] **ONVIF-19 — End-to-end resilience, security, and operator acceptance.** Verify capability-poor/partial devices, timeouts, retries, secret redaction, tombstone/reimport behavior, Frigate reconciliation, recording-preserve/discard fail-closed behavior, and health/audit paths with mocks and existing test infrastructure. Document operational limitations and rollback.

## Acceptance criteria

- [ ] All five phases complete; partial CRUD is not feature completion.
- [ ] Capabilities drive API/UI/options; unsupported operations stay unavailable. “100%” means the declared capability/profile matrix, not all vendor extensions.
- [ ] Secrets are encrypted/write-only and redacted from logs, errors, audit and stream metadata.
- [ ] Physical mutations are bounded, authorized, audited and read-back verified; failures never claim success.
- [ ] Frigate follows device/VMS lifecycle with stable identity and tombstones preventing reimport.
- [ ] Preserve recordings by default; discard requires explicit fail-closed confirmation. Never factory-reset.
- [ ] Applicable tests pass; physical/LAN checks require separate authorization.
- [ ] Record tests/harness (`N/A` reason), rollback, commit and review due/outcome per work unit. Do not change the review switch here.

## Delivery and progress record

- Delivery is `ask-on-risk` / user-approved `stacked-to-main`; no push/PR/merge authorized.
- ONVIF-02 remains partial: device reads/WSSE/discovery, TLS, probe relay/UI exist; persistence, stable VMS identity and live validation remain. Parent commits `712b56d`, `daafa0d`; planning `134a319`; review boundary `19315be`; later candidates declined.
- Per unit record RED/GREEN/REFACTOR, checks/harness, rollback, commit/lines and review boundary; no behavior/tests omitted to hit advisory 400 lines.
- Next: register agent with an authenticated actor; probe is not persistence.

### Existing-server SSH agent installation (local implementation; remote execution unperformed)

Local source work only. Frigate URL is not an SSH target. Root password stays transient; explicit IP/port and verified SHA-256 host-key fingerprint are mandatory. Both scoped permissions gate before dial. No TOFU, broad provisioning, Docker/Frigate/NTP/network changes; no real SSH has run.

| ID | Unit/status | Security/scope |
|---|---|---|
| ONVIF-DEPLOY-SSH-01 | Dedicated agent-only runner (implemented) | Pin before password auth; transient SSH credentials never persist/log/audit; separate agent bearer remains sealed. |
| ONVIF-DEPLOY-SSH-01a | Pinned runner foundation (implemented, not API-callable) | Root/IP/port/pin, fake transport, bounded preflight/install, allowlisted agent files only, optional supplied IP-SAN TLS pair; no overwrite or real SSH. Secret-safe String/GoString/JSON. |
| ONVIF-DEPLOY-SSH-01b | Existing-server install API + memory-only job (implemented, not deployed) | Dual permissions before artifact/token; explicit trusted HTTPS ingress; bounded artifact/job; generated token/TLS and insert-only registration. Existing-agent conflict is refused. |
| ONVIF-DEPLOY-SSH-02 | Servers UI (implemented; mocks only) | Existing-server selection, pin, password cleared on submit; dual permissions and HTTPS guard. Lost job is unknown; closing UI does not cancel remote work. |

**SSH proof (2026-10-06):** 01a RED `go test -count=1 ./internal/provision -run TestRunAgentInstall` (runner absent); focused, race, full Go and diff checks GREEN. Fake cases cover pin mismatch, validation, modes/SAN, cancellation, redaction. `github.com/pkg/sftp` v1.13.11. 01b RED absent service/routes; GREEN `make generate`, focused/race/full Go; tests cover proxy spoof/CIDR, dual-denial before data access, bounds, conflict, artifact integrity and fake runner outcomes. Hardening RED verified context leakage/reservation/artifact-read defects; GREEN regenerated, focused/race/full Go + web typecheck/diff PASS. 01a serialization RED verified secrets leaked through default formatting; GREEN redacts String/GoString/JSON; focused serialization, provision/race/full Go/diff PASS. UI RED absent control; focused `Servers.test.tsx` 26, typecheck, full web 120 files/863, scoped lint PASS. Full lint had unrelated errors in Layout/maps/ui. No live DB, session, SSH, agent, or camera operation. Runner/API do not support safe replacement yet; insert-only registration and existing-file refusal remain.

### Existing registered-agent secure update (local implementation; no remote execution)

The user authorized implementing an existing-agent update/configure-TLS action, not connecting to the host. Preserve the existing server/agent identity, sealed bearer token and Frigate state; do not bypass fingerprint pinning or treat the prior HTTP 401 as identity/health proof. Existing `POST /agent/update` pushes over HTTP and only updates the binary; current SSH install is insert-only and refuses existing files/services. A separate safe path is required.

| ID | Unit/status | Acceptance boundary |
|---|---|---|
| ONVIF-DEPLOY-UPDATE-01 | TLS health/version contract (implemented locally; not deployed) | Agent TLS mux only; bearer-authenticated verified HTTPS returns bounded version/build/variant. Central client uses registered IP, stored TLS port/trust, no HTTP fallback. Fake tests; health check proves agent TLS, not camera readiness. |
| ONVIF-DEPLOY-UPDATE-02 | Existing-agent backend/SSH update runner foundation (implemented locally; not API-callable) | Runner accepts existing bearer transiently for health check but never transfers or replaces its file. Pinned root SSH only; compatible Debian/Ubuntu, architecture, active exact service/unit, token-file and HTTP-only/TLS-disabled state required. Stage/backup only agent binary/env/cert/key/systemd unit, activate, verify bearer-authenticated TLS health, then remove owned stage; failed health attempts bounded rollback. Ambiguous stage claim or uncertain rollback is retained for inspection, never deleted blindly. |
| ONVIF-DEPLOY-UPDATE-02b | Existing-agent backend/API update job (implemented locally; not deployed) | Separate `POST /agent/update-ssh` and metadata-only poll. Both scoped permissions, HTTPS ingress, registered IPv4/existing sealed bearer, trusted binary and generated IP-SAN TLS. Shared bounded per-server install/update jobs; TLS trust/audit written only after runner's authenticated HTTPS health. Existing-TLS replacement is refused. |
| ONVIF-DEPLOY-UPDATE-03 | Servers UI action (implemented locally; mocks only) | Separate existing-agent update/configure-TLS action, both scoped permissions, HTTPS browser guard, verified fingerprint and transient password; destination fixed by registration. Success means authenticated TLS health only, not ONVIF/camera acceptance. |

All units require strict RED→GREEN tests with fake SSH/HTTP, permission-denial-before-unseal/dial, preserved ID/token/Frigate config, mismatch pin and architecture refusal, bounded stage/rollback uncertainty, TLS SAN/trust/health, no HTTP fallback or secret serialization. The backend API uses a separate route and never reuses the plaintext binary-update client. No remote operation, migration, camera test or credential use was performed. Preserve historical ONVIF-DEPLOY-SSH IDs and proofs above.

**UPDATE-01 proof (2026-10-06):** RED: new focused health tests failed because handler/types and TLS route did not exist; oversized metadata returned 200; exact 1024-byte JSON followed by the emitted newline also returned 200 (1025-byte body). GREEN: the response limit now includes the newline, and metrics `writeJSON` retains its original encoder implementation. `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./apps/edge-agent` and the exact-boundary regression PASS. Authenticated GET `/v1/health` is TLS-mux-only; exposes agent version, build version/commit/time/Go version and configured variant, max 1 KiB including newline, `no-store`; no token/environment dump. Dockerfile now injects build args into the agent binary. Tests are in-process only; no sockets/deployment/device checks. Rollback `main.go`, `tls_listener.go`, tests, and Dockerfile linker flags.

**UPDATE-02 runner proof (2026-10-06):** RED: initial focused tests failed to compile because the update contract did not exist. Later verifier regressions observed cleanup on an ambiguous prepare result and missing activation/rollback temp cleanup traps. GREEN: fake SSH tests cover pin-bound root target, no bearer upload/overwrite, preserved environment, allowlisted staged files, HTTPS health callback, transfer cleanup, activation/health rollback, mode-preserving `cp -p` rollback, uncertain rollback retention, incompatible architecture before writes, request validation, and secret-safe formatting. A prepare result without the explicit staged marker is treated as ownership-uncertain and is not cleaned; temp files created by activation/rollback use exclusive `mktemp` and exit/signal cleanup traps. Verification: focused `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision -run '^TestRunAgentUpdateDoesNotCleanupUnclaimedOrAmbiguousStage$|^TestAgentUpdateActivationAndRollbackTrapOwnedTemporaryFiles$|^TestAgentUpdatePrepareUsesExclusiveStageClaimBeforeSuccessMarker$|^TestRunAgentUpdate' -v`; `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision`; `GOCACHE=/tmp/openvms-go-build-cache go test -race -count=1 ./internal/provision`; `GOCACHE=/tmp/openvms-go-build-cache go test ./...`; `git diff --check` all PASS. No real SSH/TLS socket, DB, API, deployment or camera was used. The caller must supply a trusted manifest-verified artifact; runner enforces IPv4/TLS SAN/trust and compatible preflight. It only accepts HTTP-only/TLS-disabled installed agents whose exact systemd unit and token-file configuration match supported assumptions; existing-TLS replacement is not implemented. An abrupt remote process kill that bypasses shell traps can leave root-only temporary files; a lost/ambiguous stage claim intentionally leaves its generated root directory for operator inspection. Later API must authorize before reading sealed credentials/artifacts and use the built-in verified HTTPS health checker, not expose the injected test seam. Rollback `agent_update.go`, `agent_update_test.go`, `agent_install_test.go` fixture changes, and this proof; existing install flow remains insert-only and unchanged.

**UPDATE-02b proof (2026-10-06):** RED: service regressions failed to compile before the API/service contract existed; HTTPS/body-bound test failed before its strict middleware. GREEN: generated `POST /api/v1/servers/{serverId}/agent/update-ssh` and metadata-only job polling; host is loaded from registration (not request), the existing sealed bearer is only unsealed for runner health and never rotated/transferred, and the shared install reservation excludes races. `CREDENTIAL_TRUSTED_PROXY_CIDRS` ingress policy protects the new password route; strict JSON rejects unknown/trailing/oversized bodies. Permission-denial, pin/host/token preservation, wrong-server registration, unsupported existing-TLS refusal, job redaction and no TLS persistence after runner error are fake-tested. Success reauthorizes immediately before persistence, verifies unchanged tenant/host/sealed token in the write transaction, and records public CA/port plus fingerprint-only audit after verified health. `make generate`; `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision ./internal/api ./apps/api ./internal/store`; same-cache `go test -race -count=1 ./internal/provision ./internal/api`; same-cache `go test ./...`; `(cd apps/web && pnpm typecheck)`; and `git diff --check` PASS. No live DB, SSH, agent, camera, UI or deployment was used. Rollback: remove only this route/schema/generated types, update service wiring/tests and credential-ingress extension; do not revert earlier health or runner units.

**UPDATE-03 UI proof (2026-10-06):** RED: the new `Servers.test.tsx` cases failed because the existing-agent action/dialog was absent. GREEN: the Servers page now has a separate `update-ssh` action; the form accepts only SSH port, root password and required out-of-band SHA-256 pin (no editable host), requires both existing scoped permissions and HTTPS, clears password before request and on close/unmount, polls metadata only, handles lost jobs as unknown, and reports success only as authenticated HTTPS health. A shared in-component reservation disables conflicting install/update submissions. English, Spanish and Portuguese copy explains the fixed registered target and TLS-only result; the install flow remains separate. `pnpm exec vitest run src/routes/Servers.test.tsx` (31 tests), `pnpm typecheck`, full `pnpm test` (120 files/868 tests), scoped ESLint on the five touched UI paths, and `git diff --check` PASS. Tests use mocked API only; no SSH, server, deployment or camera was contacted. Rollback only this UI action/dialog, its localization keys, tests and task proof; API contract/backend are separate prior units.

**UPDATE-03 polling hardening (2026-10-06):** RED: regressions observed no 15-second GET deadline, an 11-minute timer that did not abort an in-flight poll, and a late response that could release the UI reservation. GREEN: each GET races against an abort signal and its 15-second timeout; the total 11-minute budget aborts pending GET, freezes the last safe job snapshot, guards state publication after expiry, and keeps the action in progress-view mode rather than enabling a blind retry. HTTP 409 uses generic conflict copy; it does not assume TLS is enabled. The pending/late-result tests verify no success promotion and no duplicate POST. The final correction guards both progress snapshot rendering and parent-job publication after expiry; the last safe active state remains available through “view progress,” preventing a blind retry. RED was observed for terminal state promotion after deadline; focused 34 tests, typecheck, full 120-file/871-test suite, scoped lint, and diff check pass. No remote operation was performed.

**UPDATE-02b persistence race hardening (2026-10-06):** RED: transaction regressions failed to compile before `persistAgentUpdateTLSInTransaction` existed. GREEN: capture the original agent row at request start and carry it through health verification; the write transaction locks the parent server and agent rows, rechecks both scoped permissions against the locked server, compares original tenant/host/HTTP port/sealed bearer with current row, then uses atomic `RegisterServerAgentTLSIfAbsent` and requires exactly one inserted row before audit. This prevents stale token rotation, host/port/tenant changes and concurrent trust creation from producing an incorrect/overwritten anchor. Fake query tests exercise the exact helper used by production `Store.Tx`, including revoked permission and concurrent insert; no live DB was used. `make generate`, provision/API focused + race suites, full Go, web typecheck and diff check PASS. Rollback removes the two `FOR UPDATE` queries, transaction helper, tests and update call-through; migration/schema are unchanged.

### Local Compose agent configuration slice (pre-deployment)

Optional local agent config/bootstrap only; it does not register the token, apply migrations, start containers, or test camera reachability.

| Action | Local-only instructions / guardrail |
|---|---|
| Select addresses | Choose non-overlapping subnet/API/agent IPs (examples: `172.29.240.0/24`, `.2`, `.3`); no host route scan. Set `OPENVMS_AGENT_SUBNET`, `OPENVMS_AGENT_API_IPV4`, `OPENVMS_AGENT_IPV4` in ignored `.env`; registered IP, SAN and API trust/port must agree. |
| Bootstrap local material | `deploy/agent/compose/bootstrap-local.sh <agent-ipv4> deploy/agent/compose/local` atomically creates a self-signed IP-SAN cert/key and random token only for a new directory. Ignored; modes dir/key/token `0700/0600`, cert `0644`. Never commit/print; development only, not production PKI. |
| Registration gate | Do not enable yet: registration is unrun and token unregistered/unsealed. Never retrieve token from an SSH-installed agent. |
| Configure runtime | Set `ONVIF_ALLOWED_CIDRS`, `ONVIF_DISCOVERY_INTERFACES`, UID/GID to verified values. Container UID/GID must own `0700/0600` files; don't weaken modes. Agent profile uses a fixed-IP private bridge; API joins; no host ports. Multicast discovery may not pass the bridge. |
| Start only when ready | Agent shares API image; HTTP metrics/update and HTTPS ONVIF listener stay on Compose network. Credential probe is verified HTTPS only. Verify TLS metadata migration/registration before profile startup; no plaintext fallback. |

**Observed config proof (2026-10-05):** RED because the edge-agent service/network were absent. PASS: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./deploy/agent/compose`, same-cache race test, `go test ./...`, bootstrap `sh -n`, both `docker compose config --quiet` profiles, and `git diff --check`. Tests verify generated IP-SAN certificate trust, modes, invalid IP, no-overwrite, atomic destination claim (RED exposed `mv -T` replacing a competing empty directory), and safe cleanup. Bootstrap now atomically claims output with `mkdir`. No container, migration, DB, network, or camera was accessed; registration remains unrun. Rollback: remove Compose `edge-agent`/`onvif-agent` service/network config and `.gitignore` entry; existing API/agent listeners stay unchanged.

### Secure local registration command (implementation only; not run)

`vmsctl agent-register` registers an existing server only and refuses replacement. A private `--session-file` supplies an active, non-revoked, non-expired OpenVMS session credential; its hash determines the actor, never argv. `SESSION_IDLE` uses the API's config loader/default. The hashed session and idle limit are revalidated in the write transaction before mutation; user, tenant, and session ID must match. Both scoped `servers.manage` and `servers.config.secrets` checks run before reading token/CA files and again in that transaction. The bearer token is read privately, sealed with AES-GCM/server UUID AAD, and never returned. Public CA, fixed IPv4, and distinct HTTP/HTTPS ports are persisted with secret-free audit. Agent/TLS/audit writes are atomic and insert-only. No implicit migration; schema <32 fails. No lock is taken; concurrent revocation after lookup may race.

Unrun template: `vmsctl agent-register --server-id "$SERVER_UUID" --session-file "$SESSION_FILE" --token-file deploy/agent/compose/local/agent.token --ca-file deploy/agent/compose/local/tls.crt --agent-ipv4 "$AGENT_IPV4" --http-port 7419 --https-port 7443`. Never put credentials in argv, history, output, or logs.

**TDD evidence (2026-10-05):** RED: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision` and `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./apps/vmsctl` failed before service/CLI code; `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision -run TestRegisterLocalAgentRejectsCrossTenantActorBeforeWrites` failed before tenant binding. GREEN: `make generate`; `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./apps/vmsctl ./internal/provision ./internal/store`; `GOCACHE=/tmp/openvms-go-build-cache go test -race ./apps/vmsctl ./internal/provision`; `GOCACHE=/tmp/openvms-go-build-cache go test ./...`; `git diff --check` — all PASS. CLI, DB, migration, registration, container, network and camera were not run. Rollback: remove new CLI/provision files, dispatch/tests/seams, revert insert-only query, `make generate`.

**Session revalidation follow-up (2026-10-06):** RED: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./apps/vmsctl -run TestLocalAgentSessionQueryUsesConfiguredIdleLimit` failed for the missing idle-query helper; `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision -run TestRegisterLocalAgentRejectsSessionChangedBeforeWrites` accepted mismatched session state. Added short/long idle-bound cases and write-transaction hash/idle forwarding plus actor/session-ID checks; the focused, race, and full commands above pass. No live session/DB test; concurrent revocation after lookup may race because no session row lock is taken.

### Local web HTTPS ingress (deployed and verified; agent setup pending)

| Unit | Current behavior and limit |
|---|---|
| ONVIF-DEPLOY-HTTPS-01 | Preserve HTTP `8000` → `80`; serve HTTPS `8443` → `443` from read-only self-signed IP-SAN cert/key mounts using TLS 1.2/1.3. No auto-cert/redirect; bootstrap atomic/no-overwrite; ignored modes `0700/0600`. Deployed. |
| ONVIF-DEPLOY-HTTPS-02 | Global `default_sni` selects the configured HTTPS IP for no-SNI clients; certificate verification stays enabled. Deployed in web image at `f64a51a`. |
| Proxy binding | Web `.4` and API `.2` use private+default networks; Caddy targets `.2:8080`, API trusts only web `/32`. Agent stays `.3`. |

**HTTPS ingress source proof (2026-10-06):** Initial RED: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./deploy/agent/compose -run 'TestWeb(Compose|TLS)'` failed for missing port/bootstrap; second focused RED caught unsafe short mounts. HTTPS regression RED: added `default_sni` contract assertion failed on the existing Caddyfile. GREEN: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./deploy/agent/compose`; `GOCACHE=/tmp/openvms-go-build-cache go test -race ./deploy/agent/compose`; `GOCACHE=/tmp/openvms-go-build-cache go test ./...`; `sh -n deploy/docker/bootstrap-local-tls.sh`; `docker compose config --quiet`; `git diff --check` PASS. Isolated candidate Caddy validation and IP-URL curl with `--cacert` completed TLS (HTTP 502 without API network). RFC 6066 disallows IP literals in SNI; Caddy `default_sni` selects the IP-SAN cert for no-SNI clients without disabling verification ([Caddy docs](https://caddyserver.com/docs/caddyfile/options), [RFC 6066](https://www.rfc-editor.org/rfc/rfc6066.html)). Active image/service changed only in the later web-only redeploy recorded below.

**Local deployment (2026-10-06; web active, agent pending):** Backup `.atl/onvif-deploy/openvms-https-5c8ed21.dump` (`0700/0600`, 20,317,434 bytes; SHA-256 `5c943ed3ae2dd628cd503f71a386c51fdda4e446ca5357f540f9eb0ac501a5ab`) verified by `pg_restore --list`. Rollbacks: API `openvms-api:rollback-5c8ed21` (`1765af31de0b`), web `openvms-web:rollback-5c8ed21` (`2c99d40807b9`). Local self-signed cert/key are ignored (`0600`), IP SAN `10.1.1.24`, SHA-256 `EA:CB:77:41:5A:A2:12:57:E7:49:84:14:DF:4A:DD:E9:44:1A:53:CC:60:AD:51:AD:47:97:E7:D6:B1:60:0C:AD`; explicitly trust cert, never bypass TLS. HTTPS `8443` and HTTP `8000` health/root return 200; anonymous install `{}` HTTPS 401/HTTP 403. Web `.4` → API `.2`. Web active image `sha256:220021578833aca6b167c5e3fb1284579bba12d13099cb12ffe093e656a99350`; no volume deletion/DB restore.

**Schema v33 local deployment (2026-10-06; HEAD `dc21d1a`):** Backup `.atl/onvif-deploy/migration33-pre/postgres-schema32.dump` (`0700/0600`, 20,722,647 bytes; TOC verified). Prior API rollback tag `openvms-api:rollback-pre-schema33-dc21d1a` → `sha256:552c6675b90ee95ffe7514bdf6b5e689ba3a8a97f2adad01ead2ad8b60283ad5`. Built only API (`docker compose build --pull=false api`). One-off initially collided on API static IP before migration; an ephemeral `!reset` override removed only that IP and kept API running. `vmsctl migrate` and no-op rerun PASS, version 33. Catalog: TLS columns/table, parent composite UNIQUE, validated composite FK/constraints, valid+ready indexes, RLS enabled+forced, `tenant_isolation` ALL policy using/check `app_tenant_visible(tenant_id)`. Restarted API only; active API image `sha256:498e8882085aa5ca755e16ecfaca1f2dddacf244afa248b5f13e215dc581f562`, web image unchanged; `.2`/`.4` preserved. Health HTTPS (verified with `--cacert`) and HTTP 200; anonymous install HTTPS 401/HTTP 403. No session, agent registration, SSH or camera. Keep rollback artifacts; do not restore DB.

**Local API+web redeployment (2026-10-06; HEAD `8e6611a`):** Protected backup `.atl/onvif-deploy/update-8e6611a/postgres.dump` (`0700/0600`, 21,749,285 bytes; SHA-256 `efcc97c5ea28f51df222a1c11b4b9fbe4c462a50400a14261bbbd08f7f18a3a8`); `pg_restore --list` succeeded in the running Postgres container; TOC has 514 lines (SHA-256 `94d33ae1cfb9aa66289ad2dd1af5fdbb5d12a8fb7d28df526de662e5172cee68`). Goose version was already 33, no migration run. Rollback tags: API `openvms-api:rollback-pre-update-8e6611a` → `sha256:498e8882085aa5ca755e16ecfaca1f2dddacf244afa248b5f13e215dc581f562`; web `openvms-web:rollback-pre-update-8e6611a` → `sha256:220021578833aca6b167c5e3fb1284579bba12d13099cb12ffe093e656a99350`. Built with `docker compose build --pull=false api web`, then recreated only API/web using `docker compose up -d --no-deps api web`; Postgres and all data volumes untouched. New images: API `sha256:77ed07d178a58ec59acbd410a5c6ce275e281c62dc1e9c9ff47856f4ff09abdd`, web `sha256:3d4c434a1fc99a000b1a344f842f0387a53ce2580852e7a88c049112ed78be06`. Verified HTTP `8000` and HTTPS `8443` health/root return 200; HTTPS used `--cacert deploy/docker/local-tls/tls.crt` (no verification bypass). Anonymous update route: HTTPS `{}` returns 400 because strict body validation precedes authentication; HTTPS valid-shape dummy request with no auth returns 401; HTTP `{}` returns 403 at the HTTPS ingress guard. No real credentials, authenticated session, agent update/SSH, or camera request was used. A pre-existing `openvms-relay-1` compose orphan warning was left unchanged. Web UI is deployed; existing agent remains un-updated and TLS health is not established.

## Immediate deployment-test readiness checklist

Read-only test gates; do not inspect ambient SSH, secrets, sessions or remote hosts.

| Gate | Required evidence |
|---|---|
| Browser/proxy | HTTPS 8443 with `--cacert`; explicitly trust cert; HTTPS 401 / HTTP 403; keep web `.4` → API `.2` exact `/32`. |
| Schema | v33 deployed and catalog checked above. |
| Agent TLS | Register matching IP-SAN, HTTPS port and CA; no plaintext fallback. |
| Camera | Approved target/CIDR/interface only; temporary creds; device info/services/time only. |
| Permissions | Both scoped permissions; denial before credentials/outbound. |
| Remote | Explicit destination, operation and credential/session; no ambient access. |
| Acceptance | Redaction, bounds, no saves/XAddr following/physical writes. |

Record scope, outcome and rollback. On any failed gate, stop; no plaintext fallback or broader trust.

## Compact implementation evidence ledger

Historical work-unit snapshots; current readiness is above. Exact Go runner: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 <packages>`; formatting: `gofmt -w <changed files>`. Protocol/transport tests use injected fakes only; no camera, real agent, or production listener was used. Review boundary `19315be` unless shown. Preserve rows as historical evidence, not current readiness.

| Unit/status | Observed RED → GREEN | Scope/rollback/commit |
|---|---|---|
| ONVIF-01 foundation — complete | RED missing parser/client/config APIs; protocol tests caught SOAP action and HTTP 500 fault handling. Focused/race/full Go PASS. | `internal/onvif/client.go`; remove files/tests. Commit `19cbca2`, 375 lines; medium, under budget, no approval claimed. |
| ONVIF-02 device protocol — partial | RED missing device client/service; fixed namespace-wrapper decoding. Focused/race/full Go PASS. | `device.go`/tests + wrapper change; revert these only. XAddrs are validated, never automatically followed. |
| ONVIF-02b WSSE — partial | RED absent credential/nonce/time injection. GREEN tests digest order/vector, XML escaping, no plaintext, mustUnderstand, retry nonce, RNG failure/no request, redaction; focused/race/full PASS. | Revert auth-only change; preserve `19315be`. SHA-1 is protocol-required, not storage/confidentiality. |
| Transport reliability — partial | Focused/race/full PASS after gofmt; transient 5xx and read-only retry policy. | Revert isolated transport/tests; ONVIF-02 incomplete. |
| ONVIF-02c parser — partial | RED missing parser; later RED caught wrong discovery namespace/type and endless invalid-packet loop; GREEN namespace/cancellation fix; focused/race/full PASS. | Revert parser corrections; fake input only; Core spec 5.3/7.3. |
| ONVIF-02d UDP — partial | RED absent session/transport; GREEN focused/race/full PASS with fake datagrams. | Revert UDP/session API; no production sockets/interface routing. |
| Edge-agent discovery — partial | RED missing handler/config; GREEN focused edge-agent/discovery, race/full PASS. | Remove route/handler/endpoint method. Uncommitted at `6d5c6df`; no socket/interface lookup. |
| Edge-agent mux tests — complete subunit | RED missing `buildMux`; edge-agent/race/full Go PASS. | Revert extraction/tests; recorder only, no commit. |
| Central discovery relay — partial | RED missing `discoverAgent`; focused provision/API/app-api, race, full Go, web typecheck and generation PASS. Fakes prove authority/body/auth/bounds/redirect denial. | Revert service/handler/router/schema/generated output. Uncommitted at `4d878c0`, ~463 authored lines, no commit/review/network. |
| Central deny/redirect — complete subunit | RED missing auth seam; focused/race/full, typecheck and diff PASS. Redirect defense unconditional. | Revert seam/tests only; no DB/network. |
| Cameras discovery UI — partial | RED 3 tests; GREEN 9 focused, typecheck, web 120 files/846 tests PASS (nonfatal jsdom diagnostics). | Revert route/test; mocked API only. Parent commit `ebc7f41`, 298 lines. |
| UI lifecycle hardening — partial | RED server/interface/unmount-abort/deny tests; GREEN eslint, focused 12, typecheck, web 120/849, diff PASS. | Abort stale requests; mocked only. `8a10e48` candidate declined; no approval. |
| Earlier ONVIF state — historical | Probe transport/TLS trust were then unconnected. | Superseded by relay/UI `712b56d`/`daafa0d`; persistence, identity and device validation remain. |

### Historical implementation snapshots: TLS, relay, and read-only probe

- **Optional agent TLS listener:** all three settings are required; absent disables, partial/bad/unreadable/mismatched fails closed. Separate TLS mux exposes credential-free discovery; HTTP metrics/update unchanged; TLS ≥1.2 and coordinated shutdown. RED: edge-agent missing loader/mux; GREEN `go test -count=1 ./apps/edge-agent ./internal/agent/... ./internal/provision ./internal/onvif`, relevant race, full Go PASS; temporary cert/recorder only. Rollback removes listener/config wiring; preserve HTTP.
- **Verified HTTPS client:** `internal/provision/agent_tls.go` fixes authority to registered IPv4/secure port, supports explicit system/custom CA, TLS 1.2+, verified x509 IP/SAN, no proxy/redirect/HTTP fallback, bounded timeout. RED missing builder; GREEN focused provision/agent suites, race, full Go PASS; cert-object and fake transport tests reject wrong IP/CA/expiry. Not wired to camera API then; remove helper/tests to rollback.
- **TLS trust DB/API:** `server_agent_tls` stores tenant-scoped public CA/secure port/trust mode only. Scoped server auth, validation and fingerprint-only audit; no key/password. RED missing types/API, then missing composite key. GREEN generation, focused/race/full Go, typecheck/diff PASS; embedded tests only at that time. Roll back migration/query/models/API and regenerate.
- **Agent TLS integrity:** migration 32 added `(server_id,tenant_id)` FK/parent UNIQUE; RED missing-key migration test; GREEN migration/provision/API/store focused and race, full Go/typecheck/diff PASS. Down removes child before parent; no live DB proof at that unit.
- **TLS configuration API:** authenticated server-scoped GET/PUT/DELETE; strict bounded JSON permits only trust mode/port/public CA, rejects private keys/trailing/oversize. RED missing handler/schema; GREEN generated contract, focused/race/full Go, web typecheck/diff PASS with recorder auth tests. No DB/network then.
- **Agent TLS-mux ONVIF probe:** HTTPS-only `/v1/onvif/probe`, bearer/CIDR policy, strict endpoint JSON, transient paired credentials, three read-only SOAP calls, bounded time/body, sanitized output; no redirect/XAddr follow. RED absent handler/result; focused agent/ONVIF/provision, race/full Go PASS; fake recorder tests cover denial/bounds/redaction. No socket/camera; rollback route/wiring.
- **Central probe relay:** server-scoped API requires `servers.manage` + `servers.config.secrets` before TLS/token/outbound, then fixed registered host/trust/port and verified HTTPS; safe bounded projection. RED absent service/route; GREEN generated contract, focused/race/full Go and web typecheck/diff PASS. Later RED found missing top/nested response fields accepted as zero; validator now rejects them. Fakes only; no live DB/network. Roll back relay/API wrapper/OpenAPI and regenerate.
- **Cameras UI:** generated same-origin relay, dual permissions, explicit endpoint; credentials transient/cleared, stale requests aborted, no storage/autoprobe/persistence. RED missing controls and stale cleanup race; final focused 19, typecheck, web 120 files/856 tests, diff PASS. Earlier unrelated full-suite failures did not reproduce; cause unknown. API mocks only; revert route/tests.

### Forward repair for stale TLS schema marker — source proof; now deployed

Catalog initially showed Goose v32 with TLS table/key absent. RED: missing-v33 test, Goose parser (37 Up/3 Down splits), and weakened FK/index/validation guards failed. GREEN added statement markers/exact catalog checks and fake-driver Goose parser tests; `make generate`, migration/provision/store focused + race, full Go, `git diff --check` PASS. v33 is forward-only/refusal Down. No live DB at that source unit; local deployment proof is recorded above.
