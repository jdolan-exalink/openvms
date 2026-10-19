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
- [ ] CL-02 — Rollback & cleanup: atomic `make rollback` (validate every tag before retagging, restore on partial failure), no rollback hint when nothing was tagged, retention of `rollback-*` image tags and `.atl/deploy` dumps (keep the last 5, configurable). Route: delegated direct.
- [ ] CL-03 — Audit & performance: per-organization audit counts for multi-organization recognitions/train reads (review advisories R3-001/R3-002 of `review-ddd9c44f7a5ddd3a`); skip the per-rule savepoint when the face index is a cache hit. Route: delegated direct. Waits for PR #97.
- [ ] CL-04 — Minor infra: mosquitto healthcheck; `COMMIT`/`BUILD_TIME` in the web build; document the plaintext gRPC control channel and how to enable TLS (`GRPC_TLS_CERT_FILE`/`GRPC_TLS_KEY_FILE`). Route: delegated direct.
- [ ] CL-05 — Export/Import of live presentations in the editor (JSON file, validated with the existing parser), so the user can export custom views; then promote the user's exported views to `DEFAULT_PRESENTATIONS`. Route: delegated direct (promotion needs the user's exported file).

## Constraints and checks
- Strict TDD (user global config). Runners: Go `GOPROXY=off GOTOOLCHAIN=local GOCACHE=/tmp/openvms-go-build-cache go test ...`, shell `bash deploy/deploy_test.sh`, web `cd apps/web && pnpm exec vitest run <file>`, `pnpm typecheck`, `pnpm test`, `docker compose config --quiet`, `go test ./deploy/agent/compose/`.
- Never run the real deploy/rollback from a task; dry-run only.

## Progress
- 2026-10-19: branch `feat/closeout-ops` from `main` `928e57c` (CL-02, CL-04, CL-05 first).

## Next step
CL-02, CL-04, CL-05 through one writer.
