import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createPlayerMetrics, observeFirstFrame } from "./playerMetrics";

describe("playerMetrics", () => {
  let now = 0;
  const metrics = createPlayerMetrics(() => now);
  beforeEach(() => {
    now = 1000;
    metrics.reset();
  });

  it("counts connect attempts per camera+quality and derives the reconnect count from them", () => {
    metrics.connectAttempt("cam-1", "sub");
    expect(metrics.get("cam-1", "sub")).toMatchObject({ connectAttempts: 1, reconnectCount: 0 });
    metrics.connectAttempt("cam-1", "sub");
    metrics.connectAttempt("cam-1", "main");
    expect(metrics.get("cam-1", "sub")).toMatchObject({ connectAttempts: 2, reconnectCount: 1 });
    expect(metrics.get("cam-1", "main")).toMatchObject({ connectAttempts: 1, reconnectCount: 0 });
  });

  it("measures time to first frame from the latest connect attempt, once per attempt", () => {
    metrics.connectAttempt("cam-1", "sub");
    now = 1450;
    metrics.firstFrame("cam-1", "sub");
    expect(metrics.get("cam-1", "sub")?.ttffMs).toBe(450);
    now = 2000;
    metrics.firstFrame("cam-1", "sub"); // a second frame for the same attempt is ignored
    expect(metrics.get("cam-1", "sub")?.ttffMs).toBe(450);
    metrics.connectAttempt("cam-1", "sub");
    now = 2100;
    metrics.firstFrame("cam-1", "sub");
    expect(metrics.get("cam-1", "sub")?.ttffMs).toBe(100);
  });

  it("records the latest state and notifies subscribers", () => {
    const seen: string[] = [];
    const off = metrics.subscribe(() => seen.push(metrics.get("cam-1", "sub")?.state ?? ""));
    metrics.setState("cam-1", "sub", "CONNECTING");
    metrics.setState("cam-1", "sub", "ACTIVE");
    off();
    metrics.setState("cam-1", "sub", "ERROR");
    expect(seen).toEqual(["CONNECTING", "ACTIVE"]);
    expect(metrics.get("cam-1", "sub")?.state).toBe("ERROR");
  });

  it("records state transitions with cause and timestamp", () => {
    now = 5000;
    metrics.transition("cam-1", "sub", { from: "CONNECTING", to: "ACTIVE", cause: "first-frame" });
    expect(metrics.get("cam-1", "sub")?.transitions).toEqual([
      { from: "CONNECTING", to: "ACTIVE", cause: "first-frame", at: 5000 },
    ]);
  });

  it("snapshot lists every session", () => {
    metrics.connectAttempt("a", "sub");
    metrics.connectAttempt("b", "main");
    expect(metrics.snapshot().map((s) => s.key).sort()).toEqual(["a:sub", "b:main"]);
  });
});

describe("observeFirstFrame", () => {
  afterEach(() => vi.restoreAllMocks());

  it("uses requestVideoFrameCallback when available", () => {
    const video = document.createElement("video");
    let fire: () => void = () => {};
    video.requestVideoFrameCallback = (cb) => {
      fire = () => cb(0, {} as VideoFrameCallbackMetadata);
      return 1;
    };
    const cb = vi.fn();
    observeFirstFrame(video, cb);
    expect(cb).not.toHaveBeenCalled();
    fire();
    expect(cb).toHaveBeenCalledTimes(1);
  });

  it("falls back to loadeddata and fires only once", () => {
    const video = document.createElement("video");
    const cb = vi.fn();
    observeFirstFrame(video, cb);
    video.dispatchEvent(new Event("loadeddata"));
    video.dispatchEvent(new Event("loadeddata"));
    expect(cb).toHaveBeenCalledTimes(1);
  });

  it("can be cancelled", () => {
    const video = document.createElement("video");
    const cb = vi.fn();
    const cancel = observeFirstFrame(video, cb);
    cancel();
    video.dispatchEvent(new Event("loadeddata"));
    expect(cb).not.toHaveBeenCalled();
  });
});
