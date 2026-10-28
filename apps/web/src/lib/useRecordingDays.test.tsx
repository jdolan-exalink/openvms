import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json } from "@/test-utils";
import { dayKey, useRecordingDays } from "./useRecordingDays";

afterEach(() => vi.unstubAllGlobals());

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>
);

describe("useRecordingDays", () => {
  it("names a day the way the API does", () => {
    expect(dayKey(new Date(2026, 0, 5, 23, 59))).toBe("2026-01-05");
  });

  it("marks the days the server lists for the month shown, asking in the browser's time zone", async () => {
    const urls: string[] = [];
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      urls.push(String(input instanceof Request ? input.url : input));
      return json({ items: ["2026-10-26", "2026-10-27"] });
    }));
    const { result } = renderHook(() => useRecordingDays("cam-1", new Date(2026, 9, 28)), { wrapper });
    await waitFor(() => expect(result.current.marked(new Date(2026, 9, 27))).toBe(true));
    expect(result.current.marked(new Date(2026, 9, 26))).toBe(true);
    expect(result.current.marked(new Date(2026, 9, 25))).toBe(false);
    expect(urls[0]).toContain("/api/v1/cameras/cam-1/recording-days");
    expect(urls[0]).toContain("timezone=" + encodeURIComponent(Intl.DateTimeFormat().resolvedOptions().timeZone));
  });

  it("asks for the new month when the calendar moves, and asks nothing without a camera", async () => {
    const fetchMock = vi.fn(async () => json({ items: [] }));
    vi.stubGlobal("fetch", fetchMock);
    const none = renderHook(() => useRecordingDays(undefined, new Date(2026, 9, 28)), { wrapper });
    expect(none.result.current.marked(new Date(2026, 9, 27))).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();

    const { result } = renderHook(() => useRecordingDays("cam-1", new Date(2026, 9, 28)), { wrapper });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    act(() => result.current.onMonthChange(2026, 8));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });

  it("leaves every day unmarked when the server fails", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json({ code: "frigate_unreachable" }, 502)));
    const { result } = renderHook(() => useRecordingDays("cam-1", new Date(2026, 9, 28)), { wrapper });
    await new Promise((r) => setTimeout(r, 50));
    expect(result.current.marked(new Date(2026, 9, 27))).toBe(false);
  });
});
