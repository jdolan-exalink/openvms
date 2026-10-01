import { liveSelectionKey, parseSelection, placeCameraUnique, serializeSelection, type Tile } from "@/lib/liveGrid";

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
    let targetIndex = tiles.findIndex((t) => t?.camera_id === cameraId);
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
