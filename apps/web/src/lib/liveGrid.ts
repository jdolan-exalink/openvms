/**
 * liveGrid holds pure, framework-free logic for the Live screen's camera grid: resizing,
 * placing a camera into a slot, reordering tiles by drag, and persisting/restoring the current
 * selection to/from localStorage. Kept separate from Live.tsx so it stays trivially unit-testable
 * (no React, no browser APIs beyond the storage shape itself).
 */

export type Quality = "sub" | "main";
export type Tile = { camera_id: string; quality: Quality } | null;

/** resizeTiles truncates or pads tiles to exactly n*n slots, keeping existing order. */
export function resizeTiles(tiles: Tile[], n: number): Tile[] {
  const next = tiles.slice(0, n * n);
  while (next.length < n * n) next.push(null);
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

export type StoredSelection = { columns: number; tiles: Tile[] };

const STORAGE_PREFIX = "openvms.live.selection.v1";

/** liveSelectionKey scopes the persisted grid to one user within one tenant (platform users have no tenant). */
export function liveSelectionKey(tenantId: string | null, userId: string): string {
  return `${STORAGE_PREFIX}:${tenantId ?? "platform"}:${userId}`;
}

export function serializeSelection(columns: number, tiles: Tile[]): string {
  return JSON.stringify({ columns, tiles });
}

/**
 * parseSelection turns a raw localStorage string back into a selection, dropping any camera the
 * user can no longer see (leaving its slot empty) and clamping the tile count to columns*columns.
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
  const { columns, tiles } = parsed as { columns?: unknown; tiles?: unknown };
  if (typeof columns !== "number" || !Number.isInteger(columns) || columns < 1 || !Array.isArray(tiles)) return null;
  const cleaned: Tile[] = tiles.map((t): Tile => {
    if (!t || typeof t !== "object") return null;
    const { camera_id, quality } = t as { camera_id?: unknown; quality?: unknown };
    if (typeof camera_id !== "string" || !validCameraIds.has(camera_id)) return null;
    return { camera_id, quality: quality === "main" ? "main" : "sub" };
  });
  return { columns, tiles: resizeTiles(cleaned, columns) };
}
