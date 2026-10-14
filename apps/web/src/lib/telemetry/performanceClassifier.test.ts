import { describe, expect, it } from "vitest";
import { classifyHealth, createHealthTracker, HEALTH_WINDOW_SIZE } from "./performanceClassifier";
import type { StreamTelemetryRow, VideoPerformanceMetrics } from "./types";

function sample(patch: Partial<VideoPerformanceMetrics> = {}, i = 0): VideoPerformanceMetrics {
  return {
    timestamp: 1_000 * i,
    visibleCameras: 16,
    activeStreams: 16,
    mainStreams: 0,
    subStreams: 16,
    receivedMbps: 10,
    decodedFrames: 160,
    droppedFrames: 0,
    droppedFrameRatio: 0,
    presentationFps: 60,
    renderFrameTimeMs: 12,
    renderFrameTimeP95Ms: 16,
    stalls: 0,
    ...patch,
  };
}

function windowOf(n: number, patch: Partial<VideoPerformanceMetrics> | ((i: number) => Partial<VideoPerformanceMetrics>) = {}) {
  return Array.from({ length: n }, (_, i) => sample(typeof patch === "function" ? patch(i) : patch, i));
}

function dropped(ratio: number): Partial<VideoPerformanceMetrics> {
  return { droppedFrames: Math.round(160 * ratio), droppedFrameRatio: ratio };
}

describe("classifyHealth", () => {
  it("is optimal for a clean window and keeps four layers separate", () => {
    const h = classifyHealth(windowOf(10));
    expect(h.state).toBe("optimal");
    expect(h.reasons).toEqual([]);
    expect(h.layers).toMatchObject({ transport: "optimal", decode: "optimal", render: "optimal" });
    // No expected bitrate was given, so the network layer cannot be judged.
    expect(h.layers.network).toBe("unknown");
  });

  it("is acceptable with a mild sustained drop ratio", () => {
    const h = classifyHealth(windowOf(10, dropped(0.03)));
    expect(h.state).toBe("acceptable");
    expect(h.layers.decode).toBe("acceptable");
    expect(h.reasons[0]).toMatchObject({ layer: "decode", code: "dropped_frames", state: "acceptable" });
  });

  it("is at the limit with a sustained 8% drop ratio", () => {
    const h = classifyHealth(windowOf(10, dropped(0.08)));
    expect(h.state).toBe("limit");
    expect(h.signals.droppedFrameRatio).toBeCloseTo(0.08, 1);
  });

  it("is saturated with a sustained heavy drop ratio even when presentation fps is perfect", () => {
    const h = classifyHealth(windowOf(10, { ...dropped(0.3), presentationFps: 60 }));
    expect(h.state).toBe("saturated");
    expect(h.layers.render).toBe("optimal");
  });

  it("ignores a single bad second inside the window", () => {
    const w = windowOf(10, (i) => (i === 4 ? { ...dropped(0.6), stalls: 5, renderFrameTimeP95Ms: 120 } : {}));
    expect(classifyHealth(w).state).toBe("optimal");
  });

  it("only looks at the last HEALTH_WINDOW_SIZE samples", () => {
    const old = windowOf(30, dropped(0.5));
    const recent = windowOf(HEALTH_WINDOW_SIZE).map((m, i) => ({ ...m, timestamp: 100_000 + i }));
    expect(classifyHealth([...old, ...recent]).state).toBe("optimal");
  });

  it("never degrades on presentation fps alone beyond acceptable", () => {
    const h = classifyHealth(windowOf(10, { presentationFps: 15 }));
    expect(h.state).toBe("acceptable");
    expect(h.layers.render).toBe("acceptable");
  });

  it("degrades render further when frame time corroborates low fps", () => {
    const h = classifyHealth(windowOf(10, { presentationFps: 15, renderFrameTimeP95Ms: 90 }));
    expect(h.layers.render).toBe("saturated");
    expect(h.state).toBe("saturated");
  });

  it("accepts a lower target fps", () => {
    expect(classifyHealth(windowOf(10, { presentationFps: 30 }), { targetFps: 30 }).state).toBe("optimal");
  });

  it("flags sustained stalls in the transport layer", () => {
    const h = classifyHealth(windowOf(10, { stalls: 2 }));
    expect(h.layers.transport).toBe("limit");
    expect(h.state).toBe("limit");
    expect(h.signals.stallsPerMinute).toBeCloseTo(120, 0);
  });

  it("flags streams that are visible but not delivering media", () => {
    const h = classifyHealth(windowOf(10, { visibleCameras: 16, activeStreams: 11 }));
    expect(h.layers.transport).toBe("limit");
    expect(h.reasons.some((r) => r.code === "streams_missing")).toBe(true);
  });

  it("judges the network layer only against an expected bitrate", () => {
    const w = windowOf(10, { receivedMbps: 8 });
    expect(classifyHealth(w).layers.network).toBe("unknown");
    const h = classifyHealth(w, { expectedBitrateMbps: 10 });
    expect(h.layers.network).toBe("acceptable");
    expect(h.signals.bitrateRatio).toBeCloseTo(0.8, 1);
    expect(classifyHealth(windowOf(10, { receivedMbps: 3 }), { expectedBitrateMbps: 10 }).layers.network).toBe("saturated");
  });

  it("compares per-camera decode fps with the expected stream fps", () => {
    const rows: StreamTelemetryRow[] = Array.from({ length: 10 }, (_, i) => ({
      cameraId: `cam-${i}`,
      quality: "sub",
      state: "ACTIVE",
      fps: i < 4 ? 3 : 10,
      bitrateBps: 500_000,
    }));
    const h = classifyHealth(windowOf(10), { rows, expectedStreamFps: 10 });
    expect(h.layers.decode).toBe("limit");
    expect(h.reasons.some((r) => r.code === "decode_fps_short")).toBe(true);
    expect(h.signals.starvedStreamRatio).toBeCloseTo(0.4);
  });

  it("returns an honest result for an empty or thin window", () => {
    const empty = classifyHealth([]);
    expect(empty.state).toBe("optimal");
    expect(empty.reasons[0]?.code).toBe("insufficient_data");
    expect(empty.layers.decode).toBe("unknown");
  });
});

describe("createHealthTracker", () => {
  const assess = (state: "optimal" | "acceptable" | "limit" | "saturated") => ({ state });

  it("downgrades only after consecutive worse windows and upgrades more slowly", () => {
    const t = createHealthTracker({ downgradeAfter: 2, upgradeAfter: 3 });
    expect(t.update(assess("optimal"))).toBe("optimal");
    expect(t.update(assess("limit"))).toBe("optimal");
    expect(t.update(assess("limit"))).toBe("limit");
    expect(t.update(assess("optimal"))).toBe("limit");
    expect(t.update(assess("optimal"))).toBe("limit");
    expect(t.update(assess("optimal"))).toBe("optimal");
  });

  it("resets the streak when the candidate changes", () => {
    const t = createHealthTracker({ downgradeAfter: 2, upgradeAfter: 2 });
    t.update(assess("optimal"));
    t.update(assess("limit"));
    t.update(assess("optimal"));
    expect(t.update(assess("limit"))).toBe("optimal");
  });
});
