import { queryOptions } from "@tanstack/react-query";
import { api, unwrap } from "@/api/client";
import {
  DEFAULT_LAYER_PREFERENCE,
  type CameraEntity,
  type LayerPreference,
  type MapFilters,
} from "./types";

/**
 * MapUserPrefs mirrors `MapUserPrefs` from the contract. It is intentionally not the same
 * type as `LayerPreference`: a user who never saved anything has no preferences at all, and
 * the client — not the server — owns the defaults.
 */
export interface MapUserPrefs {
  layers?: Partial<LayerPreference>;
  filters?: MapFilters;
  focus_mode?: "none" | "current-site";
  hover_live?: boolean;
}

/** fetchMapUserPrefs is the raw call; queryOptions only wraps it so tests can call it directly. */
export async function fetchMapUserPrefs(): Promise<MapUserPrefs> {
  return unwrap(await api.GET("/api/v1/me/map-prefs"));
}

export const mapUserPrefsQuery = queryOptions({
  queryKey: ["maps", "prefs"],
  queryFn: fetchMapUserPrefs,
  staleTime: 5 * 60_000,
});

/** saveMapUserPrefs replaces the stored blob; it never merges with what is already there. */
export async function saveMapUserPrefs(prefs: MapUserPrefs): Promise<MapUserPrefs> {
  return unwrap(await api.PUT("/api/v1/me/map-prefs", { body: prefs }));
}

/** mergeLayers turns the partial blob of a returning user into a renderable set. */
export function mergeLayers(stored?: Partial<LayerPreference>): LayerPreference {
  return { ...DEFAULT_LAYER_PREFERENCE, ...stored };
}

const has = <T>(values: readonly T[] | undefined): values is readonly T[] =>
  Array.isArray(values) && values.length > 0;

/**
 * applyFilters narrows the cameras that get drawn. Every dimension is optional and they
 * combine with AND. Legacy status/priority preferences never hide operational inventory.
 */
export function applyFilters(cameras: CameraEntity[], filters?: MapFilters): CameraEntity[] {
  if (!filters) return cameras;
  const sites = has(filters.site_ids) ? new Set(filters.site_ids) : null;
  const cameraIds = has(filters.camera_ids) ? new Set(filters.camera_ids) : null;
  const serverIds = has(filters.server_ids) ? new Set(filters.server_ids) : null;
  const types = has(filters.camera_types) ? new Set<string>(filters.camera_types) : null;
  if (!sites && !cameraIds && !serverIds && !types) return cameras;

  return cameras.filter((camera) => {
    if (sites && !sites.has(camera.siteId)) return false;
    if (cameraIds && !cameraIds.has(camera.id)) return false;
    if (serverIds && !serverIds.has(camera.serverId ?? "")) return false;
    if (types && !types.has(camera.camera.cameraType)) return false;
    return true;
  });
}
