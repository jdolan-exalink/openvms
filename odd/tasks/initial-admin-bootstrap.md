# Initial admin bootstrap from .env

## Objective
A fresh install comes up with a usable platform admin without running `vmsctl bootstrap`: user `admin`,
password from `OPENVMS_ADMIN_PASSWORD` (default `openvms123`), documented in `.env.example`.

## Problem and rationale
Today the first admin needs a manual `docker compose exec api /vmsctl bootstrap -password …` step
(Readme.txt §6), which new installs miss.

## Scope and constraints (user decision: "con mitigación")
- On `api` startup: if no platform user named `OPENVMS_ADMIN_USERNAME` (default `admin`) exists AND no
  platform user with a password exists, create it with every platform grant (reuse
  `bootstrap.PlatformAdmin` grant reconciliation) and the configured password. Never overwrite or reset an
  existing admin/password. Idempotent and safe on concurrent startups.
- If the effective password is the default `openvms123`: set `must_change_password = true`, and log a WARN
  on every api start while that user still has the default password (verify hash, never log the
  password).
- Password must still pass `identity.CheckPolicy` (openvms123 does); a custom password that fails policy
  → log an error and skip creation (do not crash api).
- Config via `internal/platform/config` (existing pattern); `.env.example` documents both variables with a
  warning to change the default; Readme.txt §6 updated (vmsctl bootstrap stays as alternative).
- No new migration expected (`must_change_password` exists).

## TDD
- Mode: strict (source: user global CLAUDE.md). Runners: `go test -race ./...`, integration
  `go test -race -tags integration ./internal/bootstrap/ ./internal/api/`.

## Tasks
- [ ] IAB-01 — Config + startup bootstrap + WARN + tests + .env.example + Readme. Route: delegated writer.

## Acceptance criteria and checks
- `gofmt -l` empty, `golangci-lint run` 0 issues, `go vet`, `go test -race ./...`, integration bootstrap.
- Fresh compose stack: login as admin/openvms123 works; WARN in api logs; existing stacks unaffected.

## Delivery
- Forecast ~250 lines → single PR. RDD on (global).

## Progress and evidence
- Branch `feat/initial-admin-bootstrap` from main.

## Next step
IAB-01.
