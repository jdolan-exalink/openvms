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
- [ ] FSS-01 — API without mandatory `tenant_id`: person/image/sync/watchlist routes resolve the tenant from the referenced object; list/create routes without one use the caller's tenant, or for a platform administrator the only tenant visible (400 only when several exist and none is given; `tenant_id` stays optional). Recognitions take `site_id`, `server_id`, `camera_group_id`, `camera_id`, person and dates, scoped by authorized cameras across tenants with name redaction per event tenant. Train lists attempts of every face-capable server the caller may see, filterable by `site_id`/`server_id`. Config list filterable by `site_id`. OpenAPI first + `make generate`. Route: delegated direct.
- [ ] FSS-02 — Web: remove the "Organización" selector; Recognitions becomes a search like Plates (site, camera group, camera, person, dates, chips); Train and Config gain site/server filters; queries stop sending `tenant_id`. Route: delegated direct.

## Constraints and checks
- Strict TDD (user global config). Runners: `GOPROXY=off GOTOOLCHAIN=local GOCACHE=/tmp/openvms-go-build-cache go test -race ./...`, `go test -count=1 -tags integration ./internal/faces/... ./internal/api/... ./internal/events/...`, `golangci-lint run ./...`, `cd apps/web && pnpm exec vitest run <file>`, `pnpm typecheck`, `pnpm test`.
- Privacy unchanged: names only with `faces.view` on the event's tenant; audit as today.
- Forecast above 400 authored lines: delivery strategy `ask-on-risk`; slice API and web into separate PRs.

## Progress
- 2026-10-19: branch `feat/faces-site-server-scope` from `main` `928e57c`.

## Next step
FSS-01 through one writer.
