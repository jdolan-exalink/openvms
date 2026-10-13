import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { VideoTelemetryService, HISTORY_SIZE, type TelemetrySource } from "./videoTelemetry";
import type { PlayerState } from "../live/playerState";

function source(over: Partial<{ state: PlayerState; quality: "main" | "sub"; frames: { total: number; dropped: number } | null }> = {}) {
  const s = { state: over.state ?? ("ACTIVE" as PlayerState), quality: over.quality ?? ("sub" as const), frames: over.frames ?? null };
  const video = {
    getVideoPlaybackQuality: () => (s.frames ? { totalVideoFrames: s.frames.total, droppedVideoFrames: s.frames.dropped } : undefined),
  } as unknown as HTMLVideoElement;
  const src: TelemetrySource = { cameraId: "c", quality: s.quality, getState: () => s.state, video };
  return { s, src };
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("VideoTelemetryService", () => {
  it("computes total and per-session Mbps from received bytes each second", () => {
    const svc = new VideoTelemetryService();
    const a = source();
    const b = source({ quality: "main" });
    const ha = svc.register(a.src);
    const hb = svc.register(b.src);
    const unsub = svc.subscribe(() => {});
    ha.addBytes(250_000);
    hb.addBytes(500_000);
    vi.advanceTimersByTime(1000);
    const snap = svc.getSnapshot();
    expect(snap.mbps).toBeCloseTo(6, 5);
    expect(snap.sessions.map((x) => x.mbps).sort()).toEqual([2, 4]);
    expect(snap.streams.main).toBe(1);
    expect(snap.streams.sub).toBe(1);
    unsub();
  });

  it("starts the sampler on first subscriber, stops on last, notifies once per second", () => {
    const svc = new VideoTelemetryService();
    expect(vi.getTimerCount()).toBe(0);
    const l1 = vi.fn();
    const l2 = vi.fn();
    const u1 = svc.subscribe(l1);
    const u2 = svc.subscribe(l2);
    expect(vi.getTimerCount()).toBe(1);
    vi.advanceTimersByTime(3000);
    expect(l1).toHaveBeenCalledTimes(3);
    expect(l2).toHaveBeenCalledTimes(3);
    u1();
    expect(vi.getTimerCount()).toBe(1);
    u2();
    expect(vi.getTimerCount()).toBe(0);
    vi.advanceTimersByTime(3000);
    expect(l1).toHaveBeenCalledTimes(3);
  });

  it("does not notify on byte reports, and keeps the snapshot reference stable between ticks", () => {
    const svc = new VideoTelemetryService();
    const h = svc.register(source().src);
    const l = vi.fn();
    svc.subscribe(l);
    const before = svc.getSnapshot();
    h.addBytes(1000);
    h.addBytes(1000);
    expect(l).not.toHaveBeenCalled();
    expect(svc.getSnapshot()).toBe(before);
    vi.advanceTimersByTime(1000);
    expect(svc.getSnapshot()).not.toBe(before);
  });

  it("bounds history to 60 samples (ring wraparound), oldest first", () => {
    const svc = new VideoTelemetryService();
    const h = svc.register(source().src);
    svc.subscribe(() => {});
    for (let i = 1; i <= HISTORY_SIZE + 5; i++) {
      h.addBytes(125_000 * i); // i Mbps
      vi.advanceTimersByTime(1000);
    }
    const hist = svc.getSnapshot().history;
    expect(hist).toHaveLength(HISTORY_SIZE);
    expect(hist[0]!.mbps).toBeCloseTo(6, 5);
    expect(hist[HISTORY_SIZE - 1]!.mbps).toBeCloseTo(HISTORY_SIZE + 5, 5);
  });

  it("counts sessions by state", () => {
    const svc = new VideoTelemetryService();
    for (const state of ["ACTIVE", "WARM", "CONNECTING", "BUFFERING", "RECONNECTING", "SUSPENDED", "IDLE", "ERROR", "EVICTED"] as PlayerState[]) {
      svc.register(source({ state }).src);
    }
    svc.subscribe(() => {});
    vi.advanceTimersByTime(1000);
    expect(svc.getSnapshot().streams).toMatchObject({ playing: 2, connecting: 3, paused: 2, error: 1, total: 8 });
  });

  it("derives presented fps and dropped ratio from playback-quality deltas", () => {
    const svc = new VideoTelemetryService();
    const a = source({ frames: { total: 100, dropped: 0 } });
    svc.register(a.src);
    svc.subscribe(() => {});
    a.s.frames = { total: 130, dropped: 3 }; // +30 total, +3 dropped
    vi.advanceTimersByTime(1000);
    const snap = svc.getSnapshot();
    expect(snap.fps).toBeCloseTo(27, 5);
    expect(snap.droppedFrames).toBe(3);
    expect(snap.droppedRatio).toBeCloseTo(0.1, 5);
  });

  it("tolerates missing getVideoPlaybackQuality and counters that reset on reconnect", () => {
    const svc = new VideoTelemetryService();
    const a = source({ frames: { total: 500, dropped: 20 } });
    const b = source({ frames: null });
    svc.register(a.src);
    svc.register(b.src);
    svc.subscribe(() => {});
    a.s.frames = { total: 10, dropped: 0 }; // reset
    vi.advanceTimersByTime(1000);
    const snap = svc.getSnapshot();
    // A counter reset rebaselines silently: nothing measurable this tick.
    expect(snap.fps).toBeNull();
    expect(snap.droppedRatio).toBe(0);
  });

  it("unregister removes the session and its bytes", () => {
    const svc = new VideoTelemetryService();
    const h = svc.register(source().src);
    svc.subscribe(() => {});
    h.addBytes(125_000);
    h.unregister();
    h.addBytes(125_000);
    vi.advanceTimersByTime(1000);
    expect(svc.getSnapshot().mbps).toBe(0);
    expect(svc.getSnapshot().streams.total).toBe(0);
  });
});
