import type {
  CircleLayerSpecification,
  GeoJSONSourceSpecification,
  LayerSpecification,
  SymbolLayerSpecification,
} from "maplibre-gl";

export const CAMERAS_SOURCE_ID = "cameras";

export function buildCamerasSource(): GeoJSONSourceSpecification {
  return {
    type: "geojson",
    data: {
      type: "FeatureCollection",
      features: [],
    },
    cluster: true,
    clusterRadius: 50,
    clusterMaxZoom: 16,
    promoteId: "id",
    clusterProperties: {
      alarms: ["+", ["case", [">", ["get", "alarms"], 0], 1, 0]],
      offline: ["+", ["case", ["in", ["get", "st"], ["literal", ["offline", "unreachable"]]], 1, 0]],
      warnings: ["+", ["case", ["in", ["get", "st"], ["literal", ["degraded", "no_signal"]]], 1, 0]],
    },
  };
}

export function buildCameraLayers(): LayerSpecification[] {
  // 1. Cluster circles with worst-state color
  const clusterCircleLayer: CircleLayerSpecification = {
    id: "cam-cluster",
    type: "circle",
    source: CAMERAS_SOURCE_ID,
    filter: ["has", "point_count"],
    paint: {
      "circle-color": [
        "case",
        [">", ["get", "alarms"], 0],
        "#ef3f46", // worst: alarms
        [">", ["get", "offline"], 0],
        "#7e8a9a", // next: offline / unreachable
        [">", ["get", "warnings"], 0],
        "#f59e0b", // next: warnings / degraded
        "#1683f8", // all ok
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
      "circle-stroke-color": "#ffffff",
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
      "text-allow-overlap": true,
    },
    paint: {
      "text-color": "#ffffff",
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
      "text-color": "#ffffff",
      "text-halo-color": "#ef3f46",
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
      "circle-color": ["get", "color"],
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
      "circle-stroke-color": ["get", "color"],
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
      "circle-color": ["get", "color"],
      "circle-stroke-width": 2,
      "circle-stroke-color": "#ffffff",
    },
  };

  // 6. Unclustered camera status SDF icon
  const cameraIconLayer: SymbolLayerSpecification = {
    id: "cam-point-icon",
    type: "symbol",
    source: CAMERAS_SOURCE_ID,
    filter: ["!", ["has", "point_count"]],
    layout: {
      "icon-image": ["get", "icon"],
      "icon-size": 0.65,
      "icon-allow-overlap": true,
    },
    paint: {
      "icon-color": "#ffffff",
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
      "text-color": "#ffffff",
      "text-halo-color": "#0e1523",
      "text-halo-width": 1.5,
    },
  };

  return [
    cameraHaloLayer,
    cameraCircleLayer,
    cameraIconLayer,
    clusterCircleLayer,
    clusterCountLayer,
    clusterAlarmsBadge,
    cameraLabelLayer,
  ];
}
