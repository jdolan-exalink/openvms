import type {
  CircleLayerSpecification,
  GeoJSONSourceSpecification,
  LayerSpecification,
  SymbolLayerSpecification,
} from "maplibre-gl";
import type { Feature, FeatureCollection, Point } from "geojson";
import type { Site } from "@/lib/maps/types";
import { readMapPalette } from "../palette";

export const SITES_SOURCE_ID = "sites";

export interface SiteFeatureProperties {
  id: string;
  name: string;
  camera_count: number;
  online: number;
  offline: number;
  alarms: number;
  severity: "OK" | "WARNING" | "CRITICAL";
}

export function sitesToFeatureCollection(sites: Site[]): FeatureCollection<Point, SiteFeatureProperties> {
  const features: Feature<Point, SiteFeatureProperties>[] = [];

  for (const s of sites) {
    if (!s.center) continue;
    features.push({
      type: "Feature",
      id: s.id,
      geometry: {
        type: "Point",
        coordinates: [s.center.lng, s.center.lat],
      },
      properties: {
        id: s.id,
        name: s.name,
        camera_count: s.cameraCount ?? 0,
        online: s.health?.online ?? 0,
        offline: s.health?.offline ?? 0,
        alarms: s.health?.activeAlarms ?? 0,
        severity: s.health?.severity ?? "OK",
      },
    });
  }

  return {
    type: "FeatureCollection",
    features,
  };
}

export function buildSitesSource(sites: Site[] = []): GeoJSONSourceSpecification {
  return {
    type: "geojson",
    data: sitesToFeatureCollection(sites),
  };
}

export function buildSiteLayers(): LayerSpecification[] {
  const palette = readMapPalette();
  // 1. Health ring around site
  const siteHealthRing: CircleLayerSpecification = {
    id: "site-health-ring",
    type: "circle",
    source: SITES_SOURCE_ID,
    paint: {
      "circle-radius": 18,
      "circle-color": "transparent",
      "circle-stroke-width": 3,
      "circle-stroke-color": [
        "case",
        ["==", ["get", "severity"], "CRITICAL"],
        palette.bad,
        ["==", ["get", "severity"], "WARNING"],
        palette.warn,
        palette.ok,
      ],
    },
  };

  // 2. Central site circle
  const sitePoint: CircleLayerSpecification = {
    id: "site-point",
    type: "circle",
    source: SITES_SOURCE_ID,
    paint: {
      "circle-radius": 14,
      "circle-color": ["case", [">", ["get", "online"], 0], palette.ok, [">", ["get", "offline"], 0], palette.muted, palette.primary],
      "circle-stroke-width": 2,
      "circle-stroke-color": palette.ring,
    },
  };

  // 3. Site label
  const siteLabel: SymbolLayerSpecification = {
    id: "site-label",
    type: "symbol",
    source: SITES_SOURCE_ID,
    layout: {
      "text-field": ["get", "name"],
      "text-font": ["Noto Sans Regular"],
      "text-offset": [0, 2],
      "text-anchor": "top",
      "text-size": 12,
      "text-max-width": 10,
    },
    paint: {
      "text-color": palette.label,
      "text-halo-color": palette.labelHalo,
      "text-halo-width": 2,
    },
  };

  const siteIcon: SymbolLayerSpecification = {
    id: "site-icon", type: "symbol", source: SITES_SOURCE_ID,
    layout: { "icon-image": "site-building", "icon-size": 0.7, "icon-allow-overlap": true },
    paint: { "icon-color": palette.onMarker },
  };
  const offlineBadge: SymbolLayerSpecification = {
    id: "site-offline-badge", type: "symbol", source: SITES_SOURCE_ID,
    filter: ["all", ["==", ["get", "online"], 0], [">", ["get", "offline"], 0]],
    layout: { "icon-image": "status-offline", "icon-size": 0.45, "icon-offset": [22, -22], "icon-allow-overlap": true },
    paint: { "icon-color": palette.bad, "icon-halo-color": palette.ring, "icon-halo-width": 1.5 },
  };
  return [siteHealthRing, sitePoint, siteLabel, siteIcon, offlineBadge];
}
