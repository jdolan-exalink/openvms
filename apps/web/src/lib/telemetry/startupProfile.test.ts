import { afterEach, describe, expect, it, vi } from "vitest";
import { BENCHMARK_MAX_AGE_MS, buildBenchmarkRecord, type BenchmarkRecord } from "./benchmarkCache";
import type { BenchmarkResult } from "./benchmark";
import type { ClientCapabilities } from "./types";

const h = vi.hoisted(() => ({ collect: vi.fn(), run: vi.fn() }));
vi.mock("./capabilities", async (orig) => ({ ...(await orig<typeof import("./capabilities")>()), collectClientCapabilities: h.collect }));
vi.mock("./benchmark", async (orig) => ({ ...(await orig<typeof import("./benchmark")>()), runClientBenchmark: h.run }));

import { createStartupProfile, scheduleIdle } from "./startupProfile";

const NOW = 5_000_000_000;

function caps(renderer = "RTX 3060"): ClientCapabilities {
  return {
    runtime: "browser",
    collectedAt: NOW,
    system: { browser: "Chrome", browserVersion: "126.0.1" },
    cpu: {},
    memory: { bucketed: false, source: "unavailable" },
    gpu: { webgl: true, webgl2: true, vendor: "NVIDIA", renderer },
    codecs: [],
  };
}

function record(c = caps(), date = NOW - 1000, appVersion = "v1.2.0"): BenchmarkRecord {
  const result = { id: "r", startedAt: date - 4000, finishedAt: date, durationMs: 4000, clip: { codec: "avc1.42E01E", width: 640, height: 360, fps: 15 }, levels: [], maxStableStreams: 16, clientPerformanceScore: 50, limits: { bindingFactors: [] }, stoppedReason: "completed" } satisfies BenchmarkResult;
  return buildBenchmarkRecord(result, c, appVersion);
}

function setup(over: { latest?: BenchmarkRecord; version?: string | undefined } = {}) {
  const jobs: (() => void)[] = [];
  const cancel = vi.fn();
  const cache = { latest: vi.fn(async () => over.latest) };
  h.collect.mockImplementation(async () => caps());
  const profile = createStartupProfile({
    cache,
    schedule: (fn) => {
      jobs.push(fn);
      return cancel;
    },
    now: () => NOW,
    loadAppVersion: async () => over.version,
  });
  const flush = async () => {
    jobs.shift()?.();
    await vi.waitFor(() => expect(profile.getSnapshot().status).not.toBe("collecting"));
  };
  return { profile, jobs, cache, flush, cancel };
}

afterEach(() => vi.clearAllMocks());

describe("startup profile", () => {
  it("does not block: start only schedules, and collects nothing until the scheduled job runs", () => {
    const { profile, jobs } = setup();
    profile.start();
    expect(jobs).toHaveLength(1);
    expect(h.collect).not.toHaveBeenCalled();
    expect(profile.getSnapshot().status).toBe("idle");
  });

  it("collects the LIGHT profile once, reads the cache and never runs the benchmark", async () => {
    const { profile, flush } = setup({ latest: record(), version: "v1.2.0" });
    profile.start();
    profile.start(); // idempotent
    await flush();
    expect(h.collect).toHaveBeenCalledTimes(1);
    expect(h.collect).toHaveBeenCalledWith({}, expect.objectContaining({ codecs: "light" }));
    expect(h.run).not.toHaveBeenCalled();
    const snap = profile.getSnapshot();
    expect(snap.status).toBe("ready");
    expect(snap.capabilities?.gpu.renderer).toBe("RTX 3060");
    expect(snap.latest?.score).toBe(50);
    expect(snap.reuse).toEqual({ reusable: true, reasons: [] });
  });

  it("recommends a re-run when the GPU or the app version changed, or the record is stale", async () => {
    const a = setup({ latest: record(caps("Intel UHD")), version: "v1.2.0" });
    a.profile.start();
    await a.flush();
    expect(a.profile.getSnapshot().reuse?.reasons).toEqual(["gpu_changed"]);

    const b = setup({ latest: record(caps(), NOW - BENCHMARK_MAX_AGE_MS - 1) });
    b.profile.start();
    await b.flush();
    expect(b.profile.getSnapshot().reuse?.reasons).toEqual(["too_old"]);
  });

  it("has no reuse verdict when nothing was ever benchmarked", async () => {
    const { profile, flush } = setup({ latest: undefined });
    profile.start();
    await flush();
    expect(profile.getSnapshot()).toMatchObject({ status: "ready", latest: undefined, reuse: undefined });
  });

  it("notifies subscribers and stops after unsubscribe", async () => {
    const { profile, flush } = setup();
    const listener = vi.fn();
    const off = profile.subscribe(listener);
    profile.start();
    await flush();
    expect(listener).toHaveBeenCalled();
    const calls = listener.mock.calls.length;
    off();
    profile.setAppVersion("v9.9.9");
    expect(listener.mock.calls.length).toBe(calls);
  });

  it("recomputes the verdict when the app version becomes known or the cache changes", async () => {
    const stored = { current: record(caps(), NOW - 1000, "v1.2.0") as BenchmarkRecord | undefined };
    const { profile, flush, cache } = setup({ latest: stored.current });
    profile.start();
    await flush();
    expect(profile.getSnapshot().reuse?.reusable).toBe(true);
    profile.setAppVersion("v2.0.0");
    expect(profile.getSnapshot().reuse?.reasons).toEqual(["app_version_changed"]);
    cache.latest.mockImplementation(async () => record(caps(), NOW, "v2.0.0"));
    await profile.refresh();
    expect(profile.getSnapshot().reuse).toEqual({ reusable: true, reasons: [] });
  });

  it("reports an error status when the capabilities cannot be collected, and keeps the cached result", async () => {
    const { profile, jobs } = setup({ latest: record() });
    h.collect.mockRejectedValueOnce(new Error("boom"));
    profile.start();
    jobs.shift()?.();
    await vi.waitFor(() => expect(profile.getSnapshot().status).toBe("error"));
    expect(profile.getSnapshot().latest?.score).toBe(50);
    expect(profile.getSnapshot().reuse).toBeUndefined();
  });

  it("never throws if the cache read fails", async () => {
    const { profile, cache, flush } = setup();
    cache.latest.mockRejectedValueOnce(new Error("idb"));
    profile.start();
    await flush();
    expect(profile.getSnapshot().status).toBe("ready");
    expect(profile.getSnapshot().latest).toBeUndefined();
  });

  it("dispose cancels a pending job", () => {
    const { profile, cancel } = setup();
    profile.start();
    profile.dispose();
    expect(cancel).toHaveBeenCalled();
  });
});

describe("scheduleIdle", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("uses requestIdleCallback when the browser has it", () => {
    const ric = vi.fn(() => 7);
    const cic = vi.fn();
    vi.stubGlobal("requestIdleCallback", ric);
    vi.stubGlobal("cancelIdleCallback", cic);
    const fn = vi.fn();
    const cancel = scheduleIdle(fn);
    expect(ric).toHaveBeenCalledWith(fn, expect.objectContaining({ timeout: expect.any(Number) }));
    cancel();
    expect(cic).toHaveBeenCalledWith(7);
  });

  it("falls back to a timer without blocking the caller", () => {
    vi.useFakeTimers();
    vi.stubGlobal("requestIdleCallback", undefined);
    const fn = vi.fn();
    scheduleIdle(fn);
    expect(fn).not.toHaveBeenCalled();
    vi.runAllTimers();
    expect(fn).toHaveBeenCalledTimes(1);
    vi.useRealTimers();
  });
});
