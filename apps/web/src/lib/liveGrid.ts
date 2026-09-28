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
