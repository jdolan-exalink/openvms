import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getFisheyeConfig, saveFisheyeConfig } from "./storage";
import { DEFAULT_FISHEYE_CONFIG, type FisheyeConfig } from "./types";
import { useFisheyeConfig } from "./useFisheyeConfig";

const config = (cameraId: string, over: Partial<FisheyeConfig> = {}): FisheyeConfig => ({
  ...DEFAULT_FISHEYE_CONFIG,
  cameraId,
  enabled: true,
  lensType: "fisheye_360",
  ...over,
});

describe("useFisheyeConfig", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("returns the stored config", () => {
    saveFisheyeConfig(config("cam-1"));
    const { result } = renderHook(() => useFisheyeConfig("cam-1"));
    expect(result.current).toEqual(getFisheyeConfig("cam-1"));
    expect(result.current.enabled).toBe(true);
  });

  it("returns the new config after a save for that camera", () => {
    const { result } = renderHook(() => useFisheyeConfig("cam-1"));
    expect(result.current.enabled).toBe(false);
    act(() => saveFisheyeConfig(config("cam-1", { lensType: "fisheye_180" })));
    expect(result.current.enabled).toBe(true);
    expect(result.current.lensType).toBe("fisheye_180");
  });

  it("keeps the same reference across rerenders when nothing changed", () => {
    saveFisheyeConfig(config("cam-1"));
    const { result, rerender } = renderHook(() => useFisheyeConfig("cam-1"));
    const first = result.current;
    rerender();
    rerender();
    expect(result.current).toBe(first);
  });

  it("keeps the reference when a different camera is saved", () => {
    saveFisheyeConfig(config("cam-1"));
    const { result } = renderHook(() => useFisheyeConfig("cam-1"));
    const first = result.current;
    act(() => saveFisheyeConfig(config("cam-2")));
    expect(result.current).toBe(first);
  });

  it("unsubscribes on unmount", () => {
    const remove = vi.spyOn(window, "removeEventListener");
    const { unmount } = renderHook(() => useFisheyeConfig("cam-1"));
    unmount();
    expect(remove).toHaveBeenCalledWith("openvms:fisheye-config-changed", expect.any(Function));
  });
});
