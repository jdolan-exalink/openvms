import type { Schemas } from "@/api/client";
import { camerasOfServer, type Camera, type Folder } from "./explorer";
import { cameraIdOf, type Tile } from "./liveGrid";

export type ServerChoice = {
  id: string;
  name: string;
  siteName: string;
  /** Cameras of this server currently in the grid. */
  inGrid: number;
  /** Enabled cameras the server has. */
  total: number;
  /** Checked while any of its cameras is in the grid. */
  checked: boolean;
};

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

/**
 * The servers the Live server selector offers: those with enabled cameras, by site name and then
 * server name, each with how many of its cameras are in the grid. A server is checked while any of
 * its cameras is there, so the selector always reflects what the grid shows.
 */
export function serverChoices(
  servers: readonly Schemas["Server"][],
  sites: readonly Schemas["Site"][],
  cameras: readonly Camera[],
  folders: readonly Folder[],
  tiles: readonly Tile[],
): ServerChoice[] {
  const siteName = new Map(sites.map((s) => [s.id, s.name]));
  const inGridIds = new Set(tiles.flatMap((t) => { const id = cameraIdOf(t); return id ? [id] : []; }));
  return servers
    .map((s): ServerChoice => {
      const own = camerasOfServer(s.id, cameras, folders);
      const inGrid = own.filter((c) => inGridIds.has(c.id)).length;
      return { id: s.id, name: s.name, siteName: siteName.get(s.site_id) ?? "", inGrid, total: own.length, checked: inGrid > 0 };
    })
    .filter((c) => c.total > 0)
    .sort((a, b) => a.siteName.localeCompare(b.siteName) || a.name.localeCompare(b.name));
}

/**
 * Empties the cells that show cameras of a server, virtual fisheye views included. Everything else
 * stays in its cell, so a layout is never reshuffled when a server is unchecked.
 */
export function removeServerTiles(tiles: readonly Tile[], cameras: readonly Camera[], serverId: string): Tile[] {
  const ofServer = new Set(cameras.filter((c) => c.server_id === serverId).map((c) => c.id));
  return tiles.map((t) => {
    const id = cameraIdOf(t);
    return id !== undefined && ofServer.has(id) ? null : t;
  });
}
