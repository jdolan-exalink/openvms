import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PHONE_QUERY, useIsPhoneLayout } from "./useIsPhoneLayout";

afterEach(() => vi.unstubAllGlobals());

function stubMatchMedia(initial: boolean) {
  let matches = initial;
  const listeners = new Set<() => void>();
  const queries: string[] = [];
  vi.stubGlobal("matchMedia", (query: string) => {
    queries.push(query);
    return {
      get matches() {
        return matches;
      },
      media: query,
      addEventListener: (_: string, fn: () => void) => listeners.add(fn),
      removeEventListener: (_: string, fn: () => void) => listeners.delete(fn),
    };
  });
  return {
    queries,
    set(next: boolean) {
      matches = next;
      listeners.forEach((fn) => fn());
    },
  };
}

describe("useIsPhoneLayout", () => {
  it("uses the bottom navigation breakpoint and follows media changes", () => {
    const media = stubMatchMedia(true);
    const { result } = renderHook(() => useIsPhoneLayout());
    expect(media.queries).toContain(PHONE_QUERY);
    expect(PHONE_QUERY).toBe("(max-width: 767px)");
    expect(result.current).toBe(true);
    act(() => media.set(false));
    expect(result.current).toBe(false);
    act(() => media.set(true));
    expect(result.current).toBe(true);
  });

  it("is false when matchMedia is unavailable", () => {
    vi.stubGlobal("matchMedia", undefined);
    const { result } = renderHook(() => useIsPhoneLayout());
    expect(result.current).toBe(false);
  });
});
