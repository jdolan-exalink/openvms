/**
 * Maps performance metrics (M-W10). A tiny sampler the MapShell installs as
 * `window.__openvmsMapMetrics`: the design's overlay for the perf targets (FPS from
 * rAF deltas, entities visible, events/s applied, WS lag, time to first render) plus the
 * longest main-thread task observed after the first render — that last one is the design's
 * "no main-thread task > 50 ms" check.
 *
 * Collection only starts in dev builds or with the `?perf` URL param, so production users
 * never pay for the rAF loop; the Playwright smoke passes the param to profile a
 * production build.
 */

export interface PerfMetricsSnapshot {
  /** Smoothed frames per second over the rolling frame window; 0 before two samples. */
  fps: number;
  entitiesVisible: number;
  /** Frames applied in the last second. */
  eventsPerSec: number;
  /** Lag of the last realtime frame: `now − frame.ts`, null while no frame arrived. */
  wsLagMs: number | null;
  /** From sampler install until entities were first on screen; null before that. */
  timeToFirstRenderMs: number | null;
  /** Longest main-thread task observed AFTER the first render; cold start is excluded. */
  maxLongTaskMs: number;
}

export interface PerfMetrics {
  /** Call once per animation frame with the frame timestamp. */
  sample: (nowMs: number) => void;
  setEntitiesVisible: (count: number) => void;
  recordEventsApplied: (count: number) => void;
  recordWsLag: (ms: number) => void;
  markFirstRender: () => void;
  /**
   * Starts the long-task budget: only tasks beginning after this point count. The caller
   * signals it when the render loop first goes idle, so init work (first GeoJSON build,
   * cluster index, tile decode) stays out of the steady-state budget.
   */
  markRenderSettled: () => void;
  /** `startTimeMs` decides: a task counts only when it began after the render settled. */
  recordLongTask: (startTimeMs: number, durationMs: number) => void;
  snapshot: () => PerfMetricsSnapshot;
  /** The live object installed on the window; mutated in place, cheap to read. */
  live: () => PerfMetricsSnapshot;
}

export interface CreatePerfMetricsOptions {
  now?: () => number;
  /** Sliding window for eventsPerSec, in ms. Default 1000. */
  windowMs?: number;
  /** How many frame deltas the fps window keeps. Default 30. */
  frameWindow?: number;
}

export function createPerfMetrics(opts: CreatePerfMetricsOptions = {}): PerfMetrics {
  const now = opts.now ?? (() => performance.now());
  const windowMs = opts.windowMs ?? 1_000;
  const frameWindow = opts.frameWindow ?? 30;

  const state: PerfMetricsSnapshot = {
    fps: 0,
    entitiesVisible: 0,
    eventsPerSec: 0,
    wsLagMs: null,
    timeToFirstRenderMs: null,
    maxLongTaskMs: 0,
  };
  const deltas: number[] = [];
  const events: Array<{ t: number; n: number }> = [];
  let prevFrame: number | null = null;
  let firstRenderAt: number | null = null;
  let budgetStart: number | null = null;
  const start = now();

  const updateEventRate = () => {
    const cutoff = now() - windowMs;
    while (events.length > 0 && events[0]!.t < cutoff) events.shift();
    state.eventsPerSec = events.reduce((sum, item) => sum + item.n, 0);
  };

  return {
    sample(nowMs) {
      if (prevFrame !== null) {
        const delta = nowMs - prevFrame;
        if (delta > 0) {
          deltas.push(delta);
          if (deltas.length > frameWindow) deltas.shift();
          const total = deltas.reduce((sum, d) => sum + d, 0);
          state.fps = Math.round((1000 * deltas.length) / total);
        }
      }
      prevFrame = nowMs;
      updateEventRate();
    },

    setEntitiesVisible(count) {
      state.entitiesVisible = count;
    },

    recordEventsApplied(count) {
      events.push({ t: now(), n: count });
      updateEventRate();
    },

    recordWsLag(ms) {
      state.wsLagMs = ms;
    },

    markFirstRender() {
      if (firstRenderAt === null && state.entitiesVisible > 0) {
        firstRenderAt = now();
        state.timeToFirstRenderMs = Math.max(0, Math.round(firstRenderAt - start));
      }
    },

    markRenderSettled() {
      if (budgetStart === null) budgetStart = now();
    },

    recordLongTask(startTimeMs, durationMs) {
      // Cold start is exempt: a task only counts when it STARTED after the render loop
      // first went idle. Long-task entries can be delivered late (a buffered observer
      // replays them), so the start time — not the delivery time — decides.
      if (budgetStart === null || startTimeMs < budgetStart) return;
      state.maxLongTaskMs = Math.max(state.maxLongTaskMs, Math.round(durationMs));
    },

    snapshot() {
      updateEventRate();
      return { ...state };
    },

    live() {
      updateEventRate();
      return state;
    },
  };
}

export const PERF_SEARCH_PARAM = "perf";

/** Collection runs in dev builds always, or anywhere the URL carries the perf param. */
export function perfMetricsEnabled(search: string, dev: boolean): boolean {
  return dev || new URLSearchParams(search).has(PERF_SEARCH_PARAM);
}

export interface InstallPerfMetricsOptions {
  /** Injectable frame source for tests; defaults to the window's rAF loop. */
  raf?: (callback: (t: number) => void) => () => void;
  /** Long-task observation can be turned off where PerformanceObserver is missing. */
  observeLongTasks?: boolean;
}

export interface InstalledPerfMetrics {
  metrics: PerfMetrics;
  stop: () => void;
}

/**
 * installPerfMetrics starts the sampler and publishes its live snapshot under
 * `window.__openvmsMapMetrics`. stop() halts the rAF loop and the long-task observer.
 */
export function installPerfMetrics(
  target: Window & { __openvmsMapMetrics?: PerfMetricsSnapshot },
  opts: InstallPerfMetricsOptions = {},
): InstalledPerfMetrics {
  const metrics = createPerfMetrics({ now: () => performance.now() });
  target.__openvmsMapMetrics = metrics.live();

  const raf = opts.raf ?? ((callback: (t: number) => void) => {
    const id = target.requestAnimationFrame(callback);
    return () => target.cancelAnimationFrame(id);
  });

  let running = true;
  const tick = (t: number) => {
    if (!running) return;
    metrics.sample(t);
    raf(tick);
  };
  raf(tick);

  let disconnect: (() => void) | undefined;
  const Observer = (target as { PerformanceObserver?: typeof PerformanceObserver }).PerformanceObserver;
  if (opts.observeLongTasks !== false && typeof Observer === "function") {
    try {
      const observer = new Observer(list => {
        for (const entry of list.getEntries()) metrics.recordLongTask(entry.startTime, entry.duration);
      });
      observer.observe({ type: "longtask", buffered: true } as PerformanceObserverInit);
      disconnect = () => observer.disconnect();
    } catch {
      // longtask is not universal (Safari, jsdom): the budget check just stays at 0.
    }
  }

  return {
    metrics,
    stop: () => {
      running = false;
      disconnect?.();
    },
  };
}

declare global {
  interface Window {
    __openvmsMapMetrics?: PerfMetricsSnapshot;
  }
}
