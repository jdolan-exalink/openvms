import { describe, expect, it } from "vitest";
import { createFrameSampler } from "./frameSampler";

function fakeRaf() {
  let clock = 0;
  let nextId = 1;
  const pending = new Map<number, () => void>();
  return {
    raf: (cb: () => void) => {
      const id = nextId++;
      pending.set(id, cb);
      return id;
    },
    cancel: (id: number) => void pending.delete(id),
    now: () => clock,
    /** Advances the clock by `ms` and fires the pending frame callback, which re-requests itself. */
    frame(ms: number) {
      clock += ms;
      const [id, cb] = [...pending][0] ?? [];
      if (id === undefined || !cb) throw new Error("no frame pending");
      pending.delete(id);
      cb();
    },
    pendingCount: () => pending.size,
  };
}

describe("frame sampler", () => {
  it("discards the gap after a paused rAF (hidden tab) instead of reporting it as a slow frame", () => {
    const f = fakeRaf();
    const sampler = createFrameSampler(f.raf, f.cancel, f.now);
    sampler.start();
    for (let i = 0; i < 11; i++) f.frame(20);
    f.frame(45_000); // the tab was hidden for 45 s
    // After the gap the cadence changes: measuring must resume from the new frame, not stop.
    for (let i = 0; i < 10; i++) f.frame(10);
    const s = sampler.sample();
    expect(s?.avgFrameMs).toBeCloseTo((10 * 20 + 10 * 10) / 20, 5);
    expect(s?.p95FrameMs).toBeCloseTo(20, 5);
  });

  it("measures presentation fps and frame time from frame cadence", () => {
    const f = fakeRaf();
    const sampler = createFrameSampler(f.raf, f.cancel, f.now);
    sampler.start();
    for (let i = 0; i < 31; i++) f.frame(20); // 50 fps
    const s = sampler.sample();
    expect(s?.fps).toBeCloseTo(50, 5);
    expect(s?.avgFrameMs).toBeCloseTo(20, 5);
    expect(s?.p95FrameMs).toBeCloseTo(20, 5);
  });

  it("reports the p95 frame time so a few slow frames show up", () => {
    const f = fakeRaf();
    const sampler = createFrameSampler(f.raf, f.cancel, f.now);
    sampler.start();
    f.frame(16); // first frame only sets the baseline
    for (let i = 0; i < 90; i++) f.frame(16);
    for (let i = 0; i < 10; i++) f.frame(100);
    const s = sampler.sample()!;
    expect(s.p95FrameMs).toBe(100);
    expect(s.avgFrameMs).toBeCloseTo(24.4, 1);
  });

  it("consumes the window on sample and returns undefined without enough frames", () => {
    const f = fakeRaf();
    const sampler = createFrameSampler(f.raf, f.cancel, f.now);
    sampler.start();
    expect(sampler.sample()).toBeUndefined();
    f.frame(16);
    expect(sampler.sample()).toBeUndefined(); // one frame has no interval yet
    f.frame(16);
    f.frame(16);
    expect(sampler.sample()).toBeDefined();
    expect(sampler.sample()).toBeUndefined();
  });

  it("keeps only the most recent frames when more arrive than the window holds", () => {
    const f = fakeRaf();
    const sampler = createFrameSampler(f.raf, f.cancel, f.now, 4);
    sampler.start();
    f.frame(1000); // baseline
    f.frame(500); // evicted once the window is full
    for (let i = 0; i < 4; i++) f.frame(10);
    expect(sampler.sample()?.avgFrameMs).toBe(10);
  });

  it("cancels the pending frame on stop and ignores double start/stop", () => {
    const f = fakeRaf();
    const sampler = createFrameSampler(f.raf, f.cancel, f.now);
    sampler.start();
    sampler.start();
    expect(f.pendingCount()).toBe(1);
    sampler.stop();
    sampler.stop();
    expect(f.pendingCount()).toBe(0);
  });
  it("reports the longest frame, the long-frame count and the discarded paused gaps", () => {
    const f = fakeRaf();
    const sampler = createFrameSampler(f.raf, f.cancel, f.now);
    sampler.start();
    f.frame(16);
    f.frame(16);
    f.frame(120); // a long frame: a visible hitch
    f.frame(16);
    f.frame(5_000); // paused rAF: discarded, but counted as a skipped gap
    f.frame(16);
    const s = sampler.sample();
    expect(s?.maxFrameMs).toBe(120);
    expect(s?.longFrames).toBe(1);
    expect(s?.skippedGaps).toBe(1);
    // The next window starts clean.
    f.frame(16);
    f.frame(16);
    expect(sampler.sample()).toMatchObject({ maxFrameMs: 16, longFrames: 0, skippedGaps: 0 });
  });
});
