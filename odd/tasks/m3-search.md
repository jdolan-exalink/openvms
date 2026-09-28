# M3 — Global search and LPR

## Objective
Complete milestone M3 (PRD §43-48, §82): global search of events and license plates over the central index, with every minimum filter, index-backed plate search, and permission-safe results.

## Problem and rationale
Core M3 plumbing already exists (central-index search, plate partial/exact search, keyset pagination, permission scoping). Gaps remain against PRD §44 minimum filters, §46 indexes, and test coverage. The first real target is the user's home Frigate 0.18 (`http://10.1.1.252:5000`, auth none, 10 cameras, LPR capability on).

## Scope and constraints
- In scope: tasks below.
- Out of scope (not in PRD for M3): saved searches, plate watchlists/alerts, table partitioning and index-backed partial plate search (§46/§48, deferred until real volume), result export (M6 already covers per-event export).
- Contract first: every API change starts in `packages/api-contract/openapi.yaml`, then `make generate`.
- Never leak events or plates of denied cameras or other tenants.
- Do not push or open PRs (no remote configured).

## Tasks
- [x] M3-1: Add explicit cross-tenant negative tests for `/events` and `/lpr/reads` (two tenants, zero leakage). Route: delegated direct.
- [x] M3-2: Plate filtering on `/events`: correctness test added; index-backed search deferred (user decision 2026-09-27, see evidence). Route: delegated direct.
- [x] M3-3: Add `zone` and `sub_label` filters end-to-end (OpenAPI → service SQL → Events UI) plus GIN indexes on `zones`/`sub_labels` (PRD §46). Route: delegated direct.
- [x] M3-3b: Harden zone/sub_label filters from review findings on commit `195f76a`. Route: delegated direct.
- [x] M3-4: Add `camera_group` filter to `/events` and `/lpr/reads`. Route: delegated direct.
- [x] M3-3c: Key sub_label LPR gating on camera LPR capability + review test fixes. Route: delegated direct.
- [ ] M3-5: Add `has_snapshot` / `has_preview` event flags and filters (PRD §44). BLOCKED, see evidence — needs a product decision. Route: delegated direct.
- [ ] M3-6: Web tests for Events and Plates routes (filters, infinite scroll, `lpr.search` gating). Route: delegated direct.
- [ ] M3-7: Verify LPR ingestion against the real Frigate 0.18; first sync showed events but no plates. Diagnose adapter mapping if plates exist upstream. Route: delegated direct.

## Verification mode
- Strict TDD: enabled (source: global user config `Strict TDD Mode: enabled`). RED → GREEN → REFACTOR with observed evidence.
- Runners: `go test ./...`; `go test -tags integration ./...` (needs Docker); `pnpm --filter web test`; `pnpm typecheck`; `make lint`; `make generate` + clean `git diff` of generated code.

## Acceptance criteria
- All PRD §44 minimum filters available on search, permission-scoped.
- Plate partial search returns correct, permission-scoped results. Index usage deferred (see M3-2 evidence).
- Cross-tenant and denied-camera tests pass for events and plates.
- All runners above pass.

## Delivery
- Branch `feat/m3-search` from baseline `a4443d1`. One work-unit commit per task. Forecast ~1,200-1,800 authored lines; no remote, so PR slicing is deferred until a remote exists.

## Progress and evidence
- Baseline commit `a4443d1` on `main`; CodeGraph initialized.
- M3-1: added `internal/events/cross_tenant_search_integration_test.go` (`TestCrossTenantSearchIsolation`: sanity, operator, full-access admin, raw-SQL RLS). No leak found. `go test ./...` PASS; `go test -tags integration ./internal/events/...` PASS (writer + parent re-run). Test-only task: behavior already held, so no RED phase applies; the sanity subtest proves assertions are non-vacuous.

- M3-2: Writer tried a trigger-maintained `events.plates_search` column + `gin_trgm_ops` index. Finding: with `FORCE ROW LEVEL SECURITY` on `events` and `lpr_reads`, Postgres never uses an index for a non-leakproof qual (`textlike` has `proleakproof = f`); `EXPLAIN` with `enable_seqscan=off` still chose Seq Scan. The same applies to the pre-existing `lpr_reads_plate_trgm_idx`. Fixes considered: `ALTER FUNCTION textlike LEAKPROOF` (works, but global, needs superuser, not preserved across major upgrades) and a custom leakproof opclass (crashed Postgres). User chose to defer: migration and query change reverted; kept `internal/events/plate_filter_integration_test.go` (`TestPlateFilterCorrectness`: partial match, nonexistent plate, denied camera hidden) — PASS (parent run). Queries remain narrowed by tenant/time indexes before the plate filter. Test-only outcome, so no RED applies to the kept test.

- Pending item (user-authorized): `apps/web/src/routes/Playback.tsx:32` `react-hooks/purity` fixed by moving `now` into state refreshed every 30 s. RED: `make lint` failing on that finding (baseline). GREEN: `make lint` exit 0, `pnpm typecheck` OK, web tests 3/3.

- M3-3: OpenAPI `zone`/`sub_label` query params (array, form/explode) and `internal/events/service.go` Filter.Zones/SubLabels + `e.zones && ?` / `e.sub_labels && ?` SQL were already in the working tree; wired the last two ends. Handler: `internal/api/events_handlers.go` maps `p.Zone`/`p.SubLabel` into the Filter, mirroring `p.Label` (no pre-existing handler-level filter-mapping test in the package, so none added — coverage stays at the service/integration level). UI: `apps/web/src/routes/Events.tsx` adds "Zona" and "Sub-etiqueta" free-text fields (no fixed catalog exists for these values, same free-text approach as the plate field); `apps/web/src/api/queries.ts` `EventFilter` gained `zone`/`sub_label`.
  - Test-only backend behavior (service.go filters were already implemented): ran the pre-written, never-run `internal/events/zone_sub_label_filter_integration_test.go` (`TestZoneAndSubLabelFilterCorrectness`) directly to GREEN — `go test -tags integration ./internal/events/... ./internal/api/...` PASS (same "test-only, no RED phase" situation as M3-1/M3-2, see method note there).
  - Web UI is new behavior, so TDD applied: RED — `pnpm vitest run src/routes/Events.test.tsx` failed with `Unable to find a label with the text of: Zona` (fields did not exist yet). GREEN — after adding the two `Field`/`TextInput` pairs and the `toFilter` mapping, same command passed, asserting `zone=entrada` and `sub_label=placa_reconocida` land in the request's query string.
  - Migration `migrations/00006_events_zone_sub_label_indexes.sql`: `CREATE INDEX ... USING gin (zones)` / `(sub_labels)`, with Down. EXPLAIN finding (same root cause as M3-2's plate index): with `FORCE ROW LEVEL SECURITY` on `events`, `EXPLAIN (ANALYZE, BUFFERS)` with `enable_seqscan=off` still chose Seq Scan for `zones && '{entrada}'` (cost carries the `enable_seqscan=off` disable-penalty, confirming no index alternative was considered) — `SELECT ... FROM pg_operator o JOIN pg_proc p ON p.oid=o.oprcode WHERE o.oprname='&&'` shows the backing function `arrayoverlap` has `proleakproof = false`, so Postgres refuses to push the qual through the RLS barrier into the GIN index, exactly like `textlike` in M3-2. Verified with a throwaway integration test (deleted after the finding was recorded here, not part of the deliverable). Decision: kept the indexes per PRD §46 and the task's explicit instruction — a GIN index is write overhead only, not harmful, and both indexes become usable if `events` ever drops FORCE RLS or gains a leakproof wrapper, so they are not "provably useless AND harmful."
  - Full verification: `go test ./...` PASS; `go test -tags integration ./internal/events/... ./internal/api/...` PASS (needs Docker, used testcontainers); `pnpm --filter web test` PASS (3 files / 4 tests); `pnpm typecheck` clean; `make lint` clean (`go vet` 0 issues, `eslint .` clean); `make generate` re-run twice, byte-identical output both times (sha256 match) — the residual `git diff` against HEAD before commit is expected (this task's own uncommitted contract change), and resolves once committed.
  - Commit: `195f76a` (`feat(events): filter search by zone and sub_label`, not pushed — no remote configured).

- M3-3b: Review of commit `195f76a` found `sub_label` search was gated only by `events.search`, not `lpr.search` — since `sub_label` carries recognized plate text on LPR cameras (`internal/frigatemock/generator.go` `enrich`), an actor with `events.search` but no `lpr.search`/`lpr.view` could probe exact plates through it. Fix in `internal/events/service.go`:
  - `ListEvents`: the `SubLabels` filter now additionally intersects with `CameraIDs(ctx, authz.LPRSearch)`, mirroring the existing `Plate` filter restriction.
  - `ListEvents` response and `getEvent`: `SubLabels` is now redacted (`[]string{}`) for any camera where the actor lacks `lpr.view`, mirroring how `Plates` is already redacted. Decision (sub_label semantics were flagged as possibly ambiguous — resolved, not stopped): `sub_label` is a generic Frigate field that could in principle carry non-plate data (e.g. face-recognition names) on a non-LPR camera, but this codebase has no such consumer today (`frigatemock.enrich` only ever writes plate text into `sub_label`, and only on LPR-tagged cameras) and the redaction/search gate is keyed on the actor's `lpr.view`/`lpr.search` permission on that camera, not on a camera "is LPR" flag — same mechanism `Plates` already uses. This is the conservative choice per the hard project rule (never leak plates to actors lacking LPR permission); documented here rather than guessed silently.
  - `packages/api-contract/openapi.yaml`: updated `listEvents` description and the `sub_labels` field description to state the lpr.view/lpr.search gating; `make generate` re-run (byte-identical on a second run, diff limited to the embedded spec blob and one added field comment in `internal/api/gen/api.gen.go`).
  - RED (before fix): `go test -tags integration ./internal/events/... -run TestZoneAndSubLabelFilterCorrectness -v` — failed on 2 new subtests: `sub_label_filter_requires_lpr.search,_not_just_events.search` (actor with only `events.search` got 1 event back for a probed sub_label, want 0) and `sub_labels_are_redacted_from_the_response_without_lpr.view` (event exposed unredacted sub_label to an actor without `lpr.view`). GREEN after the fix: same command, all 8 subtests PASS.
  - `internal/events/zone_sub_label_filter_integration_test.go`: per review item 2, added `events.view_alone_cannot_use_the_zone_or_sub_label_filters` (actor with only `events.view` on a camera gets 0 results filtering by zone or sub_label — proves the existing `service.go` permission escalation at the search-vs-view perm selection; test-only, behavior already correct, no RED phase). Per review item 3: the "denied camera" subtest's zone/sub_label assertions on camB now check no returned event has `CameraID == camB.ID` (previously asserted `len(items) == 0`, which conflated "denied camera never leaks" with "this actor happens to have zero permissions to search at all"); the own-sub_label check now also asserts `CameraID == camA.ID` on every returned event, mirroring the pre-existing own-zone check. That subtest's actor grant was extended from `events.search`-only to `events.search + lpr.search` on camA, since after the fix `events.search` alone can no longer produce an own-sub_label match (a new dedicated subtest covers the `events.search`-only case going to zero).
  - `apps/web/src/routes/Events.test.tsx`: fixed a race per review item 4 — after submitting the filtered search, the test was asserting on `lastEventsUrl` right after `findByText("No hay eventos que coincidan.")`, but that same text is shown by both the initial and the filtered fetch, so the assertion could observe the initial fetch's URL. Wrapped the URL assertions in `waitFor`.
  - Full verification: `go test ./...` PASS; `go test -tags integration ./internal/...` PASS (Docker/testcontainers); `pnpm --filter web test` PASS (3 files / 4 tests); `pnpm typecheck` clean; `make lint` clean; `make generate` re-run, clean `git diff` after commit.
  - Commit: `373a580` (`fix(events): gate sub_label search and visibility behind LPR permissions`, not pushed — no remote configured).

- M3-4: `camera_group` filter on `/events` and `/lpr/reads` (PRD §44). Camera groups already existed (`camera_groups`/`camera_group_members`, migration `00002`, `internal/inventory` `CreateCameraGroup`/`ListCameraGroups`, `GET /api/v1/camera-groups`); this task only added the search-side filter.
  - Contract: `packages/api-contract/openapi.yaml` adds `camera_group_id` (array, form, explode) to `listEvents` and `listPlateReads`, matching the existing `camera_id` param style; `make generate` re-run (byte-identical on a second run).
  - `internal/events/service.go`: `Filter.CameraGroupIDs` / `PlateFilter.CameraGroupIDs`; SQL adds `EXISTS (SELECT 1 FROM camera_group_members m WHERE m.camera_id = e.camera_id AND m.group_id = ANY(?))` (and the `l.camera_id` equivalent for plates), appended *after* the existing `e.camera_id = ANY(cams)` / `l.camera_id = ANY(cams)` permission-scoping clause, so the group filter only narrows an already-permitted camera set and can never widen access. `camera_group_members` carries its own tenant RLS, so a cross-tenant group id matches zero membership rows and yields zero results without any extra tenant check. No permission escalation added for this filter (mirrors `camera_id`/`site_id`, not `zone`/`sub_label`/`plate`): grouping is scoping, not a search capability.
  - `internal/api/events_handlers.go`: `ListEvents` and `ListPlateReads` map `p.CameraGroupId` into the new filter field.
  - RED: `go test -tags integration ./internal/events/... -run TestCameraGroupFilterCorrectness -v` with the two new SQL clauses temporarily disabled (`if false && len(...)`) — 6 of 7 subtests failed as expected (group filter was a no-op, so cross-camera/cross-tenant events leaked through); the 7th subtest (denied-camera-inside-group) passed even disabled because it is also protected by the pre-existing base permission scoping, which is expected and noted in the test. GREEN: same command with the real clauses restored, all 7 subtests PASS. New file `internal/events/camera_group_filter_integration_test.go`.
  - Web: `apps/web/src/api/queries.ts` `EventFilter`/`PlateFilter` gain `camera_group_id`; `apps/web/src/routes/Events.tsx` and `apps/web/src/routes/Plates.tsx` add a "Grupo de cámaras" `Select` sourced from the existing `cameraGroupsQuery` (`GET /api/v1/camera-groups`, already used elsewhere in the app). RED: `pnpm --filter web exec vitest run src/routes/Events.test.tsx` / `Plates.test.tsx` failed on `findByLabelText("Grupo de cámaras")` with the two `Field`/`Select` blocks temporarily removed. GREEN: same commands pass after restoring them. New file `apps/web/src/routes/Plates.test.tsx` (Plates had no test file yet; full route coverage is M3-6's job, this only covers the new param).
  - Full verification: `go test ./...` PASS; `go test -tags integration ./internal/...` PASS (Docker/testcontainers); `pnpm --filter web test` PASS (4 files / 6 tests); `pnpm typecheck` clean; `make lint` clean; `make generate` re-run, clean after commit.
  - Commit: `7039ec7` (`feat(search): filter events and plate reads by camera group`, not pushed — no remote configured).

- M3-3c: User decision (2026-09-28): sub_label LPR gating (introduced in M3-3b) applies only to
  cameras with LPR capability. On a non-LPR camera, sub_label is an ordinary event field (e.g. a
  Frigate face-recognition name) and follows normal `events.view`/`events.search` only; on an
  LPR-capable camera the existing gating from commit `e59dc1f` is unchanged (filter requires
  `lpr.search`, visibility requires `lpr.view`).
  - `internal/events/service.go`: `Event` gained an unexported `CameraLPR bool` (from `cameras.lpr`,
    already joined via `eventJoins`/`eventColumns`; not exposed through `toEvent`/the API). The
    `SubLabels` filter clause changed from `e.camera_id = ANY(lprSearchCams)` to
    `(NOT c.lpr OR e.camera_id = ANY(lprSearchCams))` — an LPR camera still needs `lpr.search`, a
    non-LPR camera only needs the baseline `events.search` scoping already applied via `cams`.
    `ListEvents`'s redaction loop and `getEvent` now only blank `SubLabels` when
    `!lpr.view && cam.LPR`; `Plates` redaction is unchanged (still keyed on `lpr.view` alone).
    `cameras.lpr` is a `NOT NULL DEFAULT false` column (migration `00002`) populated by inventory
    sync (`internal/store/queries/inventory.sql` upsert), so there is no literal "unknown" camera
    to fail open on in this query path; the "treat unknown as LPR" instruction has no code path to
    attach to today given that schema guarantee.
  - RED: `go test -tags integration ./internal/events/... -run TestZoneAndSubLabelFilterCorrectness -v`
    — new subtest `sub_label_on_a_non-LPR_camera_follows_normal_events_permissions,_not_LPR` failed
    (`could not filter by sub_label "face-juan-perez"` on non-LPR camera `plaza`) before the fix.
    GREEN: same command, all 9 subtests PASS. Test helper `setSubLabelOnOneEvent` directly sets
    `sub_labels` on one event of a non-LPR camera via raw SQL (`store.AllTenants` scope), since
    `frigatemock`'s `enrich` only ever writes sub_label text on LPR-tagged cameras.
  - Review test fix (a): `internal/events/camera_group_filter_integration_test.go` — the two
    cross-tenant `camera_group_id` subtests (events and plates) were vacuous (the foreign-tenant
    group had no members, so "denied" and "empty" were indistinguishable). Added
    `addForeignGroupMember` (raw insert into `camera_group_members`, `store.AllTenants` scope,
    bypassing `CreateCameraGroup`'s same-tenant validation) to give the foreign group a real
    membership row pointing at `camA` (a camera a newly scoped tenant-A actor can see), then assert
    the scoped actor still gets zero results — proving `camera_group_members`' own tenant RLS, not
    an empty membership set, is what blocks the leak. Test-only, no RED phase (the underlying
    isolation was already correct; confirmed by these tests passing unmodified after being added).
  - Review test fix (b): `apps/web/src/routes/Events.test.tsx` and `Plates.test.tsx` — the
    camera-group filter tests awaited the `<select>` element (`findByLabelText`) but not the
    "Perimeter" `<option>` inside it (populated async from `/api/v1/camera-groups`), so
    `fireEvent.change` could race the option's render. Both now `await screen.findByRole("option",
    { name: "Perimeter" })` before firing the change event.
  - OpenAPI: `listEvents` description and the `Event.sub_labels` field description updated to state
    the LPR-capability-gated semantics; `make generate` re-run twice, byte-identical diff both times.
  - Full verification: `go test ./...` PASS; `go test -tags integration ./internal/...` PASS
    (Docker/testcontainers); `pnpm --filter web test` PASS 3/3 runs (4 files / 6 tests each); `pnpm
    typecheck` clean; `make lint` clean; `make generate` clean after commit.
  - Commit: `b944930` (`fix(events): gate sub_label by LPR only on LPR-capable cameras`, not
    pushed — no remote configured).

- M3-5: STOPPED (real technical/product ambiguity, no code changed) — verified against Frigate's
  own source (`frigate/models.py`, fetched from GitHub) rather than guessed:
  - `has_snapshot` and `has_clip` are real per-tracked-object booleans, but they live on Frigate's
    `Event` model (`GET /api/events`, this codebase's `TrackedObject`/`objectResponse`), **not** on
    `ReviewSegment` (`GET /api/review`, this codebase's `Review`) — `ReviewSegment` has no
    snapshot/clip/preview field at all. This app's central `events` table is built from review
    items, one row aggregating possibly several detections (`detection_ids`); `TrackedObjects`
    sync (`internal/events/syncer.go` `syncObjects`) currently only runs `if cameraHasLPR(cams)`,
    as a plate-recognition optimization, not for every camera. Wiring a correct `has_snapshot`
    would mean either (a) always syncing `TrackedObjects` for every camera regardless of LPR (an
    unscoped increase in Frigate API load this task was not asked to make), or (b) approximating
    it from the review's own `thumb_path` (which is not the same fact Frigate reports — a review
    can have a thumb_path string that 404s, and `has_snapshot` is specifically about the object's
    own saved snapshot, independent of the review thumbnail).
  - `has_preview` has **no backing field anywhere** in Frigate's data model (confirmed absent from
    both `Event` and `ReviewSegment`). Frigate's actual "preview" feature is a separate per-camera
    timelapse asset over a time range (`/api/preview/<camera>/start/<s>/end/<e>/...`), gated by
    config and retention, not a stored per-event/per-review boolean. Filtering by it would require
    either probing that endpoint per event at query time (expensive, and its own availability
    window is unrelated to the review's row lifetime) or fabricating the value from the
    server-wide `Capabilities.Preview` flag, which would make the filter trivially true/false for
    every event on a given server and not a real per-event fact.
  - This needs a product decision before writing code that would otherwise encode a guessed,
    Frigate-ungrounded mapping: options include (1) scope `has_snapshot` to LPR cameras only
    (matching where `TrackedObjects` already syncs) and drop `has_preview` from this milestone, (2)
    accept the broader `TrackedObjects`-for-every-camera sync cost to get a correct `has_snapshot`
    everywhere, or (3) redefine "has_preview" in this app's own terms (e.g. "the central preview
    key is populated", mirroring `thumbnail_key`/`preview_key` in PRD §20) rather than a literal
    Frigate signal. Continuing to M3-6 in the meantime (independent task).

## Next step
M3-5 (blocked on a product decision, see evidence above) or M3-7.
