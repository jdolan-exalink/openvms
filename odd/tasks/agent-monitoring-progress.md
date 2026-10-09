# Agent monitoring and deployment progress

Add useful operational feedback to the existing Servers screen without changing the agent lifecycle security model. Operators should see honest install/update progress, integer CPU usage, and fresh network throughput; a known binary mismatch should be visually prominent and accessible. Keep unknown or unavailable measurements distinct from zero.

## Scope and constraints

- **Authorized scope**: implement/test local code and perform the explicitly authorized local API/web deployment. No remote SSH operation, remote agent update, authenticated session reuse, or camera contact.
- **Release identity**: the user explicitly wants to test edge-agent release `0.1.1`; retain that release and protocol `0.1.0`. Do not silently bump to `0.1.2`. If code changes alter the artifact while retaining `0.1.1`, exact digest comparison still detects the new binary; UI identity should include a short commit/digest when present rather than presenting the release label as a unique build ID.
- **Current status**: “outdated” styling is based only on the verified lifecycle result (`binary_outdated === true`), never on version-string comparison or an offline state. A match requires exact digest plus supported matching architecture, as already implemented.
- **No fake zero**: first samples, stale samples, unavailable counters, counter resets, malformed proc data, and legacy agents without the new fields display as unavailable/unknown, not `0` or “up to date.”
- **No credential or job changes**: progress is presentation of existing memory-only job status. Do not add credential retention, automatic retry, new backend job persistence, SSH controls, or expanded API access.

## Measurement and UI decisions

| Area | Contract |
|---|---|
| Network source | The installed Linux edge-agent runs in the target host network namespace and reads `/proc/net/dev`; expose per-interface byte counters/deltas. Exclude `lo`. Do not claim the sum of bridge/veth traffic is physical-link throughput: if a UI total is shown, label it as a sum across visible non-loopback interfaces and retain the per-interface breakdown. |
| Sampling | Compute RX/TX bytes per second from two successful samples at least one second apart using actual monotonic elapsed time. First sample is unavailable. Serialize concurrent samples; bounded-read/parse failures fail closed for the affected measurement. If an interface appears, disappears, or a counter decreases/resets, report unavailable for that interface until a valid new interval exists. |
| Freshness | Include a UTC sampled timestamp so the client can avoid presenting stale values as live. The Servers monitor accepts network samples no older than 15 seconds; stale, missing, or future timestamps show unavailable. |
| CPU | Render an available CPU percentage as a rounded whole integer (`0%`–`100%`); missing/stale CPU data remains unavailable. Preserve current API units and do not reinterpret absent values as zero. |
| Job elapsed time | Show a starting state while submission is pending. Start the local elapsed timer only after a queued/running job is accepted; display elapsed duration while active and freeze at terminal/unknown state. Use a monotonic clock and never alter polling, deadline, or cancellation behavior. |
| Motion | Any spinner/progress animation respects `prefers-reduced-motion`; reduced motion uses a static indicator. The textual status/elapsed value remains available without animation. |
| Outdated emphasis | When verified `binary_outdated === true`, use the project's bold text and warning/error color token plus explicit accessible text/status. Do not rely on color alone. Unknown/unreachable/untrusted is never styled as confirmed outdated. |

## Work units

| ID | Acceptance | Verification |
|---|---|---|
| `AGMON-01` — sampled network throughput | Add a testable bounded `/proc/net/dev` sampler with injected reader/clock. Return per-interface RX/TX bytes-per-second and freshness metadata only after a valid interval; exclude loopback; handle concurrency, first sample, missing interfaces, reset counters, malformed input, and read/size errors without inventing zero. Expose it through the existing agent metrics contract while keeping legacy protocol and health contracts compatible. | RED/GREEN tests use fixture proc data and fake monotonic time; cover byte-delta math/units, minimum interval, first/partial sample, reset/disappearing interface, malformed/oversized/read errors, concurrency, freshness, and no external socket. Run `make generate` if the OpenAPI contract changes. |
| `AGMON-02` — truthful active job elapsed state | Add an elapsed indicator to existing install/update progress. Start only after job acceptance, count only while accepted job is queued/running, freeze when terminal or outcome becomes unknown, and preserve existing request timeout/poll/cancel/password-clearing behavior. Motion honors reduced-motion preference. | RED/GREEN component tests use fake timers and `performance.now`: pending submission has no fabricated duration; queued/running advances; terminal and unknown freeze; stale/late polls do not restart it; normal and reduced-motion indicators remain accessible. No actual endpoint or credential submission. |
| `AGMON-03` — resource display and mismatch emphasis | Show integer CPU and fresh per-interface RX/TX rates in the existing Servers monitor. Handle unknown/stale/legacy data clearly; apply bold warning color and accessible status only for verified outdated state. Keep release version, digest/build identity, and protocol version distinct. | RED/GREEN UI/API projection tests cover CPU rounding and unavailable state, network units/freshness and unavailable state, legacy omission, verified outdated emphasis, no emphasis for unknown/current, and protocol-vs-binary labels. Run focused/full web tests, typecheck, and scoped lint. |

## TDD, route, and checks

- Organic route: delegated direct, single writer per work unit. This feature crosses the edge-agent sampler/metrics contract and the Servers UI; preparation and tests must stay with the writer.
- Strict TDD is enabled in `AGENTS.md`: observe RED before implementing each work unit, then GREEN and REFACTOR. UI tests use Vitest; Go tests use fake readers/clocks and in-process seams only.
- Go checks: `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./apps/edge-agent ./internal/agent ./internal/api`; focused race for touched runtime packages; final `GOCACHE=/tmp/openvms-go-build-cache go test ./...`; `git diff --check`. Include `make generate` if schemas change.
- Web checks: `(cd apps/web && pnpm exec vitest run src/routes/Servers.test.tsx)`, `(cd apps/web && pnpm typecheck)`, `(cd apps/web && pnpm test)`, and scoped ESLint for changed files.
- Harness boundary: fake `/proc` readers, deterministic clocks, fake API requests, and UI mocks. No real network sampling on production host, Docker service changes, SSH, DB writes, or camera access.
- Forecast: likely 400–700 authored additions/deletions due Go sampling behavior, UI state, and deterministic regression tests. This is advisory, not a cap; preserve tests and clear semantics. Strategy: `ask-on-risk`; chain preference remains `stacked-to-main` from the lifecycle feature.

## Progress

- [x] `AGMON-01` — completed. Added bounded `/proc/net/dev` parsing (64 KiB, at most 256 interfaces, names at most 64 bytes), monotonic sampling with serialized access, minimum one-second deltas, sorted per-interface rates, loopback exclusion, reset/disappearance handling, and optional timestamped metrics. CPU metrics now omit invalid `/proc/stat` readings instead of serializing a false zero. OpenAPI/Go/TypeScript projections preserve omission for legacy/unavailable samples.
- [x] `AGMON-02` — completed. Both install and update dialogs show a starting state while submission is pending, then use a `performance.now()` elapsed timer only after an accepted job; elapsed display freezes for terminal/unknown outcomes. The indicator is static under reduced-motion preference. Existing polling, deadlines, retries, and password handling were not changed.
- [x] `AGMON-03` — completed. Servers monitor rounds and clamps finite CPU percentages to integer 0–100; network values are shown per interface only when sample timestamp is within 15 seconds and not in the future. Missing/stale/legacy resource values remain unavailable. Only `binary_outdated === true` produces bold warning styling and an explicit accessible status.

## Observed verification

- Strict TDD RED: before implementation, the new network tests failed to compile on missing sampler/parser symbols; the API projection regression failed on absent generated network fields; the new Servers tests failed on missing CPU-unavailable and starting-progress UI. All failures were observed before their corresponding implementations.
- `make generate` — PASS after adding the optional network interface/timestamp schema.
- `GOCACHE=/tmp/openvms-go-build-cache go test -count=1 ./apps/edge-agent ./internal/agent ./internal/provision ./internal/api` — PASS.
- `GOCACHE=/tmp/openvms-go-build-cache go test -race -count=1 ./apps/edge-agent ./internal/agent ./internal/provision ./internal/api` — PASS.
- `GOCACHE=/tmp/openvms-go-build-cache go test ./...` — PASS.
- `(cd apps/web && pnpm exec vitest run src/routes/Servers.test.tsx)` — PASS, 62 tests.
- `(cd apps/web && pnpm typecheck)` — PASS.
- `(cd apps/web && pnpm exec eslint src/routes/Servers.tsx src/routes/Servers.test.tsx src/i18n/locales/en/servers.ts src/i18n/locales/es/servers.ts src/i18n/locales/pt/servers.ts)` — PASS.
- `(cd apps/web && pnpm test)` — PASS, 120 files / 899 tests.
- `git diff --check` — PASS.

## Authorized local deployment (2026-10-07)

The user authorized deploying this AGMON build to the local OpenVMS API/web services only. No schema migration was needed or run. The remote `opevms-mimo` agent was not updated, and no SSH, authenticated browser session, or camera operation occurred; legacy remote agents will not report the new network fields until separately updated.

| Check | Observed result |
|---|---|
| Candidate | `f9d2730`, clean `feat/onvif-camera-management` before deployment; `docker compose config --quiet` passed. |
| Protected backup | `.atl/onvif-deploy/monitoring-f9d2730/postgres.dump`, directory mode `0700`, archive mode `0600`, 23,807,057 bytes, SHA-256 `c1323517cfb730d852e8b906bbae390e64e1b8aef7d24ecf792e704cba859706`; `pg_restore --list` in local Postgres 17 container passed without printing TOC contents. |
| Rollback images | Created without overwriting existing tags: `openvms-api:rollback-pre-monitoring-f9d2730` → prior image `sha256:8cd193c6e83b1ab157301237d5757a0e06b917d70fda3f680e27a86737d97c0d`; `openvms-web:rollback-pre-monitoring-f9d2730` → prior image `sha256:7022c37098d36b47174468857b66041d42c442c4ac1e6ea4c2e4bed718601e1a`. |
| Build and artifact | `AGENT_VERSION=0.1.1 docker compose build --pull=false api web` passed. Bundled agent metadata was version `0.1.1`, architecture `amd64`; the artifact SHA-256 matched its packaged manifest (`6adbd3fad3452bb9f0dd4ead40b45cde35793a76ace57732e7d9b2f3088ca690`). Protocol remains `0.1.0`. |
| Deployment | Recreated only `api` and `web` via `docker compose up -d --no-deps api web`; PostgreSQL and worker container IDs were unchanged. API/web restart count remained `0` after a 30-second stability interval. |
| Availability | HTTP `:8000` live health `200`; HTTPS `:8443` live and ready health `200` using the local CA; HTTPS-port discovery endpoint returned `8443` on both listeners. Served Servers JavaScript bundle included network, `B/s`, and elapsed-progress strings. |
| Auth boundary | Anonymous schema-valid update/install POSTs over HTTPS returned `401`; HTTP update POST returned `403`; anonymous update-job GET returned `401`. The update POST used only a non-secret placeholder, not an SSH password. No job was created. |

Rollback is the recorded API/web image tags; restore of the protected database archive is not part of this deployment because no DB writes or migration occurred. The same worker container remained running; its observed restart count after deployment was `28` (a pre-deployment count was not captured).

The existing one-second agent metrics cache is retained; clients need one valid network interval before rates appear. Parent owns the Engram mirror refresh for this document.
