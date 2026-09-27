# Resolve build and typecheck errors

## Objective
Restore the OpenVMS backend build and frontend typecheck after installing the project toolchain and regenerating API clients.

## Problem and rationale
The first verification exposed a generated Go field-name mismatch and frontend TypeScript errors, some of which were reduced by `make generate`. Fix the remaining source-level incompatibilities so the project can proceed to full deploy-readiness checks.

## Scope and constraints
- Fix the known Go compile error, frontend TypeScript errors, and additional Go build failures explicitly authorized by the user on 2026-09-27.
- Limit edits to the 9 files implicated by the 14 exact lint diagnostics; user explicitly authorized fixing all 14 findings on 2026-09-27. Do not expand beyond those findings.
- Preserve existing generated artifacts and dependency changes from toolchain setup.
- Do not commit or deploy.

## Tasks
- [x] ODD-1: Correct generated camera field usage in media access code. (`cam.HqStream` now matches generated field.)
- [x] ODD-2: Resolve the remaining frontend TypeScript errors with behavior preserved. (`pnpm typecheck` passed.)
- [ ] ODD-3: Run frontend typecheck, Go tests, lint, and build checks; record every result. Independent verifier confirms Go tests, typecheck, and temporary-output builds pass; `make lint` fails only on the separate `apps/web/src/routes/Playback.tsx:32` purity finding.
- [ ] ODD-7: Obtain scope decision for the newly surfaced, sole remaining lint finding in `apps/web/src/routes/Playback.tsx:32`.
- [x] ODD-5: Mapped the 14 lint findings in the nine authorized files. Keep TLS-dependent cookie behavior; validate all narrowing conversions; reject negative TOTP times; apply mechanical formatting/unused-parameter/expression fixes.
- [ ] ODD-6: Fix all authorized lint findings in exactly the nine named files and rerun required checks. Writer reports all 14 original findings cleared; current `make lint` reports one separate frontend purity finding outside authorized paths. Awaiting independent verification.
- [x] ODD-4: Diagnose and fix the additional Go build errors (user authorized scope expansion); updated `internal/api/identity_handlers.go` and `internal/api/inventory_handlers.go` to match generated response/enum types and use distinct input mappers; `go test ./...` passed.

## Verification mode
- Strict TDD: disabled by explicit user choice in this session (2026-09-27).
- Exact verification runner: `go test ./...`; `make lint`; `pnpm typecheck`; `mkdir -p /tmp/openvms-build-check && for app in api worker frigate-mock vmsctl; do go build -trimpath -o /tmp/openvms-build-check/$app ./apps/$app || exit; done`.

## Acceptance criteria
- `pnpm typecheck` succeeds.
- `go test ./...` succeeds.
- `make lint` is run and its outcome recorded. `make build` was run but failed before Go build completion; subsequent Go build verification must target `/tmp` to avoid overwriting the provenance-unknown `bin/` artifacts.
- No unrelated source changes are introduced.

## Verification evidence
- Baseline: `docker compose config --quiet` passed.
- Baseline: `make generate` completed.
- Baseline: frontend typecheck then reported five remaining errors in `src/api/queries.ts`, `src/components/HlsPlayer.tsx`, `src/routes/Live.tsx`, and `src/routes/Permissions.tsx`.
- Baseline: `go test ./...` failed because `internal/media/access.go` referenced `cam.HQStream`; generated field is `HqStream`.
- Baseline: `pnpm install --frozen-lockfile` failed because the lockfile omitted `hls.js`; `pnpm install --no-frozen-lockfile` succeeded and updated the lockfile.
- First writer result: edits completed in the five initially authorized files; no commit. `pnpm typecheck` passed. Initial `go test ./...`, `make lint`, and `make build` failed on additional API/type errors. Frontend build completed with a large-chunk warning. `make build` also wrote `bin/worker` and `bin/frigate-mock` (41,612,270 and 10,041,766 bytes; timestamps 2026-09-27 21:04). These artifacts are preserved pending attribution; none removed.
- Second writer result: edited only `internal/api/identity_handlers.go` and `internal/api/inventory_handlers.go`. `go test ./...`, `pnpm typecheck`, and builds to `/tmp/openvms-build-check` passed. `make lint` failed with 14 findings, including `G124` cookie attributes and revive unused `ctx` in identity handler, plus findings in other files; baseline attribution is unknown. No generated code, commit, deployment, or make build was run.
- Independent native risk assessment failed closed (`unassessable`): Git untracked-file enumeration from `/root` exceeded 8 MiB (254,022 paths); independent verifier was run as required.
- Independent lint verification: 14 total findings. In the changed `internal/api/identity_handlers.go`: line 38 gosec G124 cookie security attributes (Secure is conditional on TLS; HttpOnly/SameSite are set), line 49 revive unused `ctx`. Outside changed files: `apps/api/main.go:108` gofmt; `internal/frigate/frigate_test.go:159` gofmt; `internal/identity/identity_test.go:31` gofmt; `internal/api/auth.go:129` gosec G124; `internal/identity/password.go:102` gosec G115; `internal/identity/service.go:157,173,839` gosec G115 and `:435` staticcheck QF1001; `internal/identity/totp.go:62,71` gosec G115; `internal/media/exports.go:96` revive unused parameter. None point to `internal/api/inventory_handlers.go`.
- User explicitly authorized fixing all 14 lint findings across the 9 named files; attribution to pre-change baseline remains unknown.
- Read-only lint mapping: preserve request-TLS-based `Secure` cookie behavior with narrowly justified G124 suppressions if needed; remove the unused `ctx` from `me`; validate `len(want)` before Argon2 `uint32`; validate `MaxFailures` before `int32` at both uses; retain pagination default/cap before DB conversion; reject negative Unix time before TOTP `uint64` conversion (also guard `now == 0` drift); rename unused transaction parameter `_`; gofmt the three reported files; rewrite the exact QF1001 boolean equivalently after inspection. Useful tests: identity password/TOTP tests and Frigate wrong-password tests.
- The two TLS cookie reports are intentional conditional-HTTPS behavior, not permission to force Secure over plain HTTP; preserve this invariant.
- Second lint-fix pass reports all 14 original findings cleared. `go test ./...`, `pnpm typecheck`, and builds to `/tmp/openvms-build-check` passed. `make lint` failed only on `react-hooks/purity` at `apps/web/src/routes/Playback.tsx:32` (`Date.now()`), outside the nine authorized files. `/root/openvms/bin/*` remained untouched.
- Native assess after this writer again returned `unassessable` because Git untracked enumeration under `/root` exceeded the 8 MiB limit (254,668 paths); independent verification completed. It confirmed Go tests, `pnpm typecheck`, and all four temporary Go builds pass, and none of the 14 authorized findings remain. The sole current lint failure is `react-hooks/purity` at `apps/web/src/routes/Playback.tsx:32` (`Date.now()` during render), outside the authorized paths. Verifier made no repo edits and preserved `bin/`.
- User explicitly authorized continuing with the additional Go errors.
- User explicitly selected strict TDD disabled; this is the effective mode source for the continued writer. No files were changed by the first writer because it requested TDD mode clarification.
- Read-only `go test ./...` diagnostics: `internal/api/inventory_handlers.go` references missing generated auth-method constants (`MeAuthMethodToken`, `MeAuthMethodSession`), redeclares `groupInput` and passes the wrong input type for `CreateCameraGroup`; `internal/api/identity_handlers.go` uses `Login401JSONResponse.Code` and `.Message` fields absent from generated type and references missing `MeAuthMethodSession`. Further errors were truncated by compiler's 'too many errors'. Many unaffected packages pass.
- Read-only mapping confirmed generated Go types match `packages/api-contract/openapi.yaml`: generated auth enum values are `Session` and `Token`; Login401 embeds `UnauthorizedJSONResponse`/`Error`; user-group and camera-group inputs are distinct, with camera group needing its own mapper. Minimal code edit surface is the two handwritten handlers; no OpenAPI/generated changes indicated.
- `bin/worker` and `bin/frigate-mock` exist with timestamps at 2026-09-27 21:04; whether generated by this build cannot be determined. They remain untouched.
- Next route: bounded writer addresses only the five remaining G115/QF1001 findings in `password.go` and `service.go`; verify all checks and preserve `bin/`.
- Route: delegated direct implementation; separate verifier was required because native risk assessment was unassessable and RDD is on. Independent verification completed; lint findings await scope decision.

## Next step
Ask the user whether to authorize the one additional `Playback.tsx:32` lint fix. Do not edit it without approval.