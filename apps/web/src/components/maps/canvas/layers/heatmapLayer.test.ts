import { describe, expect, it } from "vitest";
import {
  analyticsToFeatureCollection,
  buildHeatmapLayers,
  buildHeatmapSource,
  HEATMAP_CIRCLES_LAYER_ID,
  HEATMAP_LAYER_ID,
  HEATMAP_SOURCE_ID,
  type AnalyticsPoint,
} from "./heatmapLayer";

describe("heatmapLayer", () => {
  const points: AnalyticsPoint[] = [
    { camera_id: "cam-1", lat: -34.6037, lng: -58.3816, weight: 1.0, count: 50 },
    { camera_id: "cam-2", lat: -34.604, lng: -58.382, weight: 0.5, count: 25 },
  ];

  it("converts analytics points to GeoJSON FeatureCollection", () => {
    const fc = analyticsToFeatureCollection(points);
    expect(fc.type).toBe("FeatureCollection");
    expect(fc.features).toHaveLength(2);
    expect(fc.features[0]?.geometry.coordinates).toEqual([-58.3816, -34.6037]);
    expect(fc.features[0]?.properties?.weight).toBe(1.0);
    expect(fc.features[0]?.properties?.count).toBe(50);
  });

  it("builds heatmap source with correct source ID", () => {
    const src = buildHeatmapSource(points);
    expect(src.type).toBe("geojson");
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect(((src as any).data).features).toHaveLength(2);
  });

  it("builds heatmap and circle layers configured with HEATMAP_SOURCE_ID", () => {
    const layers = buildHeatmapLayers();
    expect(layers).toHaveLength(2);
    const heatmap = layers.find((l) => l.id === HEATMAP_LAYER_ID);
    const circles = layers.find((l) => l.id === HEATMAP_CIRCLES_LAYER_ID);

    expect(heatmap?.type).toBe("heatmap");
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect((heatmap as any).source).toBe(HEATMAP_SOURCE_ID);

    expect(circles?.type).toBe("circle");
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect((circles as any).source).toBe(HEATMAP_SOURCE_ID);
    expect(circles?.minzoom).toBe(14);
  });
});
