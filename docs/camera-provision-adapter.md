# Provision ISR on the universal camera model

Provision ISR cameras run their own LPR analytics and POST REST events to the site's edge agent
(`internal/agent/provisionlpr`). The manufacturer documents no remote configuration, so OpenVMS
reads and reports Provision cameras; it never writes to them. The UI does not change because of
it: `GET /api/v1/cameras/{id}/normalized-config` answers with the same model for every engine.

## What the adapter reports (`internal/camconfig/provision.go`)

Evidence is the camera's tags (`lpr_provision`, `provision`, `provision_isr`) or a Provision ONVIF
identity (`camconfig.ProvisionEvidence`). Everything else needs its own positive evidence:

| Part | Source |
|---|---|
| Identity, device, streams | Inventory and the stored ONVIF capabilities (nothing invented). |
| `capabilities` | `onvif` with an endpoint, `lpr` and `object_detection` with Provision LPR, `ptz`/`analytics`/`events` from stored ONVIF capabilities, `events` also when the agent counted events. |
| `detection` | The objects Provision really reports: `vehicle` (no subtype) and `license_plate`. They are collapsed through the catalog, so "Vehículos" shows as selected through `vehicle.generic` and `detection.notes` says "Provision no distingue el tipo de vehículo". Auto, Camión and the rest are never invented. |
| `recording` | Unknown (not reported). |
| `analytics.modules` | The shared derivation (`analytics.go`): LPR with engine `provision`, not writable, reason "Se configura en la cámara". |
| `integrations` | See below. |

## Integrations (S13)

`integrations[]` keeps its `name`/`enabled` entries and gains backend entries with `id`
(`frigate`, `onvif`, `provision_isr`), `label`, `status` (`connected`, `not_configured`, `error`,
`unknown`) and `detail`. They come from evidence already read, never from a new probe:

- Frigate: the `frigate` source (ok = connected, unreachable = error, camera not in Frigate = not configured).
- ONVIF: no endpoint = not configured; the `onvif` source when present; else the stored capabilities (connected) or unknown.
- Provision ISR: not configured without Provision evidence; otherwise the edge agent's read-only status for the camera: not reachable = unknown, not registered or no recent keepalive = error, online = connected. Only cameras with Provision evidence make the agent get asked (3 s limit); causes are never copied into `detail`.

The Integraciones tab shows these as the "Estado" section, before "Web de la cámara" and the ONVIF information.

## Engine selection and precedence (`camconfig.ComposedAdapter`)

| Camera | Read through | `origin.engine` | `sources` |
|---|---|---|---|
| No Provision evidence | Frigate, as before | `frigate` | `frigate`, `go2rtc`, `onvif`... |
| Provision evidence and Frigate serves it (for example Escuela, Ingreso Sur) | Frigate merged with Provision | `frigate` | the above plus `provision` |
| Provision evidence and Frigate answers 404 for the camera | Provision alone | `provision` | `frigate` (unavailable, "not served") and `provision` |
| Provision evidence and Frigate unreachable | Frigate degraded plus Provision | `frigate` | `frigate` unavailable |

When both serve a camera, **Frigate owns** identity, origin, streams, recording, detection
(objects, categories), zones and PTZ. **Provision adds** the LPR evidence: the LPR module (engine
`provision`), `lpr`/`events` capabilities, the Provision integration status and device fields
Frigate/ONVIF left empty. Provision never overrides a Frigate value. An unreachable Frigate says
nothing about the camera, so it is never treated as "Provision only".

## Writes

Provision-only cameras refuse every write:

- `POST .../normalized-config/review` answers 200 with no `changes` and every requested field under `unsupported`, as `<path>: Se configura en la cámara` (`detection`, `recording`, `analytics.lpr`, `analytics.modules.<id>`, `streams.primary|detection`).
- `POST .../normalized-config/apply` answers 400 with the same text before anything is recorded.
- On a camera Frigate also serves, the Provision LPR module stays read-only (existing guard); the rest goes through Frigate.

Permissions are those of the sibling endpoints (`cameras.view` to read, the existing review/apply checks to write); the agent status is read with the caller's own permissions.
