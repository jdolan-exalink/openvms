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

const canvasHarness = vi.hoisted(() => ({ props: null as MapCanvasProps | null, easeTo: vi.fn() }));

vi.mock("./canvas/MapCanvas", () => ({
  MapCanvas: (props: MapCanvasProps) => { canvasHarness.props = props; return <div data-testid="canvas">
    <button onClick={() => props.realtimeStore?.handleFrame({ type: "server.status", server_id: "srv", data: { status: "offline" } })}>Server outage</button>
    {props.cameras?.map(camera => <button key={camera.id}
      onClick={() => props.onSelectCamera?.(camera.id)}
      onMouseEnter={() => props.onHoverCamera?.(camera.id, { x: 10, y: 20 })}
      onMouseLeave={() => props.onHoverCamera?.(null)}
      onDoubleClick={() => props.onDoubleClickCamera?.(camera.id)}
      onContextMenu={() => props.onContextMenuCamera?.(camera.id, { x: 10, y: 20 })}
    >Marker {camera.id}</button>)}
  </div>; },
}));

afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); vi.unstubAllEnvs(); localStorage.clear(); canvasHarness.easeTo.mockReset(); });
async function setup(alarmsEnabled = false, prefs: unknown = {}, extra: {
  grants?: unknown[];
  routes?: Record<string, () => Response>;
} = {}, entry = "/maps?site=s") {
  vi.spyOn(PlayerSession.prototype, "connect").mockImplementation(() => {});
  const cameras = Array.from({ length: 5 }, (_, n) => ({
    id: `c${n}`, tenant_id: "t", site_id: "s", server_id: "srv", remote_name: `c${n}`,
    display_name: `Camera ${n}`, enabled: true, zones: [], lpr: false, status: "online",
    fps: 5, group_ids: [], default_live_quality: "sub", description: "", location: "", tags: [],
    created_at: "", updated_at: "",
  }));
  const fetchSpy = vi.fn(stubApi({
    "/api/v1/me": () => json({ id: "u", username: "admin", tenant_id: "t", grants: [
      { permission: "maps.view", effect: "allow", scope_type: "platform" },
      { permission: "live.view", effect: "allow", scope_type: "platform" },
      ...(alarmsEnabled ? [{ permission: "alarms.view", effect: "allow", scope_type: "platform" }] : []),
      ...(extra.grants ?? []),
    ] }),
    "/api/v1/me/map-prefs": () => json(prefs),
    "/api/v1/maps/config": () => json({ provider: { id: "local", kind: "pmtiles", tiles: ["/tiles/base.pmtiles"],
      attribution: "local", max_zoom: 18, offline: true }, default_center: { lat: 0, lng: 0 }, default_zoom: 14 }),
    "/api/v1/maps/overview": () => json({ items: [{ id: "s", name: "Site", lat: 0, lng: 0,
      camera_count: 5, online_cameras: 5, offline_cameras: 0, degraded_cameras: 0, alarm_count: 0 }] }),
    "/api/v1/maps/sites/s/entities": () => json({ revision: "1", entities: cameras.map((camera, n) => ({
      id: camera.id, t: "camera", site: "s", srv: "srv", name: camera.display_name,
      pos: { kind: "geo", lat: 0, lng: n * 0.001 }, st: "online", alarms: n === 0 ? 3 : 0, rev: 1,
      cam: { bearing: 0, fov: 60, range: 100, type: "fixed", ptz: false, lpr: false },
    })) }),
    "/api/v1/alarms": () => json({ items: [{ id: "a", site_id: "s", camera_id: "c0", camera_name: "Entrance", status: "open" }] }),
    "/api/v1/cameras": () => json({ items: cameras }),
    "/api/v1/sites": () => json({ items: [] }),
    "/api/v1/servers": () => json({ items: [] }),
    "/api/v1/views": () => json({ items: [] }),
    ...(extra.routes ?? {}),
  }));
  vi.stubGlobal("fetch", fetchSpy);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const router = createRouter({ routeTree, context: { queryClient },
    history: createMemoryHistory({ initialEntries: [entry] }) });
  const view = render(<QueryClientProvider client={queryClient}><RouterProvider router={router} /></QueryClientProvider>);
  await screen.findByRole("button", { name: "Marker c0" });
  return { ...view, router, queryClient, fetchSpy };
}
describe("Maps camera interaction integration", () => {
  it("installs the perf overlay only with the ?perf param", async () => {
    await setup(false, {}, {}, "/maps?site=s&perf=1");
    const metrics = window.__openvmsMapMetrics;
    expect(metrics).toBeDefined();
    await waitFor(() => expect(metrics!.entitiesVisible).toBe(5));
    await waitFor(() => expect(metrics!.timeToFirstRenderMs).not.toBeNull());
  });

  it("keeps the perf sampler off in normal renders", async () => {
    vi.stubEnv("DEV", false);
    delete window.__openvmsMapMetrics;
    await setup();
    expect(window.__openvmsMapMetrics).toBeUndefined();
  });

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
    act(() => vi.advanceTimersByTime(150));
    expect(release).toHaveBeenCalledWith("c0", "sub");
  });



  it("keeps Pin interactive while crossing from a marker into its preview", async () => {
    await setup();
    vi.useFakeTimers();
    const marker = screen.getByRole("button", { name: "Marker c0" });
    fireEvent.mouseEnter(marker);
    act(() => vi.advanceTimersByTime(400));
    const pin = screen.getByRole("button", { name: "Pin preview" });
    fireEvent.mouseLeave(marker);
    fireEvent.mouseEnter(pin.closest("[data-testid='camera-hover-preview']") ?? pin);
    act(() => vi.advanceTimersByTime(200));
    expect(pin).toBeInTheDocument();
    fireEvent.click(pin);
    expect(screen.getByRole("button", { name: "Close preview" })).toBeInTheDocument();
  });

  it("cancels stale leave when another camera enters and releases on cleanup", async () => {
    const release = vi.spyOn(PlayerSessionManager.prototype, "release");
    const view = await setup();
    vi.useFakeTimers();
    fireEvent.mouseEnter(screen.getByRole("button", { name: "Marker c0" }));
    act(() => vi.advanceTimersByTime(400));
    fireEvent.mouseLeave(screen.getByRole("button", { name: "Marker c0" }));
    expect(screen.getByRole("button", { name: "Pin preview" })).toBeInTheDocument();
    fireEvent.mouseEnter(screen.getByRole("button", { name: "Marker c1" }));
    act(() => vi.advanceTimersByTime(400));
    expect(screen.getByAltText("Camera 1")).toBeInTheDocument();
    expect(release).toHaveBeenCalledWith("c0", "sub");
    fireEvent.mouseLeave(screen.getByRole("button", { name: "Marker c1" }));
    view.unmount();
    expect(release).toHaveBeenCalledWith("c1", "sub");
    act(() => vi.advanceTimersByTime(200));
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

it("integrates authorized alarms and grouped site health with focus off by default", async () => {
  await setup(true);
  expect(await screen.findByRole("region", { name: "Alarms" })).toHaveTextContent("Entrance");
  expect(screen.getByLabelText("Incident focus")).toHaveValue("none");
  fireEvent.click(screen.getByRole("button", { name: "Server outage" }));
  expect(await screen.findByText("Server srv offline")).toBeInTheDocument();
  expect(screen.getByRole("region", { name: "Site health" })).toHaveTextContent("5 cameras affected");
});

it("focuses only explicit current-site incidents, without selecting a camera, and respects manual navigation and reduced motion", async () => {
  await setup(true);
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: true })));
  act(() => canvasHarness.props?.onMapReady?.({ easeTo: canvasHarness.easeTo, once: () => {} } as never));
  const emit = (id: string, site = "s") => act(() => canvasHarness.props?.realtimeStore?.handleFrame({
    id, type: "alarm.updated", site_id: site, camera_id: "c0", data: { id, status: "open" },
  }));
  emit("default");
  expect(canvasHarness.easeTo).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText("Incident focus"), { target: { value: "current-site" } });
  emit("unrelated", "other");
  expect(canvasHarness.easeTo).not.toHaveBeenCalled();
  emit("focus");
  expect(canvasHarness.easeTo).toHaveBeenCalledWith({ center: [0, 0], duration: 0 });
  expect(screen.queryByRole("button", { name: "Close preview" })).not.toBeInTheDocument();
  canvasHarness.easeTo.mockClear();
  const now = Date.now();
  const clock = vi.spyOn(Date, "now").mockReturnValue(now + 11_000);
  fireEvent.pointerDown(screen.getByTestId("canvas"));
  emit("manual");
  expect(canvasHarness.easeTo).not.toHaveBeenCalled();
  clock.mockReturnValue(now + 26_000);
  emit("after-manual");
  expect(canvasHarness.easeTo).toHaveBeenCalledTimes(1);
});

it("replaces realtime state at the existing user and tenant identity boundary", async () => {
  const { queryClient } = await setup(true);
  fireEvent.click(screen.getByRole("button", { name: "Server outage" }));
  expect(await screen.findByText("Server srv offline")).toBeInTheDocument();
  const previous = canvasHarness.props?.realtimeStore;
  act(() => queryClient.setQueryData(["me"], { id: "other-user", tenant_id: "other-tenant", grants: [
    { permission: "maps.view", effect: "allow", scope_type: "platform" },
    { permission: "alarms.view", effect: "allow", scope_type: "platform" },
  ] }));
  await waitFor(() => expect(canvasHarness.props?.realtimeStore).not.toBe(previous));
  expect(screen.queryByText("Server srv offline")).not.toBeInTheDocument();
  expect(canvasHarness.props?.realtimeStore?.isServerOffline("srv")).toBe(false);
});

it("seeds camera counts from real Maps queries and keeps terminal lifecycle updates idempotent", async () => {
  await setup(true);
  await waitFor(() => expect(canvasHarness.props?.realtimeStore?.getCameraAlarms("c0")).toBe(3));
  act(() => {
    canvasHarness.props?.realtimeStore?.handleFrame({ type: "alarm.updated", camera_id: "c0", data: { id: "a", status: "resolved" } });
    canvasHarness.props?.realtimeStore?.handleFrame({ type: "alarm.updated", camera_id: "c0", data: { id: "a", status: "closed" } });
  });
  expect(canvasHarness.props?.cameras?.find(camera => camera.id === "c0")?.activeAlarms).toBe(2);
});
it("navigates from grouped health to an authorized camera and site", async () => {
  const { router } = await setup(true);
  fireEvent.click(screen.getByRole("button", { name: "Server outage" }));
  fireEvent.click(await screen.findByRole("button", { name: "View Camera 0" }));
  await waitFor(() => expect(router.state.location.search).toMatchObject({ camera: "c0" }));
  fireEvent.click(screen.getByRole("button", { name: "View site" }));
  await waitFor(() => expect(router.state.location.search).toMatchObject({ site: "s" }));
  expect(router.state.location.search.camera).toBeUndefined();
});

const putPrefsRequests = (fetchSpy: { mock: { calls: unknown[][] } }) =>
  fetchSpy.mock.calls
    .map(([input]) => input as Request)
    .filter((request) => request && typeof request === "object" && request.method === "PUT"
      && new URL(request.url).pathname === "/api/v1/me/map-prefs");

it("hydrates saved preferences, applies them to the canvas and persists later changes", async () => {
  const { fetchSpy } = await setup(false, {
    layers: { coverage: false, cameras: false, sites: false, events_alarm: false },
    filters: {},
    focus_mode: "current-site",
    hover_live: true,
  });

  await waitFor(() => expect(canvasHarness.props?.layerVisibility).toEqual({
    cameras: false,
    sites: false,
    coverage: false,
    alarmFx: false,
    detectionFx: true,
  }));
  expect(screen.getByLabelText("Incident focus")).toHaveValue("current-site");
  expect(screen.getByRole("checkbox", { name: "Live on hover" })).toBeChecked();
  // Hydration must not write back: it would clobber a newer blob saved by another tab.
  expect(putPrefsRequests(fetchSpy)).toHaveLength(0);

  fireEvent.click(screen.getByTitle("Filtros"));
  fireEvent.click(await screen.findByRole("checkbox", { name: "Offline" }));
  await waitFor(() => expect(canvasHarness.props?.cameras).toHaveLength(0));
  expect(screen.queryByRole("button", { name: "Marker c0" })).not.toBeInTheDocument();

  fireEvent.click(screen.getByTitle("Capas del mapa"));
  fireEvent.click(await screen.findByRole("checkbox", { name: "Conos FOV" }));
  // One PUT per change: first the filter, then the layer toggle.
  await waitFor(() => expect(putPrefsRequests(fetchSpy).length).toBeGreaterThanOrEqual(2));
  const puts = putPrefsRequests(fetchSpy);
  const body = JSON.parse(await puts[puts.length - 1]!.text()) as {
    layers: Record<string, boolean>;
    focus_mode: string;
    hover_live: boolean;
  };
  expect(body.layers).toMatchObject({ coverage: true, cameras: false, sites: false });
  expect(body.focus_mode).toBe("current-site");
  expect(body.hover_live).toBe(true);
});

const editGrants = [{ permission: "maps.edit", effect: "allow", scope_type: "platform" }];

const placementPuts = (fetchSpy: { mock: { calls: unknown[][] } }) =>
  fetchSpy.mock.calls
    .map(([input]) => input as Request)
    .filter((request) => request && typeof request === "object" && request.method === "PUT"
      && new URL(request.url).pathname.startsWith("/api/v1/maps/placements/"));

const unplacedRoute = () => json({ site_id: "s", cameras: [
  { id: "cu1", name: "Nueva cam", site_id: "s", status: "online" },
  { id: "cu2", name: "Otra cam", site_id: "s", status: "offline" },
] });

async function setupEditor(routes: Record<string, () => Response> = {}, extraGrants: unknown[] = []) {
  const view = await setup(false, {}, {
    grants: [...editGrants, ...extraGrants],
    routes: {
      "/api/v1/maps/unplaced": unplacedRoute,
      "/api/v1/maps/placements/camera/cu1": () => json({ id: "p-cu1", revision: 1 }),
      "/api/v1/maps/placements/camera/cu2": () => json({ id: "p-cu2", revision: 1 }),
      "/api/v1/maps/placements/camera/c1": () => json({ id: "p1", revision: 2 }),
      ...routes,
    },
  });
  fireEvent.click(screen.getByRole("tab", { name: "Editor" }));
  await screen.findByRole("region", { name: "Sin ubicar" });
  return view;
}

it("drops an armed camera on the map and saves the new placement without If-Match", async () => {
  const { fetchSpy } = await setupEditor();
  fireEvent.click(screen.getByRole("button", { name: "Nueva cam" }));
  act(() => canvasHarness.props?.onMapClick?.({ lng: -58.4, lat: -34.6 }));

  const save = await screen.findByRole("button", { name: "Guardar (1)" });
  fireEvent.click(save);
  await waitFor(() => expect(placementPuts(fetchSpy)).toHaveLength(1));

  const [request] = placementPuts(fetchSpy);
  expect(request!.headers.get("If-Match")).toBeNull();
  const body = JSON.parse(await request!.text()) as Record<string, unknown>;
  expect(body).toMatchObject({ site_id: "s", lat: -34.6, lng: -58.4, fov_deg: 70, range_m: 30 });
  await waitFor(() => expect(screen.queryByRole("button", { name: "Guardar (1)" })).not.toBeInTheDocument());
});

it("discards a staged placement through undo before anything is written", async () => {
  const { fetchSpy } = await setupEditor();
  fireEvent.click(screen.getByRole("button", { name: "Nueva cam" }));
  act(() => canvasHarness.props?.onMapClick?.({ lng: -58.4, lat: -34.6 }));
  await screen.findByRole("button", { name: "Guardar (1)" });

  fireEvent.click(screen.getByRole("button", { name: "Deshacer" }));
  expect(screen.queryByRole("button", { name: "Guardar (1)" })).not.toBeInTheDocument();
  expect(placementPuts(fetchSpy)).toHaveLength(0);
});

it("holds a stale save as a conflict and rebases it on the server revision", async () => {
  let calls = 0;
  const { fetchSpy } = await setupEditor({
    "/api/v1/maps/placements/camera/c1": () =>
      (++calls === 1 ? json({ code: "conflict", message: "revisión desactualizada" }, 409)
        : json({ id: "p1", revision: 2 })),
  });
  fireEvent.click(screen.getByRole("button", { name: "Marker c1" }));
  act(() => canvasHarness.props?.onMapClick?.({ lng: 0.5, lat: 0.25 }));

  fireEvent.click(await screen.findByRole("button", { name: "Guardar (1)" }));
  expect(await screen.findByText(/revisión desactualizada/)).toBeInTheDocument();
  expect(placementPuts(fetchSpy)[0]!.headers.get("If-Match")).toBe(`"1"`);

  // Rebase re-reads the site entities and adopts the revision the server is at.
  fireEvent.click(screen.getByRole("button", { name: "Rebase" }));
  await waitFor(() => expect(screen.queryByRole("button", { name: "Rebase" })).not.toBeInTheDocument());

  fireEvent.click(screen.getByRole("button", { name: "Guardar (1)" }));
  await waitFor(() => expect(placementPuts(fetchSpy)).toHaveLength(2));
  expect(placementPuts(fetchSpy)[1]!.headers.get("If-Match")).toBe(`"1"`);
  await waitFor(() => expect(screen.queryByRole("button", { name: "Guardar (1)" })).not.toBeInTheDocument());
});

it("moves a placed camera as a draft that keeps its If-Match revision", async () => {
  const { fetchSpy } = await setupEditor();
  fireEvent.click(screen.getByRole("button", { name: "Marker c1" }));
  act(() => canvasHarness.props?.onMapClick?.({ lng: 0.5, lat: 0.25 }));

  fireEvent.click(await screen.findByRole("button", { name: "Guardar (1)" }));
  await waitFor(() => expect(placementPuts(fetchSpy)).toHaveLength(1));
  expect(placementPuts(fetchSpy)[0]!.headers.get("If-Match")).toBe(`"1"`);
  const body = JSON.parse(await placementPuts(fetchSpy)[0]!.text()) as Record<string, unknown>;
  expect(body).toMatchObject({ lat: 0.25, lng: 0.5, fov_deg: 60, range_m: 100, bearing_deg: 0 });
});

it("bulk-places every unplaced camera at the site centre in one undo step", async () => {
  const { fetchSpy } = await setupEditor();
  fireEvent.click(screen.getByRole("button", { name: "Ubicar todas en el centro del sitio" }));
  fireEvent.click(await screen.findByRole("button", { name: "Guardar (2)" }));

  await waitFor(() => expect(placementPuts(fetchSpy)).toHaveLength(2));
  const bodies = await Promise.all(
    placementPuts(fetchSpy).map(async (request) => JSON.parse(await request.text()) as Record<string, unknown>),
  );
  expect(bodies.every((body) => body.lat === 0 && body.lng === 0)).toBe(true);
});

// --- CSV import (M-B8) ------------------------------------------------------
const importRequests = (fetchSpy: { mock: { calls: unknown[][] } }) =>
  fetchSpy.mock.calls
    .map(([input]) => input as Request)
    .filter((request) => request && typeof request === "object" && request.method === "POST"
      && new URL(request.url).pathname === "/api/v1/maps/placements/import");

it("imports placements from CSV and refreshes the unplaced tray and the site", async () => {
  const { fetchSpy, queryClient } = await setupEditor(
    {
      "POST /api/v1/maps/placements/import": () =>
        json({ dry_run: false, rows: 1, upserted: 1, errors: [] }),
    },
    [{ permission: "maps.edit_device", effect: "allow", scope_type: "platform" }],
  );
  const invalidate = vi.spyOn(queryClient, "invalidateQueries");

  fireEvent.click(screen.getByRole("button", { name: "Importar CSV" }));
  fireEvent.change(await screen.findByRole("textbox"), {
    target: { value: "camera,lat,lng\nNueva cam,-34.6,-58.4" },
  });
  fireEvent.click(screen.getByRole("button", { name: /^Importar$/ }));

  await waitFor(() => expect(importRequests(fetchSpy)).toHaveLength(1));
  const body = JSON.parse(await importRequests(fetchSpy)[0]!.text()) as Record<string, unknown>;
  expect(body).toMatchObject({ site_id: "s", dry_run: false });
  await waitFor(() => {
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["maps", "unplaced", "s"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["maps", "sites", "s", "entities"] });
  });
  // The applied import closes the form and keeps the tray usable.
  await waitFor(() => expect(screen.queryByRole("textbox")).not.toBeInTheDocument());
});

it("keeps the lists untouched while the import is still a dry run", async () => {
  const { fetchSpy, queryClient } = await setupEditor(
    {
      "POST /api/v1/maps/placements/import": () =>
        json({ dry_run: true, rows: 1, upserted: 0, errors: [] }),
    },
    [{ permission: "maps.edit_device", effect: "allow", scope_type: "platform" }],
  );  const invalidate = vi.spyOn(queryClient, "invalidateQueries");

  fireEvent.click(screen.getByRole("button", { name: "Importar CSV" }));
  fireEvent.change(await screen.findByRole("textbox"), {
    target: { value: "camera,lat,lng\nNueva cam,-34.6,-58.4" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Validar" }));

  await screen.findByText(/1 filas/);
  expect(importRequests(fetchSpy)).toHaveLength(1);
  expect(invalidate).not.toHaveBeenCalled();
});

it("shows no import entry point without maps.edit_device", async () => {
  await setupEditor();
  expect(screen.queryByRole("button", { name: "Importar CSV" })).not.toBeInTheDocument();
});

// --- Monitoring center (M-W11) ---------------------------------------------
const twoSiteOverview = () => json({ items: [
  { id: "s", name: "Site", lat: 0, lng: 0, camera_count: 5, online_cameras: 5, offline_cameras: 0, degraded_cameras: 0, alarm_count: 0 },
  { id: "s2", name: "Site 2", lat: -34.1, lng: -58.9, camera_count: 2, online_cameras: 2, offline_cameras: 0, degraded_cameras: 0, alarm_count: 0 },
] });

it("re-centers an open map when the selected site changes", async () => {
  const { router } = await setup(false, {}, {
    routes: { "/api/v1/maps/overview": twoSiteOverview },
  });
  act(() => canvasHarness.props?.onMapReady?.({ easeTo: canvasHarness.easeTo, once: () => {} } as never));
  canvasHarness.easeTo.mockClear();

  await act(async () => {
    await router.navigate({ to: "/maps", search: { site: "s2" } });
  });

  await waitFor(() =>
    expect(canvasHarness.easeTo).toHaveBeenCalledWith(expect.objectContaining({ center: [-58.9, -34.1] })));
});

it("pins the monitoring center from the current view in edit mode", async () => {
  const { fetchSpy, queryClient } = await setupEditor({
    "PATCH /api/v1/sites/s/geo": () => json({ id: "s", name: "Site", lat: -34.6, lng: -58.4, default_zoom: 14 }),
  });
  const invalidate = vi.spyOn(queryClient, "invalidateQueries");
  // The operator framed the monitoring view: the fake map reports that viewport.
  act(() => canvasHarness.props?.onMapReady?.({
    easeTo: vi.fn(),
    once: () => {},
    getCenter: () => ({ lng: -58.4, lat: -34.6 }),
    getZoom: () => 14,
  } as never));

  fireEvent.click(screen.getByRole("button", { name: "Fijar centro de monitoreo aquí" }));

  const geoPatch = () =>
    fetchSpy.mock.calls.map(([input]) => input as Request)
      .find(r => r.method === "PATCH" && new URL(r.url).pathname === "/api/v1/sites/s/geo");
  await waitFor(() => expect(geoPatch()).toBeTruthy());
  expect(JSON.parse(await geoPatch()!.text())).toMatchObject({ lat: -34.6, lng: -58.4, default_zoom: 14 });
  await waitFor(() => {
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["maps", "overview"] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ["sites"] });
  });
});

it("offers no monitoring-center pin outside edit mode", async () => {
  await setup();
  expect(screen.queryByRole("button", { name: "Fijar centro de monitoreo aquí" })).not.toBeInTheDocument();
});

// --- Zone editor (M-W9) ------------------------------------------------------------
const zoneGrants = [
  ...editGrants,
  { permission: "maps.create_zone", effect: "allow", scope_type: "platform" },
];

const zoneRequests = (fetchSpy: { mock: { calls: unknown[][] } }, method: string) =>
  fetchSpy.mock.calls
    .map(([input]) => input as Request)
    .filter((request) => request && typeof request === "object" && request.method === method
      && new URL(request.url).pathname.includes("/zones"));

const zoneFixture = (over: Record<string, unknown> = {}): Record<string, unknown> => ({
  id: "z1",
  site_id: "s",
  name: "Zona Vieja",
  kind: "custom",
  geometry: {
    type: "Polygon",
    coordinates: [[[-58.4, -34.6], [-58.39, -34.6], [-58.395, -34.61], [-58.4, -34.6]]],
  },
  ...over,
});

it("draws a zone point by point and writes the closed ring the backend expects", async () => {
  const zones: Record<string, unknown>[] = [];
  const { fetchSpy } = await setup(false, {}, {
    grants: zoneGrants,
    routes: {
      "/api/v1/maps/sites/s/zones": () => json({ site_id: "s", zones }),
      "POST /api/v1/maps/sites/s/zones": () => {
        zones.push(zoneFixture({ id: "z9", name: "Zona Norte", kind: "security" }));
        return json(zones[zones.length - 1], 201);
      },
    },
  });

  fireEvent.click(screen.getByRole("tab", { name: "Editor" }));
  fireEvent.click(await screen.findByRole("button", { name: "Nueva zona" }));

  act(() => canvasHarness.props?.onMapClick?.({ lng: -58.4, lat: -34.6 }));
  act(() => canvasHarness.props?.onMapClick?.({ lng: -58.39, lat: -34.6 }));
  act(() => canvasHarness.props?.onMapClick?.({ lng: -58.395, lat: -34.61 }));
  expect(screen.getByText("Puntos: 3")).toBeInTheDocument();

  fireEvent.click(screen.getByRole("button", { name: "Cerrar polígono" }));
  fireEvent.change(screen.getByLabelText("Nombre de la zona"), { target: { value: "Zona Norte" } });
  fireEvent.click(screen.getByRole("button", { name: "Guardar" }));

  await waitFor(() => expect(zoneRequests(fetchSpy, "POST")).toHaveLength(1));
  const body = JSON.parse(await zoneRequests(fetchSpy, "POST")[0]!.text()) as {
    name: string;
    kind: string;
    geometry: { coordinates: number[][][] };
  };
  expect(body).toMatchObject({ name: "Zona Norte", kind: "security" });
  const ring = body.geometry.coordinates[0]!;
  expect(ring).toHaveLength(4);
  expect(ring[0]).toEqual(ring[ring.length - 1]);

  // The refetch after the write carries the saved zone back onto the panel.
  expect(await screen.findByText("Zona Norte")).toBeInTheDocument();
});

it("updates an existing zone and deletes it afterwards", async () => {
  const zones: Record<string, unknown>[] = [zoneFixture()];
  const { fetchSpy } = await setup(false, {}, {
    grants: zoneGrants,
    routes: {
      "/api/v1/maps/sites/s/zones": () => json({ site_id: "s", zones }),
      "PATCH /api/v1/maps/zones/z1": () => {
        zones[0] = { ...zones[0], name: "Zona Nueva" };
        return json(zones[0]);
      },
      "DELETE /api/v1/maps/zones/z1": () => {
        zones.splice(0);
        return new Response(null, { status: 204 });
      },
    },
  });

  fireEvent.click(screen.getByRole("tab", { name: "Editor" }));
  fireEvent.click(await screen.findByRole("button", { name: "Editar" }));

  const name = await screen.findByLabelText("Nombre de la zona");
  expect(name).toHaveValue("Zona Vieja");
  fireEvent.change(name, { target: { value: "Zona Nueva" } });
  fireEvent.click(screen.getByRole("button", { name: "Guardar" }));

  await waitFor(() => expect(zoneRequests(fetchSpy, "PATCH")).toHaveLength(1));
  const patch = JSON.parse(await zoneRequests(fetchSpy, "PATCH")[0]!.text()) as Record<string, unknown>;
  expect(patch).toMatchObject({ name: "Zona Nueva", kind: "custom" });
  await screen.findByText("Zona Nueva");

  fireEvent.click(screen.getByRole("button", { name: "Eliminar" }));
  await waitFor(() => expect(zoneRequests(fetchSpy, "DELETE")).toHaveLength(1));
  await waitFor(() => expect(screen.queryByText("Zona Nueva")).not.toBeInTheDocument());
});

it("surfaces the server's refusal without losing the zone draft", async () => {
  const zones: Record<string, unknown>[] = [zoneFixture()];
  const { fetchSpy } = await setup(false, {}, {
    grants: zoneGrants,
    routes: {
      "/api/v1/maps/sites/s/zones": () => json({ site_id: "s", zones }),
      "PATCH /api/v1/maps/zones/z1": () =>
        json({ code: "bad_request", message: "El polígono se cruza consigo mismo." }, 400),
    },
  });

  fireEvent.click(screen.getByRole("tab", { name: "Editor" }));
  fireEvent.click(await screen.findByRole("button", { name: "Editar" }));
  fireEvent.change(await screen.findByLabelText("Nombre de la zona"), { target: { value: "Zona Nueva" } });
  fireEvent.click(screen.getByRole("button", { name: "Guardar" }));

  expect(await screen.findByRole("alert")).toHaveTextContent("El polígono se cruza consigo mismo.");
  // The operator's draft stays on screen so nothing has to be drawn twice.
  expect(screen.getByLabelText("Nombre de la zona")).toHaveValue("Zona Nueva");
  expect(zoneRequests(fetchSpy, "PATCH")).toHaveLength(1);
});
