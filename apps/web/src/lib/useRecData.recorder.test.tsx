import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { useRecData } from "./useRecData";

afterEach(() => vi.unstubAllGlobals());

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{children}</QueryClientProvider>
);

const spans = [
  { start_time: "2026-10-28T10:27:41Z", end_time: "2026-10-28T10:28:18Z", motion: 1, objects: 0 },
  { start_time: "2026-10-28T10:40:00Z", end_time: "2026-10-28T10:40:30Z", motion: 2, objects: 0 },
];

function stub() {
  vi.stubGlobal("fetch", vi.fn(stubApi({
    "/api/v1/cameras": () => json({ items: [{ id: "rec", server_id: "s-nvr" }, { id: "frig", server_id: "s-frigate" }] }),
    "/api/v1/servers": () => json({ items: [{ id: "s-nvr", kind: "provision_nvr" }, { id: "s-frigate", kind: "frigate" }] }),
    "/api/v1/cameras/rec/recordings": () => json({ items: spans }),
    "/api/v1/cameras/frig/recordings": () => json({ items: spans }),
    "/api/v1/events": () => json({ items: [], next_cursor: null }),
  })));
}

describe("useRecData", () => {
  it("adds a recorder camera's motion as detections, once the camera is known to be a recorder's", async () => {
    stub();
    const { result } = renderHook(() => useRecData(["rec"], Date.parse("2026-10-28T03:00:00Z") / 1000, true), { wrapper });
    await waitFor(() => expect(result.current.events.length).toBe(2));
    expect(result.current.events.map((e) => [e.cameraId, e.label])).toEqual([["rec", "motion"], ["rec", "motion"]]);
    expect(result.current.events[0]!.end).toBeGreaterThan(result.current.events[0]!.time);
    expect(Object.keys(result.current.spans)).toEqual(["rec"]);
  });

  it("does not invent detections for a Frigate camera, whose spans also count motion", async () => {
    stub();
    const { result } = renderHook(() => useRecData(["frig"], Date.parse("2026-10-28T03:00:00Z") / 1000, true), { wrapper });
    await waitFor(() => expect(result.current.loaded).toEqual(["frig"]));
    expect(result.current.events).toEqual([]);
  });
});
