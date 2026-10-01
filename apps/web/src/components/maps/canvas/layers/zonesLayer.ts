import type {
  ExpressionSpecification,
  FillLayerSpecification,
  GeoJSONSourceSpecification,
  LayerSpecification,
  LineLayerSpecification,
  SymbolLayerSpecification,
} from "maplibre-gl";
import type { Feature, FeatureCollection, Polygon } from "geojson";
import type { Zone, ZoneKind } from "@/lib/maps/types";

export const ZONES_SOURCE_ID = "zones";

export interface ZoneFeatureProperties {
  id: string;
  name: string;
  kind: ZoneKind;
}

/** One colour family per kind: security reads blue, perimeter amber, warning red. */
const kindColor: ExpressionSpecification = [
  "match",
  ["get", "kind"],
  "security",
  "#1683f8",
  "perimeter",
  "#f59e0b",
  "warning",
  "#ef3f46",
  "#a855f7",
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
  const zoneFill: FillLayerSpecification = {
    id: "zone-fill",
    type: "fill",
    source: ZONES_SOURCE_ID,
    paint: {
      "fill-color": kindColor,
      "fill-opacity": 0.15,
    },
  };

  const zoneOutline: LineLayerSpecification = {
    id: "zone-outline",
    type: "line",
    source: ZONES_SOURCE_ID,
    paint: {
      "line-color": kindColor,
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
      "text-color": "#ffffff",
      "text-halo-color": "#0e1523",
      "text-halo-width": 2,
    },
  };

  return [zoneFill, zoneOutline, zoneLabel];
}
