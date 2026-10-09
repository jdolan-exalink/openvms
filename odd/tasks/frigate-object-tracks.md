# Feature: Frigate object tracks in evidence exports

Locator: `odd/tasks/frigate-object-tracks.md` · Engram mirror: `odd/frigate-object-tracks/tasks` · Branch: `feat/frigate-object-tracks`

## Objective
Show the real Frigate object trajectory (path_data dots + box) in the export evidence player, and mark when the object enters each configured zone.

## Problem
`CameraEvidenceOverlay` (`apps/web/src/components/EvidencePlayerModal.tsx`) synthesizes trajectories from zone vertices or hardcoded paths and interpolates a fixed-size box by elapsed time. The drawn path does not follow the object and often runs backwards. Inventing a path in a forensic player is not acceptable.

## Why
Frigate already provides `data.path_data` (`[[x,y], unix_ts]`, normalized, bottom-center of the box) and `data.box` (`[x,y,w,h]` normalized) on every tracked object. The syncer already reads every tracked object from `/api/events` and discards that data.

## Scope
- Persist per tracked object: label, box, path_data, zones, start/end time (`object_tracks`, keyed by `(server_id, remote_object_id)`, tenant RLS like `object_snapshots`).
- Re-read still-open objects so path_data is not frozen partially.
- Expose tracks on `Event` (`/api/v1/events`, `GET /events/{id}`) via `detection_ids`.
- Freeze tracks into the export manifest at export creation.
- Replace the synthetic overlay with real dots up to current playback time, current position, start/end, and zone-entry markers (name + time).

## Constraints
- English artifacts; Conventional Commits; no AI attribution in commits (user rule).
- Do not stage the pre-existing unrelated maps/room-planner working-tree changes.
- Events/syncer code uses raw pgx SQL, not sqlc.
- TDD: Strict TDD enabled (source: user global CLAUDE.md). Runners: `go test ./internal/frigate/...` (unit), `go test -race -tags integration ./internal/events/...` (integration, Docker), `pnpm --dir apps/web test` (vitest).

## Tasks
- [ ] T1 Ingest: adapter decodes `box`/`path_data`; migration `00039_object_tracks.sql`; syncer upserts tracks for known cameras and holds back the object cursor for open objects. Route: delegated direct (writer trigger: 3+ non-trivial files).
- [ ] T2 API + manifest: `Event.tracks` in openapi + `make generate`; aggregated subquery in `eventColumns`/`scanEvent`; `toEvent`; export manifest copies tracks. Route: delegated direct (writer trigger).
- [ ] T3 Player: pure helper `apps/web/src/lib/objectTracks.ts` (trail up to t, current position, zone entries via point-in-polygon) with vitest; replace synthetic overlay. Route: delegated direct (writer trigger).

## Acceptance criteria
- A tracked object's path_data/box from Frigate is stored and updated until the object ends.
- `/api/v1/events` items carry `tracks` for their detections; export manifest events carry the same tracks.
- Player draws only real data; with no tracks it draws no trajectory (no fabrication).
- Zone entries are shown with zone name and time.

## Checks
- `go test ./internal/frigate/... ./migrations/...`
- `go test -race -tags integration ./internal/events/... ./internal/media/...`
- `go vet ./...`, `pnpm --dir apps/web typecheck`, `pnpm --dir apps/web test`, `pnpm --dir apps/web lint`

## Delivery
Forecast: ~900 authored changed lines (> 400). Strategy: ask-on-risk (default); chain strategy pending user choice before first commit.

## Progress
- Exploration done (mapper handoff). Branch created.
- T1 code written (uncommitted): adapter Box/Path + `TrackPoint`, `00039_object_tracks.sql`, syncer `upsertObjectTrack` + open-object cursor hold (`objectOpenHold = 1h`), frigatemock track support, `TestObjectTracksSync`.
  - Unit RED: compile error `o.Box undefined`; GREEN: `ok internal/frigate`.
  - `go build ./...`, `go vet`, `go test ./internal/frigate/... ./migrations/...`: ok.
  - Integration: BLOCKED. Pre-existing bug: every integration run fails at migration 00033 (`incompatible server_agent_tls constraints`). Line 107 compares against `length(ca_pem)` after stripping parentheses. Verified by reading the file. Fixing 00033 needs user authorization (writer edit was denied by the permission classifier).

## Next step
Resolve the 00033 blocker, run `TestObjectTracksSync` (RED/GREEN), commit T1. Chain strategy still pending.
