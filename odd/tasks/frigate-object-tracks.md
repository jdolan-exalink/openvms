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
- [x] T1 Ingest (commit `5683e09`; integration test run pending): adapter decodes `box`/`path_data`; migration `00039_object_tracks.sql`; syncer upserts tracks for known cameras and holds back the object cursor for open objects. Route: delegated direct (writer trigger: 3+ non-trivial files).
- [x] T2 API + manifest: `Event.tracks` in openapi + `make generate`; aggregated subquery in `eventColumns`/`scanEvent`; `toEvent`; export manifest copies tracks. Route: delegated direct (writer trigger).
- [x] T3 Player: pure helper `apps/web/src/lib/objectTracks.ts` (trail up to t, current position, zone entries via point-in-polygon) with vitest; replace synthetic overlay. Route: delegated direct (writer trigger).

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

- 00033 fixed with user authorization: commit `31ae85d` (`fix(store)`).
- T1 committed: `5683e09` (`feat(events)`). Integration test run PENDING (Docker test run denied by permission classifier; user asked to run it). Integration RED was never observed (written while blocked).
- Deployed to local stack (`make up`), user-requested: migration 39 applied; real Frigate data ingested: 574 tracks, 568 with path, 4 open. Sample rows verified (box `[x,y,w,h]`, path `{x,y,t}`).
- RDD assess T1 range `3383496..5683e09`: medium, review_due (slice_budget_reached, 431 lines). Consent relayed to user.

- RDD consent: user declined T1 commits review (`declined_this_candidate`, target `sha256:43b1…`) and the uncommitted-workspace candidate (`sha256:fa7e…`, mostly user's maps WIP). Off-path tier medium: writer self-verification (full model) plus parent spot check (build and unit tests re-run). Done.
- T2+T3 delegated to one writer (sequential).
- T2: `Event.tracks` (`ObjectTrack`, `TrackPoint`, double floats), `events.TracksSubquery` shared with the export manifest, and `toObjectTracks`. Go RED: `undefined: parseTracks`; GREEN: `TestParseTracks*`, `TestToEventMapsTracks`, `TestToEventTracksNeverNil` pass. Deviation: regenerating `api.gen.go` (it was stale against the spec) forced `UpdateExportJob` onto the strict handler, and the manual chi route was removed. Same logic; errors map through the router's strict error handler. sqlc `models.go` drift was reverted (unrelated).
- SQL verified against the live DB: 148/812 events from the last 2h carry tracks (ingest started at deploy). EXPLAIN: PK index scan, 31 ms for 300 events.
- T3: `objectTracks.ts` (`trailUntil`, `positionAt`, `zoneEntries`, `trackWindow`) with 14 vitest tests (RED: module missing; GREEN: 14 passed). Synthetic trajectories removed; real dots, trail, current marker, zone-entry pills, and a touched-zone highlight driven by the interpolated position. No render test for the overlay (typecheck and helper tests only).
- Checks: `go build`, `go vet`, `go test ./internal/... ./migrations/...` ok; typecheck clean; vitest 942 passed, 2 failed in `Exports.test.tsx`, which fail the same way at HEAD (pre-existing); lint shows no new issues in touched files.

- RDD: user declined review for T2+T3 (`5683e09..c175f22`) and for their uncommitted maps WIP (twice more). Off-path medium: writer self-verification plus parent spot check done.
- User test 1: no line visible. Cause: the export tested was from before ingest (10:49 UTC), plus a possibly stale PWA bundle. Created export `5e67d0d7` (Escuela 21:24:40-21:28:05 UTC): manifest carries 452 tracks; user confirmed the trajectory renders.
- User test 2: the translucent box covered the video. Fix `e4965f6`: no backdrop blur or glow, outlined active zone, chip capped to the latest 3 entries.
- Bug (manifest): stale open events (Frigate reviews never closed, oldest from 2026-09-29) matched every export window. Fix `fee5f6e`: open events count only when they started within 1h of the window (matches the syncer's stale-review limit); query/scan errors are logged instead of dropped; empty index serializes as `[]`. SQL RED/GREEN on the live DB (stale event included → excluded). Verified with new export `9c39d37c`: only the real event remains.
- Investigated "empty manifest" (morning export `8c850ae2`): not a bug. The job was finalized at 11:19, before manifest events existed (`e993006`, 14:25).

## Next step
Pending: user confirms overlay look; integration tests (user-run); PATCH export (rename/protect) smoke test; optional history backfill; optional overlay render test; PR chain strategy.
