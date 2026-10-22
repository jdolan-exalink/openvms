# Close-out of pending work (October 2026)

## Objective
Close every pending item the user chose in the 2026-10-19 survey, so nothing stays open.

## Decisions (user survey, 2026-10-19)
- Forced purge for deleted persons whose face sits on an unreachable server: YES (operator button).
- Rename Frigate server `Casa`: NO. Enable face recognition on helvecia/sodecar164: NOT NOW.
- `min_area` on Casa lowered to 1000 px: DONE (revision `34ded553-3045-49da-aac0-9ab7c5b8bd80`, Frigate restarted from OpenVMS).
- Revoke session test tokens of `admin`: DONE (12 tokens named `cli` created since 2026-10-18 23:43 UTC; older tokens untouched).
- Sync the face recognition document to main: PR #98.
- Technical follow-ups: rollback & cleanup, audit & performance, minor infra: YES. Test robustness: NO (not now).
- Custom live views as defaults: add Export/Import of views in the presentation editor; then promote the exported ones to defaults.

## Scope / tasks
- [ ] CL-01 — Forced purge (faces.manage): purge a tombstoned person whose owned face is on servers that stay disabled/unreachable — scrub the name from events now, mark those servers for cleanup when they come back, audited. Route: delegated direct. Waits for the faces site/server PRs (same files).
  Status (2026-10-22): implementation candidate exists on local branch `feat/closeout-cl01` (commits `12cb26d`, `e86529b`, `b3fe4aa`), but it has not been delivered. Native Review is escalated/manual-action-required; its findings identify queued purges without a production scheduler/worker caller. Do not merge or mark complete until that review gate is resolved and the missing processing path is addressed. The main branch still has only automatic `PurgeTombstones`/`purgeOne` (`internal/faces/sync.go`, action `FACE_PERSON_PURGED`).
- [x] CL-02 — Rollback & cleanup: atomic `make rollback` (validate every tag before retagging, restore on partial failure), no rollback hint when nothing was tagged, retention of `rollback-*` image tags and `.atl/deploy` dumps (keep the last 5, configurable). Route: delegated direct.
- [x] CL-03 — Audit & performance: per-organization audit counts for multi-organization recognitions/train reads (review advisories R3-001/R3-002 of `review-ddd9c44f7a5ddd3a`); skip the per-rule savepoint when the face index is a cache hit. Route: delegated direct. Complete in PR #161, merged 2026-10-22 as `b8cb755`. The PR adds per-organization counts for recognition/train reads and avoids the per-rule savepoint on a face-index cache hit; regression coverage is in `internal/faces/scope_integration_test.go` and `internal/rules/faces_cache_test.go`.
- [x] CL-04 — Minor infra: mosquitto healthcheck; `COMMIT`/`BUILD_TIME` in the web build; document the plaintext gRPC control channel and how to enable TLS (`GRPC_TLS_CERT_FILE`/`GRPC_TLS_KEY_FILE`). Route: delegated direct.
- [x] CL-05 (export/import done; promotion to defaults still waits for the user file) — Export/Import of live presentations in the editor (JSON file, validated with the existing parser), so the user can export custom views; then promote the user's exported views to `DEFAULT_PRESENTATIONS`. Route: delegated direct (promotion needs the user's exported file).
- [x] CL-06 — First install that works in one step (user, 2026-10-19: a fresh `git clone` + `cp .env.example .env` + `docker-compose up -d` failed on another machine with Compose v1: `Invalid interpolation format ... ${OPENVMS_WEB_TLS_CERT_HOST_FILE:+/etc/caddy/tls.crt}`). `make install`: check Docker and Compose v2 (clear error and install hint when only v1 exists), create `.env` from `.env.example` when missing with random `OPENVMS_MASTER_KEY` and `POSTGRES_PASSWORD` (never overwrite an existing `.env`), `docker compose up -d --build`, wait for `/health/ready`, print how to bootstrap the admin. Route: delegated direct.
- [x] CL-07 — Clean install without mock data, optional MQTT (user, 2026-10-19): move the simulated Frigates (`frigate-helvecia`, `frigate-cayasta`, their demo passwords and mock MQTT settings) out of `docker-compose.yml` into a development-only compose file used by `make dev`; put `mosquitto` behind an `mqtt` profile (OpenVMS reads Frigate over HTTP, only the mocks need MQTT); keep production upgrades safe. Route: delegated direct.

## Constraints and checks
- Strict TDD (user global config). Runners: Go `GOPROXY=off GOTOOLCHAIN=local GOCACHE=/tmp/openvms-go-build-cache go test ...`, shell `bash deploy/deploy_test.sh`, web `cd apps/web && pnpm exec vitest run <file>`, `pnpm typecheck`, `pnpm test`, `docker compose config --quiet`, `go test ./deploy/agent/compose/`.
- Never run the real deploy/rollback from a task; dry-run only.

## Progress
- 2026-10-19: branch `feat/closeout-ops` from `main` `928e57c` (CL-02, CL-04, CL-05 first).

- CL-02 done (8faa007): RED = new deploy_test cases failed (no restore after mid-way tag failure, tags validated interleaved, hint on first deploy, no retention); GREEN = `bash deploy/deploy_test.sh` ok. Route: delegated direct.
- CL-04 done (258077e): RED = `TestMosquittoHasHealthcheckAndMocksWaitForIt` / `TestWebBuildReceivesVersionMetadata` failed; GREEN = `go test ./deploy/agent/compose/` ok, mosquitto_sub healthcheck verified in a real eclipse-mosquitto:2 container. `docker build --check` is unavailable here (no buildx --check), web image build not run.
- CL-05 done (abf543a): RED = `importPresentations is not a function`; GREEN = presentations lib + editor tests, typecheck, eslint, full `pnpm test` (1668). Id collision policy: replace.
- CL-06 done (this commit, `feat(deploy): one-step make install ...`): RED = `bash deploy/install_test.sh` failed every case (deploy/install.sh missing); GREEN = install_test ok (v2 happy path with 0600 .env and generated secrets, existing .env untouched, Compose v1-only and missing docker fail early, readiness timeout shows api logs, postgres volume warning, dry run). Not run for real on this host (live stack). Route: delegated direct.

- CL-07 done (see commit): RED = `TestProductionComposeHasNoMocksAndMQTTIsOptional` and `TestDevComposeMocksWaitForHealthyMosquitto` failed (mocks in docker-compose.yml, no docker-compose.dev.yml); GREEN = compose tests, buildinfo, golangci-lint, deploy_test, install_test, `docker compose config` variants. Mocks moved to root `docker-compose.dev.yml` (next to the base file so relative paths and `-f` stay simple); `make dev`/`dev-down` use it with `--profile mqtt`. CI compose job uses plain `docker compose up` and needs no change. Not run for real (live stack). Route: delegated direct.
- CL-03 done (this commit): RED = multi-organization face-read assertions found omitted zero-count tenant audits; warm-cache rule test found a savepoint was still opened. GREEN = per-tenant zero-count audits for recognition/train lists and direct resolution from valid cached face snapshots. Verified with scoped `internal/faces` and `internal/rules` integration tests plus the focused warm-cache unit test; the existing cold transaction and resolver-failure isolation tests pass.

## Next step
CL-01 remains blocked on the Native Review recovery gate and a working production purge processor. CL-05 code is complete; promotion to defaults still needs the user's exported presentation JSON.
