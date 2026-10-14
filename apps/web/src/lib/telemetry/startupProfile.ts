import { collectClientCapabilities } from "./capabilities";
import { benchmarkCache, fingerprintFor, isReusable, type BenchmarkCache, type BenchmarkRecord, type ReuseVerdict } from "./benchmarkCache";
import type { ClientCapabilities } from "./types";

/**
 * Light startup analysis (spec section 8): at app start, once, off the critical path, collect the
 * cheap capability profile (no per-codec matrix beyond the videowall profile), read the latest
 * cached benchmark and decide whether it still applies. It NEVER runs the benchmark: only the
 * operator does, from the client station page. Exposed as a tiny store (getSnapshot/subscribe)
 * so the UI and later the adaptive policy can read it without owning any of this logic.
 */

export type StartupProfileSnapshot = {
  status: "idle" | "collecting" | "ready" | "error";
  /** Light capabilities (videowall codec profile only). */
  capabilities: ClientCapabilities | undefined;
  /** Latest cached benchmark, if any. */
  latest: BenchmarkRecord | undefined;
  /** Whether `latest` still describes this station; undefined without a record or capabilities. */
  reuse: ReuseVerdict | undefined;
  appVersion: string | undefined;
};

export type StartupProfileDeps = {
  collect?: () => Promise<ClientCapabilities>;
  cache?: Pick<BenchmarkCache, "latest">;
  /** Runs `fn` when the browser is idle; returns a canceller. */
  schedule?: (fn: () => void) => () => void;
  now?: () => number;
  /** The OpenVMS version, for "major version changed" invalidation. */
  loadAppVersion?: () => Promise<string | undefined>;
};

export type StartupProfile = {
  /** Schedules the analysis once; returns immediately. */
  start(): void;
  /** Re-reads the cache (after a benchmark was saved) and recomputes the verdict. */
  refresh(): Promise<void>;
  setAppVersion(version: string | undefined): void;
  getSnapshot(): StartupProfileSnapshot;
  subscribe(listener: () => void): () => void;
  dispose(): void;
};

const IDLE_TIMEOUT_MS = 2000;

/** requestIdleCallback when available, otherwise a zero-delay timer; never runs synchronously. */
export function scheduleIdle(fn: () => void): () => void {
  const g = globalThis as { requestIdleCallback?: (cb: () => void, opts?: { timeout: number }) => number; cancelIdleCallback?: (handle: number) => void };
  if (typeof g.requestIdleCallback === "function") {
    const handle = g.requestIdleCallback(fn, { timeout: IDLE_TIMEOUT_MS });
    return () => g.cancelIdleCallback?.(handle);
  }
  const timer = setTimeout(fn, 1);
  return () => clearTimeout(timer);
}

async function fetchAppVersion(): Promise<string | undefined> {
  try {
    // Public endpoint: available to every user, no token needed.
    const response = await fetch("/api/v1/system/info", { headers: { Accept: "application/json" } });
    if (!response.ok) return undefined;
    const body = (await response.json()) as { version?: unknown };
    return typeof body.version === "string" ? body.version : undefined;
  } catch {
    return undefined;
  }
}

export function createStartupProfile(deps: StartupProfileDeps = {}): StartupProfile {
  const collect = deps.collect ?? (() => collectClientCapabilities({}, { codecs: "light" }));
  const cache = deps.cache ?? benchmarkCache;
  const schedule = deps.schedule ?? scheduleIdle;
  const now = deps.now ?? (() => Date.now());
  const loadAppVersion = deps.loadAppVersion ?? fetchAppVersion;
  const listeners = new Set<() => void>();
  let snapshot: StartupProfileSnapshot = { status: "idle", capabilities: undefined, latest: undefined, reuse: undefined, appVersion: undefined };
  let started = false;
  let cancel: (() => void) | undefined;

  const publish = (patch: Partial<StartupProfileSnapshot>) => {
    const next = { ...snapshot, ...patch };
    const reuse = next.capabilities && next.latest ? isReusable(next.latest, fingerprintFor(next.capabilities, next.appVersion), now()) : undefined;
    snapshot = { ...next, reuse };
    for (const listener of [...listeners]) listener();
  };

  const readLatest = async (): Promise<BenchmarkRecord | undefined> => {
    try {
      return await cache.latest();
    } catch {
      return undefined; // The cache degrades on its own; a failed read just means no previous result.
    }
  };

  const analyze = async () => {
    publish({ status: "collecting" });
    const [capabilities, latest, appVersion] = await Promise.all([
      collect().then(
        (c) => c,
        () => undefined,
      ),
      readLatest(),
      loadAppVersion().catch(() => undefined),
    ]);
    publish({ status: capabilities ? "ready" : "error", capabilities, latest, appVersion: appVersion ?? snapshot.appVersion });
  };

  return {
    start() {
      if (started) return;
      started = true;
      cancel = schedule(() => void analyze());
    },
    async refresh() {
      publish({ latest: await readLatest() });
    },
    setAppVersion(version) {
      if (version === snapshot.appVersion) return;
      publish({ appVersion: version });
    },
    getSnapshot: () => snapshot,
    subscribe(listener) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
    dispose() {
      cancel?.();
      cancel = undefined;
      listeners.clear();
    },
  };
}

/** App-wide profile; the app shell starts it once. */
export const startupProfile: StartupProfile = createStartupProfile();
