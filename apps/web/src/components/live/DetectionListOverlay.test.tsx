import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { OVERLAY_FALLBACK_ROWS, OVERLAY_FETCH_LIMIT, OVERLAY_ROW_GAP_PX, OVERLAY_ROW_PX, rowsThatFit } from "@/lib/detectionOverlay";
import { DetectionListOverlay } from "./DetectionListOverlay";

const read = (id: string, plate: string, seen_at: string, camera_id = "cam-1") => ({
  id, site_id: "s", site_name: "Sitio", server_id: "sv", server_name: "Srv", camera_id, camera_name: "Acceso Norte",
  plate, plate_normalized: plate, label: "car", zones: ["entrada"], seen_at,
});

let reads: unknown[];
let requests: URL[];

beforeEach(() => {
  reads = [];
  requests = [];
  localStorage.clear();
  const routes = stubApi({ "/api/v1/lpr/reads": () => json({ items: reads }) });
  vi.stubGlobal("fetch", vi.fn(async (input: Request) => {
    requests.push(new URL(input.url));
    return routes(input);
  }));
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("DetectionListOverlay", () => {
  it("asks only for the plates of its camera", async () => {
    renderPage(() => <DetectionListOverlay cameraId="cam-1" />);
    await waitFor(() => expect(requests.length).toBeGreaterThan(0));
    expect(requests[0]?.searchParams.getAll("camera_id")).toEqual(["cam-1"]);
  });

  it("lists the newest read first with its plate and time", async () => {
    reads = [
      read("r1", "AB123CD", "2026-10-21T10:00:00Z"),
      read("r2", "AE526KV", "2026-10-21T10:00:30Z"),
    ];
    renderPage(() => <DetectionListOverlay cameraId="cam-1" />);
    const list = await screen.findByRole("list", { name: /Lecturas de patentes/i });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent("AE526KV");
    expect(items[1]).toHaveTextContent("AB123CD");
    expect(items[0]).toHaveAttribute("data-latest", "true");
    expect(items[1]).not.toHaveAttribute("data-latest");
  });

  it("renders nothing while the camera has no plate reads", async () => {
    renderPage(() => <DetectionListOverlay cameraId="cam-1" />);
    await waitFor(() => expect(requests.length).toBeGreaterThan(0));
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByTestId("detection-overlay")).not.toBeInTheDocument();
    expect(screen.queryByText(/Esperando patentes/i)).not.toBeInTheDocument();
  });

  it("appears once the camera gets its first plate read", async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(<QueryClientProvider client={queryClient}><DetectionListOverlay cameraId="cam-1" /></QueryClientProvider>);
    await waitFor(() => expect(requests.length).toBeGreaterThan(0));
    expect(screen.queryByTestId("detection-overlay")).not.toBeInTheDocument();
    reads = [read("r1", "AB123CD", "2026-10-21T10:00:00Z")];
    await act(async () => { await queryClient.invalidateQueries({ queryKey: ["plates"] }); });
    expect(await screen.findByTestId("detection-overlay")).toBeInTheDocument();
  });

  it("does not intercept pointer events over the video", async () => {
    reads = [read("r1", "AB123CD", "2026-10-21T10:00:00Z")];
    renderPage(() => <DetectionListOverlay cameraId="cam-1" />);
    const overlay = await screen.findByTestId("detection-overlay");
    expect(overlay).toHaveClass("pointer-events-none");
  });

  it("can be collapsed with the toggle and remembers the choice", async () => {
    reads = [read("r1", "AB123CD", "2026-10-21T10:00:00Z")];
    const first = renderPage(() => <DetectionListOverlay cameraId="cam-1" />);
    expect(await screen.findByRole("list", { name: /Lecturas de patentes/i })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Ocultar lista de patentes/i }));
    expect(screen.queryByRole("list", { name: /Lecturas de patentes/i })).not.toBeInTheDocument();
    first.unmount();

    renderPage(() => <DetectionListOverlay cameraId="cam-1" />);
    expect(await screen.findByRole("button", { name: /Mostrar lista de patentes/i })).toBeInTheDocument();
    expect(screen.queryByRole("list", { name: /Lecturas de patentes/i })).not.toBeInTheDocument();
  });

  it("renders nothing when disabled by the caller", () => {
    renderPage(() => <DetectionListOverlay cameraId="cam-1" enabled={false} />);
    expect(screen.queryByTestId("detection-overlay")).not.toBeInTheDocument();
    expect(requests).toHaveLength(0);
  });
});

describe("DetectionListOverlay rows follow the available height", () => {
  type Observer = { cb: ResizeObserverCallback };
  let observers: Observer[];
  const resize = (height: number) =>
    act(() => {
      for (const o of observers) o.cb([{ contentRect: { height } } as ResizeObserverEntry], {} as ResizeObserver);
    });

  const many = (n: number) =>
    Array.from({ length: n }, (_, i) => read(`r${i}`, `AB${String(100 + i)}CD`, `2026-10-21T10:${String(i).padStart(2, "0")}:00Z`));

  beforeEach(() => {
    observers = [];
    vi.stubGlobal(
      "ResizeObserver",
      class {
        constructor(cb: ResizeObserverCallback) {
          observers.push({ cb });
        }
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
  });

  it("shows more plates in a tall list and fewer in a short one", async () => {
    reads = many(20);
    renderPage(() => <DetectionListOverlay cameraId="cam-1" />);
    const list = await screen.findByRole("list", { name: /Lecturas de patentes/i });
    resize(700);
    await waitFor(() => expect(within(list).getAllByRole("listitem")).toHaveLength(rowsThatFit(700)));
    expect(rowsThatFit(700)).toBeGreaterThan(OVERLAY_FALLBACK_ROWS);
    resize(120);
    await waitFor(() => expect(within(list).getAllByRole("listitem")).toHaveLength(rowsThatFit(120)));
    expect(rowsThatFit(120)).toBeLessThan(OVERLAY_FALLBACK_ROWS);
  });

  it("never shows more plates than it has", async () => {
    reads = many(3);
    renderPage(() => <DetectionListOverlay cameraId="cam-1" />);
    const list = await screen.findByRole("list", { name: /Lecturas de patentes/i });
    resize(2000);
    await waitFor(() => expect(within(list).getAllByRole("listitem")).toHaveLength(3));
  });

  it("asks for enough reads to fill a tall list", async () => {
    renderPage(() => <DetectionListOverlay cameraId="cam-1" />);
    await waitFor(() => expect(requests.length).toBeGreaterThan(0));
    expect(Number(requests[0]?.searchParams.get("limit"))).toBe(OVERLAY_FETCH_LIMIT);
  });

  it("keeps the default count when the height cannot be measured", async () => {
    vi.unstubAllGlobals();
    vi.stubGlobal("fetch", vi.fn(async (input: Request) => stubApi({ "/api/v1/lpr/reads": () => json({ items: many(10) }) })(input)));
    renderPage(() => <DetectionListOverlay cameraId="cam-1" />);
    const list = await screen.findByRole("list", { name: /Lecturas de patentes/i });
    expect(within(list).getAllByRole("listitem")).toHaveLength(OVERLAY_FALLBACK_ROWS);
  });
});

describe("rowsThatFit", () => {
  it("fits whole rows into the height and never fewer than one", () => {
    expect(rowsThatFit(OVERLAY_ROW_PX)).toBe(1);
    expect(rowsThatFit(1)).toBe(1);
    expect(rowsThatFit(2 * OVERLAY_ROW_PX + OVERLAY_ROW_GAP_PX)).toBe(2);
    expect(rowsThatFit(2 * OVERLAY_ROW_PX + OVERLAY_ROW_GAP_PX - 1)).toBe(1);
  });
  it("is capped by what is fetched and falls back when unmeasured", () => {
    expect(rowsThatFit(100_000)).toBe(OVERLAY_FETCH_LIMIT);
    expect(rowsThatFit(0)).toBe(OVERLAY_FALLBACK_ROWS);
    expect(rowsThatFit(Number.NaN)).toBe(OVERLAY_FALLBACK_ROWS);
  });
});

describe("DetectionListOverlay faces and tabs", () => {
  const faceEvent = (id: string, name: string, camera_id: string, start_time: string) => ({
    id, tenant_id: "t1", site_id: "s", site_name: "Sitio", server_id: "sv", server_name: "Srv", camera_id, camera_name: "Hall",
    remote_id: id, severity: "detection", labels: ["face"], sub_labels: name ? [name] : [], zones: ["recepcion"], plates: [],
    start_time, reviewed: false, has_thumbnail: true, has_snapshot: false, has_preview: false, tracks: [],
  });
  const me = (perms: string[]) => ({
    id: "u1", username: "op", display_name: "Op", role: "admin", tenant_id: "t1",
    grants: perms.map((permission) => ({ permission, effect: "allow", scope: { kind: "global" } })),
  });

  let faceEvents: unknown[];
  let perms: string[];
  let eventRequests: URL[];

  beforeEach(() => {
    faceEvents = [];
    perms = ["events.view", "lpr.view"];
    eventRequests = [];
    const routes = stubApi({
      "/api/v1/me": () => json(me(perms)),
      "/api/v1/lpr/reads": () => json({ items: reads }),
      "/api/v1/events": () => json({ items: faceEvents }),
    });
    vi.stubGlobal("fetch", vi.fn(async (input: Request) => {
      const url = new URL(input.url);
      requests.push(url);
      if (url.pathname === "/api/v1/events") eventRequests.push(url);
      return routes(input);
    }));
  });

  it("lists the face detections of its own camera with the person's name", async () => {
    faceEvents = [
      faceEvent("e1", "Ana Pérez", "cam-1", "2026-10-21T10:00:30Z"),
      faceEvent("e2", "", "cam-1", "2026-10-21T10:00:00Z"),
      faceEvent("e3", "Otra Persona", "cam-2", "2026-10-21T10:01:00Z"),
    ];
    renderPage(() => <DetectionListOverlay cameraId="cam-1" lpr={false} faces />);
    const list = await screen.findByRole("list", { name: /Detecciones faciales/i });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent("Ana Pérez");
    expect(items[1]).toHaveTextContent("Desconocido");
    expect(screen.queryByText("Otra Persona")).not.toBeInTheDocument();
    expect(eventRequests[0]?.searchParams.getAll("label")).toEqual(["face"]);
  });

  it("renders nothing for a camera without face detections", async () => {
    faceEvents = [faceEvent("e3", "Otra Persona", "cam-2", "2026-10-21T10:01:00Z")];
    renderPage(() => <DetectionListOverlay cameraId="cam-1" lpr={false} faces />);
    await waitFor(() => expect(eventRequests.length).toBeGreaterThan(0));
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByTestId("detection-overlay")).not.toBeInTheDocument();
  });

  it("does not ask for faces without the events permission or when the caller does not want them", async () => {
    perms = ["lpr.view"];
    faceEvents = [faceEvent("e1", "Ana Pérez", "cam-1", "2026-10-21T10:00:30Z")];
    const first = renderPage(() => <DetectionListOverlay cameraId="cam-1" lpr={false} faces />);
    await new Promise((r) => setTimeout(r, 60));
    expect(eventRequests).toHaveLength(0);
    expect(screen.queryByTestId("detection-overlay")).not.toBeInTheDocument();
    first.unmount();

    perms = ["events.view"];
    renderPage(() => <DetectionListOverlay cameraId="cam-1" lpr={false} faces={false} />);
    await new Promise((r) => setTimeout(r, 60));
    expect(eventRequests).toHaveLength(0);
  });

  it("asks for the latest faces once for every camera on screen, not once per tile", async () => {
    faceEvents = [faceEvent("e1", "Ana Pérez", "cam-1", "2026-10-21T10:00:30Z"), faceEvent("e2", "Luis", "cam-2", "2026-10-21T10:00:00Z")];
    renderPage(() => (
      <>
        <DetectionListOverlay cameraId="cam-1" lpr={false} faces />
        <DetectionListOverlay cameraId="cam-2" lpr={false} faces />
        <DetectionListOverlay cameraId="cam-3" lpr={false} faces />
      </>
    ));
    await waitFor(() => expect(screen.getAllByTestId("detection-overlay")).toHaveLength(2));
    expect(eventRequests).toHaveLength(1);
  });

  it("offers tabs when the camera has plates and faces, plates first", async () => {
    reads = [read("r1", "AB123CD", "2026-10-21T10:00:00Z")];
    faceEvents = [faceEvent("e1", "Ana Pérez", "cam-1", "2026-10-21T10:05:00Z")];
    renderPage(() => <DetectionListOverlay cameraId="cam-1" lpr faces />);
    const tabs = await screen.findByRole("tablist", { name: /Tipo de detección/i });
    const [lprTab, faceTab] = within(tabs).getAllByRole("tab");
    expect(lprTab).toHaveTextContent("LPR");
    expect(faceTab).toHaveTextContent("Rostros");
    expect(lprTab).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("list", { name: /Lecturas de patentes/i })).toHaveTextContent("AB123CD");
    expect(screen.queryByRole("list", { name: /Detecciones faciales/i })).not.toBeInTheDocument();

    fireEvent.click(faceTab!);
    expect(faceTab).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("list", { name: /Detecciones faciales/i })).toHaveTextContent("Ana Pérez");
    expect(screen.queryByRole("list", { name: /Lecturas de patentes/i })).not.toBeInTheDocument();

    fireEvent.click(lprTab!);
    expect(screen.getByRole("list", { name: /Lecturas de patentes/i })).toBeInTheDocument();
  });

  it("has no tabs when the camera only has one kind of detection", async () => {
    reads = [read("r1", "AB123CD", "2026-10-21T10:00:00Z")];
    renderPage(() => <DetectionListOverlay cameraId="cam-1" lpr faces />);
    await screen.findByRole("list", { name: /Lecturas de patentes/i });
    expect(screen.queryByRole("tablist")).not.toBeInTheDocument();
  });

  it("collapses both lists with one toggle", async () => {
    reads = [read("r1", "AB123CD", "2026-10-21T10:00:00Z")];
    faceEvents = [faceEvent("e1", "Ana Pérez", "cam-1", "2026-10-21T10:05:00Z")];
    renderPage(() => <DetectionListOverlay cameraId="cam-1" lpr faces />);
    await screen.findByRole("tablist");
    fireEvent.click(screen.getByRole("button", { name: /Ocultar lista/i }));
    expect(screen.queryByRole("tablist")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Mostrar lista/i })).toBeInTheDocument();
  });
});
