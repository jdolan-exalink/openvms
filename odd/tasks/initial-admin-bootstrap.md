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
- [x] IAB-01 — Config + startup bootstrap + WARN + tests + .env.example + Readme. Route: delegated writer.

- [x] IAB-02 — Default tenant + site on first startup (user decision: "Crear tenant y sitio"):
      `OPENVMS_DEFAULT_TENANT` (default `openvms`) and `OPENVMS_DEFAULT_SITE` (default `Sitio principal`),
      created only when no tenant exists (never touch existing tenants/sites; renaming later from the web
      must not be undone on restart), documented in `.env.example` and Readme.txt. Route: same writer,
      after IAB-01 (same files).

## Acceptance criteria and checks
- `gofmt -l` empty, `golangci-lint run` 0 issues, `go vet`, `go test -race ./...`, integration bootstrap.
- Fresh compose stack: login as admin/openvms123 works; WARN in api logs; existing stacks unaffected.

## Delivery
- Forecast ~250 lines → single PR. RDD on (global).

## Progress and evidence
- Branch `feat/initial-admin-bootstrap` from main.
- IAB-01 `8cb27cd` + `6c72e64`: `EnsureInitialAdmin` (one tx; skips if named user exists or any platform user
  has a password; weak password → error log, skip; must_change only for default; WARN while default still
  verifies, password never logged); config `OPENVMS_ADMIN_USERNAME/PASSWORD`; compose `x-go-env`; Readme §6.
  RED: `undefined: bootstrap.EnsureInitialAdmin` → GREEN (7 integration tests incl. 4 concurrent calls).
- IAB-02 `2a5c593` + `500f8e1`: `EnsureDefaultTenant` (only when ListTenants is empty; tenant + site in one
  tx; conflict = exists; slug from name; site tz America/Argentina/Cordoba); config `OPENVMS_DEFAULT_TENANT/
  SITE`; Readme. RED: `undefined: bootstrap.EnsureDefaultTenant` → GREEN (5 tests incl. rename kept,
  concurrency). Deviations: sqlc queries directly (no acting user → no audit entry); a soft-deleted sole
  tenant is recreated on next restart.
- Checks: gofmt empty; golangci-lint 0 issues; go vet/build clean; `go test -race ./...` OK; integration
  bootstrap + api OK; generated code in sync.
- `.env.example`: not editable from this environment (permission rule on `.env*`); applied by the user
  via `!` commands: `1d0016d` (admin/tenant/site), `ca40caf` (random password), `eddf309` (site TZ).
- Native review 1 (lineage review-3cc63d7b152f5f95, 4 lenses): CRITICAL R1-default-admin-credential (public
  default password → first visitor takes over). User chose random password. Correction `678b59c` (+ `.env.example`
  `ca40caf`); targeted validator APPROVED; acknowledged.
- Advisories fixed `b48e7df`: upgrade never adds an admin when any platform user exists (CountAllPlatformUsers);
  existence before policy; soft-deleted sole tenant not recreated (CountAllTenants); best-effort startup; INFO
  on conflict; `OPENVMS_DEFAULT_SITE_TZ` (+ `.env.example` `eddf309`). RED→GREEN per item.
- Native review 2 (lineage review-3c53275816c431fb, 4 lenses): APPROVED, acknowledged. User-selected follow-ups
  `c5698b6`: bounded startup retry (5 attempts, 1/2/4/8 s, ERROR with recovery hint) and WARN on admin-name
  collision with a tenant user. Other advisories (TZ default duplication, empty-TZ comment/test, `Local` TZ,
  test helper nil error) left as follow-ups; tzdata concern moot (distroless static ships tzdata).
- Checks: gofmt empty; golangci-lint 0; vet/build clean; `go test -race ./...` OK; integration bootstrap+api OK.

## Next step
Open PR (single PR; size noted).
