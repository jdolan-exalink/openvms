import { describe, expect, it } from "vitest";
import {
  buildFovCone,
  destinationPoint,
  floorToLocal,
  haversineDistance,
  isPointInBounds,
} from "./geo";

describe("geo utilities", () => {
  const obelisco: [number, number] = [-58.3816, -34.6037];
  const plazaDeMayo: [number, number] = [-58.3712, -34.6083];

  it("calculates haversine distance between two coordinates", () => {
    const dist = haversineDistance(obelisco, plazaDeMayo);
    // Distance between Obelisco and Plaza de Mayo is approx 1080m - 1100m
    expect(dist).toBeGreaterThan(900);
    expect(dist).toBeLessThan(1300);
  });

  it("calculates destination point given distance and bearing", () => {
    const northPt = destinationPoint(obelisco, 1000, 0); // 1 km north
    expect(northPt[0]).toBeCloseTo(obelisco[0], 3);
    expect(northPt[1]).toBeGreaterThan(obelisco[1]); // higher latitude = north in southern hemisphere (-34.59 > -34.60)

    const eastPt = destinationPoint(obelisco, 1000, 90); // 1 km east
    expect(eastPt[0]).toBeGreaterThan(obelisco[0]); // higher longitude = east
    expect(eastPt[1]).toBeCloseTo(obelisco[1], 3);
  });

  describe("buildFovCone", () => {
    it("returns null if bearing is null", () => {
      expect(buildFovCone(obelisco, null, 75, 40)).toBeNull();
    });

    it("returns null if fov or range is non-positive", () => {
      expect(buildFovCone(obelisco, 90, 0, 40)).toBeNull();
      expect(buildFovCone(obelisco, 90, 60, 0)).toBeNull();
    });

    it("builds a closed 16-segment cone polygon for directional camera", () => {
      const cone = buildFovCone(obelisco, 90, 60, 50, 16);
      expect(cone).not.toBeNull();
      expect(cone?.type).toBe("Polygon");
      expect(cone?.coordinates).toHaveLength(1);

      const ring = cone?.coordinates[0] ?? [];
      // 1 (center) + 17 (arc points) + 1 (close to center) = 19
      expect(ring).toHaveLength(19);
      // First and last points are the camera center
      expect(ring[0]).toEqual(obelisco);
      expect(ring[ring.length - 1]).toEqual(obelisco);
    });

    it("builds full circle for 360-degree fisheye camera", () => {
      const omni = buildFovCone(obelisco, 0, 360, 30, 16);
      expect(omni).not.toBeNull();
      expect(omni?.type).toBe("Polygon");
      expect(omni?.coordinates[0]).toHaveLength(17);
    });
  });

  describe("isPointInBounds", () => {
    const bbox = {
      west: -58.4,
      south: -34.7,
      east: -58.3,
      north: -34.5,
    };

    it("returns true for points inside the bbox", () => {
      expect(isPointInBounds([-58.35, -34.6], bbox)).toBe(true);
    });

    it("returns false for points outside the bbox", () => {
      expect(isPointInBounds([-58.5, -34.6], bbox)).toBe(false);
      expect(isPointInBounds([-58.35, -34.8], bbox)).toBe(false);
    });
  });

  describe("floorToLocal", () => {
    it("maps normalized [0.5, 0.5] to local origin (0, 0)", () => {
      const [lng, lat] = floorToLocal({ x: 0.5, y: 0.5 });
      expect(lng).toBe(0);
      expect(lat).toBe(0);
    });

    it("maps normalized corners proportionally", () => {
      const [lngTopLeft, latTopLeft] = floorToLocal({ x: 0, y: 0 }, 0.02, 0.01);
      expect(lngTopLeft).toBe(-0.01);
      expect(latTopLeft).toBe(0.005);
    });
  });
});
