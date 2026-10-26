# Camera configuration profiles, inheritance and bulk apply

Three levels decide the value of a camera parameter:

1. **Engine defaults** (Frigate, `source: engine`, "heredado"): only reported when the engine
   actually returned the value; OpenVMS never invents one.
2. **Profile** (`source: profile`, "definido por perfil"): a named, tenant-scoped sparse patch.
3. **Override** (`source: override`, "modificado localmente"): what the camera itself sets
   (`camera_config_state.desired`). It always wins over the profile.

A profile holds the same strict `Patch` a camera patch does (detection, recording, analytics,
streams), so it can only contain fields the engine adapters translate. `detection.selection` is
not allowed in a profile (it needs the camera's model): use raw `detection.objects`. Names are
unique per tenant, ignoring case. A camera follows at most one profile.

## Data (migration 00077)

- `camera_config_profiles`: id, tenant_id, name, description, patch, version (grows on every
  edit), audit columns; RLS like the other camera tables.
- `camera_config_state.profile_id` / `profile_version`: the link and the version the camera last
  received. The foreign key is `ON DELETE RESTRICT`.

## API

| Endpoint | Notes |
|---|---|
| `GET/POST /api/v1/camera-config/profiles` | list (cameras.view on the tenant) / create (cameras.manage) |
| `GET/PUT/DELETE /api/v1/camera-config/profiles/{profileId}` | GET lists followers the caller can view, each `outdated` or not; DELETE answers 409 `profile_in_use` while cameras follow it |
| `POST /api/v1/cameras/{id}/profile/review` | preview of assigning (or unlinking, `profile_id: null`); no side effects |
| `POST /api/v1/cameras/{id}/profile/apply` | `{profile_id\|null, keep_values}` |
| `GET /api/v1/cameras/{id}/normalized-config` | now also `profile` and `provenance` (path to source) |
| `POST /api/v1/camera-config/bulk/review` and `/bulk/apply` | `{camera_ids \| group_id, patch, origin}` |

Profile management needs `cameras.manage` on the tenant. Assigning needs `cameras.manage` on the
camera plus whatever the normal camera config review/apply needs (today `servers.config`), so
granular camera-config permissions apply automatically.

### Assigning a profile

The review lists the changes that would reach the engine: the profile's fields that the camera's
own overrides do not shadow, compared with the engine's current values. `parameters` is their
count ("Se modificarán N parámetros"), `shadowed` the profile fields an override keeps winning.
Applying stores the link and runs the normal apply (desired/effective revisions, verification,
`CAMERA_CONFIG_CHANGED` audit) with origin `profile`. A change with origin `profile` is **not**
stored as an override, so the camera keeps inheriting. Existing overrides are never rewritten.
The link itself is audited as `CAMERA_CONFIG_PROFILE_CHANGED` (`assigned`).

### Unlinking

`profile_id: null`. The engine is not touched. With `keep_values: true` (default) the profile's
current values (those not already overridden) become overrides; with `false` they stop being
desired and only the engine's current values remain. The review shows `converted` or `dropped`.

### Editing a profile

Updating bumps `version` and does **not** push to cameras. `GET /profiles/{id}` shows which
followers are `outdated`; re-apply by calling `profile/review` and `profile/apply` for each one
with the same `profile_id`.

### Bulk

Select cameras by `camera_ids` or by `group_id` (the members the caller can view); exactly one
selector, at most 200 cameras, otherwise 400. `patch` must contain exactly one top-level section
(`detection`, `recording`, `analytics` or `streams`), otherwise 400, so a bulk call never
overwrites parameters the user did not select. Each row has a status:

- `will_apply` (review) / `applied` or `failed` (apply);
- `incompatible`: the camera cannot take the change (for example a Provision-only camera, or an
  analytics module it does not have), with the reason;
- `skipped`: no permission, not visible (reported only by the id the caller sent, with no name),
  or already at those values;
- `unreachable`: the engine or camera could not be read.

`restarts` counts cameras whose change needs an engine restart. Apply runs camera by camera
through the normal pipeline with origin `group` (or `api`); a failure never stops the others.
Groups are used only to select cameras: permissions stay explicit, so reorganising groups
never grants access.
