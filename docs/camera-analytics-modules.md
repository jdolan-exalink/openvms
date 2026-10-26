# Camera analytics modules

Analytics are handled as modules, whatever engine runs them. The normalized camera model
(`GET /api/v1/cameras/{id}/normalized-config`) carries `analytics.modules[]`:
`id`, `label`, `available`, `enabled`, `writable`, `engine` (`frigate|provision|native|none`),
`reason`, `settings` and `advanced` (read-only engine summary).

## Honesty rule

A module is `available` only with positive evidence in data OpenVMS already reads. Otherwise it is
`available: false` with the reason "No disponible con este motor".

| Module | Evidence today | Writable |
|---|---|---|
| `lpr` | Provision tag (`lpr_provision`, or `lpr` + `provision`/`provision_isr`) gives engine `provision`, enabled, reason "Se configura en la cámara"; otherwise a Frigate `lpr` section, an `lpr`/`lpr_frigate` tag or the inventory LPR flag gives engine `frigate` | Frigate only, and only when Frigate answered |
| `face_recognition` | Frigate camera document has a `face_recognition` section | Yes (Frigate) |
| `face_detection`, `vehicle_classification`, `vehicle_color`, `clothing_color`, `similar_search` | None in the camera model yet (Frigate has no separate face detection switch; vehicle and clothing color run in the worker without a per-camera flag the model reads) | No |

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
  (`enabled`, `min_area`). Both are restart-class sections.
- Unknown module ids, modules without a Frigate switch, and (at apply) modules the camera reports as
  unavailable or not configurable are rejected with 400; the review lists them under `unsupported`.
- Drift is reported per module path (`analytics.modules.<id>.enabled`).

## Web

`components/camera/analytics/*`: module cards with a switch (disabled with the reason), detail only
while on, LPR block with `LprTestCard`, facial group split into detection and recognition. The raw
Provision/Frigate panels stay as "Avanzado · ..." sub-sections. The tab is hidden when no module is
available (`analyticsTabVisible`).

Not backed yet, therefore not shown: LPR zones/region/direction/filters/storage, face confidence,
face list/base association.
