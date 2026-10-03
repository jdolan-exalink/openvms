import { describe, expect, it } from "vitest";
import { IDENTITY_ZOOM, clampZoom, panBy, wheelPixels, zoomAtPoint, zoomTransform } from "./digitalZoom";

describe("digitalZoom", () => {
  it("zooms toward the cursor and keeps that point still", () => {
    const next = zoomAtPoint(IDENTITY_ZOOM, 200, 100, 100, 50, -100);
    expect(next.scale).toBeGreaterThan(1);
    expect(next.scale).toBeLessThan(1.3);
    const contentX = (100 - next.x) / next.scale;
    const contentY = (50 - next.y) / next.scale;
    expect(contentX).toBeCloseTo(100);
    expect(contentY).toBeCloseTo(50);
  });

  it("returns to 1× with no pan when zooming back out", () => {
    const zoomed = zoomAtPoint(IDENTITY_ZOOM, 200, 100, 40, 20, -400);
    const back = zoomAtPoint(zoomed, 200, 100, 40, 20, 4000);
    expect(back).toEqual(IDENTITY_ZOOM);
  });

  it("stops at the maximum scale", () => {
    let zoom = IDENTITY_ZOOM;
    for (let i = 0; i < 40; i++) zoom = zoomAtPoint(zoom, 200, 100, 100, 50, -200);
    expect(zoom.scale).toBe(8);
  });

  it("clamps a pan so the picture still covers the box", () => {
    const zoom = { scale: 2, x: 0, y: 0 };
    expect(panBy(zoom, 200, 100, 30, 10)).toEqual({ scale: 2, x: 0, y: 0 });
    expect(panBy(zoom, 200, 100, -500, -400)).toEqual({ scale: 2, x: -200, y: -100 });
  });

  it("ignores pan at 1×", () => {
    expect(panBy(IDENTITY_ZOOM, 200, 100, -20, 10)).toEqual(IDENTITY_ZOOM);
  });

  it("converts line and page wheel deltas to pixels", () => {
    expect(wheelPixels(2, 1)).toBe(32);
    expect(wheelPixels(1, 2)).toBe(800);
    expect(wheelPixels(12, 0)).toBe(12);
  });

  it("emits no transform at 1×", () => {
    expect(zoomTransform(IDENTITY_ZOOM)).toBe("");
    expect(zoomTransform({ scale: 2, x: -10, y: -4 })).toBe("translate(-10px, -4px) scale(2)");
    expect(clampZoom({ scale: 1, x: -5, y: 3 }, 100, 80)).toEqual(IDENTITY_ZOOM);
  });
});
