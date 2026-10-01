import { describe, expect, it } from "vitest";
import {
  buildFovLayers,
  buildFovSource,
  camerasToFovCollection,
  FOV_SOURCE_ID,
} from "./fovLayer";
import type { CameraEntity } from "@/lib/maps/types";
import type { BoundingBox } from "@/lib/maps/geo";

describe("fovLayer", () => {
  const testCameras: CameraEntity[] = [
    {
      id: "cam-directional",
      type: "camera",
      siteId: "site-1",
      name: "Cámara Portón",
      status: "online",
      activeAlarms: 0,
      position: { kind: "geo", lat: -34.6037, lng: -58.3816 },
      camera: {
        bearingDeg: 45,
        fovDeg: 60,
        rangeM: 40,
        cameraType: "fixed",
        ptz: false,
        lpr: false,
      },
      metadata: {},
    },
    {
      id: "cam-unknown-bearing",
      type: "camera",
      siteId: "site-1",
      name: "Domo Sin Orientación",
      status: "online",
      activeAlarms: 0,
      position: { kind: "geo", lat: -34.604, lng: -58.382 },
      camera: {
        bearingDeg: null, // Direction unknown -> no cone
        fovDeg: 90,
        rangeM: 50,
        cameraType: "dome",
        ptz: true,
        lpr: false,
      },
      metadata: {},
    },
    {
      id: "cam-outside-bounds",
      type: "camera",
      siteId: "site-1",
      name: "Cámara Lejana",
      status: "online",
      activeAlarms: 0,
      position: { kind: "geo", lat: -34.9, lng: -58.9 },
      camera: {
        bearingDeg: 180,
        fovDeg: 70,
        rangeM: 30,
        cameraType: "fixed",
        ptz: false,
        lpr: false,
      },
      metadata: {},
    },
  ];

  it("converts valid cameras to FOV cone features and skips cameras without bearing", () => {
    const fc = camerasToFovCollection(testCameras);
    expect(fc.type).toBe("FeatureCollection");
    // cam-directional and cam-outside-bounds have bearings; cam-unknown-bearing is skipped
    expect(fc.features).toHaveLength(2);
    expect(fc.features.map((f) => f.id)).toContain("cam-directional");
    expect(fc.features.map((f) => f.id)).not.toContain("cam-unknown-bearing");
  });

  it("culls cameras outside the specified viewport bounding box", () => {
    const bbox: BoundingBox = {
      west: -58.4,
      south: -34.7,
      east: -58.3,
      north: -34.5,
    };
    const fc = camerasToFovCollection(testCameras, bbox);
    expect(fc.features).toHaveLength(1);
    expect(fc.features[0]?.id).toBe("cam-directional");
  });

  it("builds FOV GeoJSON source with promoteId 'id'", () => {
    const src = buildFovSource(testCameras);
    expect(src.type).toBe("geojson");
    expect(src.promoteId).toBe("id");
  });

  it("builds FOV layers with minzoom 13 and coverage visibility toggle", () => {
    const visibleLayers = buildFovLayers(true);
    expect(visibleLayers).toHaveLength(2);

    const fillLayer = visibleLayers.find((l) => l.id === "fov-fill");
    const outlineLayer = visibleLayers.find((l) => l.id === "fov-outline");

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect((fillLayer as any).source).toBe(FOV_SOURCE_ID);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect((outlineLayer as any).source).toBe(FOV_SOURCE_ID);
    expect(fillLayer?.minzoom).toBe(13);
    expect(outlineLayer?.minzoom).toBe(13);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect((fillLayer as any)?.layout?.visibility).toBe("visible");

    const hiddenLayers = buildFovLayers(false);
    const hiddenFill = hiddenLayers.find((l) => l.id === "fov-fill");
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect((hiddenFill as any)?.layout?.visibility).toBe("none");
  });
});
