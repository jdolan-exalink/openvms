# Granular camera permissions (U12)

Migration `00078_camera_config_permissions.sql` and `internal/authz/camera_config.go`.

| Permission | Gates | Legacy equivalent (kept working) |
|---|---|---|
| `cameras.config.view` | read the normalized config | `cameras.view` |
| `cameras.config.edit` | patch sections `detection` (and identity) | `servers.config` |
| `cameras.config.video` | patch section `streams` (video) | `servers.config` |
| `cameras.config.recording` | patch section `recording` | `servers.config` |
| `cameras.config.analytics` | patch section `analytics` | `servers.config` |
| `cameras.config.ptz` | PTZ control and tours | `cameras.manage` |
| `cameras.web.access` | camera web session (`POST /api/v1/cameras/{id}/web/session`) | `servers.manage` + `servers.config.secrets` |
| `cameras.config.advanced` | `GET /cameras/{id}/advanced` (read-only technical panels), with `cameras.view` | `servers.config` |
| `cameras.config.yaml` | read, validate and apply the YAML of a camera section (`/cameras/{id}/frigate-yaml*`), with `cameras.view`; the apply also needs what the Frigate camera PATCH needs (`servers.config`, and `servers.config.secrets` to write credentials). The raw `config.yml` editor of a server keeps `servers.config` + `servers.config.secrets` | `servers.config` + `servers.config.secrets` |
| `cameras.restart` | catalog only (no restart-camera endpoint yet; server restart keeps `servers.restart`) | `servers.restart` |

## Rules

- Review/apply keeps the existing `servers.config` check and also needs the permission of EVERY
  section the patch touches. Denied sections come back together in the 403
  (`code: forbidden_sections`, message `you do not have permission to change: video, analytics`);
  nothing else about the camera is leaked.
- No lockout, no escalation. The migration mirrors every legacy grant (allow and deny) to the new
  permission at the same subject, effect and scope; the pair permissions need both legacy allows
  at the same scope, and a deny of either mirrors as a deny. Down removes only grants derived that way.
- Runtime compatibility (`authz.DecideCompat`): an explicit deny of the new permission wins; an allow
  grants; otherwise the legacy set grants only while the actor has no granular allow at all (so grants
  created later with legacy names keep working, and revoking a granular grant takes effect). The web
  pair is always accepted next to granular grants. A legacy set never grants more than it did before.
- `cameras.manage` alone never gave config writes, so it only maps to `cameras.config.ptz`.
- Scope: all are camera-narrowest, so they can be granted at platform, tenant, site, server, group or
  camera scope like `cameras.view`.

## Audit

- Config changes: `CAMERA_CONFIG_CHANGED` carries user, camera, time, previous and new values, origin and result (U3).
- Camera web access: `CAMERA_WEB_SESSION` is written when a session opens (`result: opened`) and when it
  is refused (`result: denied`), with scheme, port, server and origin. Credentials, cookies and session
  ids never reach the audit row. Proxied requests are not audited one by one.

## YAML and advanced enforcement (U10)

- Enforced in `internal/inventory/camera_yaml.go` (`requireYAML`) and `camera_advanced.go`
  (`requireAdvanced`) with `access.Checker.RequireCompat`, so the rules above apply unchanged: a deny
  wins, an allow grants, and the legacy set grants only while the actor holds no granular allow.
  Nobody gains access: `cameras.view`, `cameras.manage`, `servers.config.secrets` alone, or only other
  granular grants (`cameras.config.edit`, ...) do not open the YAML editor; `servers.config` alone does
  not open it either (it never did: raw YAML needed the secrets pair) but still opens the advanced panels.
- The YAML shows credentials only to actors holding `servers.config.secrets`; others get masked
  placeholders that round-trip without overwriting the real values.
- History and rollback (`/normalized-config/history|rollback`) need `servers.config` like the review,
  and the rollback needs the permission of each section it restores.
- Matrix test: `TestCameraYAMLAndAdvancedPermissionMatrix` in `internal/inventory/camera_advanced_integration_test.go`.
