# Faces scoped by site and server, like LPR

## Objective
Faces works for every user, platform administrators included, without picking an organization: recognitions, train attempts and configuration are filtered by site, server and camera and limited to what the caller may see, exactly like the LPR search; the person registry keeps syncing each person to every face-enabled server of its organization.

## Problem and why
- The faces API requires `tenant_id` from platform administrators (`internal/api/faces_handlers.go` `faceTenant`), so the web added an "Organización" selector (PR #93/#96). The user finds it confusing and wants Faces to behave like LPR (2026-10-19).
- LPR (`internal/events/service.go` `ListPlates`, `apps/web/src/routes/Plates.tsx`) takes no tenant: it filters by site, camera group, camera and dates and scopes rows with `access.Checker.CameraIDs` across every tenant the caller can see.
- The demo tenant was deleted on 2026-10-19 (dump `.atl/demo-cleanup/20261019T002452Z/postgres.dump`); only the real tenant `Casa` remains, with three real Frigate 0.18 servers.

## Product decisions (user, 2026-10-19)
- No organization selector. Work by site / server like LPR.
- A registered person keeps syncing to ALL face-enabled servers of its organization (current model).

## Scope / tasks
- [x] FSS-01 — API without mandatory `tenant_id`: person/image/sync/watchlist routes resolve the tenant from the referenced object; list/create routes without one use the caller's tenant, or for a platform administrator the only tenant visible (400 only when several exist and none is given; `tenant_id` stays optional). Recognitions take `site_id`, `server_id`, `camera_group_id`, `camera_id`, person and dates, scoped by authorized cameras across tenants with name redaction per event tenant. Train lists attempts of every face-capable server the caller may see, filterable by `site_id`/`server_id`. Config list filterable by `site_id`. OpenAPI first + `make generate`. Route: delegated direct.
- [x] FSS-02 — Web: remove the "Organización" selector; Recognitions becomes a search like Plates (site, camera group, camera, person, dates, chips); Train and Config gain site/server filters; queries stop sending `tenant_id`. Route: delegated direct.
- [x] FSS-03 — Detection photos in Persons (user, 2026-10-19): the person detail shows a gallery of the person's latest recognitions (event snapshot, date, camera, opens playback), and each person card in the list shows a thumbnail from the latest recognition, falling back to the first enrollment image. Same permissions as today (cameras visible with events.view, snapshots.view, faces.view on the tenant). Route: delegated direct.

## Constraints and checks
- Strict TDD (user global config). Runners: `GOPROXY=off GOTOOLCHAIN=local GOCACHE=/tmp/openvms-go-build-cache go test -race ./...`, `go test -count=1 -tags integration ./internal/faces/... ./internal/api/... ./internal/events/...`, `golangci-lint run ./...`, `cd apps/web && pnpm exec vitest run <file>`, `pnpm typecheck`, `pnpm test`.
- Privacy unchanged: names only with `faces.view` on the event's tenant; audit as today.
- Forecast above 400 authored lines: delivery strategy `ask-on-risk`; slice API and web into separate PRs.

## Progress
- 2026-10-19: branch `feat/faces-site-server-scope` from `main` `928e57c`.

- 2026-10-19: FSS-01 done (delegated writer). RED: new integration tests failed first (API: 400 `tenant_id is required` on persons/watchlists/train/config/recognitions and every object route; faces/inventory: compile errors for the new filters). GREEN: `go test -tags integration` faces, inventory, api (40m timeout; the default 10m is too short for the whole api package), events; `go test -race` faces/api/events; `golangci-lint` 0 issues; build/vet, generation (no extra diff), `pnpm typecheck`, `git diff --check` clean. Commits: `5ec99cc` (contract + generated), `6307e1b` (code + tests). Web needed no adjustment. Route: delegated direct (one writer; mapping done by the writer).
- Design notes: `faces.Service` gains `SoleTenant`/`PersonTenant`/`WatchlistTenant`/`ServerTenant`; `ListRecognitions`, `ListTrainAttempts` and `inventory.ListFaceConfig` take a filter struct with optional `TenantID`; recognitions and train audit one row per organization read. Recognition rows of organizations where the caller lacks `faces.view` are left out (never shown with names).

- 2026-10-19: FSS-02 done. RED: 8 vitest failures (no selector expected, no `tenant_id`, recognition filters, train/config site and server filters). GREEN: `vitest` Faces/components/faces/Rules/Plates 73 passed, full `pnpm test` 187 files / 1664 tests, `pnpm typecheck`, eslint clean. Commit `58715a4`. `rangeEnd` moved from Plates to `lib/eventsSearch.ts`; removed i18n `faces.tenant`/`faces.noTenants`; Config server filter is client-side (API only takes `site_id`).
- 2026-10-19: FSS-03 done. Contract: `FacePerson.last_recognition` (`FaceLastRecognition`: event_id, start_time, has_thumbnail) and `first_image_id`, list-only, filled by one lateral query (latest event on cameras with events.view) plus a subquery in the list SQL, so no per-card request. Web: gallery in the person detail reuses the recognitions query (`person_id`, limit 12); card thumbnail = latest detection (with `snapshots.view`) else first enrollment photo else placeholder. RED: Go compile errors (`LastRecognition`/`FirstImageID`) and 5 vitest failures. GREEN: `go test -tags integration` faces + api Face tests, `go test -race` faces/api, `golangci-lint` 0 issues, generation no diff. Commits `531d540` (contract + generated), `2758cfe` (backend), `f17f191` (web).

## Next step
Native review of FSS-02/FSS-03, then delivery strategy (API and web slices).
