import type { Polygon } from "geojson";

const EARTH_RADIUS_M = 6371000;

function toRad(deg: number): number {
  return (deg * Math.PI) / 180;
}

function toDeg(rad: number): number {
  return (rad * 180) / Math.PI;
}

/**
 * Calculates the great-circle distance between two [lng, lat] points in meters.
 */
export function haversineDistance(
  [lng1, lat1]: [number, number],
  [lng2, lat2]: [number, number],
): number {
  const phi1 = toRad(lat1);
  const phi2 = toRad(lat2);
  const deltaPhi = toRad(lat2 - lat1);
  const deltaLambda = toRad(lng2 - lng1);

  const a =
    Math.sin(deltaPhi / 2) * Math.sin(deltaPhi / 2) +
    Math.cos(phi1) * Math.cos(phi2) * Math.sin(deltaLambda / 2) * Math.sin(deltaLambda / 2);

  const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
  return EARTH_RADIUS_M * c;
}

/**
 * Computes destination [lng, lat] given starting point, distance in meters, and bearing in degrees.
 */
export function destinationPoint(
  [lng, lat]: [number, number],
  distanceM: number,
  bearingDeg: number,
): [number, number] {
  const delta = distanceM / EARTH_RADIUS_M;
  const theta = toRad(bearingDeg);
  const phi1 = toRad(lat);
  const lambda1 = toRad(lng);

  const phi2 = Math.asin(
    Math.sin(phi1) * Math.cos(delta) + Math.cos(phi1) * Math.sin(delta) * Math.cos(theta),
  );

  const lambda2 =
    lambda1 +
    Math.atan2(
      Math.sin(theta) * Math.sin(delta) * Math.cos(phi1),
      Math.cos(delta) - Math.sin(phi1) * Math.sin(phi2),
    );

  return [toDeg(lambda2), toDeg(phi2)];
}

/**
 * Builds a Field of View (FOV) cone polygon (16 segments by default) starting at center.
 * Returns null if bearing is null or fov/range is non-positive.
 */
export function buildFovCone(
  center: [number, number],
  bearingDeg: number | null,
  fovDeg: number,
  rangeM: number,
  segments = 16,
): Polygon | null {
  if (bearingDeg == null || fovDeg <= 0 || rangeM <= 0) {
    return null;
  }

  // 360 degree FOV represents an omnidirectional camera circle
  if (fovDeg >= 360) {
    const ring: [number, number][] = [];
    const step = 360 / segments;
    for (let i = 0; i <= segments; i++) {
      const angle = i * step;
      ring.push(destinationPoint(center, rangeM, angle));
    }
    return {
      type: "Polygon",
      coordinates: [ring],
    };
  }

  const halfFov = fovDeg / 2;
  const startAngle = bearingDeg - halfFov;
  const step = fovDeg / segments;

  const ring: [number, number][] = [center];

  for (let i = 0; i <= segments; i++) {
    const angle = startAngle + i * step;
    ring.push(destinationPoint(center, rangeM, angle));
  }

  // Close the ring back to the camera center
  ring.push(center);

  return {
    type: "Polygon",
    coordinates: [ring],
  };
}

export interface BoundingBox {
  west: number;
  south: number;
  east: number;
  north: number;
}

/**
 * Checks whether a given [lng, lat] point is inside bounding box limits.
 */
export function isPointInBounds([lng, lat]: [number, number], bounds: BoundingBox): boolean {
  return lng >= bounds.west && lng <= bounds.east && lat >= bounds.south && lat <= bounds.north;
}

/**
 * Maps normalized [x,y] in [0,1] on floor plan to local CRS coordinates near (0,0).
 */
export function floorToLocal(
  { x, y }: { x: number; y: number },
  widthDeg = 0.01,
  heightDeg = 0.01,
): [number, number] {
  const lng = (x - 0.5) * widthDeg;
  const lat = (0.5 - y) * heightDeg;
  return [lng, lat];
}
