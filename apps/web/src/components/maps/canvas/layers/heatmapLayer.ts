import type { FeatureCollection, Point } from "geojson";
import type { LayerSpecification, SourceSpecification } from "maplibre-gl";
import type { components } from "@/api/schema";

export type AnalyticsPoint = components["schemas"]["MapAnalyticsPoint"];
export type AnalyticsResponse = components["schemas"]["MapAnalyticsResponse"];

export const HEATMAP_SOURCE_ID = "analytics-heatmap";
export const HEATMAP_LAYER_ID = "analytics-heatmap-layer";
export const HEATMAP_CIRCLES_LAYER_ID = "analytics-heatmap-circles";

export function analyticsToFeatureCollection(points: AnalyticsPoint[] = []): FeatureCollection<Point> {
  return {
    type: "FeatureCollection",
    features: points.map((p, idx) => ({
      type: "Feature",
      id: idx,
      geometry: {
        type: "Point",
        coordinates: [p.lng, p.lat],
      },
      properties: {
        camera_id: p.camera_id,
        weight: p.weight,
        count: p.count,
      },
    })),
  };
}

export function buildHeatmapSource(points: AnalyticsPoint[] = []): SourceSpecification {
  return {
    type: "geojson",
    data: analyticsToFeatureCollection(points),
  };
}

export function buildHeatmapLayers(): LayerSpecification[] {
  return [
    {
      id: HEATMAP_LAYER_ID,
      type: "heatmap",
      source: HEATMAP_SOURCE_ID,
      maxzoom: 19,
      paint: {
        "heatmap-weight": [
          "interpolate",
          ["linear"],
          ["get", "weight"],
          0,
          0,
          1,
          1,
        ],
        "heatmap-intensity": [
          "interpolate",
          ["linear"],
          ["zoom"],
          0,
          1,
          9,
          3,
          15,
          5,
        ],
        "heatmap-color": [
          "interpolate",
          ["linear"],
          ["heatmap-density"],
          0,
          "rgba(33, 102, 172, 0)",
          0.2,
          "rgb(103, 169, 207)",
          0.4,
          "rgb(209, 229, 240)",
          0.6,
          "rgb(253, 219, 199)",
          0.8,
          "rgb(239, 138, 98)",
          1,
          "rgb(178, 24, 43)",
        ],
        "heatmap-radius": [
          "interpolate",
          ["linear"],
          ["zoom"],
          0,
          2,
          9,
          20,
          15,
          35,
          18,
          50,
        ],
        "heatmap-opacity": [
          "interpolate",
          ["linear"],
          ["zoom"],
          14,
          0.85,
          18,
          0.4,
        ],
      },
    },
    {
      id: HEATMAP_CIRCLES_LAYER_ID,
      type: "circle",
      source: HEATMAP_SOURCE_ID,
      minzoom: 14,
      paint: {
        "circle-radius": [
          "interpolate",
          ["linear"],
          ["zoom"],
          14,
          ["interpolate", ["linear"], ["get", "weight"], 0, 2, 1, 6],
          18,
          ["interpolate", ["linear"], ["get", "weight"], 0, 6, 1, 16],
        ],
        "circle-color": "rgb(239, 138, 98)",
        "circle-stroke-color": "#ffffff",
        "circle-stroke-width": 1,
        "circle-opacity": [
          "interpolate",
          ["linear"],
          ["zoom"],
          14,
          0,
          15,
          0.7,
        ],
        "circle-stroke-opacity": [
          "interpolate",
          ["linear"],
          ["zoom"],
          14,
          0,
          15,
          0.8,
        ],
      },
    },
  ];
}
