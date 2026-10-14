import { describe, expect, it } from "vitest";
import { estimateBottleneck } from "./bottleneck";
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

describe("estimateBottleneck", () => {
  it("reports none with high confidence for a healthy, well-sampled window", () => {
    const b = estimateBottleneck(windowOf(10));
    expect(b).toMatchObject({ layer: "none", confidence: "high", estimated: true });
    expect(b.evidence.length).toBeGreaterThan(0);
  });

  it("blames decode when presentation fps is fine but dropped frames are high", () => {
    const b = estimateBottleneck(windowOf(10, { droppedFrames: 48, droppedFrameRatio: 0.3, presentationFps: 60 }));
    expect(b.layer).toBe("decode");
    expect(b.estimated).toBe(true);
    expect(["medium", "high"]).toContain(b.confidence);
    expect(b.evidence.join(" ")).toMatch(/dropped/i);
  });

  it("raises decode confidence when it worsens as streams are added", () => {
    const w = windowOf(10, (i) => ({
      activeStreams: 8 + i * 4,
      droppedFrames: 4 + i * 8,
      droppedFrameRatio: (4 + i * 8) / 160,
    }));
    const b = estimateBottleneck(w);
    expect(b.layer).toBe("decode");
    expect(b.evidence.join(" ")).toMatch(/stream count/i);
  });

  it("blames render when frame time is high and decode is fine", () => {
    const b = estimateBottleneck(windowOf(10, { renderFrameTimeP95Ms: 80, presentationFps: 20 }));
    expect(b.layer).toBe("render");
    expect(b.confidence).toBe("high");
  });

  it("blames the network when stalls and reconnects coincide with a bitrate shortfall", () => {
    const b = estimateBottleneck(windowOf(10, { receivedMbps: 4, stalls: 2 }), undefined, { expectedBitrateMbps: 10, reconnects: 3 });
    expect(b.layer).toBe("network");
    expect(b.confidence).toBe("high");
    expect(b.evidence.join(" ")).toMatch(/bitrate/i);
  });

  it("does not claim network from low bitrate alone without an expectation", () => {
    const b = estimateBottleneck(windowOf(10, { receivedMbps: 0.5 }));
    expect(b.layer).toBe("none");
  });

  it("attributes stalls without an expected bitrate to transport, not network", () => {
    const b = estimateBottleneck(windowOf(10, { stalls: 2 }), undefined, { reconnects: 2 });
    expect(b.layer).toBe("transport");
  });

  it("uses per-camera rows as corroborating decode evidence", () => {
    const rows: StreamTelemetryRow[] = Array.from({ length: 10 }, (_, i) => ({
      cameraId: `c${i}`,
      quality: "sub",
      state: "ACTIVE",
      fps: 3,
      bitrateBps: 400_000,
    }));
    const plain = estimateBottleneck(windowOf(10, { droppedFrames: 10, droppedFrameRatio: 0.06 }));
    const withRows = estimateBottleneck(windowOf(10, { droppedFrames: 10, droppedFrameRatio: 0.06 }), rows, { expectedStreamFps: 10 });
    expect(withRows.layer).toBe("decode");
    expect(rank(withRows.confidence)).toBeGreaterThan(rank(plain.confidence));
  });

  it("lowers confidence when the window is thin", () => {
    const b = estimateBottleneck(windowOf(2, { droppedFrames: 48, droppedFrameRatio: 0.3 }));
    expect(b.confidence).toBe("low");
  });

  it("lowers confidence when two layers are equally bad", () => {
    const b = estimateBottleneck(windowOf(10, { droppedFrames: 48, droppedFrameRatio: 0.3, renderFrameTimeP95Ms: 90, presentationFps: 15 }));
    expect(b.confidence).not.toBe("high");
    expect(b.evidence.join(" ")).toMatch(/render/i);
  });

  it("marks the verdict as direct only when the input is explicitly direct", () => {
    expect(estimateBottleneck(windowOf(10), undefined, { directMetrics: true }).estimated).toBe(false);
    expect(estimateBottleneck(windowOf(10)).estimated).toBe(true);
  });

  it("ignores a single spike", () => {
    const b = estimateBottleneck(windowOf(10, (i) => (i === 3 ? { droppedFrames: 100, droppedFrameRatio: 0.6 } : {})));
    expect(b.layer).toBe("none");
  });
});

function rank(c: "low" | "medium" | "high") {
  return { low: 0, medium: 1, high: 2 }[c];
}
