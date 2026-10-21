# Platform admin password recovery via log code

## Objective and authorization
User request (2026-10-21): `tools/reset_admin.sh` reports success on host helvecia1 but logging in with the new password "does nothing". Add a browser recovery flow: "¿Olvidaste la contraseña?" on the login page generates a random 6-digit code written to the API log; entering it with a new password recovers the account. Implementation and PRs requested; merge and deploy need explicit confirmation. No remote access to helvecia1 (not authorized).

## Product decisions
- Only platform admins (tenant_id IS NULL, active) can recover this way (user choice 2026-10-21). Tenant users are reset by an admin from the UI.
- Trust boundary: whoever reads the API log already has host access, same level as `reset_admin.sh`.

## Security design
- Request `POST /api/v1/auth/recovery {username}`: always the same 202 answer whether or not the account exists or is eligible (no enumeration). Eligible accounts get a fresh code (crypto/rand, 6 digits) logged at WARN with username and request IP; one active code per user (replaces the previous one); per-user cooldown of 60 s between codes (bounds log spam).
- Confirm `POST /api/v1/auth/recovery/confirm {username, code, new_password}`: code stored as salted SHA-256, compared in constant time, valid 10 min, at most 5 attempts (then invalidated). Unknown user, wrong, expired or exhausted code all answer the same 401 `invalid_recovery_code`. Password policy errors answer 400.
- On success: set the chosen password (no forced change), clear failed logins/lock, revoke sessions, delete the code. MFA stays enabled.
- Audit: `PASSWORD_RECOVERY_REQUESTED`, `PASSWORD_RECOVERY_FAILED`, `PASSWORD_RECOVERED`. Codes never go to the audit log.
- `PASSWORD_RECOVERY_LOG_CODES` (default true) disables the flow (both endpoints answer 404 `recovery_disabled`) for deployments that ship logs to third parties.

## Tasks
- [x] R1 Backend: migration `password_recovery_codes`, sqlc queries, `identity` recovery service (request/confirm), config flag, handlers, OpenAPI. Strict TDD (unit + Postgres integration).
- [x] R2 Web: "¿Olvidaste la contraseña?" flow on Login (request step, code + new password step), success message.
- [x] R3 Web: after a successful login, verify the session (`/api/v1/me`) before navigating and show a clear error instead of silently bouncing back to /login (suspected cause of "does nothing").
- [x] R4 Docs: `docs/operations/admin-password-reset.md` gains the recovery-code procedure (`docker compose logs api | grep recovery`) and the env flag.

## Acceptance and checks
`go test -race ./internal/identity/... ./internal/api/...`, integration tests with `-tags integration`, `pnpm --dir apps/web exec vitest run <paths>`, `pnpm --dir apps/web typecheck`. One PR; CI green before merge.

## Progress
- Diagnosis: on this host every preflight step of `reset_admin.sh` passes and the reset logic clears lockout. The user's output shows the reset succeeded on helvecia1, so the failure is after the reset, in the login flow (R3).
- R1 (2026-10-21): migration 00061 `password_recovery_codes`, sqlc queries, `identity/recovery.go` (crypto/rand uniform code, salted SHA-256 + constant-time compare, 10 min TTL, 60 s cooldown, 5 attempts, audit without codes, MFA untouched), `PASSWORD_RECOVERY_LOG_CODES` (default true, passed through Compose), public `/api/v1/auth/recovery` and `/confirm`. Tests: unit (code/hash), 7 Postgres integration cases (recover, ineligible, attempts, expiry, cooldown, weak password keeps code, disabled), HTTP integration (same answer for any user, single logged code, 401/400/204, login works, 404 when disabled).
- R2-R4 (2026-10-21): Login gains "¿Olvidaste la contraseña?" (`RecoveryForm`: request step, then code + new password twice; success returns to sign-in with the username and a notice); `enter()` fetches `/api/v1/me` before navigating and shows `auth.sessionNotKept` instead of bouncing silently; strings in es/en/pt; docs gain the log-code procedure and `PASSWORD_RECOVERY_LOG_CODES`. Login tests 12/12 (RED first).
