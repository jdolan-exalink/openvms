import type {
  ExpressionSpecification,
  FillLayerSpecification,
  GeoJSONSourceSpecification,
  LayerSpecification,
  LineLayerSpecification,
} from "maplibre-gl";
import type { Feature, FeatureCollection, Polygon } from "geojson";
import type { CameraEntity } from "@/lib/maps/types";
import { type BoundingBox, buildFovCone, isPointInBounds } from "@/lib/maps/geo";
import { STATE_COLORS } from "@/lib/maps/entityIndex";
import { readMapPalette } from "../palette";

export const FOV_SOURCE_ID = "fov";

export interface FovFeatureProperties {
  id: string;
  name: string;
  color: string;
  /** 1 when the camera has active alarms; the layer picks the themed color from it. */
  alarm: number;
  camera_type: string;
}

export function camerasToFovCollection(
  cameras: CameraEntity[],
  bounds?: BoundingBox,
): FeatureCollection<Polygon, FovFeatureProperties> {
  const features: Feature<Polygon, FovFeatureProperties>[] = [];

  for (const cam of cameras) {
    if (cam.position.kind !== "geo") continue;

    const center: [number, number] = [cam.position.lng, cam.position.lat];
    if (bounds && !isPointInBounds(center, bounds)) {
      continue;
    }

    const { bearingDeg, fovDeg, rangeM } = cam.camera;
    const coneGeom = buildFovCone(center, bearingDeg, fovDeg, rangeM);
    if (!coneGeom) continue;

    const color = cam.activeAlarms > 0 ? STATE_COLORS.ALARM : STATE_COLORS.ONLINE;

    features.push({
      type: "Feature",
      id: cam.id,
      geometry: coneGeom,
      properties: {
        id: cam.id,
        name: cam.name,
        color,
        alarm: cam.activeAlarms > 0 ? 1 : 0,
        camera_type: cam.camera.cameraType,
      },
    });
  }

  return {
    type: "FeatureCollection",
    features,
  };
}

export function buildFovSource(
  cameras: CameraEntity[] = [],
  bounds?: BoundingBox,
): GeoJSONSourceSpecification {
  return {
    type: "geojson",
    data: camerasToFovCollection(cameras, bounds),
    promoteId: "id",
  };
}

export function buildFovLayers(coverageEnabled = true): LayerSpecification[] {
  const visibility = coverageEnabled ? "visible" : "none";
  const palette = readMapPalette();
  const fovColor: ExpressionSpecification = ["case", [">", ["get", "alarm"], 0], palette.bad, palette.ok];

  // 1. FOV Cone Fill
  const fovFill: FillLayerSpecification = {
    id: "fov-fill",
    type: "fill",
    source: FOV_SOURCE_ID,
    minzoom: 13,
    layout: {
      visibility,
    },
    paint: {
      "fill-color": fovColor,
      "fill-opacity": [
        "case",
        ["boolean", ["feature-state", "selected"], false],
        0.35,
        ["boolean", ["feature-state", "hover"], false],
        0.25,
        0.12,
      ],
    },
  };

  // 2. FOV Cone Outline
  const fovOutline: LineLayerSpecification = {
    id: "fov-outline",
    type: "line",
    source: FOV_SOURCE_ID,
    minzoom: 13,
    layout: {
      visibility,
    },
    paint: {
      "line-color": fovColor,
      "line-width": [
        "case",
        ["boolean", ["feature-state", "selected"], false],
        2.5,
        ["boolean", ["feature-state", "hover"], false],
        2,
        1,
      ],
      "line-opacity": 0.7,
    },
  };

  return [fovFill, fovOutline];
}
