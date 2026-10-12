import { describe, expect, it } from "vitest";
import {
  calculateClickToCenterAngles,
  calculateFisheyeRadius,
  normalize,
  rectilinearToRay,
  toDeg,
  toRad,
} from "./math";

describe("Fisheye Math Module", () => {
  it("converts between degrees and radians accurately", () => {
    expect(toRad(180)).toBeCloseTo(Math.PI);
    expect(toDeg(Math.PI)).toBeCloseTo(180);
    expect(toRad(90)).toBeCloseTo(Math.PI / 2);
    expect(toDeg(Math.PI / 2)).toBeCloseTo(90);
  });

  it("normalizes 3D vectors correctly", () => {
    const v = normalize({ x: 3, y: 0, z: 4 });
    expect(v.x).toBeCloseTo(0.6);
    expect(v.y).toBeCloseTo(0);
    expect(v.z).toBeCloseTo(0.8);
  });

  it("computes fisheye sensor radius for equidistant projection", () => {
    const maxRadius = 0.5;
    const lensFov = 360;
    // theta at optical center
    expect(calculateFisheyeRadius(0, maxRadius, lensFov, "equidistant")).toBeCloseTo(0);
    // theta at halfway max angle (90 deg out of 180 max)
    const halfTheta = toRad(90);
    expect(calculateFisheyeRadius(halfTheta, maxRadius, lensFov, "equidistant")).toBeCloseTo(0.25);
    // theta at max angle (180 deg)
    const maxTheta = toRad(180);
    expect(calculateFisheyeRadius(maxTheta, maxRadius, lensFov, "equidistant")).toBeCloseTo(0.5);
  });

  it("computes fisheye sensor radius for equisolid and stereographic projections", () => {
    const maxRadius = 0.5;
    const lensFov = 360;
    const halfTheta = toRad(90);

    const rEquisolid = calculateFisheyeRadius(halfTheta, maxRadius, lensFov, "equisolid");
    expect(rEquisolid).toBeGreaterThan(0);
    expect(rEquisolid).toBeLessThan(maxRadius);

    const rStereo = calculateFisheyeRadius(halfTheta, maxRadius, lensFov, "stereographic");
    expect(rStereo).toBeGreaterThan(0);
    expect(rStereo).toBeLessThan(maxRadius);
  });

  it("projects center pixel to straight camera ray", () => {
    const ray = rectilinearToRay(0, 0, 1.0, { yaw: 0, pitch: 0, roll: 0, fov: 90 }, "wall");
    expect(ray.x).toBeCloseTo(0);
    expect(ray.y).toBeCloseTo(0);
    expect(ray.z).toBeCloseTo(1.0);
  });

  it("calculates click-to-center delta angles accurately", () => {
    const view = { yaw: 0, pitch: 0, fov: 90 };
    // Clicking at right edge (u = 0.5) should increase yaw
    const rightAngles = calculateClickToCenterAngles(0.5, 0, 1.0, view);
    expect(rightAngles.yaw).toBeGreaterThan(0);
    expect(rightAngles.pitch).toBeCloseTo(0);

    // Clicking at top edge (v = 0.5) should increase pitch
    const topAngles = calculateClickToCenterAngles(0, 0.5, 1.0, view);
    expect(topAngles.yaw).toBeCloseTo(0);
    expect(topAngles.pitch).toBeLessThan(0); // in standard screen coords
  });
});
