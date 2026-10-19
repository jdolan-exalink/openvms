# Configurable login lockout

## Objective
Allow operators to disable the failed-login account lockout through Docker Compose while retaining the current secure default.

## Problem and why
The local OpenVMS admin is getting locked before a successful login. The identity service currently locks an account after five consecutive failures for 15 minutes, but Compose does not forward either existing login-lockout setting to the API, so operators cannot change the behavior through deployment configuration.

## Scope and constraints
- Keep the default behavior at five consecutive failures and a 15-minute lockout.
- Expose the existing `LOGIN_MAX_FAILURES` and `LOGIN_LOCKOUT` settings in the API service's Compose environment.
- Define `LOGIN_MAX_FAILURES=0` as disabling account lockout; reject negative values by falling back to the secure default.
- With lockout disabled, do not enforce an existing `locked_until` or set a new lock; continue tracking failed attempts and clear them on successful login.
- Warn that disabling account lockout reduces protection against online password guessing; operators should retain ingress/rate limiting.
- Do not edit `.env`, inspect/modify `data/`, change the live deployment, or access remote hosts as part of this implementation.
- Strict TDD enabled. Runner: `GOCACHE=/tmp/openvms-go-build-cache go test -race ./...` plus focused config/identity tests and Compose config validation.
- Delivery strategy: `ask-on-risk`; one work-unit commit, no push, PR, or merge authorized.

## Tasks
- [x] LLO-01 — Implement the Compose-configurable lockout settings, zero-disables behavior, tests, and operator documentation. Route: delegated direct writer. Strict TDD: prove failing config/identity tests before implementation; pass focused tests and full applicable checks.

## Acceptance criteria and checks
- Default configuration remains five attempts and 15 minutes.
- Compose forwards both environment values to the API, with defaults that preserve current behavior.
- `LOGIN_MAX_FAILURES=0` fully disables account lock enforcement and new lock creation without suppressing failed-login accounting.
- Negative/invalid values cannot silently disable the protection.
- Focused tests cover defaults, valid overrides, zero, invalid/negative values, and login while a prior lock is active under disabled mode.
- Run focused config and identity tests, `GOCACHE=/tmp/openvms-go-build-cache go test -race ./...`, Compose config validation with `.env` excluded, and `git diff --check`.
- Do not change live Compose state or credentials; deployment activation is a separate operator action.

## Progress and evidence
- Read-only map verified `internal/platform/config/config.go` defaults to five failures and a 15-minute lock, and `apps/api/main.go` passes these settings to `identity.Service`.
- Read-only map verified `docker-compose.yml` does not currently forward either setting to the API; `Readme.txt` documents them but not a disable value.
- The login-failure SQL currently creates a lock whenever `failed_logins + 1 >= max_failures`; passing zero without a SQL guard would lock on every failure. The identity service also currently enforces any pre-existing `locked_until` value independently of the configured threshold.
- The user has authorized the implementation of a Compose option to disable repeated-login lockout. No deployment change, remote access, or password reset is part of this task.

## Implementation and verification
- `LOGIN_MAX_FAILURES` now accepts zero as the explicit disable value; invalid, negative, or out-of-range values fall back to five. `LOGIN_LOCKOUT` rejects invalid, zero, or negative durations and falls back to 15 minutes.
- Compose now forwards both settings to the API with secure defaults (`5` and `15m`). The login service ignores active prior locks only when max failures is zero; failed attempts remain counted, SQL avoids creating new locks at zero, and successful login clears prior failure/lock state.
- Readme documents the opt-out and warns operators to retain ingress rate limiting because account lockout no longer protects against online guessing.
- RED: config test failed on `LOGIN_MAX_FAILURES=0` (loaded 5 instead of 0); identity integration test failed because a prior active lock returned `ErrLocked` when lockout was configured off.
- GREEN: `GOCACHE=/tmp/openvms-go-build-cache go test -race ./internal/platform/config ./internal/identity` passed; `GOCACHE=/tmp/openvms-go-build-cache go test -race -tags integration ./internal/identity -run TestLoginLockoutDisabledIgnoresPriorLockAndTracksFailures -count=1` passed.
- Full verification: `GOCACHE=/tmp/openvms-go-build-cache go test -race ./...` passed; `docker compose --env-file /dev/null -f docker-compose.yml config >/dev/null` passed without printing interpolated values; `git diff --check` passed.
- Runtime harness: N/A — deployment activation and service restart were explicitly out of scope.
- Rollback boundary: revert the lockout config/parser, login behavior and SQL guard, Compose forwarding, docs, tests, and this task evidence together.

## Next step
Commit the verified work unit on the current feature branch; deployment activation remains a separate operator action.
