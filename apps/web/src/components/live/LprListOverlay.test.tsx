import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { OVERLAY_FALLBACK_ROWS, OVERLAY_FETCH_LIMIT, OVERLAY_ROW_GAP_PX, OVERLAY_ROW_PX, rowsThatFit } from "@/lib/lprOverlay";
import { LprListOverlay } from "./LprListOverlay";

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

describe("LprListOverlay", () => {
  it("asks only for the plates of its camera", async () => {
    renderPage(() => <LprListOverlay cameraId="cam-1" />);
    await waitFor(() => expect(requests.length).toBeGreaterThan(0));
    expect(requests[0]?.searchParams.getAll("camera_id")).toEqual(["cam-1"]);
  });

  it("lists the newest read first with its plate and time", async () => {
    reads = [
      read("r1", "AB123CD", "2026-10-21T10:00:00Z"),
      read("r2", "AE526KV", "2026-10-21T10:00:30Z"),
    ];
    renderPage(() => <LprListOverlay cameraId="cam-1" />);
    const list = await screen.findByRole("list", { name: /Lecturas de patentes/i });
    const items = within(list).getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent("AE526KV");
    expect(items[1]).toHaveTextContent("AB123CD");
    expect(items[0]).toHaveAttribute("data-latest", "true");
    expect(items[1]).not.toHaveAttribute("data-latest");
  });

  it("shows a waiting state when there are no reads", async () => {
    renderPage(() => <LprListOverlay cameraId="cam-1" />);
    expect(await screen.findByText(/Esperando patentes/i)).toBeInTheDocument();
  });

  it("does not intercept pointer events over the video", async () => {
    reads = [read("r1", "AB123CD", "2026-10-21T10:00:00Z")];
    renderPage(() => <LprListOverlay cameraId="cam-1" />);
    const overlay = await screen.findByTestId("lpr-list-overlay");
    expect(overlay).toHaveClass("pointer-events-none");
  });

  it("can be collapsed with the toggle and remembers the choice", async () => {
    reads = [read("r1", "AB123CD", "2026-10-21T10:00:00Z")];
    const first = renderPage(() => <LprListOverlay cameraId="cam-1" />);
    expect(await screen.findByRole("list", { name: /Lecturas de patentes/i })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Ocultar lista de patentes/i }));
    expect(screen.queryByRole("list", { name: /Lecturas de patentes/i })).not.toBeInTheDocument();
    first.unmount();

    renderPage(() => <LprListOverlay cameraId="cam-1" />);
    expect(await screen.findByRole("button", { name: /Mostrar lista de patentes/i })).toBeInTheDocument();
    expect(screen.queryByRole("list", { name: /Lecturas de patentes/i })).not.toBeInTheDocument();
  });

  it("renders nothing when disabled by the caller", () => {
    renderPage(() => <LprListOverlay cameraId="cam-1" enabled={false} />);
    expect(screen.queryByTestId("lpr-list-overlay")).not.toBeInTheDocument();
    expect(requests).toHaveLength(0);
  });
});

describe("LprListOverlay rows follow the available height", () => {
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
    renderPage(() => <LprListOverlay cameraId="cam-1" />);
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
    renderPage(() => <LprListOverlay cameraId="cam-1" />);
    const list = await screen.findByRole("list", { name: /Lecturas de patentes/i });
    resize(2000);
    await waitFor(() => expect(within(list).getAllByRole("listitem")).toHaveLength(3));
  });

  it("asks for enough reads to fill a tall list", async () => {
    renderPage(() => <LprListOverlay cameraId="cam-1" />);
    await waitFor(() => expect(requests.length).toBeGreaterThan(0));
    expect(Number(requests[0]?.searchParams.get("limit"))).toBe(OVERLAY_FETCH_LIMIT);
  });

  it("keeps the default count when the height cannot be measured", async () => {
    vi.unstubAllGlobals();
    vi.stubGlobal("fetch", vi.fn(async (input: Request) => stubApi({ "/api/v1/lpr/reads": () => json({ items: many(10) }) })(input)));
    renderPage(() => <LprListOverlay cameraId="cam-1" />);
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
