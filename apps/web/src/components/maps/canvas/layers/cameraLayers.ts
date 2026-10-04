import type {
  CircleLayerSpecification,
  GeoJSONSourceSpecification,
  LayerSpecification,
  SymbolLayerSpecification,
} from "maplibre-gl";
import { connectivityColorExpression, readMapPalette } from "../palette";

export const CAMERAS_SOURCE_ID = "cameras";

/** Clustering starts only once the map is fairly far out, so nearby cameras stay editable longer. */
export const CAMERA_CLUSTER_RADIUS = 28;
export const CAMERA_CLUSTER_MAX_ZOOM = 11;

export function buildCamerasSource(options?: { cluster?: boolean }): GeoJSONSourceSpecification {
  const cluster = options?.cluster !== false;
  const source: GeoJSONSourceSpecification = {
    type: "geojson",
    data: {
      type: "FeatureCollection",
      features: [],
    },
    promoteId: "id",
  };
  if (!cluster) return source;
  source.cluster = true;
  source.clusterRadius = CAMERA_CLUSTER_RADIUS;
  source.clusterMaxZoom = CAMERA_CLUSTER_MAX_ZOOM;
  source.clusterProperties = {
    alarms: ["+", ["case", [">", ["get", "alarms"], 0], 1, 0]],
    offline: ["+", ["case", ["in", ["get", "st"], ["literal", ["offline", "unreachable", "no_signal"]]], 1, 0]],
    warnings: ["+", ["case", ["in", ["get", "st"], ["literal", ["degraded"]]], 1, 0]],
  };
  return source;
}

export function buildCameraLayers(): LayerSpecification[] {
  const palette = readMapPalette();
  const stateColor = connectivityColorExpression(palette);
  // 1. Cluster circles with worst-state color
  const clusterCircleLayer: CircleLayerSpecification = {
    id: "cam-cluster",
    type: "circle",
    source: CAMERAS_SOURCE_ID,
    filter: ["has", "point_count"],
    paint: {
      "circle-color": [
        "case",
        [">", ["get", "offline"], 0],
        palette.muted, // offline / unreachable
        [">", ["get", "warnings"], 0],
        palette.warn, // warnings / degraded
        palette.ok, // all online
      ],
      "circle-radius": [
        "step",
        ["get", "point_count"],
        16,
        10,
        20,
        50,
        26,
        100,
        32,
      ],
      "circle-stroke-width": 2,
      "circle-stroke-color": palette.ring,
      "circle-stroke-opacity": 0.9,
    },
  };

  // 2. Cluster camera count text
  const clusterCountLayer: SymbolLayerSpecification = {
    id: "cam-cluster-count",
    type: "symbol",
    source: CAMERAS_SOURCE_ID,
    filter: ["has", "point_count"],
    layout: {
      "text-field": "{point_count_abbreviated}",
      "text-size": 12,
      "text-offset": [0, 0.65],
      "text-allow-overlap": true,
    },
    paint: {
      "text-color": palette.onMarker,
    },
  };

  // 3. Cluster alarms badge (offset top-right badge)
  const clusterAlarmsBadge: SymbolLayerSpecification = {
    id: "cam-cluster-badge-alarms",
    type: "symbol",
    source: CAMERAS_SOURCE_ID,
    filter: ["all", ["has", "point_count"], [">", ["get", "alarms"], 0]],
    layout: {
      "text-field": ["concat", "!", ["to-string", ["get", "alarms"]]],
      "text-size": 10,
      "text-offset": [1.4, -1.4],
      "text-allow-overlap": true,
    },
    paint: {
      "text-color": palette.onMarker,
      "text-halo-color": palette.bad,
      "text-halo-width": 4,
    },
  };

  // 4. Unclustered camera selection/hover halo
  const cameraHaloLayer: CircleLayerSpecification = {
    id: "cam-point-halo",
    type: "circle",
    source: CAMERAS_SOURCE_ID,
    filter: ["!", ["has", "point_count"]],
    paint: {
      "circle-radius": [
        "case",
        ["boolean", ["feature-state", "selected"], false],
        22,
        ["boolean", ["feature-state", "hover"], false],
        18,
        0,
      ],
      "circle-color": [
        "case",
        ["boolean", ["feature-state", "selected"], false],
        palette.primary,
        stateColor,
      ],
      "circle-opacity": [
        "case",
        ["boolean", ["feature-state", "selected"], false],
        0.35,
        ["boolean", ["feature-state", "hover"], false],
        0.2,
        0,
      ],
      "circle-stroke-width": [
        "case",
        ["boolean", ["feature-state", "selected"], false],
        2,
        0,
      ],
      "circle-stroke-color": palette.primary,
    },
  };

  // 5. Unclustered camera base circle
  const cameraCircleLayer: CircleLayerSpecification = {
    id: "cam-point-circle",
    type: "circle",
    source: CAMERAS_SOURCE_ID,
    filter: ["!", ["has", "point_count"]],
    paint: {
      "circle-radius": 12,
      "circle-color": stateColor,
      "circle-stroke-width": 2,
      "circle-stroke-color": palette.ring,
    },
  };

  // 6. Unclustered camera status SDF icon
  const cameraIconLayer: SymbolLayerSpecification = {
    id: "cam-point-icon",
    type: "symbol",
    source: CAMERAS_SOURCE_ID,
    filter: ["!", ["has", "point_count"]],
    layout: {
      "icon-image": "cam-normal",
      "icon-size": 0.65,
      "icon-allow-overlap": true,
    },
    paint: {
      "icon-color": palette.onMarker,
    },
  };

  // 7. Unclustered camera name label (visible at zoom >= 16)
  const cameraLabelLayer: SymbolLayerSpecification = {
    id: "cam-label",
    type: "symbol",
    source: CAMERAS_SOURCE_ID,
    minzoom: 16,
    filter: ["!", ["has", "point_count"]],
    layout: {
      "text-field": ["get", "name"],
      "text-offset": [0, 1.8],
      "text-anchor": "top",
      "text-size": 11,
      "text-max-width": 10,
    },
    paint: {
      "text-color": palette.label,
      "text-halo-color": palette.labelHalo,
      "text-halo-width": 1.5,
    },
  };

  const clusterIcon: SymbolLayerSpecification = {
    id: "cam-cluster-icon", type: "symbol", source: CAMERAS_SOURCE_ID,
    filter: ["has", "point_count"],
    layout: { "icon-image": "cam-normal", "icon-size": 0.6, "icon-offset": [0, -9], "icon-allow-overlap": true },
    paint: { "icon-color": palette.onMarker },
  };
  const offlineBadge: SymbolLayerSpecification = {
    id: "cam-offline-badge", type: "symbol", source: CAMERAS_SOURCE_ID,
    filter: ["all", ["!", ["has", "point_count"]], ["in", ["get", "st"], ["literal", ["offline", "unreachable", "no_signal"]]]],
    layout: { "icon-image": "status-offline", "icon-size": 0.45, "icon-offset": [20, -20], "icon-allow-overlap": true },
    paint: { "icon-color": palette.bad, "icon-halo-color": palette.ring, "icon-halo-width": 1.5 },
  };

  const cameraAlarmsBadge: SymbolLayerSpecification = {
    id: "cam-point-badge-alarms", type: "symbol", source: CAMERAS_SOURCE_ID,
    filter: ["all", ["!", ["has", "point_count"]], [">", ["get", "alarms"], 0]],
    layout: {
      "text-field": ["concat", "!", ["to-string", ["get", "alarms"]]],
      "text-size": 10, "text-offset": [-1.4, -1.4], "text-allow-overlap": true,
    },
    paint: { "text-color": palette.onMarker, "text-halo-color": palette.bad, "text-halo-width": 3 },
  };

  return [
    cameraHaloLayer,
    cameraCircleLayer,
    cameraIconLayer,
    offlineBadge,
    cameraAlarmsBadge,
    clusterCircleLayer,
    clusterCountLayer,
    clusterIcon,
    clusterAlarmsBadge,
    cameraLabelLayer,
  ];
}
