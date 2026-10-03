import type { Schemas } from "@/api/client";

/**
 * Pure model of the Live explorer tree (LV-13): Site > Server > Folder > Camera. Folders and camera
 * order are shared per tenant, so everything here derives from the API data; the drop helpers turn
 * a drag (active, over) into the reorder batch the API accepts, and refuse moves between servers.
 */

export type Camera = Schemas["Camera"];
export type Folder = Schemas["CameraFolder"];

export type FolderNode = { folder: Folder; cameras: Camera[] };
export type ServerNode = {
  id: string;
  name: string;
  status: string;
  canManage: boolean;
  folders: FolderNode[];
  rootCameras: Camera[];
  count: number;
};
export type SiteNode = { id: string; name: string; servers: ServerNode[]; count: number };

const byOrder = (a: { sort_order: number }, b: { sort_order: number }) => a.sort_order - b.sort_order;
const byCamera = (a: Camera, b: Camera) => byOrder(a, b) || a.display_name.localeCompare(b.display_name);
const byFolder = (a: Folder, b: Folder) => byOrder(a, b) || a.name.localeCompare(b.name);

const includes = (text: string, q: string) => text.toLowerCase().includes(q);

export function buildTree(input: {
  cameras: Camera[];
  folders: Folder[];
  sites: Schemas["Site"][];
  servers: Schemas["Server"][];
  manageable: ReadonlySet<string>;
  query: string;
}): SiteNode[] {
  const q = input.query.trim().toLowerCase();
  const cams = input.cameras.filter((c) => c.enabled);
  const serverInfo = new Map(input.servers.map((s) => [s.id, s]));
  const siteName = new Map(input.sites.map((s) => [s.id, s.name]));

  // A server node exists for every server that has cameras, folders, or that the caller manages.
  const serverSite = new Map<string, string>();
  for (const c of cams) serverSite.set(c.server_id, c.site_id);
  for (const s of input.servers) serverSite.set(s.id, s.site_id);

  const sites = new Map<string, SiteNode>();
  for (const [serverId, siteId] of serverSite) {
    const info = serverInfo.get(serverId);
    const serverCams = cams.filter((c) => c.server_id === serverId);
    if (!info && serverCams.length === 0) continue;
    const canManage = input.manageable.has(serverId);
    const serverFolders = input.folders.filter((f) => f.server_id === serverId).sort(byFolder);
    const name = info?.name ?? "Servidor";
    const serverMatches = q !== "" && includes(name, q);

    const folders: FolderNode[] = [];
    for (const folder of serverFolders) {
      const inFolder = serverCams.filter((c) => c.folder_id === folder.id).sort(byCamera);
      const folderMatches = q !== "" && includes(folder.name, q);
      const shown = q === "" || serverMatches || folderMatches ? inFolder : inFolder.filter((c) => includes(c.display_name, q));
      // Empty folders are kept only while browsing (managers need them as drop targets) or on a name match.
      if (q === "" ? canManage || inFolder.length > 0 : folderMatches || serverMatches || shown.length > 0) {
        folders.push({ folder, cameras: shown });
      }
    }
    const folderIds = new Set(serverFolders.map((f) => f.id));
    const root = serverCams.filter((c) => c.folder_id === null || !folderIds.has(c.folder_id)).sort(byCamera);
    const rootCameras = q === "" || serverMatches ? root : root.filter((c) => includes(c.display_name, q));
    const count = folders.reduce((n, f) => n + f.cameras.length, 0) + rootCameras.length;
    if (q !== "" && count === 0 && !serverMatches) continue;
    if (q === "" && count === 0 && !canManage && !info) continue;

    const site = sites.get(siteId) ?? { id: siteId, name: siteName.get(siteId) ?? "Sitio", servers: [], count: 0 };
    site.servers.push({ id: serverId, name, status: info?.status ?? "unknown", canManage, folders, rootCameras, count });
    site.count += count;
    sites.set(siteId, site);
  }
  const out = [...sites.values()].sort((a, b) => a.name.localeCompare(b.name));
  for (const s of out) s.servers.sort((a, b) => a.name.localeCompare(b.name));
  return out;
}

// Drag ids. Grid drags use "camera:<id>" (draggable) and "tile:<n>"; the tree adds its own
// prefixes so both kinds of drop target coexist in the one DndContext.
const TREE_CAMERA = "tcam:";
const TREE_FOLDER = "tfolder:";
const TREE_SERVER = "tserver:";
const TREE_ROOT = "troot:";
const CAMERA = "camera:";

export const treeCameraDropId = (id: string) => `${TREE_CAMERA}${id}`;
export const treeFolderId = (id: string) => `${TREE_FOLDER}${id}`;
export const treeServerDragId = (id: string) => `${TREE_SERVER}${id}`;
export const treeRootDropId = (serverId: string) => `${TREE_ROOT}${serverId}`;

const strip = (id: string | number, prefix: string) => (String(id).startsWith(prefix) ? String(id).slice(prefix.length) : null);

export type CameraPlacement = { camera_id: string; folder_id: string | null; sort_order: number };
export type FolderPosition = { folder_id: string; sort_order: number };
export type Reorder = { cameras: CameraPlacement[]; folders: FolderPosition[] };

/** Cameras of one container (a folder, or a server root when folderId is null), in display order. */
function container(cams: Camera[], serverId: string, folderId: string | null): Camera[] {
  return cams.filter((c) => c.server_id === serverId && (c.folder_id ?? null) === folderId).sort(byCamera);
}

function renumber(list: Camera[], folderId: string | null, out: CameraPlacement[]) {
  list.forEach((c, i) => {
    if (c.sort_order !== i || (c.folder_id ?? null) !== folderId) out.push({ camera_id: c.id, folder_id: folderId, sort_order: i });
  });
}

/**
 * moveCamera puts a camera in a folder (null = server root) before another camera, or at the end.
 * Returns null when the target belongs to a different server: cameras never leave their server.
 */
export function moveCamera(cams: Camera[], folders: Folder[], cameraId: string, target: { serverId: string; folderId: string | null; beforeCameraId?: string }): Reorder | null {
  const cam = cams.find((c) => c.id === cameraId);
  if (!cam || cam.server_id !== target.serverId) return null;
  if (target.folderId !== null && folders.find((f) => f.id === target.folderId)?.server_id !== cam.server_id) return null;
  if (target.beforeCameraId === cameraId) return null;

  const from = cam.folder_id ?? null;
  const dest = container(cams, cam.server_id, target.folderId).filter((c) => c.id !== cameraId);
  const at = target.beforeCameraId ? dest.findIndex((c) => c.id === target.beforeCameraId) : -1;
  dest.splice(at < 0 ? dest.length : at, 0, cam);

  const out: CameraPlacement[] = [];
  renumber(dest, target.folderId, out);
  if (from !== target.folderId) renumber(container(cams, cam.server_id, from).filter((c) => c.id !== cameraId), from, out);
  return out.length ? { cameras: out, folders: [] } : null;
}

/** moveFolder reorders a folder before another folder of the same server, or to the end. */
export function moveFolder(folders: Folder[], folderId: string, beforeFolderId?: string): Reorder | null {
  const folder = folders.find((f) => f.id === folderId);
  if (!folder || beforeFolderId === folderId) return null;
  const list = folders.filter((f) => f.server_id === folder.server_id && f.id !== folderId).sort(byFolder);
  if (beforeFolderId && folders.find((f) => f.id === beforeFolderId)?.server_id !== folder.server_id) return null;
  const at = beforeFolderId ? list.findIndex((f) => f.id === beforeFolderId) : -1;
  list.splice(at < 0 ? list.length : at, 0, folder);
  const out: FolderPosition[] = [];
  list.forEach((f, i) => {
    if (f.sort_order !== i) out.push({ folder_id: f.id, sort_order: i });
  });
  return out.length ? { cameras: [], folders: out } : null;
}

export type TreeDrop = { reorder: Reorder } | { rejected: true } | null;

/**
 * resolveTreeDrop maps a finished drag onto tree changes. Non-tree drags (camera -> grid tile)
 * return null so the grid handler takes them. Anything that would cross servers is rejected.
 */
export function resolveTreeDrop(activeId: string | number, overId: string | number | null, cams: Camera[], folders: Folder[]): TreeDrop {
  if (overId === null) return null;
  const cameraId = strip(activeId, CAMERA);
  const folderId = strip(activeId, TREE_FOLDER);
  if (cameraId === null && folderId === null) return null;

  const overCam = strip(overId, TREE_CAMERA);
  const overFolder = strip(overId, TREE_FOLDER);
  const overRoot = strip(overId, TREE_ROOT);
  if (overCam === null && overFolder === null && overRoot === null) return null;

  if (cameraId !== null) {
    const cam = cams.find((c) => c.id === cameraId);
    if (!cam) return null;
    let target: { serverId: string; folderId: string | null; beforeCameraId?: string } | null = null;
    if (overCam !== null) {
      const t = cams.find((c) => c.id === overCam);
      if (t) target = { serverId: t.server_id, folderId: t.folder_id ?? null, beforeCameraId: t.id };
    } else if (overFolder !== null) {
      const f = folders.find((x) => x.id === overFolder);
      if (f) target = { serverId: f.server_id, folderId: f.id };
    } else if (overRoot !== null) {
      target = { serverId: overRoot, folderId: null };
    }
    if (!target) return null;
    if (target.serverId !== cam.server_id) return { rejected: true };
    const reorder = moveCamera(cams, folders, cameraId, target);
    return reorder ? { reorder } : null;
  }

  const folder = folders.find((f) => f.id === folderId);
  if (!folder || overFolder === null) return null;
  const target = folders.find((f) => f.id === overFolder);
  if (!target) return null;
  if (target.server_id !== folder.server_id) return { rejected: true };
  const reorder = moveFolder(folders, folder.id, target.id);
  return reorder ? { reorder } : null;
}

/** applyReorder mirrors a reorder batch onto cached data, for optimistic updates. */
export function applyReorder(cams: Camera[], folders: Folder[], r: Reorder): { cameras: Camera[]; folders: Folder[] } {
  const cp = new Map(r.cameras.map((c) => [c.camera_id, c]));
  const fp = new Map(r.folders.map((f) => [f.folder_id, f]));
  return {
    cameras: cams.map((c) => {
      const p = cp.get(c.id);
      return p ? { ...c, folder_id: p.folder_id, sort_order: p.sort_order } : c;
    }),
    folders: folders.map((f) => {
      const p = fp.get(f.id);
      return p ? { ...f, sort_order: p.sort_order } : f;
    }),
  };
}

export type ExplorerPrefs = { closed: Record<string, boolean>; sections: { cameras: boolean; views: boolean } };
const PREFS_KEY = "openvms.live.explorer.v1";
const SIDEBAR_KEY = "openvms.live.sidebar.collapsed";
const defaultPrefs = (): ExplorerPrefs => ({ closed: {}, sections: { cameras: true, views: true } });

export function loadPrefs(): ExplorerPrefs {
  try {
    const raw = JSON.parse(localStorage.getItem(PREFS_KEY) ?? "null") as Partial<ExplorerPrefs> | null;
    const d = defaultPrefs();
    if (!raw || typeof raw !== "object") return d;
    return {
      closed: raw.closed && typeof raw.closed === "object" ? raw.closed : d.closed,
      sections: { cameras: raw.sections?.cameras !== false, views: raw.sections?.views !== false },
    };
  } catch {
    return defaultPrefs();
  }
}

export function savePrefs(p: ExplorerPrefs) {
  try {
    localStorage.setItem(PREFS_KEY, JSON.stringify(p));
  } catch {
    // Storage unavailable: the explorer still works for this session.
  }
}

export function loadSidebarCollapsed(): boolean {
  try {
    return localStorage.getItem(SIDEBAR_KEY) === "1";
  } catch {
    return false;
  }
}

export function saveSidebarCollapsed(collapsed: boolean) {
  try {
    localStorage.setItem(SIDEBAR_KEY, collapsed ? "1" : "0");
  } catch {
    // Storage unavailable: not persisted.
  }
}
