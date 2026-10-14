import { describe, expect, it, vi } from "vitest";
import { createVideoTelemetry, type PresentationSampler } from "./videoTelemetry";
import type { StreamStatsSample, StreamStatsSource } from "./types";

class FakeStream implements StreamStatsSource {
  sample: StreamStatsSample;
  constructor(cameraId: string, quality: "sub" | "main" = "sub", patch: Partial<StreamStatsSample> = {}) {
    this.sample = { cameraId, quality, state: "ACTIVE", bytesReceived: 0, decodedFrames: 0, droppedFrames: 0, width: 640, height: 360, reconnects: 0, stalls: 0, ...patch };
  }
  streamStats() {
    return { ...this.sample };
  }
  add(patch: { bytes?: number; decoded?: number; dropped?: number; stalls?: number }) {
    this.sample.bytesReceived += patch.bytes ?? 0;
    this.sample.decodedFrames = (this.sample.decodedFrames ?? 0) + (patch.decoded ?? 0);
    this.sample.droppedFrames = (this.sample.droppedFrames ?? 0) + (patch.dropped ?? 0);
    this.sample.stalls += patch.stalls ?? 0;
  }
}

function setup(opts: { streams?: FakeStream[]; visible?: () => number; presentation?: PresentationSampler; historyLength?: number } = {}) {
  let clock = 10_000;
  const streams = opts.streams ?? [];
  let tick: (() => void) | undefined;
  const cancel = vi.fn();
  const schedule = vi.fn((fn: () => void) => {
    tick = fn;
    return cancel;
  });
  const service = createVideoTelemetry({
    now: () => clock,
    schedule,
    sessions: () => streams,
    visibleCameras: opts.visible,
    presentation: opts.presentation,
    historyLength: opts.historyLength,
  });
  return {
    service,
    streams,
    schedule,
    cancel,
    /** Advances the fake clock and fires the scheduled tick. */
    tick(ms = 1000) {
      clock += ms;
      tick!();
    },
  };
}

describe("VideoTelemetryService", () => {
  it("schedules a 1000 ms tick on start and clears it on stop", () => {
    const t = setup();
    t.service.start();
    t.service.start();
    expect(t.schedule).toHaveBeenCalledTimes(1);
    expect(t.schedule).toHaveBeenCalledWith(expect.any(Function), 1000);
    t.service.stop();
    t.service.stop();
    expect(t.cancel).toHaveBeenCalledTimes(1);
  });

  it("starts and stops the presentation sampler with the service", () => {
    const presentation = { start: vi.fn(), stop: vi.fn(), sample: vi.fn() };
    const t = setup({ presentation });
    t.service.start();
    expect(presentation.start).toHaveBeenCalledTimes(1);
    t.service.stop();
    expect(presentation.stop).toHaveBeenCalledTimes(1);
  });

  it("computes bitrate and frame rates from counter deltas over the elapsed time", () => {
    const a = new FakeStream("a");
    const t = setup({ streams: [a] });
    t.service.start();
    a.add({ bytes: 250_000, decoded: 10, dropped: 2 });
    t.tick(1000);
    const row = t.service.getSnapshot().streams[0]!;
    expect(row).toMatchObject({ cameraId: "a", quality: "sub", width: 640, height: 360, bitrateBps: 2_000_000, fps: 10, droppedFps: 2 });
    const m = t.service.getSnapshot().metrics!;
    expect(m.receivedMbps).toBeCloseTo(2, 6);
    expect(m.decodedFrames).toBe(10);
    expect(m.droppedFrames).toBe(2);
    expect(m.droppedFrameRatio).toBeCloseTo(2 / 12, 6);
  });

  it("normalizes by the real elapsed time when a tick is late", () => {
    const a = new FakeStream("a");
    const t = setup({ streams: [a] });
    t.service.start();
    a.add({ bytes: 500_000, decoded: 20 });
    t.tick(2000);
    expect(t.service.getSnapshot().streams[0]!.bitrateBps).toBe(2_000_000);
    expect(t.service.getSnapshot().streams[0]!.fps).toBe(10);
  });

  it("uses the first sight of a stream only as a baseline", () => {
    const a = new FakeStream("a", "sub", { bytesReceived: 9_999_999, decodedFrames: 5000 });
    const t = setup({ streams: [a] });
    t.service.start();
    t.tick();
    expect(t.service.getSnapshot().streams[0]!.bitrateBps).toBe(0);
    expect(t.service.getSnapshot().metrics!.receivedMbps).toBe(0);
    expect(t.service.getSnapshot().metrics!.decodedFrames).toBe(0);
  });

  it("never reports negative values after a counter reset", () => {
    const a = new FakeStream("a");
    const t = setup({ streams: [a] });
    t.service.start();
    a.add({ bytes: 1_000_000, decoded: 100, dropped: 4, stalls: 3 });
    t.tick();
    a.sample.bytesReceived = 1000; // session recreated / decoder restarted
    a.sample.decodedFrames = 5;
    a.sample.droppedFrames = 0;
    a.sample.stalls = 0;
    t.tick();
    const m = t.service.getSnapshot().metrics!;
    expect(t.service.getSnapshot().streams[0]!.bitrateBps).toBe(0);
    expect(m).toMatchObject({ receivedMbps: 0, decodedFrames: 0, droppedFrames: 0, stalls: 0 });
    a.add({ bytes: 125_000, decoded: 10 });
    t.tick();
    expect(t.service.getSnapshot().streams[0]!.bitrateBps).toBe(1_000_000);
  });

  it("does not invent frame data when the platform does not report it", () => {
    const a = new FakeStream("a", "sub", { decodedFrames: undefined, droppedFrames: undefined });
    const t = setup({ streams: [a] });
    t.service.start();
    a.sample.bytesReceived += 125_000;
    t.tick();
    const row = t.service.getSnapshot().streams[0]!;
    expect(row.fps).toBeUndefined();
    expect(row.droppedFps).toBeUndefined();
    expect(row.bitrateBps).toBe(1_000_000);
    expect(t.service.getSnapshot().metrics).toMatchObject({ decodedFrames: 0, droppedFrames: 0, droppedFrameRatio: 0 });
  });

  it("totals streams by quality and counts only media-delivering sessions as active", () => {
    const streams = [
      new FakeStream("a", "sub"),
      new FakeStream("b", "sub", { state: "WARM" }),
      new FakeStream("c", "main"),
      new FakeStream("d", "main", { state: "RECONNECTING" }),
      new FakeStream("e", "sub", { state: "SUSPENDED" }),
    ];
    const t = setup({ streams, visible: () => 4 });
    t.service.start();
    t.tick();
    expect(t.service.getSnapshot().metrics).toMatchObject({ visibleCameras: 4, activeStreams: 3, mainStreams: 2, subStreams: 3 });
  });

  it("defaults visibleCameras to 0 without a provider and sums stalls across streams", () => {
    const a = new FakeStream("a");
    const b = new FakeStream("b");
    const t = setup({ streams: [a, b] });
    t.service.start();
    a.add({ stalls: 2 });
    b.add({ stalls: 1 });
    t.tick();
    expect(t.service.getSnapshot().metrics).toMatchObject({ visibleCameras: 0, stalls: 3 });
  });

  it("handles sessions appearing and disappearing between ticks", () => {
    const a = new FakeStream("a");
    const t = setup({ streams: [a] });
    t.service.start();
    t.tick();
    const b = new FakeStream("b", "main", { bytesReceived: 5_000_000 });
    t.streams.push(b);
    a.add({ bytes: 125_000 });
    t.tick();
    expect(t.service.getSnapshot().streams.map((r) => [r.cameraId, r.bitrateBps])).toEqual([["a", 1_000_000], ["b", 0]]);
    t.streams.splice(0, 1);
    b.add({ bytes: 250_000 });
    t.tick();
    expect(t.service.getSnapshot().streams.map((r) => [r.cameraId, r.bitrateBps])).toEqual([["b", 2_000_000]]);
    // a comes back: its old baseline was dropped, so the jump is not counted as traffic.
    a.add({ bytes: 80_000_000 });
    t.streams.push(a);
    t.tick();
    expect(t.service.getSnapshot().streams.find((r) => r.cameraId === "a")!.bitrateBps).toBe(0);
  });

  it("keeps two sources with the same camera and quality apart", () => {
    const a1 = new FakeStream("a");
    const a2 = new FakeStream("a");
    const t = setup({ streams: [a1, a2] });
    t.service.start();
    a1.add({ bytes: 125_000 });
    a2.add({ bytes: 125_000 });
    t.tick();
    expect(t.service.getSnapshot().metrics!.receivedMbps).toBeCloseTo(2, 6);
    expect(t.service.getSnapshot().streams).toHaveLength(2);
  });

  it("includes the presentation sampler output in the totals", () => {
    const presentation = { start: vi.fn(), stop: vi.fn(), sample: vi.fn(() => ({ fps: 58, avgFrameMs: 17, p95FrameMs: 25 })) };
    const t = setup({ presentation });
    t.service.start();
    t.tick();
    expect(t.service.getSnapshot().metrics).toMatchObject({ presentationFps: 58, renderFrameTimeMs: 17, renderFrameTimeP95Ms: 25 });
  });

  it("leaves presentation fields undefined when the sampler has no data", () => {
    const presentation = { start: vi.fn(), stop: vi.fn(), sample: vi.fn(() => undefined) };
    const t = setup({ presentation });
    t.service.start();
    t.tick();
    expect(t.service.getSnapshot().metrics!.presentationFps).toBeUndefined();
  });

  it("keeps a bounded chronological history of totals", () => {
    const t = setup({ historyLength: 3 });
    t.service.start();
    for (let i = 0; i < 5; i++) t.tick();
    const history = t.service.history();
    expect(history).toHaveLength(3);
    expect(history.map((m) => m.timestamp)).toEqual([13_000, 14_000, 15_000]);
    history.pop();
    expect(t.service.history()).toHaveLength(3);
  });

  it("defaults to a 60-sample history", () => {
    const t = setup();
    t.service.start();
    for (let i = 0; i < 75; i++) t.tick();
    expect(t.service.history()).toHaveLength(60);
  });

  it("notifies subscribers once per tick with a new snapshot and stops after unsubscribe", () => {
    const t = setup({ streams: [new FakeStream("a")] });
    const listener = vi.fn();
    const unsubscribe = t.service.subscribe(listener);
    t.service.start();
    const before = t.service.getSnapshot();
    expect(t.service.getSnapshot()).toBe(before);
    t.tick();
    expect(listener).toHaveBeenCalledTimes(1);
    expect(t.service.getSnapshot()).not.toBe(before);
    unsubscribe();
    t.tick();
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it("starts with an empty snapshot", () => {
    const t = setup();
    expect(t.service.getSnapshot()).toEqual({ metrics: null, streams: [] });
  });

  it("drops baselines on restart so time spent stopped is not counted as traffic", () => {
    const a = new FakeStream("a");
    const t = setup({ streams: [a] });
    t.service.start();
    t.tick();
    t.service.stop();
    a.add({ bytes: 90_000_000 });
    t.service.start();
    t.tick();
    expect(t.service.getSnapshot().streams[0]!.bitrateBps).toBe(0);
  });
});
