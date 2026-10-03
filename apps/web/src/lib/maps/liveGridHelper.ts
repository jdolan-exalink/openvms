import { cameraIdOf, liveSelectionKey, parseSelection, placeCameraUnique, serializeSelection, type LiveMapRef, type Tile } from "@/lib/liveGrid";

export function addCameraToLiveGrid(
  tenantId: string,
  userId: string,
  cameraId: string,
  quality: "sub" | "main" = "sub",
): boolean {
  if (typeof window === "undefined") return false;
  try {
    const storage = window.localStorage;
    const key = liveSelectionKey(tenantId, userId);
    const raw = storage.getItem(key);
    // Storage is not an authorization source. Preserve its IDs here; Live restores
    // the selection against the current server-authorized camera list.
    const stored: unknown = raw ? JSON.parse(raw) : null;
    const preservedIds = new Set([cameraId]);
    if (stored && typeof stored === "object" && "tiles" in stored && Array.isArray(stored.tiles)) {
      for (const tile of stored.tiles) {
        if (tile && typeof tile === "object" && typeof tile.camera_id === "string") {
          preservedIds.add(tile.camera_id);
        }
      }
    }
    const parsed = parseSelection(raw, preservedIds);
    const tiles: Tile[] = parsed ? [...parsed.tiles] : [];

    // Find first empty tile or append
    let targetIndex = tiles.findIndex((t) => cameraIdOf(t) === cameraId);
    if (targetIndex === -1) targetIndex = tiles.findIndex((t) => t === null);
    if (targetIndex === -1) {
      targetIndex = tiles.length;
      tiles.push(null);
    }

    const nextTiles = placeCameraUnique(tiles, targetIndex, cameraId, quality);
    nextTiles[targetIndex] = { camera_id: cameraId, quality };
    const columns = parsed?.columns ?? Math.min(Math.max(1, Math.ceil(Math.sqrt(nextTiles.length))), 6);
    const rows = Math.max(parsed?.rows ?? 1, Math.ceil(nextTiles.length / columns));

    storage.setItem(key, serializeSelection(columns, nextTiles, rows));
    return true;
  } catch {
    return false;
  }
}

/** Puts a named map into the first empty Live cell of this browser, or refreshes the one already there. */
export function addMapToLiveGrid(tenantId: string | null, userId: string, map: LiveMapRef): boolean {
  if (typeof window === "undefined") return false;
  try {
    const key = liveSelectionKey(tenantId, userId);
    const raw = window.localStorage.getItem(key);
    const preservedIds = new Set<string>();
    const stored: unknown = raw ? JSON.parse(raw) : null;
    if (stored && typeof stored === "object" && "tiles" in stored && Array.isArray(stored.tiles)) {
      for (const tile of stored.tiles) {
        if (tile && typeof tile === "object" && typeof tile.camera_id === "string") preservedIds.add(tile.camera_id);
      }
    }
    const parsed = parseSelection(raw, preservedIds);
    const tiles: Tile[] = parsed ? [...parsed.tiles] : [];
    const same = (tile: Tile) => !!tile && "map" in tile && tile.map.site_id === map.site_id && (tile.map.floor_id ?? "") === (map.floor_id ?? "");
    let target = tiles.findIndex(same);
    if (target < 0) target = tiles.findIndex((tile) => tile === null);
    if (target < 0) {
      target = tiles.length;
      tiles.push(null);
    }
    tiles[target] = { map };
    const columns = parsed?.columns ?? Math.min(Math.max(1, Math.ceil(Math.sqrt(tiles.length))), 6);
    const rows = Math.max(parsed?.rows ?? 1, Math.ceil(tiles.length / columns));
    window.localStorage.setItem(key, serializeSelection(columns, tiles, rows));
    return true;
  } catch {
    return false;
  }
}
