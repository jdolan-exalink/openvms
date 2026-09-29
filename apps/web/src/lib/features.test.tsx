import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { FEATURE_DEFAULTS, FEATURES_OVERRIDE_KEY, useFeatures } from "./features";

function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

beforeEach(() => localStorage.clear());
afterEach(() => vi.unstubAllGlobals());

describe("useFeatures", () => {
  it("returns every flag off while loading", () => {
    vi.stubGlobal("fetch", vi.fn(() => new Promise(() => {})));
    const { result } = renderHook(() => useFeatures(), { wrapper: wrapper() });
    expect(result.current).toEqual(FEATURE_DEFAULTS);
    expect(Object.values(result.current).every((v) => v === false)).toBe(true);
  });

  it("maps the API response to camelCase flags", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/features": () =>
            json({
              persistent_players: true,
              video_surface_layer: false,
              adaptive_streaming: false,
              stream_prewarming: true,
              seamless_quality_switch: false,
            }),
        }),
      ),
    );
    const { result } = renderHook(() => useFeatures(), { wrapper: wrapper() });
    await waitFor(() => expect(result.current.persistentPlayers).toBe(true));
    expect(result.current.streamPrewarming).toBe(true);
    expect(result.current.videoSurfaceLayer).toBe(false);
  });

  it("falls back to safe defaults when the endpoint fails", async () => {
    const fetchMock = vi.fn(stubApi({ "/api/v1/features": () => json({ code: "boom", message: "x" }, 500) }));
    vi.stubGlobal("fetch", fetchMock);
    const { result } = renderHook(() => useFeatures(), { wrapper: wrapper() });
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(result.current).toEqual(FEATURE_DEFAULTS);
  });

  it("lets a development localStorage override switch a flag on", async () => {
    localStorage.setItem(FEATURES_OVERRIDE_KEY, JSON.stringify({ persistentPlayers: true, bogus: true }));
    vi.stubGlobal("fetch", vi.fn(stubApi({ "/api/v1/features": () => json({ code: "boom", message: "x" }, 500) })));
    const { result } = renderHook(() => useFeatures(), { wrapper: wrapper() });
    expect(result.current.persistentPlayers).toBe(true);
    expect(result.current.videoSurfaceLayer).toBe(false);
    expect("bogus" in result.current).toBe(false);
  });

  it("ignores a malformed override", () => {
    localStorage.setItem(FEATURES_OVERRIDE_KEY, "{not json");
    vi.stubGlobal("fetch", vi.fn(() => new Promise(() => {})));
    const { result } = renderHook(() => useFeatures(), { wrapper: wrapper() });
    expect(result.current).toEqual(FEATURE_DEFAULTS);
  });
});
