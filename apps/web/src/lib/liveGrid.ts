/**
 * liveGrid holds pure, framework-free logic for the Live screen's camera grid: resizing,
 * placing a camera into a slot, reordering tiles by drag, and persisting/restoring the current
 * selection to/from localStorage. Kept separate from Live.tsx so it stays trivially unit-testable
 * (no React, no browser APIs beyond the storage shape itself).
 */

export type Quality = "sub" | "main";
export type Tile = { camera_id: string; quality: Quality } | null;

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
  const at = tiles.findIndex((t) => t?.camera_id === cameraId);
  return at >= 0 ? swapTiles(tiles, at, index) : placeCameraAt(tiles, index, cameraId, quality);
}

/** duplicateTileIndexes lists the cells that repeat a camera already shown in an earlier cell. */
export function duplicateTileIndexes(tiles: Tile[]): Set<number> {
  const seen = new Set<string>();
  const duplicates = new Set<number>();
  tiles.forEach((t, i) => {
    if (!t) return;
    if (seen.has(t.camera_id)) duplicates.add(i);
    else seen.add(t.camera_id);
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

export type DragResolution = { type: "place"; index: number; cameraId: string } | { type: "reorder"; from: number; to: number } | null;

/**
 * resolveDragEnd turns a dnd-kit drag's (active, over) ids into what happened: placing a
 * dragged camera list item into a tile, or reordering two tiles. Pure and DOM-independent so
 * the drop logic is testable without simulating real pointer/keyboard geometry.
 */
export function resolveDragEnd(activeId: string | number, overId: string | number | null): DragResolution {
  const toIndex = tileIndexFromDragId(overId);
  if (toIndex === null) return null;
  const cameraId = cameraIdFromDragId(activeId);
  if (cameraId !== null) return { type: "place", index: toIndex, cameraId };
  const fromIndex = tileIndexFromDragId(activeId);
  if (fromIndex !== null && fromIndex !== toIndex) return { type: "reorder", from: fromIndex, to: toIndex };
  return null;
}

export type StoredSelection = { columns: number; rows: number; tiles: Tile[] };

const STORAGE_PREFIX = "openvms.live.selection.v1";

/** liveSelectionKey scopes the persisted grid to one user within one tenant (platform users have no tenant). */
export function liveSelectionKey(tenantId: string | null, userId: string): string {
  return `${STORAGE_PREFIX}:${tenantId ?? "platform"}:${userId}`;
}

export function serializeSelection(columns: number, tiles: Tile[], rows = columns): string {
  return JSON.stringify({ columns, rows, tiles });
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
  const { columns, rows, tiles } = parsed as { columns?: unknown; rows?: unknown; tiles?: unknown };
  if (typeof columns !== "number" || !Number.isInteger(columns) || columns < 1 || !Array.isArray(tiles)) return null;
  const resolvedRows = rows === undefined ? columns : rows;
  if (typeof resolvedRows !== "number" || !Number.isInteger(resolvedRows) || resolvedRows < 1) return null;
  const cleaned: Tile[] = tiles.map((t): Tile => {
    if (!t || typeof t !== "object") return null;
    const { camera_id, quality } = t as { camera_id?: unknown; quality?: unknown };
    if (typeof camera_id !== "string" || !validCameraIds.has(camera_id)) return null;
    return { camera_id, quality: quality === "main" ? "main" : "sub" };
  });
  return { columns, rows: resolvedRows, tiles: resizeTiles(cleaned, columns, resolvedRows) };
}
