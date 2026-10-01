import { describe, expect, it } from "vitest";
import {
  buildCameraLayers,
  buildCamerasSource,
  CAMERAS_SOURCE_ID,
} from "./cameraLayers";

describe("cameraLayers", () => {
  it("builds GeoJSON source specification with clusterProperties", () => {
    const source = buildCamerasSource();
    expect(source.type).toBe("geojson");
    expect(source.cluster).toBe(true);
    expect(source.clusterRadius).toBe(50);
    expect(source.clusterMaxZoom).toBe(16);
    expect(source.promoteId).toBe("id");
    expect(source.clusterProperties).toHaveProperty("alarms");
    expect(source.clusterProperties).toHaveProperty("offline");
    expect(source.clusterProperties).toHaveProperty("warnings");
  });

  it("builds camera and cluster layers with proper filters and bindings", () => {
    const layers = buildCameraLayers();
    expect(layers).toBeInstanceOf(Array);
    expect(layers.length).toBeGreaterThanOrEqual(6);

    const layerIds = layers.map((l) => l.id);
    expect(layerIds).toContain("cam-cluster");
    expect(layerIds).toContain("cam-cluster-count");
    expect(layerIds).toContain("cam-cluster-badge-alarms");
    expect(layerIds).toContain("cam-point-circle");
    expect(layerIds).toContain("cam-point-icon");
    expect(layerIds).toContain("cam-label");

    // All layers must use the cameras source
    for (const l of layers) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      expect((l as any).source).toBe(CAMERAS_SOURCE_ID);
    }

    // Cluster layers must have point_count filter
    const clusterLayer = layers.find((l) => l.id === "cam-cluster");
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect((clusterLayer as any)?.filter).toEqual(["has", "point_count"]);

    // Unclustered layers must have !has point_count filter
    const pointLayer = layers.find((l) => l.id === "cam-point-circle");
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect((pointLayer as any)?.filter).toEqual(["!", ["has", "point_count"]]);

    // Label layer must have minzoom
    const labelLayer = layers.find((l) => l.id === "cam-label");
    expect(labelLayer?.minzoom).toBe(16);
  });
});

it("keeps camera identity visible in clusters and uses a separate offline X badge", () => {
  const layers = buildCameraLayers();
  const cluster = layers.find(l => l.id === "cam-cluster-icon");
  expect(cluster?.type).toBe("symbol");
  expect(cluster?.type === "symbol" && cluster.layout?.["icon-image"]).toBe("cam-normal");
  const camera = layers.find(l => l.id === "cam-point-icon");
  expect(camera?.type === "symbol" && camera.layout?.["icon-image"]).toBe("cam-normal");
  expect(layers.find(l => l.id === "cam-offline-badge")?.type).toBe("symbol");
});
