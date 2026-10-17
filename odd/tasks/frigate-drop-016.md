# Drop Frigate 0.16 support (minimum 0.17)

## Objective
Make Frigate 0.17 the minimum supported version across the product and delete the 0.16-only code paths.

## Problem and why
0.16 lacks `PUT /api/config/set` with `config_data`, so OpenVMS keeps a second config-apply path (dotted query params, restart-required, no object lists) plus UI/mock/test variants, and safe managed-zone sync cannot run on it. The user decided (2026-10-16) to drop 0.16 product-wide rather than keep it half-supported.

## Scope
- Version gate in `internal/frigate/adapter.go` `Connect` (currently accepts 0.14–0.17 as `v017`): reject versions below 0.17 with an actionable error naming the detected version and the 0.17 minimum. Registration/probe of a 0.16 server fails; already-registered 0.16 servers surface that error on connect.
- Delete dead 0.16 paths: `supportsConfigData` branch, `setViaQuery`/`flattenQuery`, restart-only apply in `ApplyCameraPatch`, `ErrManagedZoneSyncUnsupported` gates in `config_edit.go`, and any `ConfigEditable`/`ErrConfigEditUnsupported` logic made unreachable by the gate.
- `internal/frigatemock`: remove 0.16 query-param simulation; mock default stays 0.17.
- Web: `frigateSchema.ts` `isLiveSection`, `FrigateCameraConfig.tsx` `versionOk`/"requiere 0.16"/`LiveBadge` variants, `CuratedPanels.tsx` tooltip.
- Contract/docs: OpenAPI descriptions mentioning 0.16, regenerated `schema.d.ts`; state the 0.17 minimum in `docs/architecture/decisions.md` and README.
- Out of scope: zone-sync `unsupported` state (lives only on the unmerged Phase 4–5 branch; removed there after this merges).

## Constraints
- Strict TDD (source: user global config). Runners: `GOCACHE=/tmp/openvms-go-build-cache go test -race ./...`, `go test -tags integration ./internal/...`, `cd apps/web && pnpm exec vitest run <file>`, `pnpm typecheck`, `golangci-lint run ./...`.
- OpenAPI first + `make generate` for contract text changes. No migrations expected.
- Delivery: `ask-on-risk` with cached `stacked-to-main`; one PR.

## Tasks
- [x] FR16-01 — Backend gate and dead-path removal (adapter, config_edit, inventory/api callers, mock, Go tests). Route: delegated direct (writer trigger: 2+ non-trivial files).
- [x] FR16-02 — Web UI and contract/docs cleanup (frigateSchema, FrigateCameraConfig, CuratedPanels, OpenAPI text, generated types, docs, web tests). Route: delegated direct.
- [ ] FR16-03 — PR, CI green, merge.

## Acceptance
- Connecting to a 0.16 (or older) server fails with an error that names the version and the 0.17 minimum; 0.17 and 0.18 behave unchanged.
- No code path distinguishes 0.16 from 0.17 anymore; tests no longer exercise 0.16 except the rejection test.
- Lint, unit, integration, web tests and typecheck pass.

## Progress
- 2026-10-16: document created; branch `feat/frigate-min-017` from `origin/main` (`377f427`).

- 2026-10-16: FR16-01 done (commit `37e006c`, delegated direct writer). Evidence:
  - RED: `go test -run TestConnect ./internal/frigate/` failed (0.14-0.16 accepted, 0.13 error lacked ErrUnsupportedVersion/0.17).
  - GREEN: `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (0 issues), `go test -race -count=1 ./...` all pass; `go test -tags integration ./internal/frigate/... ./internal/api/... ./internal/inventory/...` pass.
  - Removed: `ConfigEditable`, `ErrConfigEditUnsupported` (and its `config_edit_unsupported` API mapping), `supportsConfigData`, `setViaQuery`, `flattenQuery`, legacy query-param `UpdateCameraConfig` fallback, mock query-param simulation. Unreachable-gate errors now surface as 502 `frigate_unreachable` naming the version.
  - Rollback boundary: revert `37e006c`.

- 2026-10-16: FR16-02 done (commit `48343fa`, delegated direct writer). Evidence:
  - RED: `isLiveSection("detect")` test failed before implementation (version param still required).
  - GREEN: `pnpm typecheck`, eslint on changed files, full web vitest (1541 passed), `go build ./...`, `go test ./internal/api/...` pass. Servers probe test confirms the 502 version message is displayed (already covered by `ErrorNote`).
  - Changed: `isLiveSection(section)` no longer takes a version; removed `parseVersion`, `versionOk`, "requiere 0.16" copy, CuratedPanels tooltip; OpenAPI text and 409s for 0.16 removed, types regenerated; README and decisions.md state the 0.17 minimum. `LiveBadge` kept (real per-section restart semantics).
  - Rollback boundary: revert `48343fa`.

## Next step
FR16-03: PR, CI green, merge.
