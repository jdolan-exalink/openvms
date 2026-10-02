import type { Map as MapLibreMap } from "maplibre-gl";
import { buildCameraLayers } from "./cameraLayers";
import { buildFovLayers } from "./fovLayer";
import { buildFxLayers } from "./fxLayers";
import { buildSiteLayers } from "./sitesLayer";
import { buildZonesLayers } from "./zonesLayer";

/**
 * Every layer the Maps feature owns, grouped by the preference that controls it. Keeping the
 * ids in one place lets the layer builders and the visibility groups stay in sync — a new
 * layer that nobody groups would otherwise stay on screen no matter what the user toggles.
 * Zones are always on: they carry no preference key of their own but still belong to a
 * group so ownedLayerIds() covers them.
 */
export const LAYER_GROUPS = {
  cameras: ["cam-cluster", "cam-cluster-count", "cam-cluster-icon", "cam-cluster-badge-alarms", "cam-point-halo", "cam-point-circle", "cam-point-icon", "cam-offline-badge", "cam-point-badge-alarms", "cam-label"],
  sites: ["site-health-ring", "site-point", "site-label", "site-icon", "site-offline-badge"],
  zones: ["zone-fill", "zone-outline", "zone-label"],
  coverage: ["fov-fill", "fov-outline"],
  alarmFx: ["fx-alarm-pulse"],
  detectionFx: ["fx-ripple"],
} as const;

export type LayerGroup = keyof typeof LAYER_GROUPS;

/** ownedLayerIds lists every layer id the feature renders, in group order. */
export function ownedLayerIds(): string[] {
  return Object.values(LAYER_GROUPS).flat();
}

/** builtLayerIds lists every layer id the layer builders declare. */
export function builtLayerIds(): string[] {
  return [
    ...buildCameraLayers().map((l) => l.id),
    ...buildFovLayers().map((l) => l.id),
    ...buildSiteLayers().map((l) => l.id),
    ...buildZonesLayers().map((l) => l.id),
    ...buildFxLayers().map((l) => l.id),
  ];
}

/**
 * applyLayerVisibility hides or shows the groups the user turned off. Layers that are not on
 * the style yet are skipped: the effect re-runs once the style loads.
 */
export function applyLayerVisibility(
  map: Pick<MapLibreMap, "getLayer" | "setLayoutProperty">,
  visibility: Partial<Record<LayerGroup, boolean>>,
): void {
  for (const [group, ids] of Object.entries(LAYER_GROUPS) as [LayerGroup, readonly string[]][]) {
    const enabled = visibility[group];
    if (enabled === undefined) continue;
    const value = enabled ? "visible" : "none";
    for (const id of ids) {
      if (map.getLayer(id)) map.setLayoutProperty(id, "visibility", value);
    }
  }
}

/** Sources may survive a style update while individual layers do not. Repair independently,
 * then keep device identity above polygons/coverage and effects above device markers. */
export function reconcileOwnedLayers(
  map: Pick<MapLibreMap, "getLayer" | "addLayer" | "moveLayer">,
  coverage = true,
): void {
  const ordered = [
    ...buildZonesLayers(), ...buildFovLayers(coverage), ...buildSiteLayers(),
    ...buildCameraLayers(), ...buildFxLayers(),
  ];
  for (const layer of ordered) {
    if (!map.getLayer(layer.id)) map.addLayer(layer);
    map.moveLayer(layer.id);
  }
}
