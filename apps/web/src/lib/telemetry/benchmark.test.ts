import { describe, expect, it, vi } from "vitest";
import {
  BENCHMARK_CLIP,
  DEFAULT_BENCHMARK_LEVELS,
  classifyBenchmarkLevel,
  computeClientPerformanceScore,
  runClientBenchmark,
  summarizeBenchmark,
  type BenchmarkDeps,
  type BenchmarkLevelResult,
  type BenchmarkOptions,
  type BenchmarkPlayer,
  type PlayerFactory,
  type PlayerStats,
} from "./benchmark";
import type { PresentationSample } from "./frameSampler";

const MEASURE_MS = 500;

type Behavior = {
  /** Frames decoded per second by each player at this level. Default 15 (the clip rate). */
  fps?: number;
  /** Share of decoded frames that are dropped. */
  dropRatio?: number;
  stallsPerPlayer?: number;
  errorsPerPlayer?: number;
  presentation?: PresentationSample | undefined;
};

class FakePlayer implements BenchmarkPlayer {
  decoded = 0;
  dropped = 0;
  stalls = 0;
  errors = 0;
  started = false;
  disposed = 0;
  async start() {
    this.started = true;
  }
  stats(): PlayerStats {
    return { decodedFrames: this.decoded, droppedFrames: this.dropped, stalls: this.stalls, errors: this.errors };
  }
  dispose() {
    this.disposed += 1;
  }
}

function harness(behavior: (streams: number) => Behavior = () => ({}), extra: Partial<BenchmarkDeps> = {}) {
  let clock = 1_000;
  const players: FakePlayer[] = [];
  const live = () => players.filter((p) => p.disposed === 0);
  let current: Behavior = {};
  const sampler = {
    start: vi.fn(),
    stop: vi.fn(),
    sample: vi.fn((): PresentationSample | undefined => ("presentation" in current ? current.presentation : { fps: 60, avgFrameMs: 16.7, p95FrameMs: 17, maxFrameMs: 20, longFrames: 0, skippedGaps: 0 })),
  };
  const container = document.createElement("div");
  const deps: BenchmarkDeps = {
    container,
    loadClip: vi.fn(async () => new ArrayBuffer(8)),
    createPlayer: vi.fn<PlayerFactory>(() => {
      const p = new FakePlayer();
      players.push(p);
      return p;
    }),
    sampler,
    now: () => clock,
    // Time only moves when the benchmark waits; players decode while it waits.
    sleep: vi.fn(async (ms: number) => {
      clock += ms;
      const alive = live();
      current = behavior(alive.length);
      const seconds = ms / 1000;
      for (const p of alive) {
        const decoded = (current.fps ?? 15) * seconds;
        p.decoded += decoded;
        p.dropped += decoded * (current.dropRatio ?? 0);
        if (ms === MEASURE_MS) {
          p.stalls += current.stallsPerPlayer ?? 0;
          p.errors += current.errorsPerPlayer ?? 0;
        }
      }
    }),
    newId: () => "bench-1",
    ...extra,
  };
  return { deps, players, sampler, container, advance: (ms: number) => (clock += ms) };
}

const FAST: BenchmarkOptions = { warmupMs: 200, measureMs: MEASURE_MS };

describe("runClientBenchmark: progression", () => {
  it("measures every level in order with the sample clip and reports a healthy station", async () => {
    const h = harness();
    const progress: unknown[] = [];
    const result = await runClientBenchmark(h.deps, { ...FAST, onProgress: (p) => progress.push(p) });
    expect(DEFAULT_BENCHMARK_LEVELS).toEqual([4, 9, 16, 25, 32, 48]);
    expect(result.levels.map((l) => l.streams)).toEqual([4, 9, 16, 25, 32, 48]);
    expect(result.levels.every((l) => l.ran && l.state === "optimal")).toBe(true);
    expect(result.stoppedReason).toBe("completed");
    expect(result.maxStableStreams).toBe(48);
    expect(result.clientPerformanceScore).toBe(100);
    expect(result.clip).toEqual({ codec: BENCHMARK_CLIP.codec, width: 640, height: 360, fps: 15 });
    expect(result.id).toBe("bench-1");
    expect(result.durationMs).toBe(result.finishedAt - result.startedAt);
    expect(result.levels[0]!.decodedFps).toBeCloseTo(15, 5);
    expect(result.levels[0]!.droppedRatio).toBe(0);
    expect(result.levels[0]!.presentationFps).toBe(60);
    expect(result.levels[0]!.frameTimeP95Ms).toBe(17);
    // The clip is downloaded once, not once per level or per player.
    expect(h.deps.loadClip).toHaveBeenCalledTimes(1);
    expect(h.deps.createPlayer).toHaveBeenCalledTimes(4 + 9 + 16 + 25 + 32 + 48);
    expect(progress).toContainEqual({ phase: "testing", streams: 16, levelIndex: 2, levelCount: 6 });
  });

  it("keeps the whole run in the 3-5 second target at the default timings", async () => {
    const h = harness();
    const result = await runClientBenchmark(h.deps);
    expect(result.durationMs).toBeGreaterThanOrEqual(3_000);
    expect(result.durationMs).toBeLessThanOrEqual(5_000);
  });

  it("starts every player of a level before measuring and tears them all down", async () => {
    const h = harness();
    await runClientBenchmark(h.deps, { ...FAST, levels: [4, 9] });
    expect(h.players).toHaveLength(13);
    expect(h.players.every((p) => p.started)).toBe(true);
    expect(h.players.every((p) => p.disposed === 1)).toBe(true);
  });

  it("starts and stops the frame sampler around each measuring window", async () => {
    const h = harness();
    await runClientBenchmark(h.deps, { ...FAST, levels: [4, 9] });
    expect(h.sampler.start).toHaveBeenCalledTimes(2);
    expect(h.sampler.stop).toHaveBeenCalledTimes(2);
  });
});

describe("runClientBenchmark: early stop", () => {
  it("stops after the first saturated level and reports the rest as not run", async () => {
    const h = harness((n) => (n >= 25 ? { fps: 4 } : {}));
    const result = await runClientBenchmark(h.deps, FAST);
    expect(result.levels.map((l) => [l.streams, l.ran, l.state])).toEqual([
      [4, true, "optimal"],
      [9, true, "optimal"],
      [16, true, "optimal"],
      [25, true, "saturated"],
      [32, false, "unknown"],
      [48, false, "unknown"],
    ]);
    expect(result.stoppedReason).toBe("saturated");
    expect(result.maxStableStreams).toBe(16);
    expect(result.limits.saturatedAtStreams).toBe(25);
    expect(result.limits.stoppedAtStreams).toBe(25);
    expect(h.deps.createPlayer).toHaveBeenCalledTimes(4 + 9 + 16 + 25);
    expect(h.players.every((p) => p.disposed === 1)).toBe(true);
    // Not-run levels carry no invented metrics.
    const notRun = result.levels[5]!;
    expect(notRun).toMatchObject({ ran: false, state: "unknown", stalls: 0, decoderErrors: 0 });
    expect([notRun.decodedFps, notRun.droppedRatio, notRun.presentationFps, notRun.frameTimeP95Ms]).toEqual([undefined, undefined, undefined, undefined]);
  });

  it("does not stop at a level that is only at the limit", async () => {
    const h = harness((n) => (n === 16 ? { dropRatio: 0.07 } : {}));
    const result = await runClientBenchmark(h.deps, FAST);
    expect(result.levels.find((l) => l.streams === 16)?.state).toBe("limit");
    expect(result.levels.every((l) => l.ran)).toBe(true);
    expect(result.stoppedReason).toBe("completed");
    expect(result.limits.firstDegradedStreams).toBe(16);
  });

  it("stops on a decoder error and marks that level saturated", async () => {
    const h = harness((n) => (n === 9 ? { errorsPerPlayer: 1 } : {}));
    const result = await runClientBenchmark(h.deps, FAST);
    expect(result.stoppedReason).toBe("decoder_error");
    expect(result.levels[1]).toMatchObject({ streams: 9, ran: true, state: "saturated", decoderErrors: 9 });
    expect(result.levels.slice(2).every((l) => !l.ran)).toBe(true);
    expect(result.maxStableStreams).toBe(4);
    expect(h.players.every((p) => p.disposed === 1)).toBe(true);
  });

  it("stops when a single frame gap exceeds the hard cap", async () => {
    const h = harness((n) => (n === 16 ? { presentation: { fps: 30, avgFrameMs: 33, p95FrameMs: 36, maxFrameMs: 400, longFrames: 2, skippedGaps: 0 } } : {}));
    const result = await runClientBenchmark(h.deps, FAST);
    expect(result.stoppedReason).toBe("frame_gap");
    expect(result.levels[2]).toMatchObject({ streams: 16, ran: true, state: "saturated", maxFrameMs: 400 });
    expect(result.levels.slice(3).every((l) => !l.ran)).toBe(true);
  });

  it("treats a blocked main thread (the wait overshoots the window) as a frame gap", async () => {
    const h = harness();
    const base = h.deps.sleep;
    let measuring = 0;
    h.deps.sleep = vi.fn(async (ms: number, signal?: AbortSignal) => {
      await base(ms, signal);
      if (ms === MEASURE_MS && ++measuring === 2) h.advance(900); // the UI froze for 0.9 s
    });
    const result = await runClientBenchmark(h.deps, FAST);
    expect(result.stoppedReason).toBe("frame_gap");
    expect(result.levels[1]).toMatchObject({ streams: 9, state: "saturated" });
  });

  it("does not trust a level measured while the tab was hidden", async () => {
    let hidden = false;
    const h = harness(() => ({}), { isHidden: () => hidden });
    const base = h.deps.sleep;
    let measuring = 0;
    h.deps.sleep = vi.fn(async (ms: number, signal?: AbortSignal) => {
      await base(ms, signal);
      if (ms === MEASURE_MS && ++measuring === 2) hidden = true;
    });
    const result = await runClientBenchmark(h.deps, FAST);
    expect(result.stoppedReason).toBe("hidden");
    expect(result.levels.map((l) => l.ran)).toEqual([true, false, false, false, false, false]);
    expect(h.players.every((p) => p.disposed === 1)).toBe(true);
  });
});

describe("runClientBenchmark: failures and abort", () => {
  it("tears down and stops quietly when aborted in the middle of a level", async () => {
    const controller = new AbortController();
    const h = harness();
    const base = h.deps.sleep;
    let measuring = 0;
    h.deps.sleep = vi.fn(async (ms: number, signal?: AbortSignal) => {
      await base(ms, signal);
      if (ms === MEASURE_MS && ++measuring === 3) controller.abort();
    });
    const result = await runClientBenchmark(h.deps, { ...FAST, signal: controller.signal });
    expect(result.stoppedReason).toBe("aborted");
    expect(result.levels.map((l) => l.ran)).toEqual([true, true, false, false, false, false]);
    expect(h.players.every((p) => p.disposed === 1)).toBe(true);
    expect(h.sampler.stop).toHaveBeenCalled();
  });

  it("does nothing when already aborted", async () => {
    const controller = new AbortController();
    controller.abort();
    const h = harness();
    const result = await runClientBenchmark(h.deps, { ...FAST, signal: controller.signal });
    expect(result.stoppedReason).toBe("aborted");
    expect(h.deps.createPlayer).not.toHaveBeenCalled();
    expect(result.clientPerformanceScore).toBe(0);
  });

  it("disposes the players already created when the factory throws", async () => {
    const h = harness();
    let calls = 0;
    const create = h.deps.createPlayer;
    h.deps.createPlayer = vi.fn((c, clip) => {
      if (++calls === 7) throw new Error("no more decoders");
      return create(c, clip);
    });
    const result = await runClientBenchmark(h.deps, FAST);
    expect(result.stoppedReason).toBe("error");
    expect(result.error).toBe("no more decoders");
    expect(h.players).toHaveLength(6);
    expect(h.players.every((p) => p.disposed === 1)).toBe(true);
    expect(result.levels.map((l) => l.ran)).toEqual([true, false, false, false, false, false]);
  });

  it("disposes a player even when disposing another one throws", async () => {
    const h = harness();
    const create = h.deps.createPlayer;
    h.deps.createPlayer = vi.fn((c, clip) => {
      const p = create(c, clip) as FakePlayer;
      if (h.players.length === 1) p.dispose = () => {
        p.disposed += 1;
        throw new Error("boom");
      };
      return p;
    });
    await runClientBenchmark(h.deps, { ...FAST, levels: [4] });
    expect(h.players.every((p) => p.disposed === 1)).toBe(true);
  });

  it("reports a clip download failure without starting any player", async () => {
    const h = harness();
    h.deps.loadClip = vi.fn(async () => {
      throw new Error("clip 404");
    });
    const result = await runClientBenchmark(h.deps, FAST);
    expect(result.stoppedReason).toBe("error");
    expect(result.error).toBe("clip 404");
    expect(result.levels.every((l) => !l.ran)).toBe(true);
    expect(h.deps.createPlayer).not.toHaveBeenCalled();
  });

  it("reports a player that starts to fail mid-level as a decoder error, not a crash", async () => {
    const h = harness();
    const create = h.deps.createPlayer;
    h.deps.createPlayer = vi.fn((c, clip) => {
      const p = create(c, clip) as FakePlayer;
      p.start = async () => {
        p.errors += 1;
      };
      return p;
    });
    const result = await runClientBenchmark(h.deps, FAST);
    expect(result.stoppedReason).toBe("decoder_error");
    expect(result.levels[0]).toMatchObject({ ran: true, state: "saturated", decoderErrors: 4 });
  });
});

describe("classifyBenchmarkLevel", () => {
  const base = { starvedRatio: 0, droppedRatio: 0, stalledRatio: 0, decoderErrors: 0, frameTimeP95Ms: 17, presentationFps: 60 };
  it("is optimal when nothing is wrong", () => {
    expect(classifyBenchmarkLevel(base)).toEqual({ state: "optimal", reasons: [] });
  });
  it("reuses the dropped-frame thresholds of the live classifier", () => {
    expect(classifyBenchmarkLevel({ ...base, droppedRatio: 0.03 }).state).toBe("acceptable");
    expect(classifyBenchmarkLevel({ ...base, droppedRatio: 0.06 }).state).toBe("limit");
    expect(classifyBenchmarkLevel({ ...base, droppedRatio: 0.2 })).toEqual({ state: "saturated", reasons: ["dropped_frames"] });
  });
  it("judges streams that decode below the clip rate", () => {
    expect(classifyBenchmarkLevel({ ...base, starvedRatio: 0.3 })).toEqual({ state: "limit", reasons: ["decode_fps_short"] });
    expect(classifyBenchmarkLevel({ ...base, starvedRatio: 1 }).state).toBe("saturated");
  });
  it("uses frame time p95, and caps low presentation fps at acceptable on its own", () => {
    expect(classifyBenchmarkLevel({ ...base, frameTimeP95Ms: 45 }).state).toBe("limit");
    expect(classifyBenchmarkLevel({ ...base, presentationFps: 20 })).toEqual({ state: "acceptable", reasons: ["presentation_fps"] });
    expect(classifyBenchmarkLevel({ ...base, presentationFps: 20, frameTimeP95Ms: 45 }).state).toBe("saturated");
  });
  it("counts the share of players that stalled and any decoder error as saturated", () => {
    expect(classifyBenchmarkLevel({ ...base, stalledRatio: 0.3 })).toEqual({ state: "limit", reasons: ["stalls"] });
    expect(classifyBenchmarkLevel({ ...base, decoderErrors: 1 }).state).toBe("saturated");
  });
  it("skips signals the platform does not expose instead of judging them healthy or sick", () => {
    expect(classifyBenchmarkLevel({ decoderErrors: 0, stalledRatio: 0 })).toEqual({ state: "unknown", reasons: [] });
    expect(classifyBenchmarkLevel({ decoderErrors: 0, stalledRatio: 0, presentationFps: 60 }).state).toBe("optimal");
  });
});

describe("clientPerformanceScore", () => {
  const level = (streams: number, state: BenchmarkLevelResult["state"], ran = true): BenchmarkLevelResult => ({ streams, ran, state, stalls: 0, decoderErrors: 0, reasons: [] });
  const all = (states: BenchmarkLevelResult["state"][]) => [4, 9, 16, 25, 32, 48].map((n, i) => level(n, states[i] ?? "unknown", i < states.length));

  it("is 0 when nothing ran and 100 when every level is optimal", () => {
    expect(computeClientPerformanceScore(all([]))).toBe(0);
    expect(computeClientPerformanceScore(all(Array(6).fill("optimal")))).toBe(100);
  });
  it("stays within 0-100 and never rewards a worse state", () => {
    const order: BenchmarkLevelResult["state"][] = ["saturated", "limit", "acceptable", "optimal"];
    let previous = -1;
    for (const state of order) {
      const score = computeClientPerformanceScore(all(Array(6).fill(state)));
      expect(score).toBeGreaterThanOrEqual(0);
      expect(score).toBeLessThanOrEqual(100);
      expect(score).toBeGreaterThan(previous);
      previous = score;
    }
  });
  it("weights larger levels more, so holding 48 streams outranks holding only 4", () => {
    const only4 = computeClientPerformanceScore(all(["optimal", "saturated"]));
    const up16 = computeClientPerformanceScore(all(["optimal", "optimal", "optimal", "saturated"]));
    expect(up16).toBeGreaterThan(only4);
    expect(only4).toBeGreaterThan(0);
  });
});

describe("summarizeBenchmark", () => {
  it("lists only the levels that ran, biggest first, as LQ, and never claims HQ", async () => {
    const h = harness((n) => (n >= 25 ? { fps: 4 } : {}));
    const result = await runClientBenchmark(h.deps, FAST);
    const summary = summarizeBenchmark(result);
    expect(summary.rows).toEqual([
      { streams: 25, quality: "LQ", state: "saturated" },
      { streams: 16, quality: "LQ", state: "optimal" },
      { streams: 9, quality: "LQ", state: "optimal" },
      { streams: 4, quality: "LQ", state: "optimal" },
    ]);
    expect(summary.hq).toBe("not_measured");
    expect(summary.rows.some((r) => (r.quality as string) === "HQ")).toBe(false);
    expect(summary.maxStableStreams).toBe(16);
  });
});
