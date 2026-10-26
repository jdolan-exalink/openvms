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
| `cameras.web.access` | camera web session (`/cameras/{id}/web-session`) | `servers.manage` + `servers.config.secrets` |
| `cameras.config.advanced` | catalog only (U10) | `servers.config` |
| `cameras.config.yaml` | catalog only (U10; raw YAML keeps `servers.config` + `servers.config.secrets`) | same pair |
| `cameras.restart` | catalog only (server restart keeps `servers.restart`) | `servers.restart` |

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
