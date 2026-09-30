# Frigate Camera Config Editor

Locator: `odd/tasks/frigate-config-editor.md` · Engram mirror: `odd/frigate-config-editor/tasks`
Branch: `feat/frigate-config-editor` (from `feat/live-view-sessions` @bd90c6f)

## Objective
Edit every per-camera parameter Frigate supports from OpenVMS and apply it to Frigate safely
(validation, diff, backup/rollback, audit), replacing the 3-field S2-7 drawer.

## Findings (exploration 2026-09-30)
- S2-7 edits only `detect.enabled`, `lpr.enabled`, `objects.track` via `PUT /api/config/set` dotted
  query params (`internal/frigate/v017.go:312-335`). **Bug**: `requires_restart` is never sent (Frigate
  default 1) → changes are saved to config.yml but NOT applied live, while the UI implies they are.
- No backup/rollback exists (only a `SERVER_CONFIG_UPDATED` audit row with before/after of 3 fields).
- Adapters: <0.14 rejected; 0.14–0.17 → v017; ≥0.18 → v018 (embeds v017). Mock implements only a tiny
  config subset (`internal/frigatemock/server.go:117-163`).
- Frigate API: `GET /api/config` (secrets redacted), `GET /api/config/raw` (admin, YAML),
  `GET /api/config/raw_paths` (admin, unmasked ffmpeg paths), `GET /api/config/schema.json` (public,
  pydantic schema — shape to be verified by a spike), `POST /api/config/save?save_option=saveonly|restart`
  (YAML body, validates, 400 on error, no backup), `PUT /api/config/set` (JSON `config_data`,
  `requires_restart`, `update_topic=config/cameras/<cam>/<section>` for live apply; file lock, validates,
  reverts on error; restores masked-secret sentinels).
- Live-updatable camera sections: enabled, ffmpeg, detect, objects, motion, record, snapshots, zones,
  review, audio, audio_transcription, birdseye, lpr, face_recognition, semantic_search, onvif,
  autotracking, live, ui, timestamp_style, mqtt, notifications, genai. Restart (assumed): type,
  webui_url, best_image_timeout, profiles (0.18).
- Zones/masks: relative 0–1 coordinates "x1,y1,..."; 0.18 migrates masks to dicts {id, friendly_name,
  enabled, coordinates}.

## PO decisions (2026-09-30)
1. Supported versions: **Frigate ≥ 0.16** editable; older servers read-only (or legacy 3 fields).
2. Apply **live where Frigate allows**; if any field needs restart, save and show a banner with
   "Reiniciar servidor" so the operator chooses the moment.
3. **Advanced YAML editor** for admins, with Frigate validation, diff and a stored revision for rollback.
4. **Separate credentials permission**: `servers.config` for all sections; a new permission (e.g.
   `servers.config.secrets`) to view/edit stream URLs/credentials (ffmpeg paths, ONVIF user/password)
   and to use the full YAML editor.

## Constraints
- TDD: off by PO preference for recent work ("sin test"); **exception**: the backend write path
  (patch/save/backup/rollback/permissions) gets integration tests because it mutates production NVR
  config. Ordinary checks always (build, vet, lint, typecheck, existing suites).
- Never write redacted secrets back; never expose secrets without the new permission.
- Conventional Commits, no AI attribution. ~400 authored lines per task (advisory).

## Tasks
- [x] FC-1 — (done, commits 40e5f64 adapter+mock, 49b7af6 service/migration/permission/endpoints, 781c94c integration tests) Backend foundation: adapter methods (Schema, RawConfig, RawPaths, ApplyCameraPatch with Review: medium risk, declined by PO (candidate-scoped).
  requires_restart/update_topic, SaveRaw), version gate ≥0.16, fix live-apply bug, revisions table +
  rollback, new secrets permission, endpoints, mock extensions, integration tests.
- [x] FC-2 — (done, commit 6a52f9e) Web: schema-driven per-camera editor (section tabs, generated fields with descriptions/
  enums/defaults), diff preview, live/restart banner, replaces CameraSettingsDrawer.
- [x] FC-3 — (done, commit 4042e3e) Curated panels for the most used sections (ffmpeg inputs/roles/hwaccel, detect, record/
  snapshots retention, objects+filters, motion, review) + revision history with rollback UI.
- [ ] FC-4 — Visual zone & mask editor over the camera snapshot (relative coords; 0.18 dict format).
- [ ] FC-5 — Advanced YAML editor (validation via Frigate, diff, revision), ONVIF/autotracking/LPR/face/
  GenAI panels, bulk apply to several cameras.

## Delivery
Strategy ask-on-risk; push/PR are PO decisions.

## Progress
### FC-1 (2026-09-30) — Route: delegated (writer trigger: 2+ non-trivial files)
- Endpoints: `GET|PATCH /api/v1/cameras/{id}/frigate-config`, `GET /api/v1/servers/{id}/frigate-config/schema`,
  `GET|PUT /api/v1/servers/{id}/frigate-config/raw` (`?restart=`), `GET /api/v1/servers/{id}/frigate-config/revisions`
  (`camera_id`, `before`, `limit`, `include_yaml`), `POST .../revisions/{rev}/rollback`. Old `/cameras/{id}/config`
  kept; on editable servers it now runs through the same patch path (live + revision) and still writes SERVER_CONFIG_UPDATED.
- Permission `servers.config.secrets` (server scope, granted to platform admin via bootstrap catalog loop). Needed for
  ffmpeg `inputs` and ONVIF `user`/`password` edits, raw GET/PUT and rollback; revisions hide YAML and mask credentials in
  patches without it. Migration `00021_frigate_config_revisions.sql` (RLS tenant_isolation).
- Audit: FRIGATE_CONFIG_PATCHED / RAW_SAVED / ROLLED_BACK with revision_id and sections.
- Frigate API facts VERIFIED from source (raw.githubusercontent, v0.16.0 and v0.17.0):
  - 0.16.0: `AppConfigSetBody` has only `requires_restart`; config/set takes dotted query params; requires_restart=0 only
    swaps the in-memory config, nothing is published, no update_topic. So on 0.16 every change is flagged restart-required
    (query-param fallback; lists of objects such as ffmpeg.inputs are rejected with "needs 0.17").
  - 0.17.0: body `{requires_restart, update_topic, config_data}`; query params take precedence over body; update_topic
    `config/cameras/<cam>/<field>` where field must be in CameraConfigUpdateEnum (add, audio, audio_transcription, birdseye,
    detect, enabled, motion, notifications, objects, object_genai, record, remove, review, review_genai, semantic_search,
    snapshots, zones). Live sections used: that set minus add/remove/genai. NOT live (correcting the earlier assumption):
    ffmpeg, onvif, lpr, face_recognition, live, ui, timestamp_style, mqtt, genai — these are saved and flagged restart.
  - `POST /config/save?save_option=saveonly|restart` takes text/plain, `GET /config/raw_paths` is admin-only unmasked paths.
- ASSUMED (not verified against a real Frigate): redaction sentinel shapes (`://*:*@`, bare asterisks for ONVIF
  user/password), raw `GET /config/raw` body is plain YAML (JSON-string form tolerated), schema.json has `$defs.CameraConfig`
  (falls back to the full schema otherwise), 400 body `{"message": ...}` from save/set.
- Non-atomic: multiple sections are sent one request each; if a later one fails the earlier ones stay applied and recorded,
  the error is returned. Revision capture fails closed when `/config/raw` cannot be read before writing.
- Checks: go build, go vet, go test ./..., go test -tags integration ./internal/frigate/ ./internal/inventory/ ./internal/api/,
  golangci-lint (0 issues), make generate, web `tsc --noEmit` — all passing.
- Next: FC-2 (web schema-driven editor).

### FC-2 + FC-3 (2026-09-30) — Route: delegated (writer trigger: 2+ non-trivial files)
- FC-2 (6a52f9e): route `/cameras/$cameraId/frigate` (`routes/FrigateCameraConfig.tsx`), in-house schema form
  (`components/frigate/SchemaForm.tsx`, pure helpers `lib/frigateSchema.ts`), section nav with live/restart badges (live set =
  verified 0.17 list, 0.16 = all restart), diff review modal, PATCH with minimal per-key patch, per-section result list,
  restart banner (sessionStorage per server) + ConfirmDialog restart. Entry points: Cameras list (icon link + drawer link,
  gated servers.config), Live Explorer camera context menu "Configurar" (servers.config). Drawer keeps VMS fields and a
  read-only Frigate summary.
- FC-3 (4042e3e): curated panels (ffmpeg, detect, record, snapshots, objects with emoji label picker + per-label filters,
  motion, review, zones placeholder button "Editar zonas" via `onEditZones` prop) above "Opciones avanzadas" (generic form);
  revisions history tab (list, patch, YAML line diff with secrets, restore with ConfirmDialog -> rollback -> restart banner).
  Reusable `lib/labelEmoji.ts` + `components/frigate/LabelPicker.tsx`.
- Checks (apps/web): tsc --noEmit, vitest run (319 passed), eslint, build — all passing. No Go touched.
- Unverified: behaviour against a real Frigate 0.16/0.17 schema.json (curated fields render only when schema/config knows the
  path); map-key removal is sent as null (merge cannot delete) and is not verified against Frigate; model-supported labels are
  not exposed by the API so the label grid uses known + configured labels plus a free "Otra etiqueta" input.
- Next: FC-4 (zones editor, plug into `onEditZones`), FC-5.
