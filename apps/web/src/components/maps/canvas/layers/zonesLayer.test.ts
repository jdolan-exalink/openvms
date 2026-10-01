import { describe, expect, it } from "vitest";
import { LAYER_GROUPS, builtLayerIds } from "./visibility";
import { ZONES_SOURCE_ID, buildZonesSource, buildZonesLayers, zonesToFeatureCollection } from "./zonesLayer";
import type { Zone } from "@/lib/maps/types";

const zone = (over: Partial<Zone> = {}): Zone => ({
  id: "z1",
  siteId: "s",
  name: "acceso",
  kind: "security",
  geometry: {
    type: "Polygon",
    coordinates: [[[-64.19, -31.42], [-64.18, -31.42], [-64.185, -31.415], [-64.19, -31.42]]],
  },
  style: {},
  metadata: {},
  ruleIds: [],
  ...over,
});

describe("zonesLayer", () => {
  it("keeps the ring closed and carries the name and kind as feature properties", () => {
    const fc = zonesToFeatureCollection([zone(), zone({ id: "z2", kind: "perimeter" })]);
    expect(fc.features).toHaveLength(2);
    const feature = fc.features[0]!;
    expect(feature.geometry.type).toBe("Polygon");
    const ring = (feature.geometry as GeoJSON.Polygon).coordinates[0]!;
    expect(ring[0]).toEqual(ring[ring.length - 1]);
    expect(feature.properties).toMatchObject({ id: "z1", name: "acceso", kind: "security" });
  });

  it("builds a geojson source from the zones", () => {
    const source = buildZonesSource([zone()]);
    expect(source.type).toBe("geojson");
    expect(source.data).toMatchObject({ type: "FeatureCollection" });
    expect(ZONES_SOURCE_ID).toBe("zones");
  });

  it("declares layers that the visibility groups already own", () => {
    const ids = buildZonesLayers().map((layer) => layer.id);
    expect(ids).toEqual([...LAYER_GROUPS.zones]);
    for (const id of ids) expect(builtLayerIds()).toContain(id);
  });
});
