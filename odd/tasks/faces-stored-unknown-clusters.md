# Stored unknown-face groups with server-side paging

Objective: the Unknown faces tab answers in well under a second with ~22k pending sightings, by storing the grouping and paging it on the server.
Branch: `feat/faces-stored-unknown-clusters` (from `main` at `5234241`). Delivery strategy: `ask-on-risk`.
Runner: `go test ./internal/faces/... ./internal/api/...`, `go test -tags integration ./internal/faces/...`, `pnpm --dir apps/web test -- --run <path>`, `pnpm --dir apps/web typecheck`.

## Specs
- S1 "dale, hacé la solución con grupos guardados y paginación": the groups are stored (computed in the background, not on each request) and the list endpoint pages them on the server.
- S2 Measured on 2026-11-10 with all 21,992 pending sightings embedded: `GET /api/v1/faces/review-inbox/clusters` takes 56-61 s and returns 6.9 MB (14,071 groups, 691 with more than one sighting, largest 873). After the change a page must come back in about a second or less.
- S3 (assumption) The stored value is `face_sightings.cluster_id` (nullable). A sighting with no `cluster_id` is a group of one, so a new sighting shows up at once and is merged into a group by the next background run.
- S4 (assumption) Filters (site, server), the recurrence rule (saved platform rule or the `min_appearances`/`window_days` override) and the sort are applied when reading, so the stored value does not depend on them.
- S5 (assumption) The response keeps the shape of a group and adds the page total; operator actions (assign, discard, group, rule out a pair) keep working unchanged.

## Design (assumptions)
- A1 Clusters are computed per tenant by `ClusterSightings` (same threshold and rules as today), by a worker loop. A pass is skipped when no pending sighting with an embedding lacks a `cluster_id`.
- A2 The stable group id shown to the UI is the oldest member's sighting id, as today.
- A3 A manual group or a "different" verdict changes the groups the next time a pass runs (a pass runs when new embedded faces arrive).

## Tasks
- T1 (S1, S3, A1) Migration for `face_sightings.cluster_id`, queries, `Service.RefreshUnknownClusters`, worker loop, integration test. Route: inline. Status: done (`RefreshUnknownClusters`, `runFaceClusters` once a minute; integration tests `TestStoredClusters*` pass against real Postgres).
- T2 (S1, S2, S4, S5) Read endpoint aggregated and paged in SQL: `limit`, `offset`, `sort`, `total`; contract, handler, integration test. Route: inline. Status: done (paged SQL aggregate; `limit` default 48, max 200; the recurrence rule and sort applied in SQL).
- T3 (S2, S5) Web: request the page from the server, sort on the server, keep the "show more" footer. Route: inline. Status: done (vitest and typecheck green).
- T4 Docs, PR, deploy and a timing check against the real data. Route: inline. Status: pending.

## Log
- L1 (2026-11-10, user): "dale, hacé la solución con grupos guardados y paginación"
- L2 (2026-11-10, measurement): with every embedding in place the endpoint takes 56-61 s and 6.9 MB; the grouping is recomputed on each request (O(sightings x groups x 512)).
