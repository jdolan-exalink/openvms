# Resolve build and typecheck errors

## Objective
Restore the OpenVMS backend build and frontend typecheck after installing the project toolchain and regenerating API clients.

## Problem and rationale
The first verification exposed a generated Go field-name mismatch and frontend TypeScript errors, some of which were reduced by `make generate`. Fix the remaining source-level incompatibilities so the project can proceed to full deploy-readiness checks.

## Scope and constraints
- Fix the known Go compile error, frontend TypeScript errors, and additional Go build failures explicitly authorized by the user on 2026-09-27.
- Original lint scope was the 9 files implicated by 14 diagnostics. The user has now explicitly authorized the additional purity fix in `apps/web/src/routes/Playback.tsx` only; no other scope expansion.
- Preserve existing generated artifacts and dependency changes from toolchain setup.
- A work-unit commit on the feature branch is required before closure; do not deploy or push.

## Tasks
- [x] ODD-1: Correct generated camera field usage in media access code. (`cam.HqStream` now matches generated field.)
- [x] ODD-2: Resolve the remaining frontend TypeScript errors with behavior preserved. (`pnpm typecheck` passed.)
- [x] ODD-3: Run frontend typecheck, Go tests, lint, and build checks; record every result. Current run results are recorded below; Go cache writes were redirected to `/tmp`.
- [x] ODD-7: Scope authorization received for the sole remaining `react-hooks/purity` finding in `apps/web/src/routes/Playback.tsx:32`; authorized edits are that source file and the necessary regression test/task tracking changes.
- [x] ODD-5: Mapped the 14 lint findings in the nine authorized files. Keep TLS-dependent cookie behavior; validate all narrowing conversions; reject negative TOTP times; apply mechanical formatting/unused-parameter/expression fixes.
- [x] ODD-6: Fix all authorized lint findings in exactly the nine named files and rerun required checks. Independent verification confirmed all 14 original findings cleared; the only remaining source lint finding was the separate frontend purity issue in `apps/web/src/routes/Playback.tsx:32`, outside authorized paths.
- [x] ODD-4: Diagnose and fix the additional Go build errors (user authorized scope expansion); updated `internal/api/identity_handlers.go` and `internal/api/inventory_handlers.go` to match generated response/enum types and use distinct input mappers; `go test ./...` passed.
- [x] ODD-8: Fix render-time clock impurity in `Playback.tsx` without changing UI behavior and add regression coverage. The mount refresh now resets the default day, instant, and position together when no explicit `search.t` is present. A delayed-mount test verifies that module initialization at 2025-06-15 followed by route mount at 2025-07-16 displays the latter date and time.
- [x] ODD-9: Added direct regression coverage that an explicit `search.t` is preserved during mount refresh and that the 30-second clock interval updates and is cleaned up. Focused test (2 tests), full web suite (15 files, 69 tests), `pnpm typecheck`, web lint, and `git diff --check` passed. Route: delegated direct; strict TDD; test/documentation-only, under 100 authored changed lines. Work-unit commit `7eecd8b` (created after repository metadata became writable).

## Verification mode
- Strict TDD: enabled by current active project instructions; follow RED -> GREEN -> REFACTOR. Prior disabled selection (2026-09-27) is superseded for this work.
- Exact verification runner: focused web test; `pnpm --filter web test`; `pnpm typecheck`; `GOCACHE=/tmp/openvms-go-build-cache make lint`; `GOCACHE=/tmp/openvms-go-build-cache make test`; `mkdir -p /tmp/openvms-build-check && for app in api worker frigate-mock vmsctl; do go build -trimpath -o /tmp/openvms-build-check/$app ./apps/$app || exit; done`.

## Acceptance criteria
- `pnpm typecheck` succeeds.
- `go test ./...` succeeds.
- `make lint` is run and its outcome recorded. `make build` was run but failed before Go build completion; subsequent Go build verification must target `/tmp` to avoid overwriting the provenance-unknown `bin/` artifacts.
- No unrelated source changes are introduced.

## Verification evidence
- ODD-8 RED: `pnpm --filter web exec vitest run src/routes/Playback.test.tsx` failed before implementation: the Playback day input had no value instead of the current local date. GREEN: focused test passed after moving the initial clock read outside render and keeping the mount refresh asynchronous.
- ODD-8 final checks: `pnpm --filter web exec vitest run src/routes/Playback.test.tsx` passed (1 test); `pnpm --filter web test` passed (15 files, 68 tests); `pnpm typecheck` passed.
- ODD-3 final checks: `GOCACHE=/tmp/openvms-go-build-cache make lint` passed (`go vet`: 0 issues; frontend ESLint and typecheck passed). Go linter emitted cache-write warnings for read-only `/root/.cache/golangci-lint`, but reported no issues and exited successfully.
- ODD-3 Go tests: initial `GOCACHE=/tmp/openvms-go-build-cache make test` failed because sandbox policy denied loopback `httptest` listeners in `internal/api`, `internal/frigate`, and `internal/frigatemock`; after approval to bind local test ports, the exact command passed (`go test -race ./...` and `pnpm test`; 15 files, 68 tests).
- ODD-3 temp Go build: exact requested loop initially failed trying to write `/root/.cache/go-build`; rerun with `GOCACHE=/tmp/openvms-go-build-cache` passed for `api`, `worker`, `frigate-mock`, and `vmsctl`. Go logged read-only module-cache stat-cache warnings, but the build loop exited successfully. No `bin/*` files were touched.
- Follow-up review confirmed the first ODD-8 fix can leave timeline state (`instant`, `day`, `position`) based on the module-load snapshot when Playback mounts later; regression test must simulate distinct module-evaluation and mount times.
- Reopened ODD-8 RED: `pnpm --filter web test` failed the delayed-mount regression as expected: module-load date `2025-06-15` remained visible instead of mount date `2025-07-16`. GREEN: `pnpm --filter web exec vitest run src/routes/Playback.test.tsx` passed (1 test) after refreshing all dependent default state at mount; the test waits for the deferred refresh.
- Reopened ODD-8 final checks: `pnpm --filter web test` passed (15 files, 68 tests); `pnpm typecheck` passed; `pnpm --filter web lint` passed. One intermediate lint attempt caught declaration order and effect dependency issues in the timer closure; state hooks were moved above the effect and `search.t` is tracked, after which lint passed.
- ODD-9 RED: Initial focused-test runs exposed test-harness issues, not a production defect: fake timers prevented the async router from rendering until advanced, then an unwrapped click left the timer-driven rerender unflushed; a global zero-timer assertion also counted unrelated React Query timers. No production code was changed. GREEN: focused Playback test passed after advancing fake time inside `act`, wrapping the click in `act`, and asserting the specific 30-second interval handle is cleared on unmount. The test verifies `search.t=1749945600` remains `2025-06-15T00:00` despite mount at 2025-06-15 23:59:50, then advances 30 seconds across midnight and confirms “Día siguiente” can advance to 2025-06-16.
- ODD-9 final checks: `pnpm --filter web exec vitest run src/routes/Playback.test.tsx` passed (2 tests); `pnpm --filter web test` passed (15 files, 69 tests); `pnpm typecheck` passed; `pnpm --filter web lint` passed. An initial lint attempt found an unused destructured `_` in the new assertion; replaced it with indexed access and reran lint successfully.
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
- Historical note: a prior writer recorded the 2026-09-27 disabled choice for separate work; it does not override the current active strict-TDD instruction for ODD-8.
- Read-only `go test ./...` diagnostics: `internal/api/inventory_handlers.go` references missing generated auth-method constants (`MeAuthMethodToken`, `MeAuthMethodSession`), redeclares `groupInput` and passes the wrong input type for `CreateCameraGroup`; `internal/api/identity_handlers.go` uses `Login401JSONResponse.Code` and `.Message` fields absent from generated type and references missing `MeAuthMethodSession`. Further errors were truncated by compiler's 'too many errors'. Many unaffected packages pass.
- Read-only mapping confirmed generated Go types match `packages/api-contract/openapi.yaml`: generated auth enum values are `Session` and `Token`; Login401 embeds `UnauthorizedJSONResponse`/`Error`; user-group and camera-group inputs are distinct, with camera group needing its own mapper. Minimal code edit surface is the two handwritten handlers; no OpenAPI/generated changes indicated.
- `bin/worker` and `bin/frigate-mock` exist with timestamps at 2026-09-27 21:04; whether generated by this build cannot be determined. They remain untouched.
- Next route: bounded writer addresses only the five remaining G115/QF1001 findings in `password.go` and `service.go`; verify all checks and preserve `bin/`.
- Route: delegated direct implementation; separate verifier was required because native risk assessment was unassessable and RDD is on. Independent verification completed; lint findings await scope decision.

## Next step
ODD-9 closed with commit `7eecd8b` on `fix/playback-purity-lint`; its native review was approved and acknowledged (lineage `review-45f9e9cdf80ce5d7`). Advisory follow-ups: missing negative control (`Playback.test.tsx:62-68`) and timezone-dependent assertions (`:16-60`). Delivery (push/PR) follows ordinary repository policy. Preserve `bin/*`.
