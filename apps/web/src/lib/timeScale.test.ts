import { describe, expect, it } from "vitest";
import { clampView, clusterMarkers, DAY_S, MIN_SPAN_S, mergeSpans, panView, tickStep, timeToX, xToTime, zoomView } from "./timeScale";

const bounds = { start: 1000, end: 1000 + DAY_S };

describe("timeScale", () => {
  it("maps time and pixels both ways", () => {
    const view = { start: 100, end: 200 };
    expect(timeToX(150, view, 500)).toBe(250);
    expect(xToTime(250, view, 500)).toBe(150);
  });

  it("keeps the time under the cursor fixed while zooming", () => {
    const view = { ...bounds };
    const focus = bounds.start + DAY_S / 4;
    const zoomed = zoomView(view, focus, 4, bounds);
    expect(zoomed.end - zoomed.start).toBe(DAY_S / 4);
    expect((focus - zoomed.start) / (zoomed.end - zoomed.start)).toBeCloseTo(0.25);
  });

  it("never zooms below the minimum span or above the day", () => {
    const tiny = zoomView({ start: 2000, end: 2200 }, 2100, 1000, bounds);
    expect(tiny.end - tiny.start).toBe(MIN_SPAN_S);
    const wide = zoomView({ start: 2000, end: 4000 }, 3000, 0.0001, bounds);
    expect(wide).toEqual(bounds);
  });

  it("clamps pan inside the bounds", () => {
    const view = { start: bounds.start + 100, end: bounds.start + 700 };
    expect(panView(view, -5000, bounds)).toEqual({ start: bounds.start, end: bounds.start + 600 });
    expect(clampView({ start: bounds.end - 10, end: bounds.end + 590 }, bounds).end).toBe(bounds.end);
  });

  it("widens tick spacing as the view widens", () => {
    expect(tickStep(DAY_S, 1000)).toBeGreaterThan(tickStep(600, 1000));
  });

  it("merges overlapping and adjacent spans", () => {
    expect(mergeSpans([{ start: 10, end: 20 }, { start: 0, end: 5 }, { start: 15, end: 30 }, { start: 30.5, end: 40 }])).toEqual([
      { start: 0, end: 5 },
      { start: 10, end: 40 },
    ]);
  });

  it("clusters dense markers by pixel bucket", () => {
    const view = { start: 0, end: 1000 };
    const times = Array.from({ length: 500 }, (_, i) => i * 0.1); // 500 events in the first 50 s
    const clusters = clusterMarkers(times, view, 1000);
    expect(clusters.length).toBeLessThan(20);
    expect(clusters.reduce((n, c) => n + c.count, 0)).toBe(500);
    expect(clusterMarkers([-5, 2000], view, 1000)).toEqual([]);
  });
});
