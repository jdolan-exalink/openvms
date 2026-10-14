import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { createTelemetryRuntime } from "@/lib/telemetry/runtime";
import type { ClientCapabilities } from "@/lib/telemetry";
import { useClientCapabilities, useVideoTelemetry } from "./hooks";

function fakeRuntime() {
  let tick: (() => void) | undefined;
  const cancel = vi.fn();
  let clock = 0;
  const runtime = createTelemetryRuntime({
    now: () => clock,
    schedule: (fn) => {
      tick = fn;
      return cancel;
    },
    sessions: () => [],
  });
  return { runtime, cancel, tick: () => { clock += 1000; tick?.(); } };
}

describe("useVideoTelemetry", () => {
  it("acquires the runtime on mount and releases it on unmount", () => {
    const { runtime, cancel } = fakeRuntime();
    const { unmount } = renderHook(() => useVideoTelemetry(true, runtime));
    expect(runtime.consumers()).toBe(1);
    unmount();
    expect(runtime.consumers()).toBe(0);
    expect(cancel).toHaveBeenCalledTimes(1);
  });

  it("does not acquire the runtime while disabled", () => {
    const { runtime } = fakeRuntime();
    const { result, rerender, unmount } = renderHook(({ on }) => useVideoTelemetry(on, runtime), { initialProps: { on: false } });
    expect(runtime.consumers()).toBe(0);
    expect(result.current.state).toBe("unknown");
    rerender({ on: true });
    expect(runtime.consumers()).toBe(1);
    rerender({ on: false });
    expect(runtime.consumers()).toBe(0);
    unmount();
    expect(runtime.consumers()).toBe(0);
  });

  it("re-renders once per tick with the new view", () => {
    const { runtime, tick } = fakeRuntime();
    let renders = 0;
    const { result } = renderHook(() => {
      renders += 1;
      return useVideoTelemetry(true, runtime);
    });
    const before = renders;
    act(() => tick());
    expect(renders - before).toBe(1);
    expect(result.current.snapshot.metrics).not.toBeNull();
  });
});

const CAPS = { runtime: "browser", collectedAt: 1, system: {}, cpu: {}, memory: { bucketed: false, source: "unavailable" }, gpu: { webgl: false, webgl2: false }, codecs: [] } as ClientCapabilities;

describe("useClientCapabilities", () => {
  it("collects once and exposes loading then data", async () => {
    const collect = vi.fn(async () => CAPS);
    const { result } = renderHook(() => useClientCapabilities(collect));
    expect(result.current.status).toBe("loading");
    await waitFor(() => expect(result.current.status).toBe("ready"));
    expect(result.current.capabilities).toBe(CAPS);
    expect(collect).toHaveBeenCalledTimes(1);
  });

  it("exposes an error state when collection rejects", async () => {
    const collect = vi.fn(async () => {
      throw new Error("boom");
    });
    const { result } = renderHook(() => useClientCapabilities(collect));
    await waitFor(() => expect(result.current.status).toBe("error"));
    expect(result.current.capabilities).toBeUndefined();
  });

  it("ignores a result that arrives after unmount", async () => {
    let resolve: (c: ClientCapabilities) => void = () => undefined;
    const collect = vi.fn(() => new Promise<ClientCapabilities>((r) => (resolve = r)));
    const { result, unmount } = renderHook(() => useClientCapabilities(collect));
    unmount();
    await act(async () => resolve(CAPS));
    expect(result.current.status).toBe("loading");
  });
});
