import type { Schemas } from "@/api/client";

/**
 * can reports whether the user holds an ALLOW for permission anywhere. It only decides
 * what the UI offers; the API authorizes every request on its own.
 */
export function can(me: Schemas["Me"] | undefined, permission: string): boolean {
  return !!me?.grants.some((g) => g.permission === permission && g.effect === "allow");
}

const cameraConfigFamily = [
  "cameras.config.view", "cameras.config.edit", "cameras.config.video", "cameras.config.recording",
  "cameras.config.analytics", "cameras.config.ptz", "cameras.web.access", "cameras.config.advanced",
  "cameras.config.yaml", "cameras.restart",
];

// Legacy permissions that allowed each granular camera action before it existed; `always` marks
// the ones the API keeps accepting next to granular grants. Mirrors internal/authz/camera_config.go.
const legacy: Record<string, { all: string[]; always?: boolean }> = {
  "cameras.config.view": { all: ["cameras.view"] },
  "cameras.config.edit": { all: ["servers.config"] },
  "cameras.config.video": { all: ["servers.config"] },
  "cameras.config.recording": { all: ["servers.config"] },
  "cameras.config.analytics": { all: ["servers.config"] },
  "cameras.config.ptz": { all: ["cameras.manage"] },
  "cameras.config.advanced": { all: ["servers.config"] },
  "cameras.config.yaml": { all: ["servers.config", "servers.config.secrets"] },
  "cameras.web.access": { all: ["servers.manage", "servers.config.secrets"], always: true },
  "cameras.restart": { all: ["servers.restart"] },
};

/**
 * canCamera reports whether the user may use a granular camera permission, honoring the legacy
 * permissions that allowed the same action: they count while the user holds no granular grant,
 * or always for the camera web pair. An explicit grant wins; the API decides for real.
 */
export function canCamera(me: Schemas["Me"] | undefined, permission: string): boolean {
  if (can(me, permission)) return true;
  const rule = legacy[permission];
  if (!rule || !rule.all.every((p) => can(me, p))) return false;
  return rule.always === true || !cameraConfigFamily.some((p) => can(me, p));
}
