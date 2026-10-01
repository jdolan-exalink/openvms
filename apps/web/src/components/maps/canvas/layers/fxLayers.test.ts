import { describe, expect, it } from "vitest";
import { buildFxLayers, buildFxSource, FX_SOURCE_ID } from "./fxLayers";

describe("fxLayers", () => {
  it("builds an empty GeoJSON source specification for fx", () => {
    const src = buildFxSource();
    expect(src.type).toBe("geojson");
    expect(src.data).toEqual({
      type: "FeatureCollection",
      features: [],
    });
  });

  it("builds fx-ripple and fx-alarm-pulse circle layers linked to FX_SOURCE_ID", () => {
    const layers = buildFxLayers();
    expect(layers).toHaveLength(2);

    const ripple = layers.find((l) => l.id === "fx-ripple");
    expect(ripple).toBeDefined();
    expect(ripple?.type).toBe("circle");
    expect((ripple as { source?: string })?.source).toBe(FX_SOURCE_ID);

    const pulse = layers.find((l) => l.id === "fx-alarm-pulse");
    expect(pulse).toBeDefined();
    expect(pulse?.type).toBe("circle");
    expect((pulse as { source?: string })?.source).toBe(FX_SOURCE_ID);
  });
});
