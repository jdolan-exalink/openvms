import { RingBuffer } from "./ringBuffer";
import type { PresentationSample } from "./frameSampler";
import type { StreamStatsSample, StreamStatsSource, StreamTelemetryRow, VideoPerformanceMetrics, VideoTelemetrySnapshot } from "./types";

/**
 * Samples live video statistics once per interval and aggregates them for a UI.
 *
 * Scope: every byte and bitrate here is OpenVMS video traffic received by the player. It is
 * deliberately not the traffic of the machine's network interface.
 *
 * Framework-free: time, scheduling and the source of streams are injected. The snapshot API
 * (`subscribe`/`getSnapshot`) is compatible with React's useSyncExternalStore, and listeners are
 * notified at most once per tick.
 */

export type PresentationSampler = {
  start(): void;
  stop(): void;
  sample(): PresentationSample | undefined;
};

export type VideoTelemetryOptions = {
  now: () => number;
  /** Like setInterval; returns a function that cancels the schedule. */
  schedule: (tick: () => void, intervalMs: number) => () => void;
  /** The live streams to sample, for example `() => sessionRegistry.list()`. */
  sessions: () => Iterable<StreamStatsSource>;
  /** Cameras currently shown on screen; reported as 0 when not provided. */
  visibleCameras?: () => number;
  presentation?: PresentationSampler;
  intervalMs?: number;
  /** Number of per-interval totals kept for the short history chart. */
  historyLength?: number;
};

export type VideoTelemetryService = {
  start(): void;
  stop(): void;
  subscribe(listener: () => void): () => void;
  getSnapshot(): VideoTelemetrySnapshot;
  /** Chronological copy of the retained per-interval totals. */
  history(): VideoPerformanceMetrics[];
};

const DEFAULT_INTERVAL_MS = 1000;
const DEFAULT_HISTORY = 60;
/** Player states in which a stream is delivering media. */
const DELIVERING_STATES: ReadonlySet<string> = new Set(["ACTIVE", "WARM"]);

const EMPTY_SNAPSHOT: VideoTelemetrySnapshot = { metrics: null, streams: [] };

/** Browser default scheduler. */
export const intervalScheduler = (tick: () => void, intervalMs: number): (() => void) => {
  const id = setInterval(tick, intervalMs);
  return () => clearInterval(id);
};

/** Cumulative delta; a counter that went backwards was reset, so it restarts from zero. */
function delta(current: number, previous: number): number {
  return current >= previous ? current - previous : 0;
}

/**
 * readStats isolates one faulty source so it cannot abort the tick for every other stream. A source
 * that keeps failing is logged once (tracked in `reported`); the mark is cleared as soon as it reads
 * successfully, so a later new failure is reported again.
 */
function readStats(source: StreamStatsSource, reported: WeakSet<StreamStatsSource>): StreamStatsSample | undefined {
  try {
    const sample = source.streamStats();
    reported.delete(source);
    return sample;
  } catch (err) {
    if (!reported.has(source)) {
      reported.add(source);
      console.error("video telemetry source failed", err);
    }
    return undefined;
  }
}

export function createVideoTelemetry(opts: VideoTelemetryOptions): VideoTelemetryService {
  const intervalMs = opts.intervalMs ?? DEFAULT_INTERVAL_MS;
  const ring = new RingBuffer<VideoPerformanceMetrics>(opts.historyLength ?? DEFAULT_HISTORY);
  const listeners = new Set<() => void>();
  /** Sources whose current failure was already logged. */
  const reported = new WeakSet<StreamStatsSource>();
  /** Previous cumulative sample per source; sources that vanish are pruned each tick. */
  let baselines = new Map<StreamStatsSource, StreamStatsSample>();
  let snapshot = EMPTY_SNAPSHOT;
  let cancel: (() => void) | undefined;
  let lastTickAt = 0;

  const tick = (): void => {
    const timestamp = opts.now();
    const elapsedS = (timestamp - lastTickAt) / 1000;
    lastTickAt = timestamp;

    const seen = new Map<StreamStatsSource, StreamStatsSample>();
    const rows: StreamTelemetryRow[] = [];
    let active = 0;
    let main = 0;
    let sub = 0;
    let bytes = 0;
    let decoded = 0;
    let dropped = 0;
    let stalls = 0;

    for (const source of opts.sessions()) {
      const s = readStats(source, reported);
      if (!s) continue;
      seen.set(source, s);
      if (s.quality === "main") main += 1;
      else sub += 1;
      if (DELIVERING_STATES.has(s.state)) active += 1;

      const prev = baselines.get(source);
      // A stream seen for the first time only establishes its baseline.
      const byteDelta = prev ? delta(s.bytesReceived, prev.bytesReceived) : 0;
      // Frame counters that just became available only establish their baseline as well.
      const decodedDelta = s.decodedFrames !== undefined && prev?.decodedFrames !== undefined ? delta(s.decodedFrames, prev.decodedFrames) : 0;
      const droppedDelta = s.droppedFrames !== undefined && prev?.droppedFrames !== undefined ? delta(s.droppedFrames, prev.droppedFrames) : 0;
      const stallDelta = prev ? delta(s.stalls, prev.stalls) : 0;
      bytes += byteDelta;
      decoded += decodedDelta;
      dropped += droppedDelta;
      stalls += stallDelta;

      const rate = (n: number): number => (elapsedS > 0 ? n / elapsedS : 0);
      rows.push({
        cameraId: s.cameraId,
        quality: s.quality,
        state: s.state,
        width: s.width,
        height: s.height,
        fps: s.decodedFrames === undefined ? undefined : rate(decodedDelta),
        droppedFps: s.droppedFrames === undefined ? undefined : rate(droppedDelta),
        bitrateBps: rate(byteDelta * 8),
      });
    }
    baselines = seen;

    const presentation = opts.presentation?.sample();
    // decoded is getVideoPlaybackQuality().totalVideoFrames, which already counts dropped frames.
    const totalFrames = decoded;
    const metrics: VideoPerformanceMetrics = {
      timestamp,
      visibleCameras: opts.visibleCameras?.() ?? 0,
      activeStreams: active,
      mainStreams: main,
      subStreams: sub,
      receivedMbps: elapsedS > 0 ? (bytes * 8) / elapsedS / 1_000_000 : 0,
      decodedFrames: decoded,
      droppedFrames: dropped,
      droppedFrameRatio: totalFrames > 0 ? dropped / totalFrames : 0,
      presentationFps: presentation?.fps,
      renderFrameTimeMs: presentation?.avgFrameMs,
      renderFrameTimeP95Ms: presentation?.p95FrameMs,
      stalls,
    };
    ring.push(metrics);
    snapshot = { metrics, streams: rows };
    // One faulty consumer must not starve the others or break the interval.
    listeners.forEach((l) => {
      try {
        l();
      } catch (err) {
        console.error("video telemetry listener failed", err);
      }
    });
  };

  return {
    start() {
      if (cancel) return;
      // A fresh baseline: time spent stopped must not show up as a burst of traffic.
      baselines = new Map();
      for (const source of opts.sessions()) {
        const s = readStats(source, reported);
        if (s) baselines.set(source, s);
      }
      lastTickAt = opts.now();
      opts.presentation?.start();
      cancel = opts.schedule(tick, intervalMs);
    },
    stop() {
      if (!cancel) return;
      cancel();
      cancel = undefined;
      opts.presentation?.stop();
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => void listeners.delete(listener);
    },
    getSnapshot: () => snapshot,
    history: () => ring.toArray(),
  };
}
