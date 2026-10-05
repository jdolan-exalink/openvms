# ONVIF Camera Management

Deliver capability-driven ONVIF camera management through the node agent and web API, with safe Frigate synchronization and explicit auditability. This is the complete five-phase feature; the first implementation unit is only the mocked, tested ONVIF foundation, not CRUD-only scope.

## Goal and boundaries

- **Outcome:** discover and manage camera capabilities and operations, configure imaging/network/time, PTZ and audio, and ingest advanced ONVIF events/analytics while retaining stable VMS identity and safe Frigate lifecycle behavior.
- **Authorized implementation:** camera communication via the agent; frontend integration through the existing API. Use mocks in the first foundation task. No LAN camera communication, physical imaging/video/PTZ/audio/network/time changes, or other remote/physical operation is authorized by this plan.
- **Frigate:** OpenVMS must modify/synchronize Frigate configuration. Camera deletion preserves recordings by default; any discard must be explicit and fail closed. No factory reset.
- **Security and reliability:** stable VMS IDs; tombstones suppress reimport of deleted cameras; encrypted, write-only secrets; capability/operation-driven UI states and options; bounded retries/timeouts; readback plus audit for mutations.
- **Delivery:** `ask-on-risk`, with user-approved `stacked-to-main` strategy. Do not push, create PRs, or merge. Keep each coherent work unit with its tests/docs and a Conventional Commit on this feature branch. The 400 authored changed-line guidance is a slicing aid, never code-golf; this complete feature is expected to span many work units and may total many thousands of lines.
- **TDD:** Strict TDD is enabled by the user instructions. For each implementation task, record observed RED, GREEN, and REFACTOR using the exact focused runner; do not infer test results.

## Baseline and current state

- Feature branch: `feat/onvif-camera-management`, created from local `main` at `3d44726ff04f61775115e9cd37dc0c2db8cb5c0a` (`chore: ignore local soc map mockup reference`). At task creation `HEAD` and `main` resolve to the same commit; do not copy unrelated future changes from `feat/maps-phase2`.
- Prior exploration found ONVIF credentials/config represented in Frigate configuration but no native ONVIF discovery/control in the node agent. Per-camera PATCH edits an existing Frigate camera section; adding/removing sections currently needs raw config PUT or a new safe agent capability. Secrets require `servers.config.secrets` and are masked otherwise; Frigate below 0.16 cannot edit config. Recording reads exist but no recording DELETE endpoint. Existing camera sync identity is `(server_id, remote_name)`; import upsert can resurrect a soft-deleted same-name camera, so deletion tombstones/suppression are required. Prefer display-name changes over renaming remote keys to preserve VMS/event identity.
- Exact project verification runners from the baseline: `go test -race ./...`, `pnpm test`, and `pnpm typecheck`; focused tests should use the relevant `go test` package or `pnpm exec vitest run <test-file>`. `make test` combines the Go and web unit suites. Do not run tests as part of this planning-only document task.
- No source code changes, tests, commits, LAN access, camera operation, or physical operation have been performed for this plan.

## Work units

Implement in dependency order. Each task is a delegated direct work unit with a bounded writer; reading that prepares a source change belongs with that writer. Close each task with applicable functional checks, a Conventional Commit, commit ID, verification evidence, rollback boundary, and review-boundary status. First implementation task is intentionally the smallest independently testable foundation and uses mocked protocol/device responses only.

### Phase 1 — Discovery, device/media, lifecycle, and health

- [x] **ONVIF-01 — Mocked ONVIF foundation and bounded transport.** Establish protocol/client abstractions, device endpoint discovery inputs, bounded connection/read/write timeouts and retries, structured errors, and deterministic mocks. No physical camera access. RED/GREEN/REFACTOR evidence required. Rollback: remove the isolated foundation and its tests.
- [ ] **ONVIF-02 — Core discovery and device information.** Discover devices/services and read device information through the agent; expose API results without leaking credentials; map discovered devices to stable VMS identity.
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
- [ ] Device capabilities/operations determine available API operations, UI states, and option lists; unsupported operations are not offered.
- [ ] Secrets are encrypted at rest and write-only through APIs/UI; logs, errors, audit, and stream metadata redact credentials.
- [ ] Every physical mutation is bounded, authorized, audited, and verified by readback; failures report uncertainty honestly and do not claim success.
- [ ] Frigate config follows the device/VMS lifecycle without changing stable identity; deleted cameras remain suppressed from import until explicitly restored.
- [ ] Recording preservation is the default; discard is an explicit, confirmed, fail-closed path. No factory reset is introduced.
- [ ] Unit and applicable integration checks pass; physical/LAN validation remains pending unless separately authorized by the user.
- [ ] Each work unit records tests, any runtime harness scenario (or `N/A` with reason), rollback boundary, commit ID, and review due/outcome against the preceding reviewed boundary. Do not start a review or change the user-owned review switch from this plan.

## Delivery and progress record

- Strategy: `ask-on-risk`; chain strategy: `stacked-to-main` (user-approved). No push, PR creation, or merge is authorized.
- Current slice: ONVIF-01, mocked foundation and tests, committed as `19cbca2` (375 authored changed lines). Planning commit: `134a319` (76 lines). Slice base: `134a319`; no PR created.
- Estimate: complete scope is multi-phase and likely many thousands of authored changed lines across slices; avoid a false precise forecast before implementation. Keep cohesive independently reviewable work units, count authored additions plus deletions, and apply the approved stacked-to-main slicing before a delivery boundary exceeds the applicable budget. Never shrink or omit required behavior/tests to satisfy a line count.
- Per-task evidence to append: task ID; RED/GREEN/REFACTOR commands and observed results; additional checks; harness result or justified `N/A`; rollback boundary; Conventional Commit and commit ID; authored line count; risk assessment/review due reason and review outcome; PR slice boundary if/when user later authorizes PR work.
- Next step: ONVIF-02, core device discovery and agent integration. Keep all device/camera operations mocked unless the user separately authorizes a destination and operation. The first implementation slice has no API/UI, authentication, discovery, or agent wiring yet.

### ONVIF-01 implementation evidence (completed; native review under budget)

- Implemented isolated `internal/onvif` endpoint validation, bounded SOAP 1.2 client, response size limit, typed redacted failures, SOAP fault classification, and read-only-only retries. Transport is injected as `http.RoundTripper`; tests use only a deterministic mock and make no network calls.
- **RED:** `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/onvif` failed before implementation because the new tests referenced the not-yet-implemented ONVIF API (undefined `ParseEndpoint`, `NewClient`, `Config`, and related symbols). After the first implementation pass, the same test also exposed SOAP action assertion and SOAP Fault/HTTP 500 handling gaps; both were fixed before GREEN.
- **GREEN:** `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/onvif` — PASS.
- **REFACTOR:** `gofmt -w internal/onvif/client.go internal/onvif/client_test.go`, then `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/onvif` — PASS; `GOCACHE=/tmp/openvms-go-build-cache go test -race ./internal/onvif` — PASS.
- **Broader check:** `GOCACHE=/tmp/openvms-go-build-cache go test ./...` — PASS (all packages).
- **Runtime harness:** N/A; this unit has no runtime or physical-device boundary and all transport behavior is mocked.
- **Rollback boundary:** remove `internal/onvif/client.go` and `internal/onvif/client_test.go`; no integrations or external dependencies were added.
- **Commit / authored line count / review boundary:** `19cbca2`; 375 authored changed lines. Native assessment against `134a319`: `medium`, `review_due=false`, `under_budget`. Review remains pending for the accumulating slice; no approval or receipt is claimed. Parent re-ran the focused test: PASS. No real camera, LAN, or credential use occurred.

### ONVIF-02 partial implementation evidence (device protocol slice only; parent task remains open)

- Added read-only Device service client operations for `GetDeviceInformation`, `GetServices`, and `GetSystemDateAndTime`, with deterministic injected transport tests. Service XAddr values remain untrusted and must be validated via `Service.ValidatedEndpoint()` before any follow-up request. No returned service URL is followed automatically.
- Fixed SOAP response extraction to retain the operation wrapper and its namespace declarations. ONVIF devices commonly declare operation namespaces on that wrapper; removing it produced prefixed XML with undeclared prefixes and silently empty parsed fields.
- **RED:** `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/onvif` — FAIL before implementation (`NewDeviceClient` and `Service` undefined); after first implementation, this test exposed a decode-shape mismatch (all device-information fields empty), fixed by decoding the response fields from the namespaced response wrapper.
- **GREEN:** `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./internal/onvif` — PASS.
- **REFACTOR:** `gofmt -w internal/onvif/client.go internal/onvif/device.go internal/onvif/device_test.go`; focused test rerun — PASS.
- **Additional checks:** `GOCACHE=/tmp/openvms-go-build-cache go test -race ./internal/onvif` — PASS; `GOCACHE=/tmp/openvms-go-build-cache go test ./...` — PASS.
- **Runtime harness:** N/A; all SOAP exchanges use an injected fake `http.RoundTripper`; no network or camera operations.
- **Rollback boundary:** remove `internal/onvif/device.go`, `internal/onvif/device_test.go`, revert the SOAP response wrapper-preservation change in `internal/onvif/client.go`, and remove this evidence subsection.
- **Commit / authored line count / review:** no commit created by this worker (parent handles native review and commit). Current slice has not been natively assessed. Do not claim ONVIF-02 complete: WS-Discovery, WSSE UsernameToken PasswordDigest authentication, stable identity mapping, and node-agent/API wiring remain open; camera communication stays mocked and unauthenticated device-operation behavior must not be described as production-ready.
- **Next step:** continue ONVIF-02 with the smallest coherent WS-Discovery/authentication and agent/API integration tasks; preserve credential redaction and validate any device-provided service endpoint before use. Public ONVIF protocol references: https://www.onvif.org/ver10/device/wsdl/devicemgmt.wsdl and https://www.onvif.org/profiles/specifications/.
