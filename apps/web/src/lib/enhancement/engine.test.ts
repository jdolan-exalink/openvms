import { describe, expect, it } from "vitest";
import { IDENTITY_ZOOM } from "../live/digitalZoom";
import {
  computeAutoAdjustments,
  extractRoiRect,
  interpolateAdjustments,
  DEFAULT_ADJUSTMENTS,
  type EnhancementAdjustments,
  type ImageStats,
  type RoiRect,
} from "./analysis";
import { ImageEnhancementEngineImpl } from "./engine";

describe("ImageEnhancementEngine - ROI & Analysis Math", () => {
  it("extracts full frame ROI at 1× zoom", () => {
    const roi = extractRoiRect(IDENTITY_ZOOM, 640, 480);
    expect(roi).toEqual({ x: 0, y: 0, width: 1, height: 1 });
  });

  it("extracts centered ROI at 2× zoom", () => {
    // Zoom 2x centered on 640x480: translate(-320, -240) scale(2)
    const zoom = { scale: 2, x: -320, y: -240 };
    const roi = extractRoiRect(zoom, 640, 480);
    expect(roi.x).toBeCloseTo(0.25, 2);
    expect(roi.y).toBeCloseTo(0.25, 2);
    expect(roi.width).toBeCloseTo(0.5, 2);
    expect(roi.height).toBeCloseTo(0.5, 2);
  });

  it("clamps ROI coordinates to valid 0..1 bounds when panned to edge", () => {
    const zoom = { scale: 4, x: -1920, y: -1440 };
    const roi = extractRoiRect(zoom, 640, 480);
    expect(roi.x).toBeGreaterThanOrEqual(0);
    expect(roi.x + roi.width).toBeLessThanOrEqual(1.001);
    expect(roi.y).toBeGreaterThanOrEqual(0);
    expect(roi.y + roi.height).toBeLessThanOrEqual(1.001);
  });

  it("computes dark scene auto adjustments lifting shadows without blowout", () => {
    const darkStats: ImageStats = {
      minLuma: 5,
      maxLuma: 120,
      avgLuma: 45,
      p5Luma: 10,
      p95Luma: 95,
      avgR: 44,
      avgG: 45,
      avgB: 46,
      isDark: true,
      isOverexposed: false,
      isLowContrast: true,
    };

    const adj = computeAutoAdjustments(darkStats, { x: 0, y: 0, width: 1, height: 1 });

    // Should lift gamma > 1.0 to brighten dark shadows
    expect(adj.gamma).toBeGreaterThan(1.1);
    // Should boost contrast gently
    expect(adj.contrast).toBeGreaterThan(0);
    // Should keep brightness within safe limits
    expect(adj.brightness).toBeGreaterThan(0);
    expect(adj.brightness).toBeLessThanOrEqual(40);
  });

  it("computes washed-out / high-black-level scene corrections", () => {
    const washedStats: ImageStats = {
      minLuma: 40,
      maxLuma: 210,
      avgLuma: 130,
      p5Luma: 45,
      p95Luma: 200,
      avgR: 130,
      avgG: 130,
      avgB: 130,
      isDark: false,
      isOverexposed: false,
      isLowContrast: true,
    };

    const adj = computeAutoAdjustments(washedStats);

    // Black level should be lifted to crush the washed-out gray veil
    expect(adj.blackLevel).toBeGreaterThan(0.05);
    // Contrast should be boosted
    expect(adj.contrast).toBeGreaterThan(10);
  });

  it("increases sharpness in zoomed ROI to improve detail and legibility", () => {
    const normalStats: ImageStats = {
      minLuma: 10,
      maxLuma: 240,
      avgLuma: 128,
      p5Luma: 20,
      p95Luma: 230,
      avgR: 128,
      avgG: 128,
      avgB: 128,
      isDark: false,
      isOverexposed: false,
      isLowContrast: false,
    };

    const fullFrameAdj = computeAutoAdjustments(normalStats, { x: 0, y: 0, width: 1, height: 1 });
    const zoomedRoiAdj = computeAutoAdjustments(normalStats, { x: 0.3, y: 0.3, width: 0.25, height: 0.25 });

    expect(zoomedRoiAdj.sharpness).toBeGreaterThan(fullFrameAdj.sharpness);
    expect(zoomedRoiAdj.sharpness).toBeLessThanOrEqual(50); // Light sharpening, avoiding halos
  });

  it("smoothly interpolates adjustments (lerp) across frames", () => {
    const current: EnhancementAdjustments = { ...DEFAULT_ADJUSTMENTS, brightness: 0, contrast: 0 };
    const target: EnhancementAdjustments = { ...DEFAULT_ADJUSTMENTS, brightness: 20, contrast: 15 };

    const step1 = interpolateAdjustments(current, target, 0.5);
    expect(step1.brightness).toBeCloseTo(10, 1);
    expect(step1.contrast).toBeCloseTo(7.5, 1);

    const step2 = interpolateAdjustments(step1, target, 0.5);
    expect(step2.brightness).toBeCloseTo(15, 1);
    expect(step2.contrast).toBeCloseTo(11.25, 1);
  });

  it("engine manages mode, manual overrides, and reset properly", () => {
    const engine = new ImageEnhancementEngineImpl();
    expect(engine.mode).toBe("auto");

    engine.setMode("off");
    expect(engine.mode).toBe("off");
    expect(engine.getAdjustments()).toEqual(DEFAULT_ADJUSTMENTS);

    engine.setMode("manual");
    engine.setManualAdjustments({ brightness: 25, contrast: 10 });
    expect(engine.getAdjustments().brightness).toBe(25);
    expect(engine.getAdjustments().contrast).toBe(10);

    engine.reset();
    expect(engine.mode).toBe("auto");
    expect(engine.getAdjustments()).toEqual(DEFAULT_ADJUSTMENTS);
  });
});
