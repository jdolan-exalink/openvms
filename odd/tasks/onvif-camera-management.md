# ONVIF Camera Management

Deliver capability-driven ONVIF agent/API management, safe Frigate synchronization, and auditable operations. The five phases extend beyond CRUD.

## Current implementation status — 2026-10-06

- `712b56d` (relay) and `daafa0d` (UI) connect selected-server read-only probing; success is device/services/time, not saved cameras.
- Read-only flow, secure registration, SSH install, and local HTTPS source/config are implemented but **not deployed**. Tests use fakes/mocks; no production cert, real camera, or production DB test exists.
- Local steps 1–3 are authorized, not executed. No destination/session is authorized for SSH; Casa and camera acceptance remain pending.
- Proof: Cameras 19 tests; Servers 26; typecheck PASS; web 120 files/863 tests. Not device acceptance.
- Product promise: 100% support of the explicitly declared capability/profile matrix, not every vendor's proprietary extension or every ONVIF function on every device. Unsupported capabilities must remain unavailable in the UI.

## Goal and boundaries

- **Outcome:** capability/operation discovery and management, imaging/video/network/time, PTZ/audio, advanced events/analytics, stable VMS identity and safe Frigate lifecycle.
- **Scope:** agent/API/UI code and SSH-install workflow; use fakes. Remote SSH requires separate destination/operation/credential authorization. Camera/physical operations likewise need explicit authorization.
- **Frigate:** synchronize configuration; preserve recordings on deletion; discard requires explicit confirmation and fails closed; no factory reset.
- **Reliability:** stable IDs, deletion tombstones, encrypted write-only secrets, capability-driven UI, bounded operations, audited/read-back mutations.
- **Delivery:** `ask-on-risk`, user-approved `stacked-to-main`; no push/PR/merge. Keep tests/docs with Conventional Commits; 400 lines is guidance, not a cap.
- **TDD:** Strict TDD is enabled. Record observed RED/GREEN/REFACTOR per implementation task; never infer results.

## Baseline and current state

- Feature branch: `feat/onvif-camera-management`, created from local `main` at `3d44726ff04f61775115e9cd37dc0c2db8cb5c0a` (`chore: ignore local soc map mockup reference`). At task creation `HEAD` and `main` resolve to the same commit; do not copy unrelated future changes from `feat/maps-phase2`.
- Baseline: ONVIF config lived in Frigate, not native agent control. Per-camera PATCH edits existing sections; add/remove needs raw config PUT or safe agent capability. Secrets require `servers.config.secrets`; Frigate <0.16 cannot edit config. Recording reads exist, no DELETE. Sync key `(server_id, remote_name)` can resurrect deleted cameras; tombstones are required. Preserve remote keys for identity; change display names.
- Runners: `go test -race ./...`, `pnpm test`, `pnpm typecheck`; focused Go package and `pnpm exec vitest run <test-file>`. Initial planning made no source/test/device changes; evidence follows.

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

- [ ] All five phases above are complete; partial CRUD is not feature completion.
- [ ] Device capabilities/operations determine available API operations, UI states, and option lists; unsupported operations are not offered. “100%” refers to the declared supported capability/profile matrix, not all vendors' proprietary behavior.
- [ ] Secrets are encrypted at rest and write-only through APIs/UI; logs, errors, audit, and stream metadata redact credentials.
- [ ] Every physical mutation is bounded, authorized, audited, and verified by readback; failures report uncertainty honestly and do not claim success.
- [ ] Frigate config follows the device/VMS lifecycle without changing stable identity; deleted cameras remain suppressed from import until explicitly restored.
- [ ] Recording preservation is the default; discard is an explicit, confirmed, fail-closed path. No factory reset is introduced.
- [ ] Unit and applicable integration checks pass; physical/LAN validation remains pending unless separately authorized by the user.
- [ ] Each work unit records tests, any runtime harness scenario (or `N/A` with reason), rollback boundary, commit ID, and review due/outcome against the preceding reviewed boundary. Do not start a review or change the user-owned review switch from this plan.

## Delivery and progress record

- Strategy `ask-on-risk`; user-approved `stacked-to-main`. No push, PR, or merge authorized.
- ONVIF-02 partial: device reads/WSSE/discovery, TLS listener/client/config, probe relay and Cameras UI are local. Persistence, stable VMS mapping, deployment and device/database validation remain absent. Parent commits `712b56d`, `daafa0d`; no PR/deployment. Planning `134a319`; review boundary `19315be`; later candidates declined.
- Thousands of authored lines expected. Keep cohesive units and count additions/deletions; use approved stacking. Do not omit behavior/tests for line budget.
- Per unit record ID, observed RED/GREEN/REFACTOR, checks, runtime harness or justified N/A, rollback, commit/lines, risk/review status, and later PR boundary.
- Next: parent review/authorize HTTPS ingress deployment; verify HTTPS, exact proxy peer trust, schema and permissions before separately authorized remote SSH. ONVIF-02 remains partial; probe is not persistence.

### Existing-server SSH agent installation (local implementation; not deployed)

Local steps 1–3 are authorized only on this machine; remote SSH execution is not yet authorized. Frigate URL does not prove the SSH target. The UI collects explicit SSH host/port, root password, and pinned fingerprint (no TOFU). Check both scoped permissions before dial; bind a bounded memory job to the existing server.

| ID | Unit | Security/scope |
|---|---|---|
| ONVIF-DEPLOY-SSH-01 | Backend/API + dedicated agent-only runner | Verify fingerprint before password auth. SSH username/password never enter DB, job snapshots, logs, files, or audit; Go strings do not guarantee zeroization. A separately sealed server-bound agent bearer token is distinct. No new server or broad provisioning. |
| ONVIF-DEPLOY-SSH-01a | Pinned SSH runner foundation (implemented locally; not callable from API) | Explicit IPv4/port, root user, password, required SHA-256 pin; fake dial/SSH. SFTP payload only. Bounded preflight/install; no overwrite; allowlisted agent files, optional supplied cert/key 0600 with matching IP SAN. Safe `String`/`GoString`/JSON projection excludes secrets and payload bytes. No API/job store, HTTP fallback, or actual remote connection. |
| ONVIF-DEPLOY-SSH-01b | Existing-server API install + ephemeral progress (implemented locally; not deployed) | POST/poll; scoped permissions before agent/binary access; strict request; trusted artifact; generated token/TLS; insert-only registration; transient credentials. Direct TLS or single forwarded HTTPS from explicit immediate-peer `CREDENTIAL_TRUSTED_PROXY_CIDRS`; API's unset default denies, Compose now supplies only web `/32`. Worker gets a fresh bounded context; early reservation precedes artifact/token work; bounded artifact reads reject symlinks. |
| ONVIF-DEPLOY-SSH-02 | Selected-server Servers UI (implemented locally; mocked API only) | Credentials stay in an uncontrolled password input/ref only until submission, then clear immediately; never browser storage, URL, logs, response echo, or audit. HTTPS guard, dual scoped permission gate, safe bounded progress/errors. Closing active jobs does not cancel remote work; unmount/filtering loses local job tracking, and a lost job is unknown, not success. |

Runner changes only OpenVMS agent files/systemd and optional supplied TLS material; no packages, Docker/Compose, Frigate, NTP, or network. It checks OS/architecture, bounds steps, refuses overwrites/retries. Probe readiness requires verified HTTPS/trust, sealed token, and health. Fake tests only; actual SSH needs separate destination/session authorization.

Failures may leave partial files; no remote retry/cleanup; manual remediation required.

**01a proof (2026-10-06):** RED: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision -run TestRunAgentInstall` failed: runner undefined. GREEN: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision`; `GOCACHE=/tmp/openvms-go-build-cache go test -race ./internal/provision`; `GOCACHE=/tmp/openvms-go-build-cache go test ./...`; `git diff --check` — all PASS. Fake-only tests cover validation, pin mismatch, modes/SAN, cancellation, redaction, scope. `github.com/pkg/sftp` 1.13.11. No live calls. Rollback: remove `agent_install.go` + tests and SFTP/pinned-port additions in `ssh.go`; revert SFTP dependency. No commit/review.

**SSH-01b ingress design (2026-10-06):** Existing `secureRequest` and `TRUST_FORWARDED_FOR` do not authenticate the proxy peer. `CREDENTIAL_TRUSTED_PROXY_CIDRS` config is fail-closed on malformed values; only the SSH-install route accepts one forwarded `https` value from an allowed immediate peer, or direct TLS. Compose supplies the stable Caddy `/32`; app unset default denies. Existing global cookie/security behavior stays unchanged.

**SSH-01b proof (2026-10-06):** RED: contract/service tests observed absent routes/service. GREEN: `make generate`; focused, race and full Go suites PASS. Coverage: proxy spoof/multi-value/CIDR, bounds, permission-before-row/artifact, existing-agent conflict, redaction, fake runner success/failure, artifact integrity, generated cert trust. No live operations. Pending: proxy deployment config, live DB/SSH, agent HTTPS health and camera acceptance. Rollback: remove SSH-01b service/routes/schema/query and trusted-proxy code.

**SSH-02 UI proof (2026-10-06):** RED: focused UI tests failed because the install action was absent. GREEN: `cd apps/web && pnpm exec vitest run src/routes/Servers.test.tsx` (26 tests), `pnpm typecheck`, `pnpm test` (120 files/863 tests), scoped eslint for the touched route/test/locales, and `git diff --check` PASS. UI mocks only; no SSH/deployment/database/camera calls. Full-repo `pnpm lint` previously failed only in unrelated `Layout.tsx`, maps, and `ui.tsx` (9 errors/1 warning); scoped lint passes. Registration success is not HTTPS health or ONVIF readiness. Rollback `Servers.tsx`, its tests, and the three locale dictionaries. No commit/review.

**SSH-01b post-review hardening (2026-10-06):** RED: focused regressions first failed to compile due to the missing credential seam. GREEN: isolate request context values, reserve/release job slots before preparation, bound/no-follow regular artifact reads; tests cover context isolation, duplicate/capacity rejection, failure release, bounds and symlinks. `make generate`, focused/race/full Go suites, web typecheck and diff check PASS. No remote/live operations.

**01a serialization hardening (2026-10-06):** RED: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision -run TestAgentInstallRequestSerializationRedactsSecrets` exposed password/token and TLS payloads in default struct formatting. GREEN: custom safe `String`/`GoString`/JSON projection and `json:"-"` field tags now exclude password, token, binary, certificate, and private-key payloads. Focused serialization test, provision package, provision race test, full Go suite, and `git diff --check` PASS. The runner still accepts explicit binary/token/payload arguments at the internal boundary; a future authenticated API wrapper must source/validate artifacts and enforce permissions before calling it. No API/job persistence or remote connection.

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

### Local web HTTPS ingress prerequisite (source/config only; not deployed)

| Unit | Current behavior and limit |
|---|---|
| ONVIF-DEPLOY-HTTPS-01 | Preserve HTTP host `8000` → Caddy `80` unchanged; add HTTPS host `8443` → Caddy `443` with explicit cert/key read-only mounts and TLS 1.2/1.3. Caddy uses a manual self-signed IP-SAN certificate, so it does not auto-create certs or globally redirect HTTP. Bootstrap creates only ignored local files, atomically refuses an existing output directory, and sets directory/cert/key `0700/0600`. No local cert has been generated or deployed. |
| Proxy binding | Web is assigned stable `172.29.240.4` on the existing private bridge, keeps the default network, and explicitly targets API `172.29.240.2:8080`; API credential-route trust defaults to the web peer `/32`, never the whole bridge. Keep API `.2` and agent `.3` aligned with the configured subnet. |

**HTTPS ingress proof (2026-10-06):** RED: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./deploy/agent/compose -run 'TestWeb(Compose|TLS)'` failed with missing HTTPS port/bootstrap; added bind guard then focused test failed because short mounts could create absent host paths. GREEN: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./deploy/agent/compose`, `GOCACHE=/tmp/openvms-go-build-cache go test -race ./deploy/agent/compose`, `GOCACHE=/tmp/openvms-go-build-cache go test ./...`, `sh -n deploy/docker/bootstrap-local-tls.sh`, `docker compose config --quiet`, and `git diff --check` PASS. Tests validate both listeners, stable network/peer/upstream/trust, read-only no-create mounts, IP SAN via Go x509, modes, invalid IP, overwrite refusal/atomic claim. Caddy binary unavailable for `caddy validate`; no containers built/started, no cert generated outside test temp, no DB/migration/SSH/camera. Rollback: Compose/Caddy/ignore/bootstrap/tests only; no running service changed.

## Immediate deployment-test readiness checklist

Controlled **read-only** testing requires these gates. Do not inspect ambient SSH, secrets, sessions, or remote hosts to fill gaps.

| Gate | Required evidence before a deployment/device test |
|---|---|
| Browser HTTPS / proxy | Use `https://10.1.1.24:8443` only with matching self-signed IP-SAN cert and explicit browser trust; never bypass errors. HTTP `:8000` stays, but credentials are blocked. Keep Caddy `.4` → API `.2`, trust only `/32`; deploy/verify first. |
| Database/schema | Apply/verify TLS metadata migration on authorized DB; confirm server's intended tenant/agent. No real migration tested. |
| Agent TLS | Operator cert IP SAN must match registered agent IP; configure three listener settings, secure port, matching system/custom CA trust; verify no HTTP fallback. |
| Camera policy | Configure interface and `ONVIF_ALLOWED_CIDRS`; test only approved camera/IP and temporary credentials. |
| Permissions | Verify scoped `servers.manage` and `servers.config.secrets`; test denial without sending credentials. |
| Remote authorization | Name destination, operation, and credential/session before remote work. Until then stay local; don't probe or reuse sessions. |
| Read-only acceptance | Only device info/services/system time; verify safe projection, no save/secret leak/XAddr follow. Test trust, IP SAN, CIDR, deny, timeout, unavailable-agent failures. No setting/PTZ/audio/I/O/network/time mutations. |

Record exact destination, authorized operation/session scope, software versions, observed outcomes, and rollback/revocation. If any gate fails, stop and report; do not fall back to plaintext or broaden trust.

## Remaining feature phases

ONVIF-02 through ONVIF-19 remain tracked above; ONVIF-02 is partial.

## Compact implementation evidence ledger

**Historical work-unit snapshots:** each row and following subsection records its state at that unit's completion. Headings/phrases such as “not wired,” “no central API,” and “next bounded slice” are historical, not current readiness claims. Use the current-status and readiness sections above for the present state; preserve old test/rollback/commit facts below as evidence.

Conventions: exact Go runner is `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 <packages>`; Go formatting uses `gofmt -w <changed Go files>`. Every HTTP/SOAP/UDP test described here used injected fakes/recorders; no real camera, LAN, remote operation, credential, or production listener was used. Commit/review statements are per the worker who performed that unit; parent-owned commits and review decisions are never inferred. Review boundary remains `19315be` unless noted.

| Unit / status | Observed RED → GREEN and additional proof | Scope, rollback, commit/review |
|---|---|---|
| ONVIF-01 foundation — complete | RED: focused `./internal/onvif` compile failed for absent `ParseEndpoint`, `NewClient`, `Config`; initial runs also caught SOAP action and HTTP 500 fault handling. GREEN/REFACTOR: focused, `go test -race ./internal/onvif`, and `go test ./...` PASS. | `internal/onvif/client.go`, tests; remove both to rollback. Commit `19cbca2`, 375 authored lines; native assessment vs `134a319`: medium / `review_due=false` / under budget; no approval claimed. Mock transport only. |
| ONVIF-02 device protocol — partial | RED: focused `./internal/onvif` compile failed for missing `NewDeviceClient`/`Service`, then exposed namespaced response-wrapper decode mismatch. GREEN and focused/race/full Go tests PASS. | `internal/onvif/device.go` + tests and SOAP wrapper preservation in client; remove device files and revert wrapper change. No worker commit/review. XAddr must be validated; no returned URL is followed automatically. |
| ONVIF-02b WSSE — partial | RED: focused test failed because credentials option/nonce/time injection absent. GREEN covers digest vector/order, XML escaping, no plaintext, mustUnderstand, fresh retry nonce, random failure/no request, redaction; focused/race/full Go tests PASS. | Revert auth-only client/tests; prior protocol remains. No commit/review; preserve `19315be`. SHA-1 is protocol-required UsernameToken digest, not storage; no confidentiality. Source: OASIS UsernameToken Profile 1.0 (linked in prior evidence). |
| ONVIF transport reliability — partial | Focused package, race package, and full Go tests PASS after gofmt. Covers transient 5xx retries/read-only policy and SOAP extraction. | Isolated transport changes/tests; no commit/review. ONVIF-02 incomplete. |
| ONVIF-02c WS-Discovery parser — partial | RED: focused `./internal/onvif` failed for absent parser API; GREEN/focused/race/full Go tests PASS. Later protocol correction RED caught wrong 2009/01 namespace/type and an endless invalid-packet loop; GREEN after legacy namespace/cancel checks. | Revert discovery parser/correction files only; no commit/review; preserve `19315be`. Core source cited: ONVIF Core Specification sections 5.3/7.3. |
| ONVIF-02d UDP transport — partial | RED: focused tests established session API/transport absent. GREEN/REFACTOR: focused/race/full Go tests PASS. Fake datagrams only; production interface routing and socket behavior were not executed. | Remove `internal/onvif/discovery_udp.go` + tests and revert session API migration. No commit/review; boundary `19315be`. |
| Edge-agent discovery — partial | RED: `./internal/agent/onvifdiscover` failed for absent handler/config. GREEN, then focused edge-agent/onvif/discover tests, race, full Go tests PASS. | `apps/edge-agent/main.go`, `internal/agent/onvifdiscover/`, endpoint string support; remove route/handler and endpoint method to rollback. Uncommitted at `6d5c6df`; credential-free bearer route, explicit interface/CIDR config; no socket/interface lookup. |
| Edge-agent mux tests — complete subunit | RED: `./apps/edge-agent` failed because `buildMux` absent. GREEN: edge-agent; race edge-agent/onvif/discover/onvif; full Go tests PASS. | Revert `buildMux` extraction/tests in `apps/edge-agent/main.go` and `main_test.go`. In-process recorder tests; no commit/review. |
| Central discovery relay — partial | RED: `./internal/provision` failed because `discoverAgent` absent. GREEN/final: focused provision/API/app-api, race provision/API, full Go, `pnpm typecheck`, `make generate` PASS. Fake RoundTripper/recorders prove exact target/body/auth, strict bounds, redirect refusal and credential-free projection. | Service/API/OpenAPI and regenerated Go/TS files; rollback service + handler/router/schema changes and run `make generate`. Uncommitted at `4d878c0`, ~463 authored lines (generated excluded); exceeds advisory budget for coherent security/contract coverage; no commit/review. No real agent/network. |
| Central deny + redirect regression — complete subunit | RED: focused provision compile failed for absent auth seam. GREEN and final focused provision/API/app-api, race provision/API, full Go, `pnpm typecheck`, `git diff --check` PASS. | Revert seam and denial/redirect tests in provision service/tests; redirect defense remains unconditional. No DB/network/commit/review. |
| Cameras discovery UI — partial | RED: 3 new `Cameras.test.tsx` cases failed. GREEN 9 tests; typecheck and full web suite PASS (120 files/846 tests; jsdom media-load diagnostics nonfatal). | Revert Cameras route/test. API mocked; no camera requests or credentials. Parent commit `ebc7f41`, 298 authored lines, not a worker commit. |
| UI lifecycle hardening — partial | RED: 3 server/interface abort + deny-control cases failed; standalone unmount abort also failed. GREEN after fix: eslint, focused 12 tests, typecheck, web suite 120 files/849 tests, diff check PASS; media-load diagnostics nonfatal. | Abort on server/interface change/unmount; deny suppresses controls. No live requests. Earlier `8a10e48` candidate was declined (no approval); no new review/commit by worker. |
| Current ONVIF state — historical snapshot | At this earlier point, probe transport and TLS trust API were not connected; do not use this row as the current status. | Superseded by the connected relay/UI commits `712b56d` and `daafa0d`; stable identity, persistence, real-device validation and later phases still remain. |

### Historical implementation snapshot: optional agent HTTPS listener foundation

- Opt-in TLS requires all three settings `OPENVMS_AGENT_ONVIF_TLS_LISTEN`, `TLS_CERT_FILE`, `TLS_KEY_FILE`; all absent disables, any partial/invalid/unreadable/mismatched input fails closed. Separate HTTPS mux exposes credential-free discovery only; metrics/update and existing HTTP routes are unchanged. TLS ≥1.2; peer failure coordinates shutdown. No production cert/credential route.
- **RED:** `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./apps/edge-agent` failed (missing loader/mux). **GREEN:** gofmt on `main.go`/TLS listener/tests; focused `go test -count=1 ./apps/edge-agent ./internal/agent/... ./internal/provision ./internal/onvif`, race `go test -race ./apps/edge-agent ./internal/agent/onvifdiscover`, `go test ./...` PASS. Temp certs and recorder/serve seams only; no socket.
- **Limit/rollback:** operator-installed cert must include registered IPv4 IP SAN and central trust. Remove listener files and startup/config from main; preserve HTTP; boundary `19315be`, no commit.
### Historical implementation snapshot: central verified-agent HTTPS client foundation

- `internal/provision/agent_tls.go` binds registered IPv4 + secure port, accepts only relative query-free paths, and constructs fixed HTTPS authority. Explicit system/custom CA trust; invalid trust fails closed. TLS 1.2+, default verification plus x509 IP/ServerAuth check, no proxy, bounded timeout, redirect refusal; no HTTP fallback.
- **RED:** focused provision compile failed for missing builder/config. **GREEN:** gofmt; `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision`; focused `./apps/edge-agent ./internal/agent/... ./internal/provision ./internal/onvif`; race `./apps/edge-agent ./internal/agent/onvifdiscover ./internal/provision`; and `go test ./...` PASS. Generated-cert tests accept trusted IP SAN and reject wrong IP/CA/expiry; fake transports verify fixed authority, redirects, timeout.
- Not wired to API/camera credentials; caller still loads agent and TLS settings. Rollback: remove `agent_tls.go` + tests; preserve `19315be`, no commit.
### Historical implementation snapshot: server-scoped agent TLS trust configuration

- Tenant-scoped `server_agent_tls` supports registered agents, secure port, system/custom trust, public CA only. Get/set/delete require server-manage + registered agent; set validates IP/trust, audits CA fingerprint (not PEM) with mutation. No private key/password or fallback.
- **RED:** focused provision failed before config API/types. **GREEN:** `make generate`; focused `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/provision ./internal/api ./internal/store ./apps/api`; race `... ./internal/provision ./internal/api`; `go test ./...`; web typecheck PASS. Tests cover invalid trust/port/mode and deny-before-store. Schema/query inspection only; no DB migration run.
- Rollback migration `00032_server_agent_tls.sql`, its query/generated models, and `agent_tls_config.go` + tests; rerun generation. No commit/review.
### Historical implementation snapshot: tenant-integrity correction for agent TLS metadata

- Migration `00032` added composite `(server_id, tenant_id)` FK to `server_agents` and named parent unique key; Down removes child first. Embedded migration tests cover types/FK/Down order; no live Postgres proof.
- **RED:** `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./migrations` failed for missing composite key. **GREEN:** generate; focused `./migrations ./internal/provision ./internal/api ./internal/store ./apps/api`; race `./migrations ./internal/provision ./internal/api`; full Go, web typecheck, diff check PASS. No DB/migration execution or commit/review.
### Historical implementation snapshot: server-scoped agent TLS API exposure

- Generated authenticated GET/PUT/DELETE `/api/v1/servers/{serverId}/agent/tls`; management-gated service requires registered agent and never contacts it. Strict bounded JSON allows only trust mode/secure port/CA PEM; rejects keys incl. private key, trailing/oversize, bad trust shapes. PEM is JSON text; no bearer/private key.
- **RED:** API compile failed before handler/types/schema. **GREEN:** generate; focused `./internal/provision ./internal/api ./internal/store ./apps/api`; race `./internal/provision ./internal/api`; full Go, web typecheck, diff check PASS. Recorder tests cover auth/schema. No DB/network/device execution.
- Rollback OpenAPI + generate, remove TLS handler/tests/error mapping/body middleware; retain DB/service config. No commit/review.
### Historical implementation snapshot: authenticated ONVIF device probe on agent TLS mux

- `POST /v1/onvif/probe` exists only on optional HTTPS mux, not HTTP 7419. Constant-time bearer auth + `ONVIF_ALLOWED_CIDRS`; bounded strict JSON and explicit HTTP(S) IPv4 endpoint; paired credentials transient. Read-only three SOAP calls, 4s each/10s total/64KiB response; no redirects or XAddr following; bounded sanitized result/error.
- **RED:** focused `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/agent/onvifdiscover` failed before handler/result API. **GREEN:** gofmt; focused `./apps/edge-agent ./internal/agent/... ./internal/onvif ./internal/provision`; race `./apps/edge-agent ./internal/agent/... ./internal/onvif`; full Go + diff check PASS. Fake recorder/transport covers deny, endpoint/CIDR, redaction, SOAP, bounds, timeout and HTTP/TLS isolation; no socket/camera.
- Rollback probe files and wiring/mux tests; discovery/TLS remain. No commit/review.
### Historical implementation snapshot: central authenticated HTTPS ONVIF probe relay

- Generated server-scoped `POST /api/v1/servers/{serverId}/onvif/probe`; strict 4 KiB body with endpoint + paired transient credentials. Requires `servers.manage` and `servers.config.secrets` before agent/TLS lookup, bearer decrypt, or network; fixed registered IP/secure port/trust and verified HTTPS only. Strict bounded/redacted response; no raw remote error.
- **RED:** provision compile missing service; API route initially 404. **GREEN:** `make generate`; focused provision/API/app-api, race provision/API, full Go, web typecheck, diff checks PASS; earlier local-socket errors did not reproduce. Fake transports verify authority/bearer/body and denial before secret/outbound.
- Response-shape fix: RED tests showed omitted top/nested required JSON fields decoded as zero values; validator now rejects them. No DB integration, sockets, camera persistence, or actual network. Rollback probe service/API + scoped wrapper, revert OpenAPI then generate. No commit/review.
### Historical implementation snapshot: central ONVIF probe UI

- Cameras UI uses generated same-origin API, never direct device connection or persistence; dual permissions, strict IPv4 endpoint, safe results/errors. Credentials transient and cleared; abort/version guards reject stale completions and preserve newer inputs. No autoprobe or browser storage.
- **RED:** 3 focused tests failed before controls; stale-completion regression caught old cleanup clearing new credentials. **GREEN:** focused tests final 19 PASS; typecheck PASS; full web 120 files/856 tests PASS; diff check PASS. Initial unrelated-suite failures did not reproduce; causality unknown. API mocks only.
- Rollback only `Cameras.tsx` and tests. No commit/review.
