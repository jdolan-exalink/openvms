import type { StreamTelemetryRow, VideoPerformanceMetrics } from "./types";

/**
 * Pure, platform-neutral classifier of videowall health.
 *
 * It reads a short window of 1 Hz aggregates (and optionally the per-camera rows) and answers
 * "how is this station doing?" without touching the DOM, timers or any browser API, so a native
 * client can feed the same inputs.
 *
 * Presentation fps is deliberately NOT the video health indicator (spec section 5): it only feeds
 * the render layer, and on its own it can never push a window past `acceptable`. Dropped frames,
 * stalls and missing streams are what describe video delivery.
 */

/** A severity a signal or layer can reach, ordered from best to worst. */
export type SeverityState = "optimal" | "acceptable" | "limit" | "saturated";
/**
 * Overall verdict. `unknown` means there is not enough data to judge (fewer than
 * MIN_HEALTH_SAMPLES samples); it is never reported as healthy.
 */
export type HealthState = SeverityState | "unknown";
export type HealthLayer = "network" | "transport" | "decode" | "render";
/** `unknown` means the platform did not expose what that layer needs; it never counts as healthy or sick. */
export type LayerStatus = HealthState;

export type HealthReasonCode =
  | "dropped_frames"
  | "decode_fps_short"
  | "presentation_fps"
  | "frame_time"
  | "stalls"
  | "streams_missing"
  | "bitrate_short"
  | "insufficient_data";

export type HealthReason = {
  layer: HealthLayer;
  code: HealthReasonCode;
  /** Severity this single signal reached (`unknown` only for `insufficient_data`). */
  state: HealthState;
  /** The observed value over the window (ratio, fps, ms or count, depending on the code). */
  value: number;
  /** The threshold that was crossed to reach `state`. */
  threshold: number;
};

export type HealthSignals = {
  samples: number;
  /** Aggregate droppedFrames / decodedFrames over the window. */
  droppedFrameRatio: number;
  presentationFps?: number;
  /** Mean of the per-sample p95 frame time (falls back to the mean frame time). */
  frameTimeP95Ms?: number;
  stallsPerMinute: number;
  /** Mean share of visible cameras that were not delivering media. */
  streamsMissingRatio: number;
  receivedMbps: number;
  /** receivedMbps / expectedBitrateMbps; only when an expectation was given. */
  bitrateRatio?: number;
  /** Share of active streams below the expected fps; only with rows and an expectation. */
  starvedStreamRatio?: number;
};

export type HealthAssessment = {
  state: HealthState;
  layers: Record<HealthLayer, LayerStatus>;
  reasons: HealthReason[];
  signals: HealthSignals;
};

export type HealthOptions = {
  /** Number of most recent samples to look at. Default HEALTH_WINDOW_SIZE. */
  windowSize?: number;
  /** Presentation fps the station should reach. Default TARGET_PRESENTATION_FPS. */
  targetFps?: number;
  /** Total video bitrate the active streams should deliver, in Mbps. Without it the network layer is `unknown`. */
  expectedBitrateMbps?: number;
  /** Frames per second each active stream should decode. Needed to judge per-camera rows. */
  expectedStreamFps?: number;
  /** Latest per-camera rows. */
  rows?: readonly StreamTelemetryRow[];
};

/** Samples considered per assessment (about 10 s at the 1 Hz telemetry tick). */
export const HEALTH_WINDOW_SIZE = 10;
/** Default presentation fps target (display refresh). */
export const TARGET_PRESENTATION_FPS = 60;
/**
 * A signal degrades the window only when at least this share of the samples reached the level
 * (and never fewer than MIN_SUSTAINED_SAMPLES samples), so one bad second never flips the state:
 * sustained loss does.
 */
export const SUSTAINED_FRACTION = 0.3;
/** A level is sustained only when at least this many samples reached it, whatever the window size. */
export const MIN_SUSTAINED_SAMPLES = 2;
/** Below this many samples the assessment is `unknown`: no verdict, healthy or otherwise. */
export const MIN_HEALTH_SAMPLES = 3;
/** Samples decoding fewer frames than this are skipped for the dropped-frame ratio (too noisy). */
export const MIN_DECODED_FRAMES_PER_SAMPLE = 5;
/** Below this many samples a verdict carries low confidence. */
export const MIN_SAMPLES_FOR_CONFIDENCE = 5;

/** Each tuple is [acceptable, limit, saturated]: the value at which that level starts. */
export type LevelThresholds = readonly [number, number, number];

export const HEALTH_THRESHOLDS = {
  /** Dropped / decoded frames (higher is worse). */
  droppedRatio: [0.02, 0.05, 0.15],
  /** Presentation fps / target fps (lower is worse). */
  presentationFpsRatio: [0.9, 0.7, 0.5],
  /** Render frame time p95 in ms (higher is worse). 25 ms is a missed 60 Hz frame plus slack. */
  frameTimeP95Ms: [25, 40, 70],
  /** Stalls within one 1 s sample (higher is worse). */
  stallsPerSample: [1, 2, 4],
  /** Share of visible cameras not delivering media (higher is worse). */
  streamsMissingRatio: [0.1, 0.25, 0.5],
  /** received / expected bitrate (lower is worse). */
  bitrateRatio: [0.9, 0.7, 0.5],
  /** Share of active streams decoding below the expected fps (higher is worse). */
  starvedStreamRatio: [0.1, 0.25, 0.5],
} as const satisfies Record<string, LevelThresholds>;

/** A stream counts as starved when it decodes below this share of the expected fps. */
export const STARVED_FPS_FACTOR = 0.8;

export const HEALTH_STATES: readonly SeverityState[] = ["optimal", "acceptable", "limit", "saturated"];

/** 0 = optimal ... 3 = saturated. */
export type HealthLevel = 0 | 1 | 2 | 3;

export function stateOf(level: number): SeverityState {
  return HEALTH_STATES[Math.max(0, Math.min(3, level))] as SeverityState;
}

export function levelOf(state: SeverityState): HealthLevel {
  return HEALTH_STATES.indexOf(state) as HealthLevel;
}

function levelAbove(value: number, t: LevelThresholds): HealthLevel {
  return value >= t[2] ? 3 : value >= t[1] ? 2 : value >= t[0] ? 1 : 0;
}

function levelBelow(value: number, t: LevelThresholds): HealthLevel {
  return value < t[2] ? 3 : value < t[1] ? 2 : value < t[0] ? 1 : 0;
}

/**
 * The level reached by at least SUSTAINED_FRACTION of the measured samples, and by no fewer than
 * MIN_SUSTAINED_SAMPLES of them. Too few measured samples to sustain anything count as level 0.
 */
function sustainedLevel(levels: number[]): HealthLevel {
  const need = Math.max(MIN_SUSTAINED_SAMPLES, Math.ceil(SUSTAINED_FRACTION * levels.length));
  if (levels.length < need) return 0;
  const sorted = [...levels].sort((a, b) => b - a);
  return sorted[need - 1] as HealthLevel;
}

const mean = (xs: number[]) => (xs.length === 0 ? undefined : xs.reduce((a, b) => a + b, 0) / xs.length);

type Contribution = { layer: HealthLayer; code: HealthReasonCode; level: HealthLevel; value: number; thresholds: LevelThresholds };

export type HealthAnalysis = {
  window: VideoPerformanceMetrics[];
  layerLevels: Record<HealthLayer, HealthLevel | undefined>;
  contributions: Contribution[];
  signals: HealthSignals;
};

/**
 * Shared analysis used by the classifier and by the bottleneck estimator, so both read the same
 * numbers. Exposed for the estimator; UI code should call `classifyHealth`.
 */
export function analyzeHealth(input: readonly VideoPerformanceMetrics[], opts: HealthOptions = {}): HealthAnalysis {
  const size = Math.max(1, opts.windowSize ?? HEALTH_WINDOW_SIZE);
  const window = input.slice(-size);
  const n = window.length;
  const targetFps = opts.targetFps ?? TARGET_PRESENTATION_FPS;
  const T = HEALTH_THRESHOLDS;

  const decoded = window.reduce((a, m) => a + m.decodedFrames, 0);
  const droppedTotal = window.reduce((a, m) => a + m.droppedFrames, 0);
  const frameTimes = window.map((m) => m.renderFrameTimeP95Ms ?? m.renderFrameTimeMs).filter((v): v is number => v !== undefined);
  const fpsValues = window.map((m) => m.presentationFps).filter((v): v is number => v !== undefined);
  const missing = window.map((m) => (m.visibleCameras > 0 ? Math.max(0, (m.visibleCameras - m.activeStreams) / m.visibleCameras) : 0));
  const span = n > 1 ? (window[n - 1] as VideoPerformanceMetrics).timestamp - (window[0] as VideoPerformanceMetrics).timestamp : 0;
  const interval = span > 0 ? span / (n - 1) : 1000;
  const stallsTotal = window.reduce((a, m) => a + (m.stalls ?? 0), 0);

  const signals: HealthSignals = {
    samples: n,
    droppedFrameRatio: decoded > 0 ? droppedTotal / decoded : 0,
    presentationFps: mean(fpsValues),
    frameTimeP95Ms: mean(frameTimes),
    stallsPerMinute: n > 0 ? stallsTotal / ((n * interval) / 60_000) : 0,
    streamsMissingRatio: mean(missing) ?? 0,
    receivedMbps: mean(window.map((m) => m.receivedMbps)) ?? 0,
  };

  const contributions: Contribution[] = [];
  const layerLevels: Record<HealthLayer, HealthLevel | undefined> = { network: undefined, transport: undefined, decode: undefined, render: undefined };
  const add = (layer: HealthLayer, code: HealthReasonCode, level: HealthLevel, value: number, thresholds: LevelThresholds) => {
    layerLevels[layer] = Math.max(layerLevels[layer] ?? 0, level) as HealthLevel;
    contributions.push({ layer, code, level, value, thresholds });
  };

  // Decode: dropped frames, then per-camera fps against the expectation.
  const dropLevels = window.filter((m) => m.decodedFrames >= MIN_DECODED_FRAMES_PER_SAMPLE).map((m) => levelAbove(m.droppedFrames / m.decodedFrames, T.droppedRatio));
  if (dropLevels.length > 0) add("decode", "dropped_frames", sustainedLevel(dropLevels), signals.droppedFrameRatio, T.droppedRatio);
  if (opts.expectedStreamFps && opts.rows) {
    const live = opts.rows.filter((r) => r.fps !== undefined && (r.state === "ACTIVE" || r.state === "WARM"));
    if (live.length > 0) {
      const starved = live.filter((r) => (r.fps as number) < opts.expectedStreamFps! * STARVED_FPS_FACTOR).length / live.length;
      signals.starvedStreamRatio = starved;
      add("decode", "decode_fps_short", levelAbove(starved, T.starvedStreamRatio), starved, T.starvedStreamRatio);
    }
  }

  // Render: frame time is the strong signal; low fps alone is capped at `acceptable`.
  const frameLevel = sustainedLevel(window.map((m) => m.renderFrameTimeP95Ms ?? m.renderFrameTimeMs).filter((v): v is number => v !== undefined).map((v) => levelAbove(v, T.frameTimeP95Ms)));
  if (frameTimes.length > 0) add("render", "frame_time", frameLevel, signals.frameTimeP95Ms as number, T.frameTimeP95Ms);
  if (fpsValues.length > 0) {
    const raw = sustainedLevel(fpsValues.map((v) => levelBelow(v / targetFps, T.presentationFpsRatio)));
    add("render", "presentation_fps", (frameLevel >= 1 ? raw : Math.min(raw, 1)) as HealthLevel, signals.presentationFps as number, T.presentationFpsRatio);
  }

  // Transport: stalls and streams that never started delivering.
  const stallLevels = window.map((m) => levelAbove(m.stalls ?? 0, T.stallsPerSample));
  if (n > 0) {
    add("transport", "stalls", sustainedLevel(stallLevels), signals.stallsPerMinute, T.stallsPerSample);
    add("transport", "streams_missing", sustainedLevel(missing.map((v) => levelAbove(v, T.streamsMissingRatio))), signals.streamsMissingRatio, T.streamsMissingRatio);
  }

  // Network: only against an explicit expectation; bitrate alone is not a verdict otherwise.
  if (opts.expectedBitrateMbps && opts.expectedBitrateMbps > 0 && n > 0) {
    const expected = opts.expectedBitrateMbps;
    signals.bitrateRatio = signals.receivedMbps / expected;
    const levels = window.filter((m) => m.activeStreams > 0).map((m) => levelBelow(m.receivedMbps / expected, T.bitrateRatio));
    if (levels.length > 0) add("network", "bitrate_short", sustainedLevel(levels), signals.bitrateRatio, T.bitrateRatio);
  }

  return { window, layerLevels, contributions, signals };
}

/**
 * Classifies the most recent samples into Optimal / Acceptable / At the limit / Saturated, or
 * Unknown when the window holds fewer than MIN_HEALTH_SAMPLES samples.
 */
export function classifyHealth(window: readonly VideoPerformanceMetrics[], opts: HealthOptions = {}): HealthAssessment {
  const a = analyzeHealth(window, opts);
  if (a.signals.samples < MIN_HEALTH_SAMPLES) {
    return {
      state: "unknown",
      layers: { network: "unknown", transport: "unknown", decode: "unknown", render: "unknown" },
      reasons: [{ layer: "transport", code: "insufficient_data", state: "unknown", value: a.signals.samples, threshold: MIN_HEALTH_SAMPLES }],
      signals: a.signals,
    };
  }
  const layers = {} as Record<HealthLayer, LayerStatus>;
  for (const layer of ["network", "transport", "decode", "render"] as const) {
    const level = a.layerLevels[layer];
    layers[layer] = level === undefined ? "unknown" : stateOf(level);
  }
  const reasons: HealthReason[] = a.contributions
    .filter((c) => c.level >= 1)
    .sort((x, y) => y.level - x.level)
    .map((c) => ({ layer: c.layer, code: c.code, state: stateOf(c.level), value: c.value, threshold: c.thresholds[c.level - 1] as number }));
  const worst = Math.max(0, ...Object.values(a.layerLevels).map((l) => l ?? 0));
  return { state: stateOf(worst), layers, reasons, signals: a.signals };
}

export type HealthTrackerOptions = {
  /** Consecutive worse assessments required before the state degrades. Default 2. */
  downgradeAfter?: number;
  /** Consecutive better assessments required before the state recovers. Default 5. */
  upgradeAfter?: number;
};

export type HealthTracker = {
  /** Feeds one assessment and returns the stable (hysteresis-filtered) state. */
  update(assessment: { state: HealthState }): HealthState;
  readonly state: HealthState | undefined;
};

/**
 * Debounces the displayed state: degrade quickly, recover slowly, so the label does not flicker.
 * `unknown` is not a severity: it passes through immediately and clears any pending streak, and the
 * first known state after it is adopted immediately as well (there is nothing to debounce against).
 */
export function createHealthTracker(opts: HealthTrackerOptions = {}): HealthTracker {
  const downgradeAfter = opts.downgradeAfter ?? 2;
  const upgradeAfter = opts.upgradeAfter ?? 5;
  let current: HealthState | undefined;
  let candidate: HealthState | undefined;
  let streak = 0;
  return {
    get state() {
      return current;
    },
    update({ state }) {
      if (current === undefined || current === "unknown" || state === "unknown" || state === current) {
        current = state;
        candidate = undefined;
        streak = 0;
        return current;
      }
      streak = candidate === state ? streak + 1 : 1;
      candidate = state;
      const need = levelOf(state) > levelOf(current) ? downgradeAfter : upgradeAfter;
      if (streak >= need) {
        current = state;
        candidate = undefined;
        streak = 0;
      }
      return current;
    },
  };
}
