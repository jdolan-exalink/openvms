import { isConclusiveBenchmark, runClientBenchmark, type BenchmarkDeps, type BenchmarkOptions, type BenchmarkProgress, type BenchmarkResult } from "./benchmark";
import { benchmarkCache, buildBenchmarkRecord, type BenchmarkCache } from "./benchmarkCache";
import { createBrowserBenchmarkDeps } from "./benchmarkPlayer";
import { startupProfile, type StartupProfile } from "./startupProfile";
import type { ClientCapabilities } from "./types";

/**
 * Framework-free controller of one operator-triggered benchmark run: it owns the abort signal, the
 * progress and the result, stores a conclusive result in the cache and tells the startup profile to
 * re-read it. React only subscribes to its state.
 */

export type BenchmarkRunState = {
  status: "idle" | "running" | "done" | "cancelled" | "error";
  progress: BenchmarkProgress | undefined;
  result: BenchmarkResult | undefined;
  /** Whether the result was written to the cache (it is only kept in memory otherwise). */
  saved: boolean;
  error: string | undefined;
};

export type BenchmarkContext = { capabilities: ClientCapabilities; appVersion?: string };

export type BenchmarkController = {
  /** Resolves when the run ended. Ignored while a run is in progress. */
  start(container: HTMLElement, context: BenchmarkContext): Promise<void>;
  cancel(): void;
  getState(): BenchmarkRunState;
  subscribe(listener: () => void): () => void;
};

export type BenchmarkControllerDeps = {
  run?: (deps: BenchmarkDeps, options?: BenchmarkOptions) => Promise<BenchmarkResult>;
  createDeps?: (container: HTMLElement) => BenchmarkDeps;
  cache?: Pick<BenchmarkCache, "save">;
  profile?: Pick<StartupProfile, "refresh">;
};

const IDLE: BenchmarkRunState = { status: "idle", progress: undefined, result: undefined, saved: false, error: undefined };

export function createBenchmarkController(deps: BenchmarkControllerDeps = {}): BenchmarkController {
  const run = deps.run ?? runClientBenchmark;
  const createDeps = deps.createDeps ?? createBrowserBenchmarkDeps;
  const cache = deps.cache ?? benchmarkCache;
  const profile = deps.profile ?? startupProfile;
  const listeners = new Set<() => void>();
  let state = IDLE;
  let abort: AbortController | undefined;

  const set = (patch: Partial<BenchmarkRunState>) => {
    state = { ...state, ...patch };
    for (const listener of [...listeners]) listener();
  };

  return {
    async start(container, context) {
      if (abort) return;
      const controller = new AbortController();
      abort = controller;
      set({ ...IDLE, status: "running" });
      try {
        const result = await run(createDeps(container), {
          signal: controller.signal,
          onProgress: (progress) => set({ progress }),
        });
        if (result.stoppedReason === "aborted") {
          set({ status: "cancelled", progress: undefined });
        } else if (result.stoppedReason === "error") {
          set({ status: "error", progress: undefined, error: result.error ?? "benchmark failed" });
        } else {
          let saved = false;
          if (isConclusiveBenchmark(result)) {
            try {
              await cache.save(buildBenchmarkRecord(result, context.capabilities, context.appVersion));
              saved = true;
              await profile.refresh();
            } catch {
              // Not being able to remember the result must not hide it from the operator.
            }
          }
          set({ status: "done", progress: undefined, result, saved });
        }
      } catch (e) {
        set({ status: "error", progress: undefined, error: e instanceof Error ? e.message : String(e) });
      } finally {
        abort = undefined;
      }
    },
    cancel() {
      abort?.abort();
    },
    getState: () => state,
    subscribe(listener) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
  };
}

/** App-wide controller for the client station page. */
export const benchmarkController: BenchmarkController = createBenchmarkController();
