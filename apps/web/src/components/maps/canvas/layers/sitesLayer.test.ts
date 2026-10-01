import { describe, expect, it } from "vitest";
import {
  buildSiteLayers,
  buildSitesSource,
  SITES_SOURCE_ID,
  sitesToFeatureCollection,
} from "./sitesLayer";
import type { Site } from "@/lib/maps/types";

describe("sitesLayer", () => {
  const sites: Site[] = [
    {
      id: "s1",
      name: "Sede Centro",
      center: { kind: "geo", lat: -34.6037, lng: -58.3816 },
      defaultZoom: 14,
      cameraCount: 15,
      health: {
        online: 12,
        offline: 2,
        degraded: 1,
        activeAlarms: 1,
        severity: "CRITICAL",
      },
    },
    {
      id: "s2",
      name: "Depósito Norte",
      center: { kind: "geo", lat: -34.5000, lng: -58.5000 },
      cameraCount: 5,
      health: {
        online: 5,
        offline: 0,
        degraded: 0,
        activeAlarms: 0,
        severity: "OK",
      },
    },
  ];

  it("converts sites list to GeoJSON FeatureCollection", () => {
    const fc = sitesToFeatureCollection(sites);
    expect(fc.type).toBe("FeatureCollection");
    expect(fc.features).toHaveLength(2);

    const f1 = fc.features[0]!;
    expect(f1.id).toBe("s1");
    expect(f1.geometry.coordinates).toEqual([-58.3816, -34.6037]);
    expect(f1.properties.name).toBe("Sede Centro");
    expect(f1.properties.camera_count).toBe(15);
    expect(f1.properties.severity).toBe("CRITICAL");
  });

  it("builds sites GeoJSON source", () => {
    const src = buildSitesSource(sites);
    expect(src.type).toBe("geojson");
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect((src.data as any).features).toHaveLength(2);
  });

  it("keeps building identity markers across zoom levels", () => {
    const layers = buildSiteLayers();
    expect(layers.length).toBe(5);

    const ids = layers.map((l) => l.id);
    expect(ids).toContain("site-health-ring");
    expect(ids).toContain("site-point");
    expect(ids).toContain("site-label");

    for (const l of layers) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      expect((l as any).source).toBe(SITES_SOURCE_ID);
      expect(l.maxzoom).toBeUndefined();
    }
  });
});
