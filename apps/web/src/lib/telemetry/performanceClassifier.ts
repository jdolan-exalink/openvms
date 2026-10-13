/**
 * Pure classification of how well the client station is keeping up with the live videowall.
 * Thresholds are heuristics over the last few 1 Hz samples; the worst signal wins.
 */
export type PerformanceHealth = "optimal" | "acceptable" | "limit" | "saturated";

export type ClassifierSample = {
  /** Average presented frames per second per playing stream; null when it could not be measured. */
  fps: number | null;
  /** Dropped / total decoded frames in the sample, 0..1. */
  droppedRatio: number;
  playing: number;
  connecting: number;
  error: number;
  total: number;
};

/** Frame rate a typical CCTV substream should present; below this share the station is struggling. */
export const EXPECTED_FPS = 15;
/** Number of most recent samples averaged to avoid flapping. */
export const CLASSIFIER_WINDOW = 5;

/** Lower bounds (inclusive) of each degraded level for the dropped-frame ratio. */
export const DROPPED_THRESHOLDS = { acceptable: 0.03, limit: 0.08, saturated: 0.2 } as const;
/** Upper bounds (exclusive) of presented-fps / EXPECTED_FPS for each level, from best to worst. */
export const FPS_RATIO_THRESHOLDS = { optimal: 0.8, acceptable: 0.6, limit: 0.4 } as const;
/** Lower bounds (inclusive) of the share of streams in ERROR for each degraded level. */
export const ERROR_SHARE_THRESHOLDS = { acceptable: 0.1, limit: 0.25, saturated: 0.5 } as const;
/** Share of streams still connecting above which the station is at least "acceptable" (not "optimal"). */
export const CONNECTING_SHARE_ACCEPTABLE = 0.5;

const ORDER: readonly PerformanceHealth[] = ["optimal", "acceptable", "limit", "saturated"];

export const HEALTH_LABELS: Readonly<Record<PerformanceHealth, string>> = {
  optimal: "Óptimo",
  acceptable: "Aceptable",
  limit: "Al límite",
  saturated: "Saturado",
};

function worst(a: PerformanceHealth, b: PerformanceHealth): PerformanceHealth {
  return ORDER.indexOf(a) >= ORDER.indexOf(b) ? a : b;
}

export function classifyPerformance(samples: readonly ClassifierSample[]): PerformanceHealth {
  const window = samples.slice(-CLASSIFIER_WINDOW);
  const withStreams = window.filter((s) => s.total > 0);
  if (withStreams.length === 0) return "optimal";
  const mean = (values: number[]) => values.reduce((a, b) => a + b, 0) / values.length;

  let health: PerformanceHealth = "optimal";

  const dropped = mean(withStreams.map((s) => s.droppedRatio));
  if (dropped >= DROPPED_THRESHOLDS.saturated) health = worst(health, "saturated");
  else if (dropped >= DROPPED_THRESHOLDS.limit) health = worst(health, "limit");
  else if (dropped >= DROPPED_THRESHOLDS.acceptable) health = worst(health, "acceptable");

  const fpsValues = withStreams.filter((s) => s.fps !== null && s.playing > 0).map((s) => s.fps as number);
  if (fpsValues.length > 0) {
    const ratio = mean(fpsValues) / EXPECTED_FPS;
    if (ratio < FPS_RATIO_THRESHOLDS.limit) health = worst(health, "saturated");
    else if (ratio < FPS_RATIO_THRESHOLDS.acceptable) health = worst(health, "limit");
    else if (ratio < FPS_RATIO_THRESHOLDS.optimal) health = worst(health, "acceptable");
  }

  const errorShare = mean(withStreams.map((s) => s.error / s.total));
  if (errorShare >= ERROR_SHARE_THRESHOLDS.saturated) health = worst(health, "saturated");
  else if (errorShare >= ERROR_SHARE_THRESHOLDS.limit) health = worst(health, "limit");
  else if (errorShare >= ERROR_SHARE_THRESHOLDS.acceptable) health = worst(health, "acceptable");

  const connectingShare = mean(withStreams.map((s) => s.connecting / s.total));
  if (connectingShare >= CONNECTING_SHARE_ACCEPTABLE) health = worst(health, "acceptable");

  return health;
}
