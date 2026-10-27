# Camera configuration: desired vs effective state

OpenVMS keeps, per camera, what it wants (desired) next to what the engine reports (effective).
The UI only talks to the normalized model; adapters translate it (Frigate today).

## Endpoints

| Endpoint | Purpose |
|---|---|
| `GET /cameras/{id}/normalized-config` | Effective model plus `state` |
| `POST /cameras/{id}/normalized-config/review` | Changes and impact of a patch. No side effects |
| `POST /cameras/{id}/normalized-config/apply` | Record, apply, verify, audit |

Review and apply need the permission the Frigate config PATCH needs (`servers.config` on the
camera's server). Granular permissions come with U12.

## Patch

A sparse patch. Today: `detection.enabled`, `detection.objects` (raw engine labels until the
object catalog), `recording.enabled`, `analytics.lpr`. Any other field, nested ones included, is
rejected with 400; nothing is silently ignored.

## States (`state.sync_state`)

| Value | UI label | Meaning |
|---|---|---|
| `synced` | Sincronizado | Effective equals desired |
| `pending` | Cambios pendientes | An apply is in flight |
| `drift` | Diferencia detectada | Effective differs from desired with no apply in flight ("La cámara fue modificada externamente.") |
| `error` | Error | The last apply failed; `last_error` is sanitized (no credentials, no query strings) |
| `unverifiable` | No verificable | The engine could not be read, so nothing can be confirmed |

`state.drift[]` lists `{path, desired, effective}`. A failed apply is never `synced`.

## Revisions

`desired_revision` increases by one on every apply and is the optimistic-concurrency token:
`apply` takes `expected_revision` (the value last read) and answers 409 when it is stale.
`applied_revision` only advances to `desired_revision` when the engine confirmed the change on a
read-back. The desired state is a sparse patch accumulated over all applies.

## Apply

1. In one transaction: check the permission and the revision, merge the patch into `desired`,
   `desired_revision + 1`, `sync_state = pending`.
2. Write to the engine through the same service as the `frigate-config` PATCH (its validation,
   revisions and audit apply too).
3. Read the camera back and decide: verified -> `synced` (or `drift` if older desired fields
   differ); engine error -> `error`; engine unreadable -> `unverifiable`.
4. In one transaction: store the final state and the audit row.

If the process dies between 1 and 4 the state stays `pending`, never `synced`.

## Audit

Action `CAMERA_CONFIG_CHANGED` (target: the camera): user, camera, time, `previous_values`,
`new_values`, `origin`, `result` (`ok`, `error`, `unverified`), revisions and the patch.

## Origins

`ui` (default), `profile`, `group`, `api`, `sync`, `yaml`, `external_device`.
