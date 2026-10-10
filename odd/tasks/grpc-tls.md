# Server-side TLS for the gRPC control channel

## Objective

Let the central API serve its gRPC control channel (`GRPC_ADDR`, default `:9090`) over TLS, and let every gRPC client (edge-agent worker, desktop SDK) verify the server certificate, so the port can later be published without sending traffic in plaintext.

## Problem

`internal/control/server.go` builds `grpc.NewServer` with no transport credentials, and all clients dial with `insecure.NewCredentials()` (`internal/agent/worker.go`, `pkg/client/client.go`). User bearer tokens travel in plaintext metadata, and nothing stops a client from talking to an impostor server. This blocks publishing 9090 (see follow-ups in `odd/tasks/merge-maps-phase2.md`).

## Why option A

User chose server-side TLS now (option A) over full mTLS (option B, the target in `docs/architecture/decisions.md`). A encrypts the channel and authenticates the server with operator-provided PEM files, mirroring `OPENVMS_WEB_TLS_*`. B (internal CA, agent enrollment, peer-cert identity binding for `Heartbeat`) stays a separate feature and A does not constrain it.

## Scope

- Server: optional cert/key files; when both are set the gRPC server uses TLS (min TLS 1.2). Exactly one set is a startup error. Neither set keeps plaintext and logs a warning.
- Clients: opt-in TLS with an optional CA bundle file (system roots otherwise) and optional server-name override. Plaintext stays the default.
- Docs for the new settings.
- Out of scope: mTLS / agent identity, cert hot reload, rotation, the relay (HTTP, not gRPC), port publication in compose (`deploy/agent/compose/compose_contract_test.go` still forbids it), reflection toggling, the Prometheus 9090 overlap.

## Constraints

- Conventional Commits, no AI attribution.
- TDD: on (source: global `Strict TDD Mode: enabled`). Runner: `go test ./...` (focused: `go test ./internal/control/... ./internal/agent/... ./pkg/client/... ./internal/platform/...`). RED before GREEN for each task.
- Existing tests must keep passing with the plaintext default.
- Delivery: `ask-on-risk`. Forecast ≈ 400 authored changed lines; single PR unless the running count clearly exceeds it.

## Tasks

- [x] T1 Shared TLS credential builders (server from cert/key files; client from CA file / system roots + server name), with tests using a generated self-signed cert. Route: delegated direct (writer trigger: T1–T4 span 2+ non-trivial files).
- [x] T2 API server: config fields `GRPC_TLS_CERT_FILE` / `GRPC_TLS_KEY_FILE`, wire into `control.NewServer`, validation (one-without-the-other fails), plaintext warning; TLS handshake test against the control server.
- [x] T3 Edge-agent worker: `OPENVMS_CONTROL_TLS`, `OPENVMS_CONTROL_TLS_CA_FILE`, `OPENVMS_CONTROL_TLS_SERVER_NAME`; worker dials with TLS when enabled; test against a TLS server.
- [x] T4 Desktop SDK `pkg/client`: TLS options in `Config`; test against a TLS server.
- [ ] T5 Docs for the settings and the rollout note (enable on server, then clients).

## Acceptance criteria

- With cert/key configured, a plaintext client fails and a TLS client trusting the CA succeeds.
- A TLS client that does not trust the server cert fails.
- Without TLS settings, behavior and existing tests are unchanged.
- `go build ./...`, `go vet ./...`, `go test ./...` pass.

## Progress

- Exploration done (control server, clients, existing TLS infra, relay, tests, docs). No reusable CA; `server_agent_tls` is for the API→agent HTTPS direction.
- T1 done (commit see `git log`): `internal/platform/grpctls` (ServerCredentials, ClientCredentials) + `grpctlstest.WriteSelfSigned`. pkg/client is in the same module, so it may import `internal/platform/grpctls` (no duplication). RED: `no non-test Go files in .../internal/platform/grpctls` / `[build failed]`; GREEN: `ok .../grpctls`. Checks: build, vet, test (below).
- T2 done: config `GRPC_TLS_CERT_FILE`/`GRPC_TLS_KEY_FILE` (Load errors if only one), `control.Config.Credentials`, wiring + plaintext warning in `apps/api/main.go`. RED: `unknown field Credentials in struct literal` / `cfg.GRPCTLSEnabled undefined`; GREEN: `ok internal/control`, `ok internal/platform/config`.
- T3 done: `WorkerConfig.TLS/TLSCAFile/TLSServerName`, `controlTLSFromEnv` (CA/name without TLS = startup error). RED: `unknown field TLS in struct literal of type WorkerConfig` / `undefined: controlTLSFromEnv`; GREEN: `ok internal/agent`, `ok apps/edge-agent`.
- T4 done: `pkg/client.Config{TLS,TLSCAFile,TLSServerName}`; CA/name without TLS is an error in `New`. RED: `unknown field TLS in struct literal of type Config`; GREEN: `ok pkg/client`.

## Next step

T5.
