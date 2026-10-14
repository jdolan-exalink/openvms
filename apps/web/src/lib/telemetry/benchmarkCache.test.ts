import { describe, expect, it, vi } from "vitest";
import type { BenchmarkResult } from "./benchmark";
import {
  BENCHMARK_DB_NAME,
  BENCHMARK_MAX_AGE_MS,
  BENCHMARK_PIPELINE_VERSION,
  BENCHMARK_STORE,
  buildBenchmarkRecord,
  createBenchmarkCache,
  createIndexedDbStorage,
  createMemoryStorage,
  fingerprintFor,
  isReusable,
  type BenchmarkRecord,
  type BenchmarkStorage,
} from "./benchmarkCache";
import type { ClientCapabilities } from "./types";

function caps(patch: { browser?: string; browserVersion?: string; renderer?: string; vendor?: string } = {}): ClientCapabilities {
  return {
    runtime: "browser",
    collectedAt: 1,
    system: {
      browser: "browser" in patch ? patch.browser : "Chrome",
      browserVersion: "browserVersion" in patch ? patch.browserVersion : "126.0.6478.61",
      screen: { width: 1920, height: 1080, devicePixelRatio: 1 },
    },
    cpu: { logicalProcessors: 8 },
    memory: { bucketed: true, source: "deviceMemory", reportedGB: 8 },
    gpu: { webgl: true, webgl2: true, vendor: patch.vendor ?? "NVIDIA", renderer: "renderer" in patch ? patch.renderer : "RTX 3060" },
    codecs: [],
  };
}

function result(patch: Partial<BenchmarkResult> = {}): BenchmarkResult {
  return {
    id: "r1",
    startedAt: 1_000,
    finishedAt: 5_000,
    durationMs: 4_000,
    clip: { codec: "avc1.42E01E", width: 640, height: 360, fps: 15 },
    levels: [],
    maxStableStreams: 32,
    clientPerformanceScore: 77,
    limits: { bindingFactors: [], saturatedAtStreams: 48 },
    stoppedReason: "saturated",
    ...patch,
  };
}

const NOW = 10_000_000_000;
const DAY = 24 * 60 * 60 * 1000;

function record(date = NOW - DAY, c = caps(), appVersion = "v1.4.2", id = `rec-${date}`): BenchmarkRecord {
  return buildBenchmarkRecord(result({ id, finishedAt: date }), c, appVersion);
}

describe("buildBenchmarkRecord", () => {
  it("captures what the spec asks to cache next to the raw result", () => {
    const r = record(NOW - DAY);
    expect(r).toMatchObject({
      date: NOW - DAY,
      appVersion: "v1.4.2",
      browser: "Chrome",
      browserVersion: "126.0.6478.61",
      gpuVendor: "NVIDIA",
      gpuRenderer: "RTX 3060",
      screen: { width: 1920, height: 1080, devicePixelRatio: 1 },
      codec: "avc1.42E01E",
      pipelineVersion: BENCHMARK_PIPELINE_VERSION,
      score: 77,
      limits: { saturatedAtStreams: 48 },
    });
    expect(r.result.maxStableStreams).toBe(32);
    expect(r.id).toBe(`rec-${NOW - DAY}`);
  });
});

describe("fingerprintFor", () => {
  it("reduces the browser version to its major and the app version to its major (0.x keeps the minor)", () => {
    expect(fingerprintFor(caps(), "v2.3.4")).toMatchObject({ browser: "Chrome", browserMajor: "126", gpu: "NVIDIA | RTX 3060", appMajor: "2", pipelineVersion: BENCHMARK_PIPELINE_VERSION });
    expect(fingerprintFor(caps(), "0.4.1").appMajor).toBe("0.4");
    expect(fingerprintFor(caps(), "dev").appMajor).toBe("dev");
    expect(fingerprintFor(caps(), undefined).appMajor).toBeUndefined();
  });
  it("does not invent a GPU or a browser", () => {
    const fp = fingerprintFor(caps({ browser: undefined, browserVersion: undefined, renderer: undefined, vendor: "" }), "v1.0.0");
    expect(fp.browser).toBeUndefined();
    expect(fp.browserMajor).toBeUndefined();
    expect(fp.gpu).toBeUndefined();
  });
});

describe("isReusable", () => {
  const current = fingerprintFor(caps(), "v1.4.2");

  it("reuses a recent result for the same browser, version, GPU, app and pipeline", () => {
    expect(isReusable(record(), current, NOW)).toEqual({ reusable: true, reasons: [] });
  });
  it("tolerates minor changes: a browser patch release and an app minor/patch", () => {
    const c = fingerprintFor(caps({ browserVersion: "126.0.9999.1" }), "v1.9.0");
    expect(isReusable(record(), c, NOW).reusable).toBe(true);
  });
  it.each([
    ["browser_changed", fingerprintFor(caps({ browser: "Firefox", browserVersion: "126.0" }), "v1.4.2")],
    ["browser_version_changed", fingerprintFor(caps({ browserVersion: "127.0.0.0" }), "v1.4.2")],
    ["gpu_changed", fingerprintFor(caps({ renderer: "Intel UHD" }), "v1.4.2")],
    ["app_version_changed", fingerprintFor(caps(), "v2.0.0")],
  ])("recommends repeating when %s", (reason, fp) => {
    const verdict = isReusable(record(), fp, NOW);
    expect(verdict.reusable).toBe(false);
    expect(verdict.reasons).toContain(reason);
  });
  it("recommends repeating when the video pipeline configuration changed", () => {
    const verdict = isReusable(record(), { ...current, pipelineVersion: BENCHMARK_PIPELINE_VERSION + 1 }, NOW);
    expect(verdict).toEqual({ reusable: false, reasons: ["pipeline_changed"] });
  });
  it("expires after the maximum age, and reports every reason at once", () => {
    expect(isReusable(record(NOW - BENCHMARK_MAX_AGE_MS + 1), current, NOW).reusable).toBe(true);
    expect(isReusable(record(NOW - BENCHMARK_MAX_AGE_MS - 1), current, NOW)).toEqual({ reusable: false, reasons: ["too_old"] });
    const many = isReusable(record(NOW - 60 * DAY), fingerprintFor(caps({ renderer: "x" }), "v9.0.0"), NOW);
    expect(many.reasons).toEqual(expect.arrayContaining(["too_old", "gpu_changed", "app_version_changed"]));
  });
  it("does not blame the app version when the current one is not known yet", () => {
    expect(isReusable(record(), fingerprintFor(caps(), undefined), NOW).reusable).toBe(true);
  });
  it("treats an unreadable date as too old", () => {
    expect(isReusable({ ...record(), date: Number.NaN }, current, NOW).reasons).toContain("too_old");
  });
});

describe("memory storage and cache", () => {
  it("returns the newest record first and keeps only the latest few", async () => {
    const cache = createBenchmarkCache({ storage: createMemoryStorage(), keep: 3 });
    for (let i = 1; i <= 5; i++) await cache.save(record(i * 1000));
    const all = await cache.list();
    expect(all.map((r) => r.date)).toEqual([5000, 4000, 3000]);
    expect((await cache.latest())?.date).toBe(5000);
  });
  it("has no latest record when empty", async () => {
    expect(await createBenchmarkCache({ storage: createMemoryStorage() }).latest()).toBeUndefined();
  });
  it("replaces a record saved twice with the same id", async () => {
    const cache = createBenchmarkCache({ storage: createMemoryStorage() });
    await cache.save(record(1000, caps(), "v1", "same"));
    await cache.save(record(2000, caps(), "v1", "same"));
    expect(await cache.list()).toHaveLength(1);
  });
});

describe("cache fallback", () => {
  const failing = (): BenchmarkStorage => ({
    list: vi.fn(async () => {
      throw new Error("denied");
    }),
    put: vi.fn(async () => {
      throw new Error("denied");
    }),
    remove: vi.fn(async () => {
      throw new Error("denied");
    }),
  });

  it("falls back to memory when the persistent store fails, without throwing", async () => {
    const log = vi.fn();
    const primary = failing();
    const cache = createBenchmarkCache({ storage: primary, log });
    expect(cache.mode()).toBe("persistent");
    await expect(cache.save(record(1000))).resolves.toBeUndefined();
    expect(cache.mode()).toBe("memory");
    expect((await cache.latest())?.date).toBe(1000);
    expect(log).toHaveBeenCalledTimes(1);
    // Once degraded it stays on memory and does not hammer the broken store.
    await cache.save(record(2000));
    expect(primary.put).toHaveBeenCalledTimes(1);
    expect((await cache.list()).map((r) => r.date)).toEqual([2000, 1000]);
  });
  it("falls back on a failing read too", async () => {
    const cache = createBenchmarkCache({ storage: failing() });
    expect(await cache.latest()).toBeUndefined();
    expect(cache.mode()).toBe("memory");
  });
  it("uses memory when IndexedDB does not exist (private mode, old browsers)", async () => {
    const cache = createBenchmarkCache({ storage: createIndexedDbStorage(undefined) });
    await cache.save(record(1000));
    expect((await cache.latest())?.date).toBe(1000);
    expect(cache.mode()).toBe("memory");
  });
});

// A tiny IDBFactory fake: enough to prove the adapter uses the right database, store and key.
function fakeIndexedDb() {
  const rows = new Map<string, unknown>();
  const created: { name?: string; version?: number; store?: string; options?: unknown } = {};
  const asRequest = <T,>(value: T) => {
    const req: { result: T; onsuccess?: () => void; onerror?: () => void } = { result: value };
    queueMicrotask(() => req.onsuccess?.());
    return req;
  };
  const store = {
    put: (row: { id: string }) => asRequest((rows.set(row.id, row), row.id)),
    getAll: () => asRequest([...rows.values()]),
    delete: (id: string) => asRequest((rows.delete(id), undefined)),
  };
  const db = {
    objectStoreNames: { contains: (n: string) => n === created.store },
    createObjectStore: (name: string, options: unknown) => {
      created.store = name;
      created.options = options;
    },
    transaction: () => {
      const tx: { objectStore: () => typeof store; oncomplete?: () => void; onerror?: () => void } = { objectStore: () => store };
      queueMicrotask(() => queueMicrotask(() => tx.oncomplete?.()));
      return tx;
    },
  };
  const factory = {
    open: (name: string, version: number) => {
      created.name = name;
      created.version = version;
      const req: { result: typeof db; onupgradeneeded?: () => void; onsuccess?: () => void; onerror?: () => void } = { result: db };
      queueMicrotask(() => {
        req.onupgradeneeded?.();
        req.onsuccess?.();
      });
      return req;
    },
  };
  return { factory: factory as unknown as IDBFactory, rows, created };
}

describe("createIndexedDbStorage", () => {
  it("stores records by id in the openvms-telemetry/benchmarks store", async () => {
    const idb = fakeIndexedDb();
    const storage = createIndexedDbStorage(idb.factory);
    await storage.put(record(1000, caps(), "v1", "a"));
    await storage.put(record(2000, caps(), "v1", "b"));
    expect(idb.created).toMatchObject({ name: BENCHMARK_DB_NAME, version: 1, store: BENCHMARK_STORE, options: { keyPath: "id" } });
    expect((await storage.list()).map((r) => r.id).sort()).toEqual(["a", "b"]);
    await storage.remove(["a"]);
    expect((await storage.list()).map((r) => r.id)).toEqual(["b"]);
  });
  it("rejects when the database cannot be opened, so the cache can degrade", async () => {
    const factory = {
      open: () => {
        const req: { onerror?: () => void; error: Error } = { error: new Error("blocked") };
        queueMicrotask(() => req.onerror?.());
        return req;
      },
    } as unknown as IDBFactory;
    await expect(createIndexedDbStorage(factory).list()).rejects.toThrow("blocked");
  });
});
