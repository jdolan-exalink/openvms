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
