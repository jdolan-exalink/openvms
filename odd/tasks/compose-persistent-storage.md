# Configurable Compose Persistent Storage

## Objective
Move the central OpenVMS Compose stack's persistent state from Docker-managed named volumes to configurable host directories, keeping PostgreSQL project-local and other state under `/mnt/cctv` by default.

## Problem and why
The initial admin password is only used when PostgreSQL is initialized without an existing platform user. The current named PostgreSQL volume makes it hard to locate and intentionally reset that state. The user wants storage locations visible and configurable in Docker Compose.

## Authorized scope
- Modify the central `docker-compose.yml`, related deployment/install behavior and tests, and user-facing deployment/storage documentation as needed.
- Default PostgreSQL bind directory: `./data/postgres`.
- Default root for other persistent application state: `/mnt/cctv/openvms`, with service-specific subdirectories.
- Expose both host roots through Compose interpolation variables with the defaults above.
- Preserve unrelated Frigate-agent Compose files and do not read or edit `.env*` files.
- Do not access or deploy to any remote host; do not push or open/merge a PR.
- Do not automatically copy or delete existing named-volume data. Document that bind mounts start empty and old named volumes remain until a deliberate migration/removal.

## Constraints and checks
- Strict TDD: enabled by the user-provided project instructions. Test runner: Go Compose contract tests, shell install tests, and `docker compose config --quiet`.
- No destructive Docker commands or live deployment during implementation.
- Runtime host storage ownership/availability cannot be verified locally; document any host prerequisites and remaining operational step.
- Delivery strategy: `ask-on-risk`; estimate one cohesive work unit and split only if a substantive boundary requires it.

## Tasks
- [x] STOR-01 — Replace central Compose persistent named volumes with configurable host bind paths; preserve service-specific data directories; adjust installer warnings/tests for bind-mounted PostgreSQL; add/update storage documentation and Compose contract coverage. Route: delegated direct (mapping trigger: behavior spans Compose, install scripts/tests, and docs; writer trigger: multiple non-trivial files). Commit: pending final hash.

## Acceptance criteria
- PostgreSQL writes to the configurable project-local data path by default.
- NATS, SeaweedFS, exports, WAHA sessions, Caddy state, and generated web TLS state write beneath the configurable `/mnt/cctv/openvms` root by default.
- Compose renders valid bind mounts and retains container-side paths and service sharing.
- Existing named-volume contents are not silently removed or copied; docs explain the one-time data/empty-directory behavior and safe reset implications.
- Installer diagnostics/tests remain accurate for an existing bind-mounted database.
- The task is checked only after focused test evidence and a work-unit commit are recorded.

## Progress
- 2026-10-19: Read-only mapping found seven central data mounts: PostgreSQL, NATS JetStream, SeaweedFS, shared API/worker exports, WAHA sessions, Caddy state, and generated web TLS. Valkey is ephemeral; Prometheus/Grafana mounts are config-only. Separate Frigate agent mounts are out of scope.
- Migration note: changing named volumes to bind directories does not move data. Existing volumes must remain untouched; new bind directories start empty unless the operator copies data deliberately.
- 2026-10-19: STOR-01 implemented. Central Compose now binds PostgreSQL to `${OPENVMS_POSTGRES_DATA_DIR:-./data/postgres}` and NATS, SeaweedFS, shared API/worker exports, WAHA, Caddy, and generated web TLS beneath `${OPENVMS_DATA_DIR:-/mnt/cctv/openvms}`. Installer warns when the configured PostgreSQL bind directory already contains data. README documents private environment overrides, writable host storage prerequisites, and the no-automatic-migration behavior.
- Verification: `GOCACHE=/tmp/openvms-go-build-cache go test ./deploy/agent/compose` — PASS; `bash deploy/install_test.sh` — PASS; `docker compose --env-file /dev/null config --quiet` — PASS; `git diff --check` — PASS. No live deployment performed (out of scope).
- Route: delegated direct. Rollback boundary: revert the STOR-01 work-unit commit to restore named volumes, installer volume warning, and prior documentation/tests.

## Next step
Record the work-unit commit hash here and in the Engram mirror.
