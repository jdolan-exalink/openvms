import type { Schemas } from "@/api/client";
import type { Camera } from "./explorer";

export type ServerOption = {
  id: string;
  name: string;
  siteName: string;
  /** Enabled cameras the server has. */
  cameras: number;
  /** Shown unless the user left it out. */
  checked: boolean;
};

/**
 * The servers the selector offers: those with enabled cameras, by site name and then server name.
 * A server is checked unless it is in `excluded`, so "every server" is the default and a server
 * added later is shown without anyone touching the selector.
 */
export function serverOptions(
  servers: readonly Schemas["Server"][],
  sites: readonly Schemas["Site"][],
  cameras: readonly Camera[],
  excluded: readonly string[],
): ServerOption[] {
  const siteName = new Map(sites.map((s) => [s.id, s.name]));
  const left = new Set(excluded);
  return servers
    .map((s): ServerOption => ({
      id: s.id,
      name: s.name,
      siteName: siteName.get(s.site_id) ?? "",
      cameras: cameras.filter((c) => c.enabled && c.server_id === s.id).length,
      checked: !left.has(s.id),
    }))
    .filter((o) => o.cameras > 0)
    .sort((a, b) => a.siteName.localeCompare(b.siteName) || a.name.localeCompare(b.name));
}

/**
 * The checked servers when some are left out, or undefined while all are checked (or there is
 * nothing to choose from): "no filter" is how every list asks for all servers.
 */
export function serverFilter(options: readonly ServerOption[]): string[] | undefined {
  if (options.length === 0 || options.every((o) => o.checked)) return undefined;
  return options.filter((o) => o.checked).map((o) => o.id);
}

/**
 * The exclusions after checking or unchecking one server. At least one server stays checked:
 * with none there would be nothing to show, and an empty filter elsewhere means "all". Exclusions
 * of servers that are not offered are kept untouched.
 */
export function toggleServer(excluded: readonly string[], options: readonly ServerOption[], serverId: string, checked: boolean): string[] {
  if (checked) return excluded.filter((id) => id !== serverId);
  const stillChecked = options.filter((o) => o.checked && o.id !== serverId).length;
  if (stillChecked === 0) return [...excluded];
  return excluded.includes(serverId) ? [...excluded] : [...excluded, serverId];
}

/** Ids of the enabled cameras of these servers, for lists that can only filter by camera. */
export function cameraIdsOfServers(cameras: readonly Camera[], serverIds: readonly string[]): string[] {
  const wanted = new Set(serverIds);
  return cameras.filter((c) => c.enabled && wanted.has(c.server_id)).map((c) => c.id);
}
