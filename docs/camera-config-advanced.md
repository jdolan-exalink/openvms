# Camera advanced tab (U10, backend)

PRD §32-§34 and §42. Everything here is read from, or written through, paths that already
existed: the Frigate sectioned PATCH, the normalized-config pipeline and its audit trail. The web tab
consumes these endpoints; none of them calls the camera itself.

## Contextual YAML (S16)

"Avanzado → Editar YAML de detección": edit one top-level camera section (`detect`, `objects`, `record`,
`snapshots`, `motion`, `zones`, `ffmpeg`, ...) instead of the whole `config.yml`.

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/cameras/{id}/frigate-yaml?section=detect` | YAML of the section, `managed_paths`, `revision` |
| `POST .../frigate-yaml/validate` `{section, yaml}` | syntax, schema, diff and managed conflicts; no side effects |
| `POST .../frigate-yaml/apply` `{section, yaml, expected_revision, confirm_managed_conflicts}` | save through the existing PATCH |

- **Text.** The YAML is the contents of the section (what goes under `detect:`), 2-space indent, sorted
  keys. Frigate's effective section is shown, defaults included. A scalar section (`enabled`) is one value.
- **Secrets.** Credentials are masked exactly like the `frigate-config` document unless the actor holds
  `servers.config.secrets` (`secrets_visible`). A masked placeholder (`rtsp://*:*@host/..`, `********`) left
  untouched never overwrites the real value: the apply puts the real value back where the engine can supply
  it and otherwise drops the key (Frigate merges maps, so the stored value stays). A placeholder inside a
  list that cannot be restored, or a placeholder that is not the one that was shown, is a 400. Only changed
  values are sent to Frigate, so an untouched `ffmpeg.inputs` never needs the secrets permission.
- **Validation.**
  - Syntax errors carry `line` and `column`. Line is what the YAML parser reports (it can point at the
    line before the real problem); column is the first non-blank column of that line, or exact for tabs.
  - Schema: a conservative structural check against the same Frigate camera schema the schema editor uses
    (`GET /servers/{id}/frigate-config/schema`): types, enums, ranges, lengths, required keys, unknown keys.
    A construct it does not understand is accepted, the engine stays the final judge. If the schema cannot
    be read, `schema_checked` is false and a warning says only the syntax was checked.
  - Diff: leaf changes (`added`, `removed`, `changed`; lists are compared whole) with credentials removed.
  - Keys removed from the YAML are not deleted from Frigate (its config API merges); a warning lists them.
- **Managed parameters (never overwrite silently).** `managed_paths` are the parts of the section owned
  by OpenVMS through the camera's effective desired state (profile overlaid with overrides, see
  docs/camera-config-profiles.md): `detect.enabled`, `objects.track`, `record.enabled`, `lpr.enabled`,
  module switches and `min_area`, the detection resolution/FPS, and zones named `ovms_*`. An edit that moves
  one away from the desired value is a `managed_conflict`. `apply` answers **409 `managed_conflict`** until
  `confirm_managed_conflicts` is true. A confirmed edit does not change the desired state: the camera is
  left in `drift` ("Diferencia detectada") with `last_error` naming the paths, so the next review shows
  the difference instead of overwriting it.
- **Apply.** 400 for syntax, schema or "no hay cambios que aplicar"; 409 `revision_conflict` when
  `expected_revision` is not the camera's desired revision (read it from `revision`); otherwise
  `PatchCameraFrigateConfig` runs unchanged (Frigate revisions with before/after `config.yml`,
  `FRIGATE_CONFIG_PATCHED`). Result: Frigate's per-section result, `marked_drift`, the new `revision`.
- **Audit.** `CAMERA_CONFIG_CHANGED` with `origin: yaml`, user, camera, `section`, `previous_values` /
  `new_values` keyed `section.path` (credentials removed), `managed_conflicts`, `marked_drift`,
  `frigate_revision_id` and `result`. Failures are audited too (`result: error`, `error_plain`).
- **Permission.** `cameras.config.yaml` (legacy holders of `servers.config` + `servers.config.secrets`
  keep it; nobody else gains it), see docs/camera-config-permissions.md. The apply also needs what the
  Frigate camera PATCH needs.

## History and rollback (S15)

- `GET /api/v1/cameras/{id}/normalized-config/history?limit=20` lists recent changes from the audit trail,
  newest first: `id`, `time`, `user`, `origin`, `result`, `section` (YAML), `changes[]` (`path`, `from`, `to`),
  `error` (plain), `rollback_of`, `rolled_back_by`, `rollbackable`, `rollback_reason`.
- `POST .../normalized-config/rollback` `{audit_id?, expected_revision}` ("Restaurar configuración
  anterior") builds the inverse patch from the old values of that change and applies it through the normal
  review/apply pipeline with origin `ui` (permissions per section, desired state, drift check, audit). The
  audit row of the rollback carries `rollback_of`. Without `audit_id` it undoes the latest change that can be
  undone, skipping earlier rollbacks and changes already undone. The response is the usual apply result
  plus `rolled_back` (the audit id).
- **Rollbackable** means: the change was applied (`result` ok or unverified), it is not a YAML edit, every
  changed field has a recorded old value and is a field the patch type can write
  (`detection.enabled|objects`, `recording.enabled`, `analytics.lpr`, `analytics.modules.<id>.enabled|settings.min_area`)
  and no later change already undid it. Video (`streams.*`) changes are not restored automatically. A
  rollback is all or nothing: otherwise 400 with the reason and nothing is written. Undo a YAML edit by
  editing the YAML again (the diff is in the history).
- Needs `servers.config` on the camera's server (like the review); the rollback also needs each touched
  section's permission.

## Advanced panels (S13)

`GET /api/v1/cameras/{id}/advanced` is read only and never calls the camera:

- **frigate**: engine version, sections of the generated camera config, sections holding managed
  parameters, desired and applied revision, sync state. `available: false` with a `reason` when Frigate
  cannot be read.
- **onvif**: media profiles (token, name, encoding, size, FPS, bitrate) with stream URIs reduced to scheme,
  host and path, capabilities (`has_*`), service namespaces and the other scalar parameters the stored probe
  reported. The serial number and credentials are never returned.
- **provision**: evidence (tags, ONVIF identity) and the ISR link from the edge agent's status (reachable,
  registered, online, event count, last event); the agent is only asked for cameras with Provision evidence.

Permission: `cameras.config.advanced` (legacy `servers.config`) and `cameras.view`.

## Plain errors (S20)

`internal/camconfig/errors_plain.go` maps engine, ffmpeg and ONVIF failures to
`{message, technical}`. `message` is plain Spanish ("No se pudo iniciar el stream de grabación."),
`technical` is the sanitized raw text for "Ver información técnica" (no credentials, query strings or
key=value secrets). Where it appears:

- `plain_error` in the apply and rollback results (additive; `state.last_error` keeps the sanitized raw
  text as before);
- `error` in history entries and `error_plain` in the audit row.

Order of the catalog: ffmpeg (record / detect / generic), credentials, permission, TLS, DNS, connection and
timeout, unsupported action, invalid argument, ONVIF fault, engine did not reflect the change, engine
validation, not found, engine 5xx; anything else is "No se pudo completar el cambio.". Messages that OpenVMS
already wrote for people (validation errors) are used as they are.
