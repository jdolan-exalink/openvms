import { parsePanes, type Pane } from "./presentations";

/**
 * liveGrid holds pure, framework-free logic for the Live screen's camera grid: resizing,
 * placing a camera into a slot, reordering tiles by drag, and persisting/restoring the current
 * selection to/from localStorage. Kept separate from Live.tsx so it stays trivially unit-testable
 * (no React, no browser APIs beyond the storage shape itself).
 */

export type Quality = "sub" | "main";
export type LiveMapRef = { site_id: string; floor_id?: string; name: string };
export type CameraTile = { camera_id: string; quality: Quality };
export type MapGridTile = { map: LiveMapRef };
export type Tile = CameraTile | MapGridTile | null;

export function cameraIdOf(tile: Tile): string | undefined {
  return tile && "camera_id" in tile ? tile.camera_id : undefined;
}

export function isMapTile(tile: Tile): tile is MapGridTile {
  return !!tile && "map" in tile;
}

/** resizeTiles truncates or pads tiles to the requested grid dimensions, keeping existing order. */
export function resizeTiles(tiles: Tile[], columns: number, rows = columns): Tile[] {
  const count = columns * rows;
  const next = tiles.slice(0, count);
  while (next.length < count) next.push(null);
  return next;
}

/** placeCameraAt returns a new tiles array with the camera placed at index. */
export function placeCameraAt(tiles: Tile[], index: number, cameraId: string, quality: Quality): Tile[] {
  return tiles.map((t, i) => (i === index ? { camera_id: cameraId, quality } : t));
}

/**
 * reorderTiles moves the tile at `from` to `to`, shifting the tiles in between by one slot
 * (the standard "array move" semantics dnd-kit's sortable lists use) — a no-op for an
 * out-of-range index or when `from === to`.
 */
export function reorderTiles(tiles: Tile[], from: number, to: number): Tile[] {
  if (from === to || from < 0 || to < 0 || from >= tiles.length || to >= tiles.length) return tiles;
  const next = tiles.slice();
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved ?? null);
  return next;
}

/**
 * swapTiles exchanges the tiles at `a` and `b`, leaving every other tile where it is. Unlike
 * reorderTiles it never shifts intermediate tiles, so with tiles keyed by camera only the two
 * swapped cameras change cell (and none of them reconnects) — a no-op for an out-of-range
 * index or when `a === b`.
 */
export function swapTiles(tiles: Tile[], a: number, b: number): Tile[] {
  if (a === b || a < 0 || b < 0 || a >= tiles.length || b >= tiles.length) return tiles;
  const next = tiles.slice();
  next[a] = tiles[b] ?? null;
  next[b] = tiles[a] ?? null;
  return next;
}

/**
 * placeCameraUnique places a camera at `index` without ever showing it twice: when it is
 * already in the grid it swaps with the tile at `index` (keeping its stream and quality),
 * otherwise it behaves like placeCameraAt.
 */
export function placeCameraUnique(tiles: Tile[], index: number, cameraId: string, quality: Quality): Tile[] {
  const at = tiles.findIndex((t) => cameraIdOf(t) === cameraId);
  return at >= 0 ? swapTiles(tiles, at, index) : placeCameraAt(tiles, index, cameraId, quality);
}

/** duplicateTileIndexes lists the cells that repeat a camera already shown in an earlier cell. */
export function duplicateTileIndexes(tiles: Tile[]): Set<number> {
  const seen = new Set<string>();
  const duplicates = new Set<number>();
  tiles.forEach((t, i) => {
    const id = cameraIdOf(t);
    if (!id) return;
    if (seen.has(id)) duplicates.add(i);
    else seen.add(id);
  });
  return duplicates;
}

// Drag-and-drop ids: a camera list item drags as "camera:<id>", a grid tile drags/drops as
// "tile:<index>". Kept as plain string ids (dnd-kit accepts string | number) so the drag
// resolution logic below stays framework-free and independently testable.
const TILE_PREFIX = "tile:";
const CAMERA_PREFIX = "camera:";

export function tileDragId(index: number): string {
  return `${TILE_PREFIX}${index}`;
}

export function cameraDragId(cameraId: string): string {
  return `${CAMERA_PREFIX}${cameraId}`;
}

function tileIndexFromDragId(id: string | number | null): number | null {
  if (id === null) return null;
  const s = String(id);
  if (!s.startsWith(TILE_PREFIX)) return null;
  const n = Number(s.slice(TILE_PREFIX.length));
  return Number.isInteger(n) ? n : null;
}

function cameraIdFromDragId(id: string | number): string | null {
  const s = String(id);
  return s.startsWith(CAMERA_PREFIX) ? s.slice(CAMERA_PREFIX.length) : null;
}

export type DragResolution =
  | { type: "place"; index: number; cameraId: string }
  | { type: "reorder"; from: number; to: number }
  | { type: "fill-server"; serverId: string }
  | { type: "fill-folder"; folderId: string }
  | null;

/** The whole live grid, so a server or folder can be dropped on the gap between tiles. */
export const LIVE_GRID_DROP_ID = "live-grid";

const SERVER_DRAG_PREFIX = "tserver:";
const FOLDER_DRAG_PREFIX = "tfolder:";

function stripPrefix(id: string | number, prefix: string): string | null {
  const s = String(id);
  return s.startsWith(prefix) ? s.slice(prefix.length) : null;
}

/**
 * resolveDragEnd turns a dnd-kit drag's (active, over) ids into what happened: placing a
 * dragged camera list item into a tile, or reordering two tiles. Pure and DOM-independent so
 * the drop logic is testable without simulating real pointer/keyboard geometry.
 */
export function resolveDragEnd(activeId: string | number, overId: string | number | null): DragResolution {
  const toIndex = tileIndexFromDragId(overId);
  const overGrid = overId !== null && (String(overId) === LIVE_GRID_DROP_ID || toIndex !== null);
  if (overGrid) {
    const serverId = stripPrefix(activeId, SERVER_DRAG_PREFIX);
    if (serverId) return { type: "fill-server", serverId };
    const folderId = stripPrefix(activeId, FOLDER_DRAG_PREFIX);
    if (folderId) return { type: "fill-folder", folderId };
  }
  if (toIndex === null) return null;
  const cameraId = cameraIdFromDragId(activeId);
  if (cameraId !== null) return { type: "place", index: toIndex, cameraId };
  const fromIndex = tileIndexFromDragId(activeId);
  if (fromIndex !== null && fromIndex !== toIndex) return { type: "reorder", from: fromIndex, to: toIndex };
  return null;
}

/**
 * layoutForCount picks the smallest layout that can show `count` cameras.
 * When none is large enough it returns the largest one.
 */
export function layoutForCount(layouts: { columns: number; rows: number }[], count: number): { columns: number; rows: number } {
  const sorted = [...layouts].sort((a, b) => a.columns * a.rows - b.columns * b.rows);
  const fallback = sorted[0] ?? { columns: 1, rows: 1 };
  if (count <= 1) return fallback;
  return sorted.find((layout) => layout.columns * layout.rows >= count) ?? sorted[sorted.length - 1] ?? fallback;
}

/** tilesForCameras fills a fresh grid with cameras in order, leaving leftover cells empty. */
export function tilesForCameras(cameras: { id: string; quality: Quality }[], columns: number, rows: number): { tiles: Tile[]; skipped: number } {
  const cells = Math.max(0, columns * rows);
  const tiles: Tile[] = Array.from({ length: cells }, (_, index) => {
    const camera = cameras[index];
    return camera ? { camera_id: camera.id, quality: camera.quality } : null;
  });
  return { tiles, skipped: Math.max(0, cameras.length - cells) };
}

/**
 * placeInOpenCell puts a camera in the first empty cell of the current grid.
 * A full grid replaces the first cell. A camera already on the grid stays where it is.
 */
export function placeInOpenCell(tiles: Tile[], cameraId: string, quality: Quality): { tiles: Tile[]; index: number } {
  const existing = tiles.findIndex((tile) => cameraIdOf(tile) === cameraId);
  if (existing >= 0) return { tiles, index: existing };
  const empty = tiles.findIndex((tile) => tile === null);
  const index = empty >= 0 ? empty : 0;
  if (index >= tiles.length) return { tiles, index: -1 };
  return { tiles: placeCameraUnique(tiles, index, cameraId, quality), index };
}

export function fitTiles(tiles: Tile[], count: number): Tile[] {
  const next = tiles.slice(0, Math.max(0, count));
  while (next.length < count) next.push(null);
  return next;
}

export type StoredSelection = { columns: number; rows: number; tiles: Tile[]; panes?: Pane[] };

const STORAGE_PREFIX = "openvms.live.selection.v1";

/** liveSelectionKey scopes the persisted grid to one user within one tenant (platform users have no tenant). */
export function liveSelectionKey(tenantId: string | null, userId: string): string {
  return `${STORAGE_PREFIX}:${tenantId ?? "platform"}:${userId}`;
}

export function serializeSelection(columns: number, tiles: Tile[], rows = columns, panes?: Pane[]): string {
  return JSON.stringify(panes ? { columns, rows, tiles, panes } : { columns, rows, tiles });
}

/**
 * parseSelection turns a raw localStorage string back into a selection, dropping any camera the
 * user can no longer see (leaving its slot empty) and clamping the tile count to the grid dimensions.
 * Returns null for missing, malformed, or structurally invalid input so the caller can fall back
 * to the default grid instead of throwing.
 */
export function parseSelection(raw: string | null, validCameraIds: ReadonlySet<string>): StoredSelection | null {
  if (!raw) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;
  const record = parsed as { columns?: unknown; rows?: unknown; tiles?: unknown; panes?: unknown };
  const { columns, rows, tiles } = record;
  if (typeof columns !== "number" || !Number.isInteger(columns) || columns < 1 || !Array.isArray(tiles)) return null;
  const resolvedRows = rows === undefined ? columns : rows;
  if (typeof resolvedRows !== "number" || !Number.isInteger(resolvedRows) || resolvedRows < 1) return null;
  const cleaned: Tile[] = tiles.map((t): Tile => {
    if (!t || typeof t !== "object") return null;
    const record = t as { camera_id?: unknown; quality?: unknown; map?: unknown };
    if (record.map && typeof record.map === "object") {
      const map = record.map as { site_id?: unknown; floor_id?: unknown; name?: unknown };
      if (typeof map.site_id !== "string" || typeof map.name !== "string" || !map.name) return null;
      return { map: { site_id: map.site_id, name: map.name, floor_id: typeof map.floor_id === "string" ? map.floor_id : undefined } };
    }
    if (typeof record.camera_id !== "string" || !validCameraIds.has(record.camera_id)) return null;
    return { camera_id: record.camera_id, quality: record.quality === "main" ? "main" : "sub" };
  });
  const panes = parsePanes(columns, resolvedRows, record.panes);
  if (panes) return { columns, rows: resolvedRows, tiles: fitTiles(cleaned, panes.length), panes };
  return { columns, rows: resolvedRows, tiles: resizeTiles(cleaned, columns, resolvedRows) };
}

export type FillResult = { tiles: Tile[]; added: number; already: number; skipped: number };

/**
 * fillTiles adds cameras to the first empty cells in order. Cameras already in the grid are not
 * added twice (`already`); cameras that do not fit are counted in `skipped`.
 */
export function fillTiles(tiles: Tile[], cameras: { id: string; quality: Quality }[]): FillResult {
  const next = tiles.slice();
  const present = new Set(next.flatMap((t) => {
    const id = cameraIdOf(t);
    return id ? [id] : [];
  }));
  let added = 0;
  let already = 0;
  let skipped = 0;
  for (const c of cameras) {
    if (present.has(c.id)) {
      already++;
      continue;
    }
    const empty = next.findIndex((t) => t === null);
    if (empty < 0) {
      skipped++;
      continue;
    }
    next[empty] = { camera_id: c.id, quality: c.quality };
    present.add(c.id);
    added++;
  }
  return { tiles: next, added, already, skipped };
}

/**
 * growLayout picks the smallest layout (by cell count) that fits `needed` cameras and is larger
 * than the current grid, or the largest available one when none fits. Null when the grid cannot grow.
 */
export function growLayout(layouts: { columns: number; rows: number }[], currentCells: number, needed: number): { columns: number; rows: number } | null {
  const bigger = layouts.filter((l) => l.columns * l.rows > currentCells).sort((a, b) => a.columns * a.rows - b.columns * b.rows);
  if (bigger.length === 0) return null;
  return bigger.find((l) => l.columns * l.rows >= needed) ?? bigger[bigger.length - 1] ?? null;
}
