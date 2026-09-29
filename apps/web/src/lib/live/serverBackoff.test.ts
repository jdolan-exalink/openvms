import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ServerBackoff } from "./serverBackoff";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("ServerBackoff", () => {
  const make = () => new ServerBackoff({ random: () => 0, baseMs: 1000, probeTimeoutMs: 5000, staggerMs: 100 });

  it("retries a session without a server after its own jittered delay", () => {
    const run = vi.fn();
    make().schedule(undefined, 1, run);
    vi.advanceTimersByTime(999);
    expect(run).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1500);
    expect(run).toHaveBeenCalledTimes(1);
  });

  it("lets one session of a failing server probe while the others wait for it", () => {
    const backoff = make();
    const runs = [vi.fn(), vi.fn(), vi.fn()];
    runs.forEach((run) => backoff.schedule("srv", 1, run));
    vi.advanceTimersByTime(2100);
    expect(runs.map((r) => r.mock.calls.length)).toEqual([1, 0, 0]);
    // Still no media after the probe timeout: nobody else reconnects.
    vi.advanceTimersByTime(4000);
    expect(runs[1]).not.toHaveBeenCalled();
    // The probe gets media: the rest are released, staggered.
    backoff.succeeded("srv");
    vi.advanceTimersByTime(1000);
    expect(runs.map((r) => r.mock.calls.length)).toEqual([1, 1, 1]);
  });

  it("does not run a cancelled waiter", () => {
    const backoff = make();
    const run = vi.fn();
    const cancel = backoff.schedule("srv", 1, run);
    cancel();
    vi.advanceTimersByTime(60_000);
    expect(run).not.toHaveBeenCalled();
  });
});
