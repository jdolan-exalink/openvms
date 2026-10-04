import type * as maplibregl from "maplibre-gl";
import { readMapPalette } from "../palette";

export const FX_SOURCE_ID = "fx";

export function buildFxSource(): maplibregl.GeoJSONSourceSpecification {
  return {
    type: "geojson",
    data: {
      type: "FeatureCollection",
      features: [],
    },
  };
}

export function buildFxLayers(): maplibregl.LayerSpecification[] {
  const palette = readMapPalette();
  return [
    {
      id: "fx-ripple",
      type: "circle",
      source: FX_SOURCE_ID,
      filter: ["==", ["get", "fxType"], "ripple"],
      paint: {
        "circle-radius": ["coalesce", ["get", "radius"], 8],
        "circle-color": ["coalesce", ["get", "color"], palette.ok],
        "circle-opacity": 0,
        "circle-stroke-width": ["coalesce", ["get", "strokeWidth"], 2],
        "circle-stroke-color": ["coalesce", ["get", "color"], palette.ok],
        "circle-stroke-opacity": ["coalesce", ["get", "opacity"], 0.8],
      },
    },
    {
      id: "fx-alarm-pulse",
      type: "circle",
      source: FX_SOURCE_ID,
      filter: ["==", ["get", "fxType"], "pulse"],
      paint: {
        "circle-radius": ["coalesce", ["get", "radius"], 16],
        "circle-color": ["coalesce", ["get", "color"], palette.bad],
        "circle-opacity": ["*", ["coalesce", ["get", "opacity"], 0.5], 0.25],
        "circle-stroke-width": ["coalesce", ["get", "strokeWidth"], 2],
        "circle-stroke-color": ["coalesce", ["get", "color"], palette.bad],
        "circle-stroke-opacity": ["coalesce", ["get", "opacity"], 0.5],
      },
    },
  ];
}
