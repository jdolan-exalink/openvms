import type {
  FisheyeCalibration,
  FisheyeMountType,
  FisheyeProjectionModel,
  VirtualFisheyeView,
} from "./types";

export interface Vector3D {
  x: number;
  y: number;
  z: number;
}

/**
 * Degrees to radians conversion.
 */
export const toRad = (deg: number): number => (deg * Math.PI) / 180;

/**
 * Radians to degrees conversion.
 */
export const toDeg = (rad: number): number => (rad * 180) / Math.PI;

/**
 * Normalizes a 3D vector.
 */
export function normalize(v: Vector3D): Vector3D {
  const len = Math.hypot(v.x, v.y, v.z);
  if (len < 1e-7) return { x: 0, y: 0, z: 1 };
  return { x: v.x / len, y: v.y / len, z: v.z / len };
}

/**
 * Compute the radius r on the fisheye sensor for an incidence angle theta (in radians)
 * given the projection model, total lens FOV, and maximum sensor radius.
 */
export function calculateFisheyeRadius(
  theta: number,
  maxRadius: number,
  lensFovDeg: number,
  model: FisheyeProjectionModel,
): number {
  const maxTheta = toRad(lensFovDeg / 2);
  if (maxTheta <= 1e-5) return 0;

  switch (model) {
    case "equidistant":
      // r = f * theta
      return maxRadius * (theta / maxTheta);
    case "equisolid":
      // r = 2 * f * sin(theta / 2)
      return maxRadius * (Math.sin(theta / 2) / Math.sin(maxTheta / 2));
    case "stereographic":
      // r = 2 * f * tan(theta / 2)
      return maxRadius * (Math.tan(theta / 2) / Math.tan(maxTheta / 2));
    case "orthographic":
      // r = f * sin(theta)
      return maxRadius * (Math.sin(theta) / Math.sin(maxTheta));
    default:
      return maxRadius * (theta / maxTheta);
  }
}

/**
 * Generates a 3D ray for a normalized rectilinear view coordinate (u, v) in [-1, 1],
 * oriented by virtual view parameters (yaw, pitch, roll, fov) and camera mount type.
 */
export function rectilinearToRay(
  u: number,
  v: number,
  aspectRatio: number,
  view: Pick<VirtualFisheyeView, "yaw" | "pitch" | "roll" | "fov">,
  mount: FisheyeMountType,
): Vector3D {
  const halfFovRad = toRad(view.fov / 2);
  const tanHalfFov = Math.tan(halfFovRad);

  // Ray in camera coordinate frame (looking down +Z)
  const rx = u * tanHalfFov;
  const ry = -v * (tanHalfFov / aspectRatio);
  const rz = 1.0;

  let ray = normalize({ x: rx, y: ry, z: rz });

  // Apply Roll (around Z)
  if (view.roll !== 0) {
    const rollRad = toRad(view.roll);
    const cosR = Math.cos(rollRad);
    const sinR = Math.sin(rollRad);
    ray = {
      x: ray.x * cosR - ray.y * sinR,
      y: ray.x * sinR + ray.y * cosR,
      z: ray.z,
    };
  }

  // Apply Pitch (around X)
  const pitchRad = toRad(view.pitch);
  const cosP = Math.cos(pitchRad);
  const sinP = Math.sin(pitchRad);
  ray = {
    x: ray.x,
    y: ray.y * cosP + ray.z * sinP,
    z: -ray.y * sinP + ray.z * cosP,
  };

  // Apply Yaw (around Y)
  const yawRad = toRad(view.yaw);
  const cosY = Math.cos(yawRad);
  const sinY = Math.sin(yawRad);
  ray = {
    x: ray.x * cosY + ray.z * sinY,
    y: ray.y,
    z: -ray.x * sinY + ray.z * cosY,
  };

  // Adapt to Mount orientation:
  // Ceiling mount: looking downward from ceiling (Z down)
  // Wall mount: standard horizontal look
  // Floor mount: looking upward (Z up)
  if (mount === "ceiling") {
    return {
      x: ray.x,
      y: ray.z,
      z: -ray.y,
    };
  } else if (mount === "floor") {
    return {
      x: ray.x,
      y: -ray.z,
      z: ray.y,
    };
  }

  return ray;
}

/**
 * Map a 3D ray back to UV texture coordinates on the raw fisheye sensor image.
 * Returns null if outside the lens FOV or behind the camera.
 */
export function rayToFisheyeUv(
  ray: Vector3D,
  calibration: FisheyeCalibration,
  model: FisheyeProjectionModel,
  mount: FisheyeMountType,
): { u: number; v: number } | null {
  // Convert ray back relative to the lens optical axis
  let lx = ray.x;
  let ly = ray.y;
  let lz = ray.z;

  if (mount === "ceiling") {
    lx = ray.x;
    ly = ray.z;
    lz = -ray.y;
  } else if (mount === "floor") {
    lx = ray.x;
    ly = -ray.z;
    lz = ray.y;
  }

  const norm = normalize({ x: lx, y: ly, z: lz });
  // Angle theta from optical axis (+Z for lens)
  const theta = Math.acos(Math.max(-1, Math.min(1, norm.z)));
  const maxTheta = toRad(calibration.fov / 2);

  if (theta > maxTheta) {
    return null; // Outside lens field of view
  }

  // Polar angle phi in sensor plane
  let phi = Math.atan2(norm.y, norm.x) + toRad(calibration.rotation);

  const r = calculateFisheyeRadius(theta, calibration.radius, calibration.fov, model);

  // Map to normalized UV texture coordinates [0, 1]
  const u = calibration.centerX + r * Math.cos(phi);
  const v = calibration.centerY + r * Math.sin(phi);

  return { u, v };
}

/**
 * Click-to-center helper:
 * Given a normalized click position (clickU, clickV) on a virtual rectilinear viewport
 * in [-1, 1], compute the new target (yaw, pitch) that centers this point.
 */
export function calculateClickToCenterAngles(
  clickU: number,
  clickV: number,
  aspectRatio: number,
  currentView: Pick<VirtualFisheyeView, "yaw" | "pitch" | "fov">,
): { yaw: number; pitch: number } {
  const halfFovRad = toRad(currentView.fov / 2);
  const tanHalf = Math.tan(halfFovRad);

  // Calculate angular delta offsets
  const deltaYawDeg = toDeg(Math.atan(clickU * tanHalf));
  const deltaPitchDeg = toDeg(Math.atan(-clickV * (tanHalf / aspectRatio)));

  let newYaw = currentView.yaw + deltaYawDeg;
  // Normalize yaw to [-180, 180]
  while (newYaw > 180) newYaw -= 360;
  while (newYaw < -180) newYaw += 360;

  let newPitch = currentView.pitch + deltaPitchDeg;
  // Clamp pitch to [-89, 89]
  newPitch = Math.max(-89, Math.min(89, newPitch));

  return {
    yaw: Math.round(newYaw * 10) / 10,
    pitch: Math.round(newPitch * 10) / 10,
  };
}
