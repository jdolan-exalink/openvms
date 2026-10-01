import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { routeTree } from "@/router";
import { json, stubApi } from "@/test-utils";
import { PlayerSession } from "@/lib/live/PlayerSession";
import { PlayerSessionManager } from "@/lib/live/PlayerSessionManager";
import { liveSelectionKey, parseSelection, serializeSelection } from "@/lib/liveGrid";
import type { MapCanvasProps } from "./canvas/MapCanvas";

vi.mock("./canvas/MapCanvas", () => ({
  MapCanvas: (props: MapCanvasProps) => <div data-testid="canvas">
    {props.cameras?.map(camera => <button key={camera.id}
      onClick={() => props.onSelectCamera?.(camera.id)}
      onMouseEnter={() => props.onHoverCamera?.(camera.id, { x: 10, y: 20 })}
      onMouseLeave={() => props.onHoverCamera?.(null)}
      onDoubleClick={() => props.onDoubleClickCamera?.(camera.id)}
      onContextMenu={() => props.onContextMenuCamera?.(camera.id, { x: 10, y: 20 })}
    >Marker {camera.id}</button>)}
  </div>,
}));

afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); localStorage.clear(); });
async function setup() {
  vi.spyOn(PlayerSession.prototype, "connect").mockImplementation(() => {});
  const cameras = Array.from({ length: 5 }, (_, n) => ({
    id: `c${n}`, tenant_id: "t", site_id: "s", server_id: "srv", remote_name: `c${n}`,
    display_name: `Camera ${n}`, enabled: true, zones: [], lpr: false, status: "online",
    fps: 5, group_ids: [], default_live_quality: "sub", description: "", location: "", tags: [],
    created_at: "", updated_at: "",
  }));
  vi.stubGlobal("fetch", vi.fn(stubApi({
    "/api/v1/me": () => json({ id: "u", username: "admin", tenant_id: "t", grants: [
      { permission: "maps.view", effect: "allow", scope_type: "platform" },
      { permission: "live.view", effect: "allow", scope_type: "platform" },
    ] }),
    "/api/v1/maps/config": () => json({ provider: { id: "local", kind: "pmtiles", tiles: ["/tiles/base.pmtiles"],
      attribution: "local", max_zoom: 18, offline: true }, default_center: { lat: 0, lng: 0 }, default_zoom: 14 }),
    "/api/v1/maps/overview": () => json({ items: [{ id: "s", name: "Site", lat: 0, lng: 0,
      camera_count: 5, online_cameras: 5, offline_cameras: 0, degraded_cameras: 0, alarm_count: 0 }] }),
    "/api/v1/maps/sites/s/entities": () => json({ revision: "1", entities: cameras.map((camera, n) => ({
      id: camera.id, t: "camera", site: "s", srv: "srv", name: camera.display_name,
      pos: { kind: "geo", lat: 0, lng: n * 0.001 }, st: "online", alarms: 0,
      cam: { bearing: 0, fov: 60, range: 100, type: "fixed", ptz: false, lpr: false },
    })) }),
    "/api/v1/cameras": () => json({ items: cameras }),
    "/api/v1/sites": () => json({ items: [] }),
    "/api/v1/servers": () => json({ items: [] }),
    "/api/v1/views": () => json({ items: [] }),
  })));
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createRouter({ routeTree, context: { queryClient },
    history: createMemoryHistory({ initialEntries: ["/maps?site=s"] }) });
  const view = render(<QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  await screen.findByRole("button", { name: "Marker c0" });
  return { ...view, router };
}
describe("Maps camera interaction integration", () => {
  it("prewarms at 400ms, defaults live hover off, and releases on leave", async () => {
    const acquire = vi.spyOn(PlayerSessionManager.prototype, "acquire");
    const release = vi.spyOn(PlayerSessionManager.prototype, "release");
    await setup();
    vi.useFakeTimers();
    fireEvent.mouseEnter(screen.getByRole("button", { name: "Marker c0" }));
    expect(acquire).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(150));
    expect(screen.getByAltText("Camera 0")).toBeInTheDocument();
    expect(acquire).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(250));
    expect(acquire).toHaveBeenCalledWith("c0", "sub", "srv");
    act(() => vi.advanceTimersByTime(300));
    expect(document.querySelector("[data-surface-slot='c0']")).toBeNull();
    fireEvent.mouseLeave(screen.getByRole("button", { name: "Marker c0" }));
    expect(release).toHaveBeenCalledWith("c0", "sub");
  });


  it("opts into live hover at 700ms without acquiring a second session", async () => {
    const acquire = vi.spyOn(PlayerSessionManager.prototype, "acquire");
    await setup();
    fireEvent.click(screen.getByRole("checkbox", { name: "Live on hover" }));
    vi.useFakeTimers();
    fireEvent.mouseEnter(screen.getByRole("button", { name: "Marker c0" }));
    act(() => vi.advanceTimersByTime(400));
    expect(acquire).toHaveBeenCalledTimes(1);
    act(() => vi.advanceTimersByTime(300));
    expect(document.querySelector("[data-surface-slot='c0']")).not.toBeNull();
    expect(acquire).toHaveBeenCalledTimes(1);
  });

  it("caps pins at four and avoids a second hover preview for a pinned camera", async () => {
    await setup();
    for (let n = 0; n < 5; n++) fireEvent.click(screen.getByRole("button", { name: `Marker c${n}` }));
    expect(screen.getAllByRole("button", { name: "Close preview" })).toHaveLength(4);
    fireEvent.mouseEnter(screen.getByRole("button", { name: "Marker c4" }));
    expect(screen.queryByRole("button", { name: "Pin preview" })).not.toBeInTheDocument();
  });

  it.each(["double-click", "context menu", "panel"])("%s hands off the camera through the real Live route", async (action) => {
    localStorage.setItem(liveSelectionKey("t", "u"), serializeSelection(2, [
      { camera_id: "c0", quality: "main" }, null, null, null,
    ]));
    const { router } = await setup();
    const marker = screen.getByRole("button", { name: "Marker c1" });
    if (action === "double-click") fireEvent.doubleClick(marker);
    else {
      if (action === "context menu") fireEvent.contextMenu(marker);
      else fireEvent.click(marker);
      fireEvent.click(screen.getByRole("button", { name: "Open in Live View" }));
    }
    await waitFor(() => expect(router.state.location.pathname).toBe("/live"));
    await waitFor(() => {
      const saved = parseSelection(localStorage.getItem(liveSelectionKey("t", "u")), new Set(["c0", "c1"]));
      expect(saved?.tiles.slice(0, 2)).toEqual([
        { camera_id: "c0", quality: "main" }, { camera_id: "c1", quality: "sub" },
      ]);
    });
  });
});
