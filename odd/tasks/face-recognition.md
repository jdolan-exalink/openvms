# Face recognition (enterprise layer on Frigate)

## Objective
Make face recognition work end-to-end across federated Frigate servers, with a central per-tenant person registry, a dedicated Faces page, managed Frigate configuration, privacy controls like LPR, and known-person watchlist alarms.

## Problem and why
Today OpenVMS only detects whether a Frigate has `face_recognition.enabled` (badge in Servers). There is no way to enroll faces (no proxy to Frigate's `/faces` API), so libraries stay empty and nothing is recognized; recognized names only surface as raw `sub_labels` in events, with no per-person view, no score, no alarms (rules cannot match a person), and names are visible to anyone with `events.view`. Frigate's face recognition is off by default and needs a restart to enable. PRD audit (`docs/PRD_AUDIT.md`) lists face recognition as the first epic; the user prioritized it (2026-10-17).

## Frigate facts (sources: docs.frigate.video/configuration/face_recognition, docs.frigate.video/integrations/mqtt, github.com/blakeblackshear/frigate frigate/api/classification.py)
- Enabled under `face_recognition:` (global; per-camera `enabled`, `min_area`); keys `model_size` small|large, `detection_threshold`, `unknown_score`, `recognition_threshold`, `min_faces`, `save_attempts`, `blur_confidence_filter`, `device`. Restart required. Runs only on `person` detections, on the detect stream.
- A recognized name becomes the event `sub_label` (`["Name", score]`); MQTT `tracked_object_update` publishes every attempt (`type: face`, `name|null`, `score`).
- Library API: `GET /faces`, `POST /faces/{name}/create`, `POST /faces/{name}/register` (multipart), `POST /faces/{name}/delete` `{ids}`, `PUT /faces/{old}/rename`, `POST /faces/{name}/reclassify`, `POST /faces/reprocess`, `POST /faces/train/{name}/classify` (`training_file` or `event_id`), `POST /faces/recognize`. Admin auth on mutating routes. Per-version availability (0.17 vs 0.18) to be verified against the mock/real servers.

## Product decisions (user, 2026-10-17)
1. **Central registry**: OpenVMS stores persons and enrollment images per tenant and syncs them to every face-enabled Frigate in scope (drift detection, deletion propagation).
2. **Watchlists v1 = known persons only** ("person X seen", "anyone in list Y seen") via rules on ingested `sub_labels`; unknown-face alarms (MQTT ingestion) deferred.
3. **Strict privacy like LPR**: `faces.view` (see names/recognitions) and `faces.manage` (enroll/edit); face names redacted in events/alarms without `faces.view`; all access and changes audited; deleting a person removes central images, removes the face from every Frigate, and scrubs the name from stored event `sub_labels`.
4. **Managed config**: enable/tune face recognition per server and camera from OpenVMS (model size, thresholds) through the existing Frigate config editor, with restart-required notice.

## Scope / tasks
- [x] FACE-01 — Foundation: permissions (`faces.view`, `faces.manage`) + role grants migration, persons/images schema (RLS), image storage (existing central object storage), audit actions, Frigate adapter face-library methods + mock support. Route: delegated direct. Commits: `8361d3f` (schema, permissions, service), `247f741` (Frigate adapter + mock).
- [ ] FACE-02 — Registry sync: worker pushes persons/images to face-enabled servers (create/register/rename/delete), drift detection, deletion propagation + `sub_label` scrub, idempotent retries (reuse camera-zone sync patterns). Route: delegated direct.
- [ ] FACE-03 — API + Faces page: persons CRUD, enrollment upload, train from recent detections (Frigate train list / `event_id` classify), per-person recognitions timeline from events, name redaction without `faces.view`. Route: delegated direct.
- [ ] FACE-04 — Managed configuration: per server/camera enable, model size, thresholds via config editor; capability per camera; restart notice. Route: delegated direct.
- [ ] FACE-05 — Watchlists: rule conditions on persons/watchlists → alarms (known persons). Route: delegated direct.
- [ ] FACE-06 — Review per work unit, chained PRs (`stacked-to-main`), CI, merge, deploy (with user OK).

## Constraints
- Strict TDD (user global config). Runners: `GOCACHE=/tmp/openvms-go-build-cache go test -race ./...`, `go test -tags integration ./internal/...`, `cd apps/web && pnpm exec vitest run <file>`, `pnpm typecheck`, `golangci-lint run ./...`. OpenAPI first + `make generate`; forward-only migrations with RLS.
- Biometric data: never log images or embeddings; images only via authorized media endpoints.
- Delivery: forecast well above 400 authored lines → chained PRs, cached `stacked-to-main`.

## Progress
- 2026-10-17: branch `feat/face-recognition` from `origin/main` (`a578c6b`); research and decisions recorded.

### FACE-01 evidence (2026-10-17)
- Verified Frigate API (source `frigate/api/classification.py`, tag v0.17.0): `GET /faces` returns `{name: [files]}` and lists a `train` key for unclassified attempts; no admin or enabled check. All other `/faces` routes answer 400 `{"message":"Face recognition is not enabled.","success":false}` when disabled. `PUT /faces/{old}/rename` body `{new_name}` EXISTS in 0.17 (so no 0.17-only gap; adapter still maps 404/405 to `ErrFaceRenameUnsupported`). `POST /faces/{name}/create` reports `success:false` even on success (Frigate quirk; adapter relies on the HTTP status). `POST /faces/train/{name}/classify` has no admin dependency; accepts `training_file` or `event_id`. Register/delete/create/rename need Frigate admin credentials. Not verified against 0.18 source or a live server.
- Schema `migrations/00056_face_persons.sql`: `face_persons` (soft delete via `deleted_at`, `frigate_name` unique per tenant including tombstones, `display_name` unique per tenant case-insensitively among live rows) and `face_person_images` (FK `(person_id, tenant_id)` cascade, unique `(person_id, sha256)`); RLS forced on both. Image bytes in the central object store (`internal/platform/objectstore`, key `tenant/{tid}/faces/{person}/{image}`), same as branding logos.
- Permissions `faces.view` / `faces.manage` (tenant scope). Seeding mirrors platform/tenant grants: `tenant.manage` allow/deny -> `faces.manage`; `lpr.view` allow/deny and `tenant.manage` allow -> `faces.view`. Camera/site-scoped LPR grants do not carry over.
- TDD RED: `go test ./internal/authz` failed (undefined `FacesView`/`FacesManage`); `internal/faces` tests failed to compile (undefined `validateImage`, `NormalizeFrigateName`, `Service`); schema/RLS/grant tests ran against a tree without the migration; `internal/frigate` faces tests failed to compile (unknown `Server.FaceRecognition`, `ListFaces`...). GREEN after implementation.
- Checks (all pass): `go build ./... && go vet ./...`; `golangci-lint run ./...` 0 issues; `go test -race -count=1 ./...` ok; `go test -count=1 -tags integration ./internal/faces/... ./internal/frigate/... ./internal/authz/... ./migrations/...` ok; also `./internal/bootstrap/... ./internal/inventory/... ./internal/frigatemock/...` integration ok; `git diff --check` clean.
- Rollback boundary: revert commits `247f741` and `8361d3f`. Migration 00056 Down drops both tables and deletes all `faces.*` grants (including hand-made ones) but not object-store blobs; treat as forward-only once persons exist in production.
- Audit: actions `FACE_PERSON_CREATED/UPDATED/DELETED`, `FACE_IMAGE_ADDED/REMOVED/READ`; details hold ids/sha256/size only, never names, notes or bytes.
- No HTTP/OpenAPI yet: handlers + OpenAPI belong to FACE-03.
- Route: delegated direct (single bounded writer).

## Next step
FACE-02.
