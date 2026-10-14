import type { FrameSampler } from "./frameSampler";
import {
  HEALTH_THRESHOLDS,
  STARVED_FPS_FACTOR,
  TARGET_PRESENTATION_FPS,
  levelAbove,
  levelBelow,
  stateOf,
  type HealthLevel,
  type HealthReasonCode,
  type HealthState,
  type SeverityState,
} from "./performanceClassifier";

/**
 * Station benchmark: plays a local sample clip in N simultaneous MSE players, level by level
 * (4, 9, 16, 25, 32, 48), and measures how this station decodes and renders them. It is framework
 * free: the DOM, the clock, the frame sampler and the player factory are injected, so the logic
 * is testable and a native client can reuse it.
 *
 * Scope: decode and render of a low-resolution (LQ substream-like) clip only. Network is covered
 * by the live telemetry, and the main stream (HQ) is NOT measured: nothing here states a HQ capacity.
 * The clip is local, so the benchmark puts no load on Frigate, go2rtc or the network.
 */

/** The bundled sample clip: synthetic H.264 Constrained Baseline 3.0, 640x360 at 15 fps, 4 s. */
export const BENCHMARK_CLIP = {
  url: "/benchmark/videowall-360p15.mp4",
  mimeType: 'video/mp4; codecs="avc1.42E01E"',
  codec: "avc1.42E01E",
  width: 640,
  height: 360,
  fps: 15,
} as const;

export const DEFAULT_BENCHMARK_LEVELS: readonly number[] = [4, 9, 16, 25, 32, 48];
/** Time players run before measuring (decoder start-up, first frames). */
export const DEFAULT_WARMUP_MS = 200;
/** Length of the measuring window per level. 6 levels x 700 ms = 4.2 s, inside the 3-5 s target. */
export const DEFAULT_MEASURE_MS = 500;
/**
 * A single frame interval above this is a UI freeze: the run stops at once. (A normal 60 Hz frame
 * is 17 ms; the existing classifier already calls 70 ms saturated.)
 */
export const MAX_BENCHMARK_FRAME_GAP_MS = 250;

export type PlayerStats = {
  /** Frames handed to the decoder, dropped ones included (`totalVideoFrames`); undefined if not exposed. */
  decodedFrames?: number;
  droppedFrames?: number;
  /** Cumulative `waiting` events after playback began. */
  stalls: number;
  /** Cumulative element, source buffer or start-up errors. */
  errors: number;
};

export type BenchmarkPlayer = {
  /** Mounts media and starts playback; settles on its own (never hangs) and never rejects. */
  start(): Promise<void>;
  stats(): PlayerStats;
  /** Idempotent: removes the media, revokes object URLs and removes the element. */
  dispose(): void;
};

export type PlayerFactory = (container: HTMLElement, clip: ArrayBuffer) => BenchmarkPlayer;

export type BenchmarkDeps = {
  /** Visible element where the tiles are mounted. */
  container: HTMLElement;
  /** Downloads the sample clip. Called once per run. */
  loadClip(): Promise<ArrayBuffer>;
  createPlayer: PlayerFactory;
  sampler: FrameSampler;
  now(): number;
  /** Resolves after `ms`, or early when the signal aborts (never rejects). */
  sleep(ms: number, signal?: AbortSignal): Promise<void>;
  /** True while the tab is hidden: rAF is paused, so the level cannot be measured. */
  isHidden?(): boolean;
  newId?(): string;
};

export type BenchmarkProgress = { phase: "loading" } | { phase: "testing"; streams: number; levelIndex: number; levelCount: number };

export type BenchmarkOptions = {
  levels?: readonly number[];
  warmupMs?: number;
  measureMs?: number;
  /** Frame interval that aborts the run. Default MAX_BENCHMARK_FRAME_GAP_MS. */
  maxFrameGapMs?: number;
  signal?: AbortSignal;
  onProgress?(progress: BenchmarkProgress): void;
};

export type BenchmarkReason = HealthReasonCode | "decoder_error" | "frame_gap";

export type BenchmarkStopReason = "completed" | "saturated" | "decoder_error" | "frame_gap" | "hidden" | "aborted" | "error";

export type BenchmarkLevelResult = {
  streams: number;
  /** False when the level was never started (earlier stop, abort); its metrics are absent, never invented. */
  ran: boolean;
  /** `unknown` for levels that did not run or exposed no signal. */
  state: HealthState;
  /** Mean decoded frames per second of one player (expected: the clip fps). */
  decodedFps?: number;
  /** Dropped / decoded frames across all players. */
  droppedRatio?: number;
  stalls: number;
  decoderErrors: number;
  presentationFps?: number;
  frameTimeAvgMs?: number;
  frameTimeP95Ms?: number;
  /** Longest frame interval seen while measuring. */
  maxFrameMs?: number;
  /** Frames above LONG_FRAME_MS: main-thread hitches. */
  longFrames?: number;
  reasons: BenchmarkReason[];
};

export type BenchmarkLimits = {
  /** First level worse than acceptable (limit or saturated). */
  firstDegradedStreams?: number;
  saturatedAtStreams?: number;
  /** Level at which the run stopped early. */
  stoppedAtStreams?: number;
  /** What degraded the first degraded level. */
  bindingFactors: BenchmarkReason[];
};

export type BenchmarkResult = {
  id: string;
  startedAt: number;
  finishedAt: number;
  durationMs: number;
  clip: { codec: string; width: number; height: number; fps: number };
  levels: BenchmarkLevelResult[];
  /** Highest level that was optimal or acceptable (0 when none). */
  maxStableStreams: number;
  /** Internal 0-100 summary; never the only basis for a decision (the raw metrics stay in `levels`). */
  clientPerformanceScore: number;
  limits: BenchmarkLimits;
  stoppedReason: BenchmarkStopReason;
  error?: string;
};

// ---------------------------------------------------------------------------------------------
// Classification
// ---------------------------------------------------------------------------------------------

export type LevelSignals = {
  /** Share of players decoding below STARVED_FPS_FACTOR of the clip fps; undefined without frame counters. */
  starvedRatio?: number;
  droppedRatio?: number;
  /** Share of players that stalled during the window. */
  stalledRatio: number;
  decoderErrors: number;
  frameTimeP95Ms?: number;
  presentationFps?: number;
};

/**
 * Classifies one level with the live classifier's thresholds (HEALTH_THRESHOLDS, untuned here):
 * dropped ratio, starved players (decode fps against the clip rate), stalls, frame time p95 and
 * presentation fps (which alone is capped at `acceptable`). Any decoder error is saturated.
 * `unknown` means no decode, drop or render signal was exposed.
 */
export function classifyBenchmarkLevel(s: LevelSignals): { state: HealthState; reasons: BenchmarkReason[] } {
  const T = HEALTH_THRESHOLDS;
  const found: { code: BenchmarkReason; level: HealthLevel }[] = [];
  let known = false;
  if (s.decoderErrors > 0) found.push({ code: "decoder_error", level: 3 });
  if (s.droppedRatio !== undefined) {
    known = true;
    found.push({ code: "dropped_frames", level: levelAbove(s.droppedRatio, T.droppedRatio) });
  }
  if (s.starvedRatio !== undefined) {
    known = true;
    found.push({ code: "decode_fps_short", level: levelAbove(s.starvedRatio, T.starvedStreamRatio) });
  }
  found.push({ code: "stalls", level: levelAbove(s.stalledRatio, T.starvedStreamRatio) });
  let frameLevel: HealthLevel = 0;
  if (s.frameTimeP95Ms !== undefined) {
    known = true;
    frameLevel = levelAbove(s.frameTimeP95Ms, T.frameTimeP95Ms);
    found.push({ code: "frame_time", level: frameLevel });
  }
  if (s.presentationFps !== undefined) {
    known = true;
    const raw = levelBelow(s.presentationFps / TARGET_PRESENTATION_FPS, T.presentationFpsRatio);
    found.push({ code: "presentation_fps", level: (frameLevel >= 1 ? raw : Math.min(raw, 1)) as HealthLevel });
  }
  const worst = Math.max(0, ...found.map((f) => f.level));
  const reasons = found.filter((f) => f.level >= 1).sort((a, b) => b.level - a.level).map((f) => f.code);
  if (worst === 0 && !known) return { state: "unknown", reasons };
  return { state: stateOf(worst), reasons };
}

/** How much a level counts toward the score: a worse state earns less, never more. */
const SCORE_QUALITY: Record<HealthState, number> = { optimal: 1, acceptable: 0.75, limit: 0.4, saturated: 0, unknown: 0 };

/**
 * Internal normalized score, 0-100: the stream-weighted average of per-level quality over ALL
 * configured levels (optimal 1, acceptable 0.75, limit 0.4, saturated 0). A level that did not
 * run earns 0, so stopping early lowers the score, and a bigger level weighs more (a station that
 * holds 48 streams outranks one that only holds 4). Monotonic: a better state never lowers it.
 */
export function computeClientPerformanceScore(levels: readonly BenchmarkLevelResult[]): number {
  const total = levels.reduce((a, l) => a + l.streams, 0);
  if (total === 0) return 0;
  const earned = levels.reduce((a, l) => a + (l.ran ? l.streams * SCORE_QUALITY[l.state] : 0), 0);
  return Math.round((earned / total) * 100);
}

// ---------------------------------------------------------------------------------------------
// Operator summary
// ---------------------------------------------------------------------------------------------

export type BenchmarkSummaryRow = { streams: number; quality: "LQ"; state: SeverityState };

export type BenchmarkSummary = {
  /** Levels that ran, biggest first. */
  rows: BenchmarkSummaryRow[];
  /** HQ (main stream) is never measured by the sample clip. */
  hq: "not_measured";
  maxStableStreams: number;
};

export function summarizeBenchmark(result: BenchmarkResult): BenchmarkSummary {
  const rows = result.levels
    .filter((l): l is BenchmarkLevelResult & { state: SeverityState } => l.ran && l.state !== "unknown")
    .map((l) => ({ streams: l.streams, quality: "LQ" as const, state: l.state }))
    .sort((a, b) => b.streams - a.streams);
  return { rows, hq: "not_measured", maxStableStreams: result.maxStableStreams };
}

/** A result worth caching: the station was actually measured (not aborted, hidden or broken). */
export function isConclusiveBenchmark(result: BenchmarkResult): boolean {
  return (result.stoppedReason === "completed" || result.stoppedReason === "saturated" || result.stoppedReason === "decoder_error" || result.stoppedReason === "frame_gap") && result.levels.some((l) => l.ran);
}

// ---------------------------------------------------------------------------------------------
// Runner
// ---------------------------------------------------------------------------------------------

function notRun(streams: number): BenchmarkLevelResult {
  return { streams, ran: false, state: "unknown", stalls: 0, decoderErrors: 0, reasons: [] };
}

type LevelOutcome = { level?: BenchmarkLevelResult; stop?: BenchmarkStopReason };

const delta = (after: number | undefined, before: number | undefined): number | undefined =>
  after === undefined || before === undefined ? undefined : Math.max(0, after - before);

function safeDispose(player: BenchmarkPlayer): void {
  try {
    player.dispose();
  } catch {
    // Teardown is best effort: one failing player must not leave the others mounted.
  }
}

async function runLevel(deps: BenchmarkDeps, clip: ArrayBuffer, streams: number, o: Required<Pick<BenchmarkOptions, "warmupMs" | "measureMs" | "maxFrameGapMs">> & { signal?: AbortSignal }): Promise<LevelOutcome> {
  const players: BenchmarkPlayer[] = [];
  try {
    for (let i = 0; i < streams; i++) players.push(deps.createPlayer(deps.container, clip));
    let startFailures = 0;
    await Promise.all(players.map((p) => p.start().catch(() => void (startFailures += 1))));
    if (o.signal?.aborted) return { stop: "aborted" };
    await deps.sleep(o.warmupMs, o.signal);
    if (o.signal?.aborted) return { stop: "aborted" };

    const before = players.map((p) => p.stats());
    const t0 = deps.now();
    let presentation: ReturnType<FrameSampler["sample"]>;
    deps.sampler.start();
    try {
      await deps.sleep(o.measureMs, o.signal);
    } finally {
      presentation = deps.sampler.sample();
      deps.sampler.stop();
    }
    const elapsedMs = deps.now() - t0;
    if (o.signal?.aborted) return { stop: "aborted" };
    // rAF is paused in a hidden tab, so this window says nothing about the station.
    if (deps.isHidden?.()) return { stop: "hidden" };
    const after = players.map((p) => p.stats());

    const seconds = Math.max(elapsedMs, 1) / 1000;
    let decoded = 0;
    let dropped = 0;
    let counted = 0;
    let starved = 0;
    let stalls = 0;
    let stalledPlayers = 0;
    let errors = startFailures;
    after.forEach((a, i) => {
      const b = before[i]!;
      const dec = delta(a.decodedFrames, b.decodedFrames);
      if (dec !== undefined) {
        counted += 1;
        decoded += dec;
        dropped += delta(a.droppedFrames, b.droppedFrames) ?? 0;
        if (dec / seconds < BENCHMARK_CLIP.fps * STARVED_FPS_FACTOR) starved += 1;
      }
      const st = Math.max(0, a.stalls - b.stalls);
      stalls += st;
      if (st > 0) stalledPlayers += 1;
      errors += a.errors;
    });

    const signals: LevelSignals = {
      starvedRatio: counted > 0 ? starved / counted : undefined,
      droppedRatio: counted > 0 ? (decoded > 0 ? dropped / decoded : 0) : undefined,
      stalledRatio: stalledPlayers / streams,
      decoderErrors: errors,
      frameTimeP95Ms: presentation?.p95FrameMs,
      presentationFps: presentation?.fps,
    };
    let { state, reasons } = classifyBenchmarkLevel(signals);
    // The measuring wait overshooting its window means the main thread was blocked.
    const blocked = elapsedMs > o.measureMs + o.maxFrameGapMs;
    const gap = blocked || (presentation?.maxFrameMs ?? 0) > o.maxFrameGapMs;
    if (gap) {
      state = "saturated";
      reasons = [...reasons, "frame_gap"];
    }
    const level: BenchmarkLevelResult = {
      streams,
      ran: true,
      state,
      decodedFps: counted > 0 ? decoded / counted / seconds : undefined,
      droppedRatio: signals.droppedRatio,
      stalls,
      decoderErrors: errors,
      presentationFps: presentation?.fps,
      frameTimeAvgMs: presentation?.avgFrameMs,
      frameTimeP95Ms: presentation?.p95FrameMs,
      maxFrameMs: presentation?.maxFrameMs,
      longFrames: presentation?.longFrames,
      reasons,
    };
    const stop: BenchmarkStopReason | undefined = errors > 0 ? "decoder_error" : gap ? "frame_gap" : state === "saturated" ? "saturated" : undefined;
    return { level, stop };
  } finally {
    // Every mounted player goes away between levels and on abort or error.
    for (const p of players) safeDispose(p);
  }
}

function summarizeLimits(levels: readonly BenchmarkLevelResult[], stopped: BenchmarkStopReason): BenchmarkLimits {
  const degraded = levels.find((l) => l.ran && (l.state === "limit" || l.state === "saturated"));
  const saturated = levels.find((l) => l.ran && l.state === "saturated");
  const last = [...levels].reverse().find((l) => l.ran);
  const earlyStop = stopped === "saturated" || stopped === "decoder_error" || stopped === "frame_gap";
  return {
    firstDegradedStreams: degraded?.streams,
    saturatedAtStreams: saturated?.streams,
    stoppedAtStreams: earlyStop ? last?.streams : undefined,
    bindingFactors: degraded?.reasons ?? [],
  };
}

function defaultId(): string {
  return typeof crypto !== "undefined" && typeof crypto.randomUUID === "function" ? crypto.randomUUID() : `bench-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

/**
 * Runs the benchmark and always resolves with a result: an abort, a hidden tab or a failure ends
 * the run early with `stoppedReason` set (and `error` for failures), after tearing every player down.
 */
export async function runClientBenchmark(deps: BenchmarkDeps, opts: BenchmarkOptions = {}): Promise<BenchmarkResult> {
  const configured = opts.levels ?? DEFAULT_BENCHMARK_LEVELS;
  const settings = {
    warmupMs: opts.warmupMs ?? DEFAULT_WARMUP_MS,
    measureMs: opts.measureMs ?? DEFAULT_MEASURE_MS,
    maxFrameGapMs: opts.maxFrameGapMs ?? MAX_BENCHMARK_FRAME_GAP_MS,
    signal: opts.signal,
  };
  const startedAt = deps.now();
  const levels = configured.map(notRun);
  let stoppedReason: BenchmarkStopReason = "completed";
  let error: string | undefined;
  try {
    if (opts.signal?.aborted) {
      stoppedReason = "aborted";
    } else {
      opts.onProgress?.({ phase: "loading" });
      const clip = await deps.loadClip();
      for (let i = 0; i < configured.length; i++) {
        if (opts.signal?.aborted) {
          stoppedReason = "aborted";
          break;
        }
        opts.onProgress?.({ phase: "testing", streams: configured[i]!, levelIndex: i, levelCount: configured.length });
        const outcome = await runLevel(deps, clip, configured[i]!, settings);
        if (outcome.level) levels[i] = outcome.level;
        if (outcome.stop) {
          stoppedReason = outcome.stop;
          break;
        }
      }
    }
  } catch (e) {
    stoppedReason = "error";
    error = e instanceof Error ? e.message : String(e);
  }
  const finishedAt = deps.now();
  const stable = levels.filter((l) => l.ran && (l.state === "optimal" || l.state === "acceptable")).map((l) => l.streams);
  return {
    id: (deps.newId ?? defaultId)(),
    startedAt,
    finishedAt,
    durationMs: finishedAt - startedAt,
    clip: { codec: BENCHMARK_CLIP.codec, width: BENCHMARK_CLIP.width, height: BENCHMARK_CLIP.height, fps: BENCHMARK_CLIP.fps },
    levels,
    maxStableStreams: stable.length > 0 ? Math.max(...stable) : 0,
    clientPerformanceScore: computeClientPerformanceScore(levels),
    limits: summarizeLimits(levels, stoppedReason),
    stoppedReason,
    ...(error === undefined ? {} : { error }),
  };
}
