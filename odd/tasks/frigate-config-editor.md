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
- [ ] FC-1 — Backend foundation: adapter methods (Schema, RawConfig, RawPaths, ApplyCameraPatch with
  requires_restart/update_topic, SaveRaw), version gate ≥0.16, fix live-apply bug, revisions table +
  rollback, new secrets permission, endpoints, mock extensions, integration tests.
- [ ] FC-2 — Web: schema-driven per-camera editor (section tabs, generated fields with descriptions/
  enums/defaults), diff preview, live/restart banner, replaces CameraSettingsDrawer.
- [ ] FC-3 — Curated panels for the most used sections (ffmpeg inputs/roles/hwaccel, detect, record/
  snapshots retention, objects+filters, motion, review) + revision history with rollback UI.
- [ ] FC-4 — Visual zone & mask editor over the camera snapshot (relative coords; 0.18 dict format).
- [ ] FC-5 — Advanced YAML editor (validation via Frigate, diff, revision), ONVIF/autotracking/LPR/face/
  GenAI panels, bulk apply to several cameras.

## Delivery
Strategy ask-on-risk; push/PR are PO decisions.

## Progress
(none yet)
