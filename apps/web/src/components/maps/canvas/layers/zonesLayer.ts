import type {
  ExpressionSpecification,
  FillLayerSpecification,
  GeoJSONSourceSpecification,
  LayerSpecification,
  CircleLayerSpecification,
  LineLayerSpecification,
  SymbolLayerSpecification,
} from "maplibre-gl";
import type { Feature, FeatureCollection, LineString, Point, Polygon } from "geojson";
import { ZONE_KIND_COLOR } from "@/lib/maps/zoneDraft";
import type { Zone, ZoneKind } from "@/lib/maps/types";
import { readMapPalette } from "../palette";

export const ZONES_SOURCE_ID = "zones";

export interface ZoneFeatureProperties {
  id: string;
  name: string;
  kind: ZoneKind;
  color?: string;
}

/** One colour family per kind: security reads blue, perimeter amber, warning red. */
const kindColor: ExpressionSpecification = [
  "match",
  ["get", "kind"],
  "security",
  ZONE_KIND_COLOR.security,
  "perimeter",
  ZONE_KIND_COLOR.perimeter,
  "warning",
  ZONE_KIND_COLOR.warning,
  ZONE_KIND_COLOR.custom,
];

export function zonesToFeatureCollection(zones: Zone[]): FeatureCollection<Polygon, ZoneFeatureProperties> {
  const features: Feature<Polygon, ZoneFeatureProperties>[] = [];

  for (const zone of zones) {
    const ring = zone.geometry.coordinates[0];
    if (!ring || ring.length < 4) continue;
    features.push({
      type: "Feature",
      id: zone.id,
      geometry: {
        type: "Polygon",
        coordinates: [ring],
      },
      properties: {
        id: zone.id,
        name: zone.name,
        kind: zone.kind,
        ...(zone.style.color ? { color: zone.style.color } : {}),
      },
    });
  }

  return {
    type: "FeatureCollection",
    features,
  };
}

export function buildZonesSource(zones: Zone[] = []): GeoJSONSourceSpecification {
  return {
    type: "geojson",
    data: zonesToFeatureCollection(zones),
  };
}

export function buildZonesLayers(): LayerSpecification[] {
  const palette = readMapPalette();
  const zoneFill: FillLayerSpecification = {
    id: "zone-fill",
    type: "fill",
    source: ZONES_SOURCE_ID,
    paint: {
      "fill-color": ["case", ["has", "color"], ["get", "color"], kindColor],
      "fill-opacity": 0.15,
    },
  };

  const zoneOutline: LineLayerSpecification = {
    id: "zone-outline",
    type: "line",
    source: ZONES_SOURCE_ID,
    paint: {
      "line-color": ["case", ["has", "color"], ["get", "color"], kindColor],
      "line-width": 2,
      "line-opacity": 0.9,
    },
  };

  const zoneLabel: SymbolLayerSpecification = {
    id: "zone-label",
    type: "symbol",
    source: ZONES_SOURCE_ID,
    minzoom: 12,
    layout: {
      "text-field": ["get", "name"],
      "text-size": 11,
      "text-offset": [0, 0.6],
      "text-anchor": "top",
      "text-max-width": 8,
    },
    paint: {
      "text-color": palette.label,
      "text-halo-color": palette.labelHalo,
      "text-halo-width": 2,
    },
  };

  return [zoneFill, zoneOutline, zoneLabel];
}

export const ZONE_SKETCH_SOURCE_ID = "zone-sketch";

export interface ZoneSketch {
  points: Array<{ lng: number; lat: number }>;
  color: string;
  closed: boolean;
}

/** sketchToFeatureCollection draws the polygon under construction, including the first vertices. */
export function sketchToFeatureCollection(sketch?: ZoneSketch): FeatureCollection {
  const features: Feature[] = [];
  if (!sketch || sketch.points.length === 0) return { type: "FeatureCollection", features };
  const color = sketch.color;
  sketch.points.forEach((point, index) => {
    const geometry: Point = { type: "Point", coordinates: [point.lng, point.lat] };
    features.push({ type: "Feature", geometry, properties: { color, index } });
  });
  if (sketch.points.length >= 2) {
    const coordinates = sketch.points.map((point) => [point.lng, point.lat]);
    if (sketch.closed) coordinates.push([sketch.points[0]!.lng, sketch.points[0]!.lat]);
    const geometry: LineString = { type: "LineString", coordinates };
    features.push({ type: "Feature", geometry, properties: { color } });
  }
  if (sketch.points.length >= 3) {
    const ring = sketch.points.map((point) => [point.lng, point.lat]);
    ring.push([sketch.points[0]!.lng, sketch.points[0]!.lat]);
    const geometry: Polygon = { type: "Polygon", coordinates: [ring] };
    features.push({ type: "Feature", geometry, properties: { color } });
  }
  return { type: "FeatureCollection", features };
}

export function buildZoneSketchLayers(): LayerSpecification[] {
  const palette = readMapPalette();
  const fill: FillLayerSpecification = {
    id: "zone-sketch-fill",
    type: "fill",
    source: ZONE_SKETCH_SOURCE_ID,
    filter: ["==", ["geometry-type"], "Polygon"],
    paint: { "fill-color": ["get", "color"], "fill-opacity": 0.28 },
  };
  const line: LineLayerSpecification = {
    id: "zone-sketch-line",
    type: "line",
    source: ZONE_SKETCH_SOURCE_ID,
    filter: ["==", ["geometry-type"], "LineString"],
    paint: { "line-color": ["get", "color"], "line-width": 2, "line-dasharray": [1.2, 1] },
  };
  const vertex: CircleLayerSpecification = {
    id: "zone-sketch-vertex",
    type: "circle",
    source: ZONE_SKETCH_SOURCE_ID,
    filter: ["==", ["geometry-type"], "Point"],
    paint: {
      "circle-radius": 6,
      "circle-color": ["get", "color"],
      "circle-stroke-width": 2,
      "circle-stroke-color": palette.ring,
    },
  };
  return [fill, line, vertex];
}
