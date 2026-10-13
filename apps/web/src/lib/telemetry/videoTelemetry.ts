import type { PlayerState } from "../live/playerState";
import { classifyPerformance, type ClassifierSample, type PerformanceHealth } from "./performanceClassifier";

/** Number of 1 Hz samples kept (one minute). */
export const HISTORY_SIZE = 60;
const SAMPLE_INTERVAL_MS = 1000;

/** What the service needs to observe from a live session (kept structural to stay framework- and class-agnostic). */
export type TelemetrySource = {
  cameraId: string;
  quality: "main" | "sub";
  getState: () => PlayerState;
  video: HTMLVideoElement;
};

export type TelemetryHandle = {
  /** O(1): adds received media bytes. */
  addBytes: (bytes: number) => void;
  unregister: () => void;
};

export type StreamCounts = {
  /** Media flowing (ACTIVE / WARM). */
  playing: number;
  /** CONNECTING / BUFFERING / RECONNECTING. */
  connecting: number;
  /** SUSPENDED / IDLE: "En pausa". */
  paused: number;
  error: number;
  main: number;
  sub: number;
  /** All counted sessions (excludes not-yet-started and evicted ones). */
  total: number;
};

export type SessionTelemetry = { cameraId: string; quality: "main" | "sub"; state: PlayerState; mbps: number };

export type HistoryPoint = ClassifierSample & { at: number; mbps: number };

export type VideoTelemetrySnapshot = {
  at: number;
  /** Total received video, megabits per second over the last interval. */
  mbps: number;
  /** Average presented frames per second per ACTIVE stream; null when not measurable (informational). */
  fps: number | null;
  droppedFrames: number;
  /** Dropped / decoded frames in the last interval, 0..1. */
  droppedRatio: number;
  streams: StreamCounts;
  sessions: readonly SessionTelemetry[];
  /** Oldest first, at most HISTORY_SIZE entries. */
  history: readonly HistoryPoint[];
  health: PerformanceHealth;
};

type Entry = {
  source: TelemetrySource;
  bytes: number;
  lastBytes: number;
  lastTotalFrames: number | null;
  lastDroppedFrames: number;
};

function emptyCounts(): StreamCounts {
  return { playing: 0, connecting: 0, paused: 0, error: 0, main: 0, sub: 0, total: 0 };
}

const EMPTY_SNAPSHOT: VideoTelemetrySnapshot = {
  at: 0,
  mbps: 0,
  fps: null,
  droppedFrames: 0,
  droppedRatio: 0,
  streams: emptyCounts(),
  sessions: [],
  history: [],
  health: "optimal",
};

function readFrames(video: HTMLVideoElement): { total: number; dropped: number } | null {
  try {
    const q = video.getVideoPlaybackQuality?.();
    return q ? { total: q.totalVideoFrames, dropped: q.droppedVideoFrames } : null;
  } catch {
    return null;
  }
}

/**
 * Framework-agnostic telemetry for live video. Sessions report bytes in O(1); a single 1 Hz
 * sampler (alive only while someone subscribes) turns counters into a snapshot and a ring
 * buffer. Nothing here renders; the UI only reads snapshots.
 */
export class VideoTelemetryService {
  private readonly entries = new Set<Entry>();
  private readonly listeners = new Set<() => void>();
  private readonly ring: HistoryPoint[] = new Array<HistoryPoint>(HISTORY_SIZE);
  private ringStart = 0;
  private ringLength = 0;
  private timer: ReturnType<typeof setInterval> | undefined;
  private lastTickAt = 0;
  private snapshot: VideoTelemetrySnapshot = EMPTY_SNAPSHOT;

  constructor(private readonly now: () => number = () => Date.now()) {}

  register(source: TelemetrySource): TelemetryHandle {
    const entry: Entry = { source, bytes: 0, lastBytes: 0, lastTotalFrames: null, lastDroppedFrames: 0 };
    this.entries.add(entry);
    return {
      addBytes: (bytes) => {
        entry.bytes += bytes;
      },
      unregister: () => {
        this.entries.delete(entry);
      },
    };
  }

  getSnapshot = (): VideoTelemetrySnapshot => this.snapshot;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    if (this.listeners.size === 1) this.start();
    return () => {
      this.listeners.delete(listener);
      if (this.listeners.size === 0) this.stop();
    };
  };

  private start(): void {
    this.lastTickAt = this.now();
    for (const e of this.entries) {
      e.lastBytes = e.bytes;
      const frames = readFrames(e.source.video);
      e.lastTotalFrames = frames ? frames.total : null;
      e.lastDroppedFrames = frames ? frames.dropped : 0;
    }
    this.timer = setInterval(() => this.tick(), SAMPLE_INTERVAL_MS);
  }

  private stop(): void {
    clearInterval(this.timer);
    this.timer = undefined;
    // Nobody is watching: drop history and snapshot so a later restart never joins pre- and
    // post-pause samples (the classifier window and histogram would otherwise span the gap).
    this.ringStart = 0;
    this.ringLength = 0;
    this.snapshot = EMPTY_SNAPSHOT;
  }

  private tick(): void {
    const at = this.now();
    const seconds = Math.max((at - this.lastTickAt) / 1000, 0.001);
    this.lastTickAt = at;

    const streams = emptyCounts();
    const sessions: SessionTelemetry[] = [];
    let totalBytes = 0;
    let presented = 0;
    let measured = 0;
    let stalled = 0;
    let decoded = 0;
    let dropped = 0;

    for (const e of this.entries) {
      const state = e.source.getState();
      const delta = e.bytes - e.lastBytes;
      e.lastBytes = e.bytes;
      totalBytes += delta;

      const frames = readFrames(e.source.video);
      if (frames) {
        const prev = e.lastTotalFrames;
        e.lastTotalFrames = frames.total;
        const prevDropped = e.lastDroppedFrames;
        e.lastDroppedFrames = frames.dropped;
        // Counters restart with a new MediaSource: rebaseline instead of reporting garbage.
        // Only ACTIVE sessions count: WARM ones present few frames by design and would skew health.
        if (prev !== null && frames.total >= prev && frames.dropped >= prevDropped && state === "ACTIVE") {
          const dTotal = frames.total - prev;
          if (dTotal === 0) stalled++;
          const dDropped = frames.dropped - prevDropped;
          decoded += dTotal;
          dropped += dDropped;
          presented += dTotal - dDropped;
          measured++;
        }
      }

      if (state === "UNINITIALIZED" || state === "EVICTED") continue;
      streams.total++;
      streams[e.source.quality]++;
      if (state === "ACTIVE" || state === "WARM") streams.playing++;
      else if (state === "CONNECTING" || state === "BUFFERING" || state === "RECONNECTING") streams.connecting++;
      else if (state === "SUSPENDED" || state === "IDLE") streams.paused++;
      else if (state === "ERROR") streams.error++;
      sessions.push({ cameraId: e.source.cameraId, quality: e.source.quality, state, mbps: (delta * 8) / 1e6 / seconds });
    }

    const mbps = (totalBytes * 8) / 1e6 / seconds;
    const fps = measured > 0 ? presented / measured / seconds : null;
    const droppedRatio = decoded > 0 ? dropped / decoded : 0;

    const point: HistoryPoint = {
      at,
      mbps,
      fps,
      droppedRatio,
      playing: streams.playing,
      connecting: streams.connecting,
      error: streams.error,
      total: streams.total,
      measured,
      stalled,
    };
    this.push(point);
    const history = this.history();
    this.snapshot = {
      at,
      mbps,
      fps,
      droppedFrames: dropped,
      droppedRatio,
      streams,
      sessions,
      history,
      health: classifyPerformance(history),
    };
    for (const l of [...this.listeners]) l();
  }

  private push(point: HistoryPoint): void {
    const index = (this.ringStart + this.ringLength) % HISTORY_SIZE;
    this.ring[index] = point;
    if (this.ringLength < HISTORY_SIZE) this.ringLength++;
    else this.ringStart = (this.ringStart + 1) % HISTORY_SIZE;
  }

  private history(): HistoryPoint[] {
    const out: HistoryPoint[] = [];
    for (let i = 0; i < this.ringLength; i++) out.push(this.ring[(this.ringStart + i) % HISTORY_SIZE]!);
    return out;
  }
}

/** Shared instance used by the live players and the UI. */
export const videoTelemetry = new VideoTelemetryService();
