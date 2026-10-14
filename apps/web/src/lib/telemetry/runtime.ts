import { estimateBottleneck, type BottleneckEstimate } from "./bottleneck";
import { createFrameSampler } from "./frameSampler";
import { classifyHealth, createHealthTracker, type HealthAssessment, type HealthState } from "./performanceClassifier";
import { sessionRegistry } from "./sessionRegistry";
import type { StreamStatsSource, VideoPerformanceMetrics, VideoTelemetrySnapshot } from "./types";
import { createVideoTelemetry, intervalScheduler, type PresentationSampler } from "./videoTelemetry";

/**
 * App-wide telemetry runtime. One aggregator serves every consumer (the Client Station page, the
 * Live developer overlay), and it runs only while at least one consumer holds it: with no
 * consumer there is no interval timer and no requestAnimationFrame loop.
 *
 * Framework-free. React reads it through `useVideoTelemetry`, which gets a new view once per
 * tick (1 Hz), never per frame.
 */

/** Everything a UI shows, replaced by a new object once per tick. */
export type TelemetryView = {
  snapshot: VideoTelemetrySnapshot;
  /** Chronological per-second totals (up to 60 s). */
  history: VideoPerformanceMetrics[];
  health: HealthAssessment;
  /** The hysteresis-filtered state to display; `health.state` is the raw one. */
  state: HealthState;
  /** Always an estimate in a browser. */
  bottleneck: BottleneckEstimate;
};

export type TelemetryRuntime = {
  /** Starts sampling on the first call; every acquire must be paired with one release. */
  acquire(): void;
  release(): void;
  consumers(): number;
  /** Cameras currently shown on screen; 0 when no grid is mounted. */
  setVisibleCameras(count: number): void;
  subscribe(listener: () => void): () => void;
  getView(): TelemetryView;
};

export type TelemetryRuntimeDeps = {
  now: () => number;
  schedule: (tick: () => void, intervalMs: number) => () => void;
  sessions: () => Iterable<StreamStatsSource>;
  presentation?: PresentationSampler;
};

function emptyView(): TelemetryView {
  const snapshot: VideoTelemetrySnapshot = { metrics: null, streams: [] };
  return { snapshot, history: [], health: classifyHealth([]), state: "unknown", bottleneck: estimateBottleneck([]) };
}

export function createTelemetryRuntime(deps: TelemetryRuntimeDeps): TelemetryRuntime {
  let visible = 0;
  let holders = 0;
  let view = emptyView();
  let tracker = createHealthTracker();
  let detach: (() => void) | undefined;
  const listeners = new Set<() => void>();
  const service = createVideoTelemetry({ ...deps, visibleCameras: () => visible });

  const onTick = (): void => {
    const snapshot = service.getSnapshot();
    const history = service.history();
    const health = classifyHealth(history, { rows: snapshot.streams });
    view = {
      snapshot,
      history,
      health,
      state: tracker.update(health),
      bottleneck: estimateBottleneck(history, snapshot.streams),
    };
    listeners.forEach((l) => {
      try {
        l();
      } catch (err) {
        console.error("telemetry runtime listener failed", err);
      }
    });
  };

  return {
    acquire() {
      holders += 1;
      if (holders > 1) return;
      detach = service.subscribe(onTick);
      service.start();
    },
    release() {
      if (holders === 0) return;
      holders -= 1;
      if (holders > 0) return;
      service.stop();
      detach?.();
      detach = undefined;
      // Stale numbers would mislead the next consumer: start from a clean slate.
      view = emptyView();
      tracker = createHealthTracker();
    },
    consumers: () => holders,
    setVisibleCameras(count) {
      visible = Number.isFinite(count) && count > 0 ? Math.floor(count) : 0;
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
    getView: () => view,
  };
}

/** The process-wide runtime, wired to the live player sessions and the browser's timers. */
export const telemetryRuntime: TelemetryRuntime = createTelemetryRuntime({
  now: () => Date.now(),
  schedule: intervalScheduler,
  sessions: () => sessionRegistry.list(),
  presentation: createFrameSampler(
    (cb) => requestAnimationFrame(cb),
    (handle) => cancelAnimationFrame(handle),
    () => performance.now(),
  ),
});
