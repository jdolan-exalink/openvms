import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { COARSE_QUERY, PHONE_SHORT_SIDE_PX, useIsPhoneDevice } from "./useIsPhoneDevice";

afterEach(() => vi.unstubAllGlobals());

function stubDevice({ coarse, screen, viewport }: { coarse: boolean; screen?: { width: number; height: number } | null; viewport?: { w: number; h: number } }) {
  let isCoarse = coarse;
  const listeners = new Set<() => void>();
  vi.stubGlobal("matchMedia", (query: string) => ({
    get matches() {
      return query === COARSE_QUERY ? isCoarse : false;
    },
    media: query,
    addEventListener: (_: string, fn: () => void) => listeners.add(fn),
    removeEventListener: (_: string, fn: () => void) => listeners.delete(fn),
  }));
  vi.stubGlobal("screen", screen === null ? undefined : (screen ?? { width: 390, height: 844 }));
  if (viewport) {
    vi.stubGlobal("innerWidth", viewport.w);
    vi.stubGlobal("innerHeight", viewport.h);
  }
  return {
    setCoarse(next: boolean) {
      isCoarse = next;
      listeners.forEach((fn) => fn());
    },
  };
}

describe("useIsPhoneDevice", () => {
  it("uses a coarse-pointer query and a 600px short-side threshold", () => {
    expect(COARSE_QUERY).toBe("(pointer: coarse)");
    expect(PHONE_SHORT_SIDE_PX).toBe(600);
  });

  it("is a phone with a coarse pointer and a short side of 390", () => {
    stubDevice({ coarse: true, screen: { width: 390, height: 844 } });
    expect(renderHook(() => useIsPhoneDevice()).result.current).toBe(true);
  });

  it("does not flip when the phone rotates (short side stays 390)", () => {
    stubDevice({ coarse: true, screen: { width: 844, height: 390 } });
    expect(renderHook(() => useIsPhoneDevice()).result.current).toBe(true);
  });

  it("is not a phone for a tablet (coarse, short side 820)", () => {
    stubDevice({ coarse: true, screen: { width: 820, height: 1180 } });
    expect(renderHook(() => useIsPhoneDevice()).result.current).toBe(false);
  });

  it("is not a phone with a fine pointer even in a narrow window", () => {
    stubDevice({ coarse: false, screen: { width: 1920, height: 1080 }, viewport: { w: 400, h: 800 } });
    expect(renderHook(() => useIsPhoneDevice()).result.current).toBe(false);
  });

  it("falls back to the viewport when screen is unavailable", () => {
    stubDevice({ coarse: true, screen: null, viewport: { w: 390, h: 800 } });
    expect(renderHook(() => useIsPhoneDevice()).result.current).toBe(true);
    stubDevice({ coarse: true, screen: null, viewport: { w: 900, h: 1200 } });
    expect(renderHook(() => useIsPhoneDevice()).result.current).toBe(false);
  });

  it("reacts to pointer media changes", () => {
    const device = stubDevice({ coarse: false, screen: { width: 390, height: 844 } });
    const { result } = renderHook(() => useIsPhoneDevice());
    expect(result.current).toBe(false);
    act(() => device.setCoarse(true));
    expect(result.current).toBe(true);
    act(() => device.setCoarse(false));
    expect(result.current).toBe(false);
  });

  it("is false when matchMedia is unavailable", () => {
    vi.stubGlobal("matchMedia", undefined);
    expect(renderHook(() => useIsPhoneDevice()).result.current).toBe(false);
  });
});
