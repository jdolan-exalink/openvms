# Platform data retention in Settings, with unnamed faces purged after 30 days

## Header
Task document: `odd/tasks/retention-settings.md` (child of `odd/tasks/faces-proactive-recognition.md`, T2).
Objective: a Settings > Retention page where the platform administrator sets how long OpenVMS keeps unnamed faces and
notifications, sees what each server keeps and how full the volumes are, and a background job that enforces it.
Branch: `feat/retention-settings` (stacked on `feat/faces-proactive-recognition`, PR #301). Delivery: three work units
(storage and purge, API, page), one PR each or stacked. Test runner: `go test ./internal/retention/... ./internal/api/...`,
integration tests under `internal/store`; `cd apps/web && npx vitest run <paths>`; `npx tsc --noEmit`.
Visual reference: the published mockup "Retención de datos OpenVMS", local copy
`/tmp/claude-0/-root-openvms/d5b0993b-df5b-4114-ade9-b4f57cf7c445/scratchpad/retention-demo/index.html`.

## Specs
- S1 "armá la retención con 30 días": unnamed (pending) faces are kept 30 days by default and then erased: crop, vector and
  queue entry. 0 days keeps them forever.
- S2 "la retención de openvms tenemos que poder configurarla en configuración ... poder variar las retenciones": a
  Retention page in Settings edits the days of each data class that OpenVMS purges today or will purge: unnamed faces,
  delivered notifications and read notifications. Audit, events, clips and exports stay "keep forever" in this version (assumption).
- S3 "Para toda la plataforma": one setting set for the whole platform, not per organization; only a platform-level
  `tenant.manage` holder can read or change it.
- S4 "tener en cuenta retenciones de los servidores": the page shows each server's recording and event retention (read-only,
  edited on the server) and how many days its disk can actually hold.
- S5 "espacio en el volumen de guardado": the page shows the host volume usage against the safety limit; an option
  brings the cleanup of unnamed faces and notifications forward when the volume passes the limit. It never touches recordings,
  evidence or exports.
- S6 (assumption) shortening a retention shows how many records and how much space would be deleted and asks for confirmation;
  every purge is audited; the job deletes in bounded batches.

## Tasks
- U1 (S1, S2, S6) Storage and purge: migration `retention_settings`, queries, `internal/retention` (settings, purge, preview), the purge job in the worker, notifications reading their days from the setting. Route: inline. Status: done (migration 00090, internal/retention, ExpireFaceSightings, notify.Worker.RetentionSource, worker wiring; integration tests green against Postgres: faces, notify). Risk: item 1 (migration, deletes stored data).
- U2 (S2, S3, S4, S5) API: `GET/PUT /api/v1/system/retention` and `GET /api/v1/system/retention/preview` (platform-level tenant.manage, audit `RETENTION_UPDATED`, volume usage, unnamed faces stored). Route: inline. Status: done (internal/retention/service.go, handlers, OpenAPI; integration tests incl. a tenant admin being refused). Per-server (Frigate) retention is not in the response: it lives in each server's Frigate YAML and stays a follow-up. Risk: item 2 (guard) and item 3 (new public API).
- U3 (S2, S4, S5, S6) Web: Settings > Retention page. Route: inline. Status: pending.

## Log
- L1 (2026-11-02, user): "dale, armá la retención con 30 días, la retención de openvms tenemos que poder configurarla en configuración así que hay que pensar eso y ahí poder variar las retenciones y obviamente tener en cuenta retenciones de los servidores y espacio en el volumen de guardado"
- L2 (2026-11-02, user): "Para toda la plataforma, dale"
- L3 (2026-11-02, mapping): there is no generic settings table (`alarm_settings` is the platform singleton pattern, no RLS); notifications are pruned by `notify.Worker.Prune` with env defaults 30/90 days; `RejectFaceSighting` clears the crop and queues `face_blob_purge` but keeps the embedding vector, so the retention purge must also null the vector; the platform gate is `Require(authz.TenantManage, Resource{Kind: ScopePlatform})`; host disk comes from `hoststat.Read` (`GET /system/capacity`) and server disk from agent metrics.
- L4 (2026-11-02, finding): the manual reject kept the embedding vector; the retention query clears the vector, its profile, model and embedded_at together because `face_sightings_embedding_complete` requires all four to be null or all set. The volume measured is the one holding `OPENVMS_DATA_DIR` (worker path `/mnt/openvms/exports`, env `RETENTION_VOLUME_PATH`), not the host root.
- L5 (2026-11-02, review of U1, approved, two non-blocking warnings): a failing batch skipped the audit of the batches already committed (fixed: `record` audits partial sweeps); pressure mode only shortens the faces class, not notifications (left as designed; the cleanable class today is faces).
