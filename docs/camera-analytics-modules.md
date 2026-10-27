# Camera analytics modules

Analytics are handled as modules, whatever engine runs them. The normalized camera model
(`GET /api/v1/cameras/{id}/normalized-config`) carries `analytics.modules[]`:
`id`, `label`, `available`, `enabled`, `writable`, `engine` (`frigate|provision|native|none`),
`reason`, `settings` and `advanced` (read-only engine summary).

## Honesty rule

A module is `available` only with positive evidence in data OpenVMS already reads. Otherwise it is
`available: false` with the reason "No disponible con este motor".

| Module | Backing | Writable |
|---|---|---|
| `lpr` | Provision tag gives engine `provision` (read-only, "Se configura en la cámara"); otherwise a Frigate `lpr` section, tag or inventory flag gives engine `frigate`. Settings: `min_area`, `enhancement` (0-10), both real per-camera Frigate fields. Read-only: `expire_time` and the fact that OpenVMS stores each reading in the event | Frigate only, when Frigate answered |
| `face_recognition` | Frigate camera document has a `face_recognition` section. Settings: `min_area` (the only per-camera field Frigate has). Read-only, server-wide: model, detection and recognition thresholds. Face lists: tenant watchlists are applied in alarm rules, there is no per-camera list (stated in the detail) | Yes (Frigate) |
| `face_detection` | Available when the camera has the `face_recognition` section; it mirrors recognition. Frigate runs the face detector only as part of recognition, so there is no switch: read-only with the reason | No |
| `vehicle_classification` | OpenVMS worker body classifier. Per-camera switch = existing `body_classify_cameras`; a server-wide off (`body_classify_servers`) locks it with "Desactivado para todo el servidor". Without the ONNX model the Frigate label is kept | Yes (OpenVMS) |
| `vehicle_color`, `clothing_color` | OpenVMS worker colors (`vehicle_attributes`, `person_attributes`). Per-camera switch in `camera_analytics_settings` (migration 00080); a missing row is on, so behavior is unchanged. Off: new vehicle events store color `unknown`; new person events get no clothing row | Yes (OpenVMS) |
| `similar_search` | None: Frigate semantic search is server-wide and OpenVMS does not compute vehicle similarity. Unavailable with that specific reason | No |

Provision documents no remote way to switch LPR, so Provision LPR is read-only.

## Patch

Sparse, through the existing review/apply/desired-state/audit path:

```json
{"analytics": {"modules": {"lpr": {"enabled": true, "settings": {"min_area": 1200}},
                           "face_recognition": {"enabled": false}}}}
```

- `analytics.lpr` keeps working and is the same switch as `modules.lpr.enabled` (sending both with
  different values is a 400).
- Translation: `lpr` -> Frigate `lpr`, `face_recognition` -> Frigate `face_recognition`
  (`enabled`, `min_area`, and `enhancement` for LPR). Both are restart-class sections.
- `vehicle_classification`, `vehicle_color`, `clothing_color` take only `enabled`; they are stored by OpenVMS (not sent to Frigate), have no engine impact in the review, and go through the same review/apply/desired/audit path. Permission: section `analytics` (`cameras.config.analytics`).
- Unknown module ids, modules without a Frigate switch, and (at apply) modules the camera reports as
  unavailable or not configurable are rejected with 400; the review lists them under `unsupported`.
- Drift is reported per module path (`analytics.modules.<id>.enabled`).

## Web

`components/camera/analytics/*`: module cards with a switch (disabled with the reason), detail only
while on, LPR block with `LprTestCard`, facial group split into detection and recognition. The raw
Provision/Frigate panels stay as "Avanzado · ..." sub-sections. The tab is hidden when no module is
available (`analyticsTabVisible`).

Not shown because Frigate has no per-camera field: LPR zones/region/direction/filters, recognition
thresholds (server-wide, shown read-only for faces), per-camera face lists.
