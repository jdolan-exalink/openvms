import { describe, expect, it } from "vitest";
import {
  addZonePoint,
  closeZonePolygon,
  emptyZoneDraft,
  reopenZonePolygon,
  undoZonePoint,
  validateZoneDraft,
  zoneDraftToPolygon,
  zoneToDraft,
  type ZoneDraft,
} from "./zoneDraft";
import type { Zone } from "./types";

const triangle: ZoneDraft = {
  name: "acceso",
  kind: "security",
  points: [
    { lng: -64.19, lat: -31.42 },
    { lng: -64.18, lat: -31.42 },
    { lng: -64.185, lat: -31.415 },
  ],
  closed: true,
};

describe("zoneDraft", () => {
  it("grows point by point and refuses more once the polygon is closed", () => {
    let draft = emptyZoneDraft();
    draft = addZonePoint(draft, { lng: 1, lat: 2 });
    draft = addZonePoint(draft, { lng: 3, lat: 4 });
    expect(draft.points).toEqual([{ lng: 1, lat: 2 }, { lng: 3, lat: 4 }]);
    expect(draft.closed).toBe(false);

    draft = closeZonePolygon(draft); // needs three points, so nothing closes yet
    expect(draft.closed).toBe(false);

    draft = addZonePoint(draft, { lng: 5, lat: 6 });
    draft = closeZonePolygon(draft);
    expect(draft.closed).toBe(true);
    expect(addZonePoint(draft, { lng: 7, lat: 8 }).points).toHaveLength(3);
    expect(reopenZonePolygon(draft).closed).toBe(false);

    draft = undoZonePoint(draft);
    expect(draft.points).toHaveLength(2);
    expect(undoZonePoint(draft).points).toHaveLength(2);
  });

  it("reports exactly what the backend would reject", () => {
    expect(validateZoneDraft({ ...triangle, name: "" })).toEqual(["El nombre es obligatorio."]);
    expect(validateZoneDraft({ ...triangle, points: triangle.points.slice(0, 2) }))
      .toContain("Se necesitan al menos 3 puntos.");
    expect(validateZoneDraft({ ...triangle, closed: false })).toContain("Cerrá el polígono antes de guardar.");
    // Bowtie: segments cross, which ValidatePolygon refuses.
    expect(validateZoneDraft({
      ...triangle,
      points: [{ lng: 0, lat: 0 }, { lng: 1, lat: 1 }, { lng: 0, lat: 1 }, { lng: 1, lat: 0 }],
    })).toContain("El polígono se cruza consigo mismo.");
    expect(validateZoneDraft(triangle)).toEqual([]);
  });

  it("closes the ring the way GeoJSON and the backend require", () => {
    const polygon = zoneDraftToPolygon(triangle);
    const ring = polygon.coordinates[0]!;
    expect(ring).toHaveLength(4);
    expect(ring[0]).toEqual(ring[ring.length - 1]);
    expect(ring[0]).toEqual([-64.19, -31.42]);
  });

  it("round-trips an existing zone into a draft and back", () => {
    const zone: Zone = {
      id: "z1",
      siteId: "s",
      name: "perimetro",
      kind: "security",
      geometry: zoneDraftToPolygon(triangle),
      style: {},
      metadata: {},
      ruleIds: [],
    };
    const draft = zoneToDraft(zone);
    expect(draft).toMatchObject({ zoneId: "z1", name: "perimetro", kind: "security", closed: true });
    expect(draft.points).toHaveLength(3);
    expect(zoneDraftToPolygon(draft)).toEqual(zone.geometry);
  });
});
