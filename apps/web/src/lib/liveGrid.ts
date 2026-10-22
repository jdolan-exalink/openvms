import type { Schemas } from "@/api/client";
import { parsePanes, type Pane } from "./presentations";

/**
 * liveGrid holds pure, framework-free logic for the Live screen's camera grid: resizing,
 * placing a camera into a slot, reordering tiles by drag, and persisting/restoring the current
 * selection to/from localStorage. Kept separate from Live.tsx so it stays trivially unit-testable
 * (no React, no browser APIs beyond the storage shape itself).
 */

export type Quality = "sub" | "main";
export type LiveMapRef = { site_id: string; floor_id?: string; name: string };
export type LiveVisionRef = { id: string; name: string; layout?: string };
export type CameraTile = { camera_id: string; quality: Quality; fisheye_view_id?: string };
export type MapGridTile = { map: LiveMapRef };
export type VisionGridTile = { vision: LiveVisionRef };
export type Tile = CameraTile | MapGridTile | VisionGridTile | null;

export function cameraIdOf(tile: Tile): string | undefined {
  return tile && "camera_id" in tile ? tile.camera_id : undefined;
}

export function tileIdentityKey(tile: Tile): string | undefined {
  if (!tile) return undefined;
  if (isMapTile(tile)) return `map:${tile.map.site_id}:${tile.map.floor_id ?? ""}`;
  if (isVisionTile(tile)) return `vision:${tile.vision.id}`;
  return tile.fisheye_view_id ? `${tile.camera_id}#${tile.fisheye_view_id}` : tile.camera_id;
}

export function isMapTile(tile: Tile): tile is MapGridTile {
  return !!tile && "map" in tile;
}

export function isVisionTile(tile: Tile): tile is VisionGridTile {
  return !!tile && "vision" in tile;
}

export function isCameraTile(tile: Tile): tile is CameraTile {
  return !!tile && "camera_id" in tile;
}

/** resizeTiles truncates or pads tiles to the requested grid dimensions, keeping existing order. */
export function resizeTiles(tiles: Tile[], columns: number, rows = columns): Tile[] {
  const count = columns * rows;
  const next = tiles.slice(0, count);
  while (next.length < count) next.push(null);
  return next;
}

/** placeCameraAt returns a new tiles array with the camera placed at index. */
export function placeCameraAt(tiles: Tile[], index: number, cameraId: string, quality: Quality, fisheyeViewId?: string): Tile[] {
  return tiles.map((t, i) => (i === index ? { camera_id: cameraId, quality, ...(fisheyeViewId ? { fisheye_view_id: fisheyeViewId } : {}) } : t));
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
export function placeCameraUnique(tiles: Tile[], index: number, cameraId: string, quality: Quality, fisheyeViewId?: string): Tile[] {
  const matchKey = fisheyeViewId ? `${cameraId}#${fisheyeViewId}` : cameraId;
  const at = tiles.findIndex((t) => tileIdentityKey(t) === matchKey);
  return at >= 0 ? swapTiles(tiles, at, index) : placeCameraAt(tiles, index, cameraId, quality, fisheyeViewId);
}

/** duplicateTileIndexes lists the cells that repeat a camera already shown in an earlier cell. */
export function duplicateTileIndexes(tiles: Tile[]): Set<number> {
  const seen = new Set<string>();
  const duplicates = new Set<number>();
  tiles.forEach((t, i) => {
    const key = tileIdentityKey(t);
    if (!key) return;
    if (seen.has(key)) duplicates.add(i);
    else seen.add(key);
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
  | { type: "place"; index: number; cameraId: string; fisheyeViewId?: string }
  | { type: "place-map"; index: number; map: LiveMapRef }
  | { type: "place-vision"; index: number; vision: LiveVisionRef }
  | { type: "reorder"; from: number; to: number }
  | { type: "fill-server"; serverId: string }
  | { type: "fill-site"; siteId: string }
  | { type: "fill-folder"; folderId: string }
  | null;

/** The whole live grid, so a site, server or folder can be dropped on the gap between tiles. */
export const LIVE_GRID_DROP_ID = "live-grid";

const SERVER_DRAG_PREFIX = "tserver:";
const SITE_DRAG_PREFIX = "tsite:";
const FOLDER_DRAG_PREFIX = "tfolder:";
const MAP_PREFIX = "tmap:";
const VISION_PREFIX = "tvision:";
export const FEVIEW_DRAG_PREFIX = "tfeview:";

export function feviewDragId(cameraId: string, viewId: string): string {
  return `${FEVIEW_DRAG_PREFIX}${encodeURIComponent(cameraId)}:${encodeURIComponent(viewId)}`;
}

export function feviewFromDragId(id: string | number): { cameraId: string; viewId: string } | null {
  const s = String(id);
  if (!s.startsWith(FEVIEW_DRAG_PREFIX)) return null;
  const rest = s.slice(FEVIEW_DRAG_PREFIX.length);
  const colon = rest.indexOf(":");
  if (colon <= 0) return null;
  const cameraId = decodeURIComponent(rest.slice(0, colon));
  const viewId = decodeURIComponent(rest.slice(colon + 1));
  if (!cameraId || !viewId) return null;
  return { cameraId, viewId };
}

/** mapDragId identifies a site or floor map dragged from the live sidebar onto a cell. */
export function mapDragId(map: LiveMapRef): string {
  return MAP_PREFIX + encodeURIComponent(JSON.stringify({ site_id: map.site_id, name: map.name, floor_id: map.floor_id }));
}

/** mapFromDragId reads a map drag id. Invalid ids return null. */
export function mapFromDragId(id: string | number): LiveMapRef | null {
  const raw = stripPrefix(id, MAP_PREFIX);
  if (raw === null) return null;
  try {
    const parsed = JSON.parse(decodeURIComponent(raw)) as { site_id?: unknown; name?: unknown; floor_id?: unknown };
    if (typeof parsed.site_id !== "string" || typeof parsed.name !== "string" || !parsed.name) return null;
    return { site_id: parsed.site_id, name: parsed.name, floor_id: typeof parsed.floor_id === "string" ? parsed.floor_id : undefined };
  } catch {
    return null;
  }
}

/** placeMapAt puts the map in one cell. The same map already on the grid swaps into that cell. */
export function placeMapAt(tiles: Tile[], index: number, map: LiveMapRef): Tile[] {
  if (index < 0 || index >= tiles.length) return tiles;
  const same = (tile: Tile) => isMapTile(tile) && tile.map.site_id === map.site_id && (tile.map.floor_id ?? "") === (map.floor_id ?? "");
  const at = tiles.findIndex(same);
  if (at === index) return tiles;
  if (at >= 0) return swapTiles(tiles, at, index);
  return tiles.map((tile, i) => (i === index ? { map } : tile));
}

/** visionDragId identifies a Vision AI instance dragged from the live sidebar onto a cell. */
export function visionDragId(vision: LiveVisionRef): string {
  return VISION_PREFIX + encodeURIComponent(JSON.stringify({ id: vision.id, name: vision.name, layout: vision.layout }));
}

/** visionFromDragId reads a vision drag id. Invalid ids return null. */
export function visionFromDragId(id: string | number): LiveVisionRef | null {
  const raw = stripPrefix(id, VISION_PREFIX);
  if (raw === null) return null;
  try {
    const parsed = JSON.parse(decodeURIComponent(raw)) as { id?: unknown; name?: unknown; layout?: unknown };
    if (typeof parsed.id !== "string" || typeof parsed.name !== "string" || !parsed.name) return null;
    return { id: parsed.id, name: parsed.name, layout: typeof parsed.layout === "string" ? parsed.layout : undefined };
  } catch {
    return null;
  }
}

/** placeVisionAt puts the Vision AI instance in one cell. */
export function placeVisionAt(tiles: Tile[], index: number, vision: LiveVisionRef): Tile[] {
  if (index < 0 || index >= tiles.length) return tiles;
  return tiles.map((tile, i) => (i === index ? { vision } : tile));
}

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
    const siteId = stripPrefix(activeId, SITE_DRAG_PREFIX);
    if (siteId) return { type: "fill-site", siteId };
    const folderId = stripPrefix(activeId, FOLDER_DRAG_PREFIX);
    if (folderId) return { type: "fill-folder", folderId };
  }
  if (stripPrefix(activeId, MAP_PREFIX) !== null) {
    const map = mapFromDragId(activeId);
    if (!map || toIndex === null) return null;
    return { type: "place-map", index: toIndex, map };
  }
  if (stripPrefix(activeId, VISION_PREFIX) !== null) {
    const vision = visionFromDragId(activeId);
    if (!vision || toIndex === null) return null;
    return { type: "place-vision", index: toIndex, vision };
  }
  if (toIndex === null) return null;
  const feview = feviewFromDragId(activeId);
  if (feview !== null) {
    return { type: "place", index: toIndex, cameraId: feview.cameraId, fisheyeViewId: feview.viewId };
  }
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
export function placeInOpenCell(tiles: Tile[], cameraId: string, quality: Quality, fisheyeViewId?: string): { tiles: Tile[]; index: number } {
  const matchKey = fisheyeViewId ? `${cameraId}#${fisheyeViewId}` : cameraId;
  const existing = tiles.findIndex((tile) => tileIdentityKey(tile) === matchKey);
  if (existing >= 0) return { tiles, index: existing };
  const empty = tiles.findIndex((tile) => tile === null);
  const index = empty >= 0 ? empty : 0;
  if (index >= tiles.length) return { tiles, index: -1 };
  return { tiles: placeCameraUnique(tiles, index, cameraId, quality, fisheyeViewId), index };
}

/**
 * placeVisionInOpenCell puts a Vision AI instance in the first empty cell of the current grid.
 * If already placed, returns the existing cell index without duplicating.
 * A full grid replaces the first cell.
 */
export function placeVisionInOpenCell(tiles: Tile[], vision: LiveVisionRef): { tiles: Tile[]; index: number } {
  const matchKey = `vision:${vision.id}`;
  const existing = tiles.findIndex((tile) => tileIdentityKey(tile) === matchKey);
  if (existing >= 0) return { tiles, index: existing };
  const empty = tiles.findIndex((tile) => tile === null);
  const index = empty >= 0 ? empty : 0;
  if (index >= tiles.length) return { tiles, index: -1 };
  return { tiles: placeVisionAt(tiles, index, vision), index };
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
    const record = t as { camera_id?: unknown; quality?: unknown; map?: unknown; vision?: unknown };
    if (record.map && typeof record.map === "object") {
      const map = record.map as { site_id?: unknown; floor_id?: unknown; name?: unknown };
      if (typeof map.site_id !== "string" || typeof map.name !== "string" || !map.name) return null;
      return { map: { site_id: map.site_id, name: map.name, floor_id: typeof map.floor_id === "string" ? map.floor_id : undefined } };
    }
    if (record.vision && typeof record.vision === "object") {
      const v = record.vision as { id?: unknown; name?: unknown; layout?: unknown };
      if (typeof v.id === "string" && typeof v.name === "string" && v.name) {
        return { vision: { id: v.id, name: v.name, layout: typeof v.layout === "string" ? v.layout : undefined } };
      }
      return null;
    }
    if (typeof record.camera_id !== "string" || !validCameraIds.has(record.camera_id)) return null;
    const feViewId = typeof (record as { fisheye_view_id?: unknown }).fisheye_view_id === "string" ? (record as { fisheye_view_id: string }).fisheye_view_id : undefined;
    return { camera_id: record.camera_id, quality: record.quality === "main" ? "main" : "sub", ...(feViewId ? { fisheye_view_id: feViewId } : {}) };
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

/**
 * tilesForFisheyeMultiView generates tiles for populating an empty grid with a fisheye camera:
 * - Tile 0: real camera view (original stream, no fisheye_view_id)
 * - Tiles 1..N: the virtual fixed views (e.g. 3 of 120° or 4 quadrants)
 */
export function tilesForFisheyeMultiView(
  cameraId: string,
  virtualViews: { id: string }[],
  quality: Quality = "sub",
  totalSlots: number,
): Tile[] {
  const cells = Math.max(totalSlots, 1 + virtualViews.length);
  const result: Tile[] = Array.from({ length: cells }, () => null);
  // Tile 0: real view
  result[0] = { camera_id: cameraId, quality };
  // Subsequent tiles: virtual views
  virtualViews.forEach((v, idx) => {
    if (idx + 1 < result.length) {
      result[idx + 1] = { camera_id: cameraId, quality, fisheye_view_id: v.id };
    }
  });
  return result;
}

/**
 * cleanFisheyeVirtualViewTiles clears virtual views of a fisheye camera from the grid
 * when the camera is switched back to a single view mode.
 * If all non-null tiles in the grid belonged to this camera and resetIfOnlyThisCamera is true,
 * it returns shouldResetGridToSingle: true and a single 1x1 tile of the root camera.
 */
export function cleanFisheyeVirtualViewTiles(
  currentTiles: Tile[],
  cameraId: string,
  options?: { resetIfOnlyThisCamera?: boolean; defaultQuality?: Quality },
): { tiles: Tile[]; shouldResetGridToSingle: boolean; changed: boolean } {
  const hasVirtualViews = currentTiles.some(
    (t) => isCameraTile(t) && t.camera_id === cameraId && Boolean(t.fisheye_view_id),
  );

  if (!hasVirtualViews) {
    return { tiles: currentTiles, shouldResetGridToSingle: false, changed: false };
  }

  const onlyThisCamera = currentTiles.every(
    (t) => t === null || (isCameraTile(t) && t.camera_id === cameraId),
  );

  if (onlyThisCamera && options?.resetIfOnlyThisCamera) {
    const quality = options.defaultQuality ?? "main";
    return {
      tiles: [{ camera_id: cameraId, quality }],
      shouldResetGridToSingle: true,
      changed: true,
    };
  }

  const next = currentTiles.map((t) => {
    if (isCameraTile(t) && t.camera_id === cameraId && t.fisheye_view_id) {
      return null;
    }
    return t;
  });

  return {
    tiles: next,
    shouldResetGridToSingle: false,
    changed: true,
  };
}

export type ViewCell = Schemas["ViewCell"];

/**
 * Saved-view cells for the grid. Cameras and virtual (vision) tiles are kept; map tiles are not
 * part of the saved-view contract and become empty cells.
 */
/** Mirrors the server rule for ViewVisionRef ids (internal/media visionIDPattern). */
export const VISION_TILE_ID = /^visionai:[a-z0-9-]{1,40}$/;
const VISION_TILE_NAME_MAX = 100;

export function tilesToViewCells(tiles: readonly Tile[]): ViewCell[] {
  return tiles.map((t): ViewCell => {
    if (t && "camera_id" in t) return { camera_id: t.camera_id, quality: t.quality };
    // A tile the server would reject becomes empty instead of failing the whole save.
    if (isVisionTile(t) && VISION_TILE_ID.test(t.vision.id)) {
      return { vision: { id: t.vision.id, name: t.vision.name.slice(0, VISION_TILE_NAME_MAX) }, quality: "sub" };
    }
    return { quality: "sub" };
  });
}

/** Grid tiles for saved-view cells (inverse of tilesToViewCells). */
export function viewCellsToTiles(cells: readonly ViewCell[]): Tile[] {
  return cells.map((c): Tile => {
    if (c.camera_id) return { camera_id: c.camera_id, quality: c.quality ?? "sub" };
    if (c.vision) return { vision: { id: c.vision.id, name: c.vision.name } };
    return null;
  });
}
