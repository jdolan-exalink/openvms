import { analyzeHealth, MIN_SAMPLES_FOR_CONFIDENCE, stateOf, type HealthLayer, type HealthOptions } from "./performanceClassifier";
import type { StreamTelemetryRow, VideoPerformanceMetrics } from "./types";

/**
 * Estimates which layer limits the station (spec section 12).
 *
 * Browsers expose no decoder or GPU utilization, so every verdict is an inference from indirect
 * signals and carries `estimated: true` unless the caller states the metrics were direct
 * (`directMetrics`, for example from a native client).
 */

export type BottleneckLayer = "none" | HealthLayer;
export type BottleneckConfidence = "low" | "medium" | "high";

export type BottleneckEstimate = {
  layer: BottleneckLayer;
  confidence: BottleneckConfidence;
  estimated: boolean;
  /** Human-readable facts behind the verdict, in English (support-facing). */
  evidence: string[];
};

export type BottleneckOptions = HealthOptions & {
  /** Reconnects observed during the window (the caller sums them from the stream sessions). */
  reconnects?: number;
  /** True only when the metrics come from a native source that measures them directly. */
  directMetrics?: boolean;
};

/** Upstream layers win ties: a starved decoder is a symptom when the data never arrived. */
const LAYER_ORDER: readonly HealthLayer[] = ["network", "transport", "decode", "render"];
const CONFIDENCES: readonly BottleneckConfidence[] = ["low", "medium", "high"];
/** Active streams must grow by this factor between window halves to count as "worsens with stream count". */
const STREAM_GROWTH_FACTOR = 1.1;

const pct = (v: number) => `${(v * 100).toFixed(1)}%`;

function streamCountCorrelation(window: VideoPerformanceMetrics[]): boolean {
  if (window.length < 4) return false;
  const mid = Math.floor(window.length / 2);
  const avg = (xs: VideoPerformanceMetrics[], f: (m: VideoPerformanceMetrics) => number) => xs.reduce((a, m) => a + f(m), 0) / xs.length;
  const ratio = (xs: VideoPerformanceMetrics[]) => {
    const decoded = xs.reduce((a, m) => a + m.decodedFrames, 0);
    return decoded > 0 ? xs.reduce((a, m) => a + m.droppedFrames, 0) / decoded : 0;
  };
  const first = window.slice(0, mid);
  const second = window.slice(mid);
  return avg(second, (m) => m.activeStreams) >= avg(first, (m) => m.activeStreams) * STREAM_GROWTH_FACTOR && ratio(second) > ratio(first);
}

export function estimateBottleneck(window: readonly VideoPerformanceMetrics[], rows?: readonly StreamTelemetryRow[], opts: BottleneckOptions = {}): BottleneckEstimate {
  const estimated = !opts.directMetrics;
  const a = analyzeHealth(window, { ...opts, rows: rows ?? opts.rows });
  const n = a.signals.samples;
  const thin = n < MIN_SAMPLES_FOR_CONFIDENCE;
  const level = (l: HealthLayer) => a.layerLevels[l] ?? 0;

  if (n === 0) return { layer: "none", confidence: "low", estimated, evidence: ["no telemetry samples yet"] };

  const top = Math.max(...LAYER_ORDER.map(level));
  if (top === 0) {
    return { layer: "none", confidence: thin ? "low" : "high", estimated, evidence: [`all measured layers within thresholds over ${n} samples`] };
  }

  const layer = LAYER_ORDER.find((l) => level(l) === top) as HealthLayer;
  const rivals = LAYER_ORDER.filter((l) => l !== layer && level(l) === top);
  const mine = a.contributions.filter((c) => c.layer === layer && c.level >= 1);
  const reconnects = opts.reconnects ?? 0;

  const evidence = mine.map((c) => describe(c.code, c.value));
  let corroborated = mine.length >= 2;
  if (layer === "network" || layer === "transport") {
    if (reconnects > 0) {
      evidence.push(`${reconnects} reconnect(s) in the window`);
      corroborated = true;
    }
  }
  if (layer === "decode" && streamCountCorrelation(a.window)) {
    evidence.push("dropped frames grow as the active stream count grows");
    corroborated = true;
  }
  if (layer === "decode" && level("render") === 0 && a.signals.presentationFps !== undefined) {
    evidence.push("render keeps up while frames are dropped before presentation");
  }
  if (layer === "network" && level("transport") >= 1) {
    evidence.push("stalls accompany the bitrate shortfall");
    corroborated = true;
  }
  if (rivals.length > 0) evidence.push(`${rivals.join(", ")} equally degraded; cannot separate the causes`);
  const lower = LAYER_ORDER.filter((l) => l !== layer && !rivals.includes(l) && level(l) >= 1);
  if (lower.length > 0) evidence.push(`also degraded: ${lower.map((l) => `${l} (${stateOf(level(l))})`).join(", ")}`);

  let score = top >= 2 ? 1 : 0;
  if (corroborated) score += 1;
  if (thin) score -= 1;
  if (rivals.length > 0) score -= 1;
  if (thin) evidence.push(`only ${n} sample(s); verdict is tentative`);
  const confidence = CONFIDENCES[Math.max(0, Math.min(2, score))] as BottleneckConfidence;

  return { layer, confidence, estimated, evidence };
}

function describe(code: string, value: number): string {
  switch (code) {
    case "dropped_frames":
      return `dropped frames ${pct(value)}`;
    case "decode_fps_short":
      return `${pct(value)} of streams decode below the expected fps`;
    case "frame_time":
      return `render frame time p95 ${value.toFixed(1)} ms`;
    case "presentation_fps":
      return `presentation fps ${value.toFixed(1)}`;
    case "stalls":
      return `${value.toFixed(1)} stalls per minute`;
    case "streams_missing":
      return `${pct(value)} of visible cameras not delivering media`;
    case "bitrate_short":
      return `received bitrate ${pct(value)} of the expected bitrate`;
    default:
      return code;
  }
}
