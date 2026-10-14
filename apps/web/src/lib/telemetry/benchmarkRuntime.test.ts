import { describe, expect, it, vi } from "vitest";
import type { BenchmarkRecord } from "./benchmarkCache";
import type { BenchmarkDeps, BenchmarkOptions, BenchmarkResult } from "./benchmark";
import { createBenchmarkController } from "./benchmarkRuntime";
import type { ClientCapabilities } from "./types";

const caps: ClientCapabilities = {
  runtime: "browser",
  collectedAt: 1,
  system: { browser: "Chrome", browserVersion: "126.0.1" },
  cpu: {},
  memory: { bucketed: false, source: "unavailable" },
  gpu: { webgl: true, webgl2: true, vendor: "NVIDIA", renderer: "RTX" },
  codecs: [],
};

function result(patch: Partial<BenchmarkResult> = {}): BenchmarkResult {
  return { id: "r1", startedAt: 1, finishedAt: 4001, durationMs: 4000, clip: { codec: "avc1.42E01E", width: 640, height: 360, fps: 15 }, levels: [{ streams: 4, ran: true, state: "optimal", stalls: 0, decoderErrors: 0, reasons: [] }], maxStableStreams: 4, clientPerformanceScore: 3, limits: { bindingFactors: [] }, stoppedReason: "completed", ...patch };
}

function setup(run: (deps: BenchmarkDeps, opts?: BenchmarkOptions) => Promise<BenchmarkResult>) {
  const cache = { save: vi.fn<(record: BenchmarkRecord) => Promise<void>>(async () => undefined) };
  const profile = { refresh: vi.fn(async () => undefined) };
  const container = document.createElement("div");
  const createDeps = vi.fn((c: HTMLElement) => ({ container: c }) as unknown as BenchmarkDeps);
  const controller = createBenchmarkController({ run, createDeps, cache, profile });
  return { controller, cache, profile, container, createDeps };
}

describe("benchmark controller", () => {
  it("runs on the given container, reports progress, then stores the result and refreshes the profile", async () => {
    const states: string[] = [];
    const { controller, cache, profile, container, createDeps } = setup(async (_deps, opts) => {
      opts?.onProgress?.({ phase: "testing", streams: 16, levelIndex: 2, levelCount: 6 });
      expect(controller.getState().progress).toEqual({ phase: "testing", streams: 16, levelIndex: 2, levelCount: 6 });
      return result();
    });
    controller.subscribe(() => states.push(controller.getState().status));
    expect(controller.getState().status).toBe("idle");
    await controller.start(container, { capabilities: caps, appVersion: "v1.0.0" });
    expect(createDeps).toHaveBeenCalledWith(container);
    expect(states[0]).toBe("running");
    expect(controller.getState()).toMatchObject({ status: "done", result: { id: "r1" }, saved: true });
    expect(cache.save).toHaveBeenCalledTimes(1);
    const saved = cache.save.mock.calls[0]![0];
    expect(saved).toMatchObject({ appVersion: "v1.0.0", browser: "Chrome", score: 3 });
    expect(profile.refresh).toHaveBeenCalledTimes(1);
  });

  it("marks running synchronously and ignores a second start while running", async () => {
    let finish: (r: BenchmarkResult) => void = () => undefined;
    const run = vi.fn(() => new Promise<BenchmarkResult>((r) => (finish = r)));
    const { controller, container } = setup(run);
    const first = controller.start(container, { capabilities: caps });
    expect(controller.getState().status).toBe("running");
    await controller.start(container, { capabilities: caps });
    expect(run).toHaveBeenCalledTimes(1);
    finish(result());
    await first;
    expect(controller.getState().status).toBe("done");
  });

  it("cancels through the abort signal and keeps the aborted run out of the cache", async () => {
    const { controller, cache, profile, container } = setup(async (_deps, opts) => {
      await new Promise<void>((resolve) => opts!.signal!.addEventListener("abort", () => resolve()));
      return result({ stoppedReason: "aborted" });
    });
    const running = controller.start(container, { capabilities: caps });
    controller.cancel();
    await running;
    expect(controller.getState()).toMatchObject({ status: "cancelled", result: undefined });
    expect(cache.save).not.toHaveBeenCalled();
    expect(profile.refresh).not.toHaveBeenCalled();
  });

  it("does not cache a run that failed or that was interrupted by a hidden tab", async () => {
    const a = setup(async () => result({ stoppedReason: "error", error: "clip 404", levels: [] }));
    await a.controller.start(a.container, { capabilities: caps });
    expect(a.controller.getState()).toMatchObject({ status: "error", error: "clip 404" });
    expect(a.cache.save).not.toHaveBeenCalled();

    const b = setup(async () => result({ stoppedReason: "hidden" }));
    await b.controller.start(b.container, { capabilities: caps });
    expect(b.controller.getState().status).toBe("done");
    expect(b.cache.save).not.toHaveBeenCalled();
  });

  it("surfaces an unexpected throw as an error state", async () => {
    const { controller, container } = setup(async () => {
      throw new Error("kaboom");
    });
    await controller.start(container, { capabilities: caps });
    expect(controller.getState()).toMatchObject({ status: "error", error: "kaboom" });
  });

  it("keeps the result when the cache cannot save it", async () => {
    const { controller, cache, container } = setup(async () => result());
    cache.save.mockRejectedValueOnce(new Error("quota"));
    await controller.start(container, { capabilities: caps });
    expect(controller.getState()).toMatchObject({ status: "done", saved: false, result: { id: "r1" } });
  });

  it("can run again after finishing and clears the previous result while running", async () => {
    const { controller, container } = setup(async () => result());
    await controller.start(container, { capabilities: caps });
    const second = controller.start(container, { capabilities: caps });
    expect(controller.getState()).toMatchObject({ status: "running", result: undefined });
    await second;
    expect(controller.getState().status).toBe("done");
  });

  it("cancel is a no-op when nothing runs", () => {
    const { controller } = setup(async () => result());
    expect(() => controller.cancel()).not.toThrow();
    expect(controller.getState().status).toBe("idle");
  });
});
