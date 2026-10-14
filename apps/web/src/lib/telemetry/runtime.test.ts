import { describe, expect, it, vi } from "vitest";
import { createTelemetryRuntime } from "./runtime";
import type { StreamStatsSample, StreamStatsSource } from "./types";

class FakeStream implements StreamStatsSource {
  bytes = 0;
  decoded = 0;
  dropped = 0;
  constructor(private readonly cameraId: string, private readonly quality: "sub" | "main" = "sub") {}
  streamStats(): StreamStatsSample {
    return { cameraId: this.cameraId, quality: this.quality, state: "ACTIVE", bytesReceived: this.bytes, decodedFrames: this.decoded, droppedFrames: this.dropped, width: 640, height: 360, reconnects: 0, stalls: 0 };
  }
}

function setup(streams: FakeStream[] = []) {
  let clock = 1_000;
  let tick: (() => void) | undefined;
  const cancelSchedule = vi.fn();
  const schedule = vi.fn((fn: () => void) => {
    tick = fn;
    return cancelSchedule;
  });
  const presentation = { start: vi.fn(), stop: vi.fn(), sample: vi.fn(() => ({ fps: 60, avgFrameMs: 16.6, p95FrameMs: 17 })) };
  const runtime = createTelemetryRuntime({ now: () => clock, schedule, sessions: () => streams, presentation });
  const advance = () => {
    clock += 1000;
    for (const s of streams) {
      s.bytes += 500_000;
      s.decoded += 15;
    }
    tick?.();
  };
  return { runtime, schedule, cancelSchedule, presentation, advance, streams };
}

describe("telemetry runtime", () => {
  it("does not schedule anything until someone acquires it", () => {
    const { schedule, presentation } = setup();
    expect(schedule).not.toHaveBeenCalled();
    expect(presentation.start).not.toHaveBeenCalled();
  });

  it("starts once for many consumers and stops only when the last one releases", () => {
    const { runtime, schedule, cancelSchedule, presentation } = setup();
    runtime.acquire();
    runtime.acquire();
    expect(schedule).toHaveBeenCalledTimes(1);
    expect(presentation.start).toHaveBeenCalledTimes(1);
    expect(runtime.consumers()).toBe(2);
    runtime.release();
    expect(cancelSchedule).not.toHaveBeenCalled();
    runtime.release();
    expect(cancelSchedule).toHaveBeenCalledTimes(1);
    expect(presentation.stop).toHaveBeenCalledTimes(1);
    expect(runtime.consumers()).toBe(0);
  });

  it("ignores a release without a matching acquire", () => {
    const { runtime, cancelSchedule } = setup();
    runtime.release();
    expect(runtime.consumers()).toBe(0);
    expect(cancelSchedule).not.toHaveBeenCalled();
    runtime.acquire();
    expect(runtime.consumers()).toBe(1);
  });

  it("restarts after a full stop", () => {
    const { runtime, schedule } = setup();
    runtime.acquire();
    runtime.release();
    runtime.acquire();
    expect(schedule).toHaveBeenCalledTimes(2);
  });

  it("starts with an unknown, empty view", () => {
    const { runtime } = setup();
    const v = runtime.getView();
    expect(v.snapshot.metrics).toBeNull();
    expect(v.history).toEqual([]);
    expect(v.health.state).toBe("unknown");
    expect(v.state).toBe("unknown");
  });

  it("publishes a new view once per tick with totals, history and health", () => {
    const { runtime, advance } = setup([new FakeStream("cam-1"), new FakeStream("cam-2", "main")]);
    const listener = vi.fn();
    runtime.subscribe(listener);
    runtime.acquire();
    const first = runtime.getView();
    advance();
    expect(listener).toHaveBeenCalledTimes(1);
    const v = runtime.getView();
    expect(v).not.toBe(first);
    expect(runtime.getView()).toBe(v);
    expect(v.snapshot.metrics?.subStreams).toBe(1);
    expect(v.snapshot.metrics?.mainStreams).toBe(1);
    expect(v.history).toHaveLength(1);
    expect(v.health.state).toBe("unknown");
    advance();
    advance();
    expect(runtime.getView().history).toHaveLength(3);
    expect(runtime.getView().health.state).not.toBe("unknown");
    expect(runtime.getView().bottleneck.estimated).toBe(true);
  });

  it("resets the history and the health tracker after the last release", () => {
    const { runtime, advance } = setup([new FakeStream("cam-1")]);
    runtime.acquire();
    advance();
    advance();
    advance();
    runtime.release();
    expect(runtime.getView().history).toEqual([]);
    expect(runtime.getView().state).toBe("unknown");
  });

  it("reports the visible camera count from the provider, or 0", () => {
    const { runtime, advance } = setup([new FakeStream("cam-1")]);
    runtime.acquire();
    advance();
    expect(runtime.getView().snapshot.metrics?.visibleCameras).toBe(0);
    runtime.setVisibleCameras(26);
    advance();
    expect(runtime.getView().snapshot.metrics?.visibleCameras).toBe(26);
  });

  it("stops notifying a subscriber that unsubscribed", () => {
    const { runtime, advance } = setup();
    const listener = vi.fn();
    const off = runtime.subscribe(listener);
    runtime.acquire();
    off();
    advance();
    expect(listener).not.toHaveBeenCalled();
  });
});
