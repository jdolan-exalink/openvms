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
- [ ] R2 Web: "¿Olvidaste la contraseña?" flow on Login (request step, code + new password step), success message.
- [ ] R3 Web: after a successful login, verify the session (`/api/v1/me`) before navigating and show a clear error instead of silently bouncing back to /login (suspected cause of "does nothing").
- [ ] R4 Docs: `docs/operations/admin-password-reset.md` gains the recovery-code procedure (`docker compose logs api | grep recovery`) and the env flag.

## Delivery
The full change exceeded the review context budget, so it ships as chained PRs:
- Part 1 (#137): identity service, storage, config. No public endpoint.
- Part 2: API contract, handlers, public paths, per-IP rate limit, concurrent-request fix.
- Part 2b: review follow-ups before deploy: per-account cap of 10 failed confirmations per 24 h (counted from audit_log; blocks new codes and confirmations), 4 KiB body limit and handler checks for username/code/password lengths, shared path constants.
- Part 3: web Login flow (R2, R3) and docs (R4). Implemented on `feat/admin-password-recovery`, reviewed and merged with part 3.

## Acceptance and checks
`go test -race ./internal/identity/... ./internal/api/...`, integration tests with `-tags integration`, `pnpm --dir apps/web exec vitest run <paths>`, `pnpm --dir apps/web typecheck`. One PR; CI green before merge.

## Progress
- Diagnosis: on this host every preflight step of `reset_admin.sh` passes and the reset logic clears lockout. The user's output shows the reset succeeded on helvecia1, so the failure is after the reset, in the login flow (R3).
- R1 (2026-10-21): migration 00061 `password_recovery_codes`, sqlc queries, `identity/recovery.go` (crypto/rand uniform code, salted SHA-256 + constant-time compare, 10 min TTL, 60 s cooldown, 5 attempts, audit without codes, MFA untouched), `PASSWORD_RECOVERY_LOG_CODES` (default true, passed through Compose), public `/api/v1/auth/recovery` and `/confirm`. Tests: unit (code/hash), 7 Postgres integration cases (recover, ineligible, attempts, expiry, cooldown, weak password keeps code, disabled), HTTP integration (same answer for any user, single logged code, 401/400/204, login works, 404 when disabled).
- R2-R4 implemented (2026-10-21, not yet delivered; ships in part 3): Login gains "¿Olvidaste la contraseña?" (`RecoveryForm`: request step, then code + new password twice; success returns to sign-in with the username and a notice); `enter()` fetches `/api/v1/me` before navigating and shows `auth.sessionNotKept` instead of bouncing silently; strings in es/en/pt; docs gain the log-code procedure and `PASSWORD_RECOVERY_LOG_CODES`. Login tests 12/12 (RED first).
- Part 1 review (4 lenses) required one bounded correction: exhausted/expired codes were deleted, which reset the request cooldown and allowed unlimited guessing; they are now kept unusable (`TestPasswordRecoveryCooldownSurvivesExhaustion`).
- Part 2 (2026-10-21): review follow-ups: concurrent requests logged one code each (4 for a burst); the request now locks the user row (`TestPasswordRecoveryConcurrentRequestsLogOneCode`). Both public endpoints share a per-IP token bucket (burst 10, 1 per 30 s) answering 429 with Retry-After, bounding log and audit growth from anonymous callers.
- Part 2b (2026-10-21): the part 2 review showed the per-IP limit does not stop slow guessing across IPs (~7200 guesses/day). `MaxRecoveryFailuresPerDay = 10` bounds it to ~3650/year; trade-off: an attacker can burn the admin's daily recovery budget, and the admin still has `tools/reset_admin.sh` on the host. Tests: `TestPasswordRecoveryDailyFailureCap`, `TestPasswordRecoveryBodyIsBounded`, `TestPasswordRecoveryHandlersRejectOutOfContractInput`.
- Part 2b review (4 lenses, approved) found the daily cap renewed itself: refusals caused by the cap were audited as failures and counted, so a trickle of confirmations (or the admin's own retries) kept recovery blocked forever. Now only `wrong_code` guesses count (migration 00062 partial index `audit_log_recovery_guesses_idx`), a blocked request logs a WARN without a code, and the cap test confirms with the real issued code and proves recovery reopens after 24 h (verified to fail against the old query).
