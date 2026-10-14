import type { BenchmarkLimits, BenchmarkResult } from "./benchmark";
import type { ClientCapabilities } from "./types";

/**
 * Local cache of station benchmarks (spec section 9). A result is reused for the same browser,
 * browser major version, GPU, major OpenVMS version and video pipeline configuration, and stays
 * valid for BENCHMARK_MAX_AGE_MS; otherwise a re-run is recommended (never forced, never automatic).
 *
 * IndexedDB is wrapped behind `BenchmarkStorage`. When it is missing or throws (private mode,
 * blocked storage) the cache degrades to an in-memory store for the rest of the page's life.
 */

export const BENCHMARK_DB_NAME = "openvms-telemetry";
export const BENCHMARK_STORE = "benchmarks";
const DB_VERSION = 1;

/**
 * Version of the benchmark pipeline (sample clip, levels, player setup, thresholds). Bump it when
 * any of those change so cached results measured with the old pipeline are recommended for a re-run.
 */
export const BENCHMARK_PIPELINE_VERSION = 1;
export const BENCHMARK_MAX_AGE_MS = 30 * 24 * 60 * 60 * 1000;
/** Records kept; older ones are pruned on save. */
export const BENCHMARK_KEEP = 5;

export type BenchmarkFingerprint = {
  browser?: string;
  browserVersion?: string;
  /** Major version of the browser, as a string. */
  browserMajor?: string;
  /** `vendor | renderer` as reported by WebGL. */
  gpu?: string;
  appVersion?: string;
  /** Major OpenVMS version; for 0.x releases the minor counts as the major (`0.4`). */
  appMajor?: string;
  pipelineVersion: number;
};

export type BenchmarkRecord = {
  id: string;
  /** Epoch milliseconds when the run finished. */
  date: number;
  appVersion?: string;
  browser?: string;
  browserVersion?: string;
  gpuVendor?: string;
  gpuRenderer?: string;
  screen?: { width: number; height: number; devicePixelRatio: number };
  /** Codec string of the sample clip that was decoded. */
  codec: string;
  pipelineVersion: number;
  fingerprint: BenchmarkFingerprint;
  result: BenchmarkResult;
  score: number;
  limits: BenchmarkLimits;
};

function appMajorOf(version: string | undefined): string | undefined {
  if (!version) return undefined;
  const m = /^v?(\d+)\.(\d+)/.exec(version.trim());
  if (!m) return version.trim();
  return m[1] === "0" ? `0.${m[2]}` : m[1];
}

export function fingerprintFor(capabilities: ClientCapabilities, appVersion?: string): BenchmarkFingerprint {
  const { system, gpu } = capabilities;
  const major = system.browserVersion ? /^\d+/.exec(system.browserVersion)?.[0] : undefined;
  const gpuLabel = [gpu.vendor, gpu.renderer].filter(Boolean).join(" | ");
  return {
    browser: system.browser,
    browserVersion: system.browserVersion,
    browserMajor: major,
    gpu: gpuLabel || undefined,
    appVersion,
    appMajor: appMajorOf(appVersion),
    pipelineVersion: BENCHMARK_PIPELINE_VERSION,
  };
}

export function buildBenchmarkRecord(result: BenchmarkResult, capabilities: ClientCapabilities, appVersion?: string): BenchmarkRecord {
  return {
    id: result.id,
    date: result.finishedAt,
    appVersion,
    browser: capabilities.system.browser,
    browserVersion: capabilities.system.browserVersion,
    gpuVendor: capabilities.gpu.vendor,
    gpuRenderer: capabilities.gpu.renderer,
    screen: capabilities.system.screen,
    codec: result.clip.codec,
    pipelineVersion: BENCHMARK_PIPELINE_VERSION,
    fingerprint: fingerprintFor(capabilities, appVersion),
    result,
    score: result.clientPerformanceScore,
    limits: result.limits,
  };
}

export type ReuseReason = "browser_changed" | "browser_version_changed" | "gpu_changed" | "app_version_changed" | "pipeline_changed" | "too_old";
export type ReuseVerdict = { reusable: boolean; reasons: ReuseReason[] };

/**
 * Whether a cached record still describes this station. Every reason that applies is reported.
 * An unknown current app version (not fetched yet) never counts as a change.
 */
export function isReusable(record: BenchmarkRecord, current: BenchmarkFingerprint, now: number, maxAgeMs = BENCHMARK_MAX_AGE_MS): ReuseVerdict {
  const was = record.fingerprint;
  const reasons: ReuseReason[] = [];
  if (was.browser !== current.browser) reasons.push("browser_changed");
  else if (was.browserMajor !== current.browserMajor) reasons.push("browser_version_changed");
  if (was.gpu !== current.gpu) reasons.push("gpu_changed");
  if (current.appMajor !== undefined && was.appMajor !== current.appMajor) reasons.push("app_version_changed");
  if (was.pipelineVersion !== current.pipelineVersion) reasons.push("pipeline_changed");
  if (!Number.isFinite(record.date) || now - record.date > maxAgeMs) reasons.push("too_old");
  return { reusable: reasons.length === 0, reasons };
}

// ---------------------------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------------------------

export interface BenchmarkStorage {
  list(): Promise<BenchmarkRecord[]>;
  put(record: BenchmarkRecord): Promise<void>;
  remove(ids: string[]): Promise<void>;
}

export function createMemoryStorage(): BenchmarkStorage {
  const rows = new Map<string, BenchmarkRecord>();
  return {
    async list() {
      return [...rows.values()];
    },
    async put(record) {
      rows.set(record.id, record);
    },
    async remove(ids) {
      for (const id of ids) rows.delete(id);
    },
  };
}

function settled<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

/** Thin IndexedDB adapter. `factory` is undefined when the browser has no IndexedDB. */
export function createIndexedDbStorage(factory: IDBFactory | undefined): BenchmarkStorage {
  let opened: Promise<IDBDatabase> | undefined;
  const open = () =>
    (opened ??= new Promise<IDBDatabase>((resolve, reject) => {
      if (!factory) return reject(new Error("IndexedDB is not available"));
      const req = factory.open(BENCHMARK_DB_NAME, DB_VERSION);
      req.onupgradeneeded = () => {
        if (!req.result.objectStoreNames.contains(BENCHMARK_STORE)) req.result.createObjectStore(BENCHMARK_STORE, { keyPath: "id" });
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error ?? new Error("IndexedDB open failed"));
    }));
  const write = async (apply: (store: IDBObjectStore) => void) => {
    const db = await open();
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(BENCHMARK_STORE, "readwrite");
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
      apply(tx.objectStore(BENCHMARK_STORE));
    });
  };
  return {
    async list() {
      const db = await open();
      return settled(db.transaction(BENCHMARK_STORE, "readonly").objectStore(BENCHMARK_STORE).getAll() as IDBRequest<BenchmarkRecord[]>);
    },
    put: (record) => write((store) => void store.put(record)),
    remove: (ids) => write((store) => ids.forEach((id) => store.delete(id))),
  };
}

function browserIndexedDb(): IDBFactory | undefined {
  try {
    return typeof indexedDB === "undefined" ? undefined : indexedDB;
  } catch {
    return undefined; // Some browsers throw on access when storage is blocked.
  }
}

// ---------------------------------------------------------------------------------------------
// Cache
// ---------------------------------------------------------------------------------------------

export type BenchmarkCacheOptions = {
  storage?: BenchmarkStorage;
  fallback?: BenchmarkStorage;
  keep?: number;
  log?: (message: string, error: unknown) => void;
};

export type BenchmarkCache = {
  save(record: BenchmarkRecord): Promise<void>;
  /** Newest first. */
  list(): Promise<BenchmarkRecord[]>;
  latest(): Promise<BenchmarkRecord | undefined>;
  /** `memory` once the persistent store failed: results will not survive a reload. */
  mode(): "persistent" | "memory";
};

export function createBenchmarkCache(options: BenchmarkCacheOptions = {}): BenchmarkCache {
  const primary = options.storage ?? createIndexedDbStorage(browserIndexedDb());
  const memory = options.fallback ?? createMemoryStorage();
  const keep = options.keep ?? BENCHMARK_KEEP;
  const log = options.log ?? ((message, error) => console.warn(message, error));
  let degraded = false;

  async function run<T>(op: (storage: BenchmarkStorage) => Promise<T>): Promise<T> {
    if (!degraded) {
      try {
        return await op(primary);
      } catch (error) {
        degraded = true;
        log("benchmark cache: persistent storage failed, using memory", error);
      }
    }
    return op(memory);
  }

  const newestFirst = (rows: BenchmarkRecord[]) => [...rows].sort((a, b) => b.date - a.date);

  const list = () => run(async (storage) => newestFirst(await storage.list()));

  return {
    async save(record) {
      await run(async (storage) => {
        await storage.put(record);
        const rows = newestFirst(await storage.list());
        if (rows.length > keep) await storage.remove(rows.slice(keep).map((r) => r.id));
      });
    },
    list,
    async latest() {
      return (await list())[0];
    },
    mode: () => (degraded ? "memory" : "persistent"),
  };
}

/** App-wide cache. */
export const benchmarkCache: BenchmarkCache = createBenchmarkCache();
