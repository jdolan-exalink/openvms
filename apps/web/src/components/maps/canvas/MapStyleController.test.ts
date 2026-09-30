import { describe, expect, it } from "vitest";
import { buildMapStyle, type ThemeColors } from "./MapStyleController";
import type { MapProviderConfig } from "@/lib/maps/types";

describe("MapStyleController buildMapStyle", () => {
  const darkTheme: ThemeColors = {
    isDark: true,
    background: "#0e1523",
    water: "#101827",
    roadsMinor: "#222e42",
    roadsMajor: "#29364a",
    buildings: "#151f30",
    labels: "#7e8a9a",
    labelsHalo: "#0e1523",
  };

  const lightTheme: ThemeColors = {
    isDark: false,
    background: "#f4f6f5",
    water: "#d8e6e2",
    roadsMinor: "#d6dfdc",
    roadsMajor: "#eef2f0",
    buildings: "#eef2f0",
    labels: "#586864",
    labelsHalo: "#ffffff",
  };

  it("builds vector PMTiles style with dark tokens", () => {
    const provider: MapProviderConfig = {
      id: "protomaps-local",
      kind: "pmtiles",
      tiles: ["/tiles/base.pmtiles"],
      attribution: "© OpenStreetMap contributors",
      maxZoom: 18,
      offline: true,
    };

    const style = buildMapStyle(provider, darkTheme);
    expect(style.sources).toHaveProperty("protomaps");
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const src = (style.sources as any).protomaps;
    expect(src.type).toBe("vector");
    expect(src.url).toBe("pmtiles:///tiles/base.pmtiles");

    const bgLayer = style.layers.find((l) => l.id === "background");
    expect(bgLayer).toBeDefined();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect((bgLayer?.paint as any)?.["background-color"]).toBe("#0e1523");

    const waterLayer = style.layers.find((l) => l.id === "water");
    expect(waterLayer).toBeDefined();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect((waterLayer?.paint as any)?.["fill-color"]).toBe("#101827");
  });

  it("builds vector PMTiles style with light tokens", () => {
    const provider: MapProviderConfig = {
      id: "protomaps-local",
      kind: "pmtiles",
      tiles: ["/tiles/base.pmtiles"],
      attribution: "© OpenStreetMap contributors",
      maxZoom: 18,
      offline: true,
    };

    const style = buildMapStyle(provider, lightTheme);
    const bgLayer = style.layers.find((l) => l.id === "background");
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect((bgLayer?.paint as any)?.["background-color"]).toBe("#f4f6f5");
  });

  it("builds raster style with dark mode dimming filters", () => {
    const provider: MapProviderConfig = {
      id: "osm-raster",
      kind: "raster",
      tiles: ["https://tile.openstreetmap.org/{z}/{x}/{y}.png"],
      attribution: "© OpenStreetMap contributors",
      maxZoom: 19,
      offline: false,
    };

    const style = buildMapStyle(provider, darkTheme);
    expect(style.sources).toHaveProperty("raster-tiles");

    const rasterLayer = style.layers.find((l) => l.id === "raster-layer");
    expect(rasterLayer).toBeDefined();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const paint = rasterLayer?.paint as any;
    expect(paint?.["raster-brightness-max"]).toBe(0.35);
    expect(paint?.["raster-saturation"]).toBe(-0.6);
    expect(paint?.["raster-contrast"]).toBe(-0.1);
  });

  it("builds raster style with light mode without dimming", () => {
    const provider: MapProviderConfig = {
      id: "osm-raster",
      kind: "raster",
      tiles: ["https://tile.openstreetmap.org/{z}/{x}/{y}.png"],
      attribution: "© OpenStreetMap contributors",
      maxZoom: 19,
      offline: false,
    };

    const style = buildMapStyle(provider, lightTheme);
    const rasterLayer = style.layers.find((l) => l.id === "raster-layer");
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const paint = rasterLayer?.paint as any;
    expect(paint?.["raster-brightness-max"]).toBeUndefined();
    expect(paint?.["raster-opacity"]).toBe(1.0);
  });
});
