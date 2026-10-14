/** Presentation cadence measured since the previous sample. */
export type PresentationSample = {
  fps: number;
  avgFrameMs: number;
  p95FrameMs: number;
};

export type FrameSampler = {
  start(): void;
  stop(): void;
  /** Summarizes the frames seen since the last call and starts a new window; undefined without data. */
  sample(): PresentationSample | undefined;
};

const DEFAULT_WINDOW = 256;

/**
 * A gap longer than this is a paused requestAnimationFrame (hidden tab, minimized window), not
 * a slow frame: it is discarded so it cannot distort fps and frame time after the tab returns.
 */
export const MAX_FRAME_GAP_MS = 1000;

/**
 * Measures requestAnimationFrame cadence, a proxy for how often the browser actually presents
 * a frame. Per frame it only writes one number into a preallocated typed-array ring.
 * `raf`, `cancelRaf` and `now` are injected so the sampler stays testable and platform-neutral.
 */
export function createFrameSampler(
  raf: (callback: () => void) => number,
  cancelRaf: (handle: number) => void,
  now: () => number,
  windowSize = DEFAULT_WINDOW,
): FrameSampler {
  const deltas = new Float64Array(windowSize);
  const scratch = new Float64Array(windowSize);
  let head = 0;
  let count = 0;
  let lastFrameAt: number | undefined;
  let handle: number | undefined;

  const onFrame = (): void => {
    const t = now();
    if (lastFrameAt !== undefined && t - lastFrameAt <= MAX_FRAME_GAP_MS) {
      deltas[head] = t - lastFrameAt;
      head = (head + 1) % windowSize;
      if (count < windowSize) count += 1;
    }
    lastFrameAt = t;
    handle = raf(onFrame);
  };

  return {
    start() {
      if (handle !== undefined) return;
      lastFrameAt = undefined;
      head = 0;
      count = 0;
      handle = raf(onFrame);
    },
    stop() {
      if (handle === undefined) return;
      cancelRaf(handle);
      handle = undefined;
    },
    sample() {
      if (count === 0) return undefined;
      let sum = 0;
      for (let i = 0; i < count; i++) {
        const d = deltas[i]!;
        scratch[i] = d;
        sum += d;
      }
      const sorted = scratch.subarray(0, count).sort();
      const avg = sum / count;
      const p95 = sorted[Math.min(count - 1, Math.ceil(count * 0.95) - 1)]!;
      head = 0;
      count = 0;
      return { fps: avg > 0 ? 1000 / avg : 0, avgFrameMs: avg, p95FrameMs: p95 };
    },
  };
}
