import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { AppShell, TopBarActionsSlot } from "@/components/AppShell";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { liveSelectionKey, parseSelection, serializeSelection } from "@/lib/liveGrid";
import { json, renderPage, stubApi } from "@/test-utils";
import { FEATURES_OVERRIDE_KEY } from "@/lib/features";
import { PlayerSessionProvider } from "@/lib/live/PlayerSessionProvider";
import { playerMetrics } from "@/lib/live/playerMetrics";
import { Live } from "./Live";

afterEach(() => vi.unstubAllGlobals());
beforeEach(() => {
  localStorage.clear();
  localStorage.setItem("openvms.live.sidebar.pinned", "1");
  FakeSocket.created = 0;
});

/** FakeSocket/FakeMediaSource let MsePlayer mount without a real browser MSE stack. */
class FakeSocket {
  static created = 0;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 0;
  binaryType = "blob";
  constructor(public url: string) {
    FakeSocket.created += 1;
  }
  send() {}
  close() {}
}
class FakeMediaSource {
  static isTypeSupported() {
    return true;
  }
  addEventListener() {}
}
function stubBrowserAPIs() {
  vi.stubGlobal("WebSocket", FakeSocket as unknown as typeof WebSocket);
  vi.stubGlobal("MediaSource", FakeMediaSource as unknown as typeof MediaSource);
}

function meResponse(grants: { permission: string; effect: "allow" | "deny"; scope_type: string }[] = [
  { permission: "live.view", effect: "allow", scope_type: "platform" },
]) {
  return json({
    id: "u1",
    username: "operator",
    display_name: "Operator",
    mfa_enabled: false,
    must_change_password: false,
    auth_method: "session",
    tenant_id: "t1",
    grants,
  });
}

const camera = (id: string, name: string, extra: object = {}) => ({
  id,
  tenant_id: "t1",
  site_id: "s1",
  server_id: "srv1",
  remote_name: name,
  display_name: name,
  enabled: true,
  zones: [],
  lpr: false,
  status: "online",
  fps: 5,
  group_ids: [],
  default_live_quality: "sub",
  description: "",
  location: "",
  tags: [],
  created_at: "",
  updated_at: "",
  ...extra,
});

const emptyCatalogs = {
  "/api/v1/sites": () => json({ items: [] }),
  "/api/v1/servers": () => json({ items: [] }),
  "/api/v1/views": () => json({ items: [] }),
};

describe("Live", () => {
  it("marks the selected tile with aria-current and moves it on click", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "North")] }),
      ...emptyCatalogs,
    })));
    renderPage(Live);
    const first = await screen.findByLabelText("Cuadro 1");
    const second = screen.getByLabelText("Cuadro 2");
    expect(first).toHaveAttribute("aria-current", "true");
    expect(second).not.toHaveAttribute("aria-current");
    fireEvent.click(second);
    expect(second).toHaveAttribute("aria-current", "true");
    expect(first).not.toHaveAttribute("aria-current");
  });

  it("keeps video tiles square, selected or not, and marks selection with an outline", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "North")] }),
      ...emptyCatalogs,
    })));
    renderPage(Live);
    const first = await screen.findByLabelText("Cuadro 1");
    const second = screen.getByLabelText("Cuadro 2");
    for (const tile of [first, second]) {
      expect(tile.className).not.toMatch(/rounded/);
      expect(tile.className).not.toMatch(/border-radius/);
    }
    expect(first.className).toContain("outline-primary");
    expect(first.className).toContain("outline-offset");
  });

  it("keeps camera overlay controls above video without isolation trapping and stretches video to frame", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "North")] }),
      ...emptyCatalogs,
    })));
    renderPage(Live);
    const tile = await screen.findByLabelText("Cuadro 1");
    expect(tile.className).not.toContain("isolate");
    expect(tile.className).not.toContain("contain:paint");

    const camBtn = await screen.findByRole("button", { name: /North/ });
    fireEvent.click(camBtn);

    const nameEl = await within(tile).findByText("North");
    expect(nameEl).toBeInTheDocument();
    const overlay = nameEl.closest(".absolute");
    expect(overlay?.className).toContain("z-[3]");

    const video = tile.querySelector("video");
    expect(video?.className).toContain("object-fill");
  });

  it("consumes an authorized camera handoff while preserving the saved selection", async () => {
    stubBrowserAPIs();
    localStorage.setItem(liveSelectionKey("t1", "u1"), serializeSelection(2, [
      { camera_id: "cam-1", quality: "main" }, null, null, null,
    ]));
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "North"), camera("cam-2", "East")] }),
      ...emptyCatalogs,
    })));
    const { router } = renderPage(Live, "/?camera=cam-2");
    await waitFor(() => {
      const saved = parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-1", "cam-2"]));
      expect(saved?.tiles.slice(0, 2)).toEqual([
        { camera_id: "cam-1", quality: "main" }, { camera_id: "cam-2", quality: "sub" },
      ]);
    });
    await waitFor(() => expect(router.state.location.search).not.toHaveProperty("camera"));
  });

  it("ignores an unauthorized camera handoff", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "North")] }),
      ...emptyCatalogs,
    })));
    renderPage(Live, "/?camera=hidden");
    await screen.findByRole("button", { name: /North/ });
    await waitFor(() => expect(localStorage.getItem(liveSelectionKey("t1", "u1"))).not.toBeNull());
    expect(parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["hidden"]))?.tiles.every(t => t === null)).toBe(true);
  });

  it("places the camera tree in the shell context sidebar without remounting live media", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(stubApi({
        "/api/v1/me": meResponse,
        "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
        "/api/v1/sites": () => json({ items: [{ id: "s1", tenant_id: "t1", name: "Campus" }] }),
        "/api/v1/servers": () => json({ items: [{ id: "srv1", tenant_id: "t1", site_id: "s1", name: "Frigate A", status: "online" }] }),
        "/api/v1/views": () => json({ items: [] }),
      })),
    );
    const LiveWithShell = () => (
      <AppShell primaryNav={<nav aria-label="Primary navigation" />} contextSidebar={<div id="live-context-sidebar" />}>
        <Live />
      </AppShell>
    );
    renderPage(LiveWithShell);

    const cameraSearch = await screen.findByLabelText("Buscar en el explorador");
    const edge = screen.getByTestId("live-edge");
    expect(edge).toContainElement(cameraSearch);
    expect(document.querySelector('[aria-label="Context Sidebar"]')).not.toContainElement(cameraSearch);
    expect(screen.getByRole("main", { name: "Main Workspace" })).not.toContainElement(cameraSearch);

    fireEvent.click(await screen.findByRole("button", { name: /Puerta norte/ }));
    const tile = await screen.findByLabelText("Cuadro 1");
    await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
    const video = tile.querySelector("video");
    const socketCount = FakeSocket.created;
    fireEvent.click(screen.getByRole("button", { name: /^Campus/ }));

    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBe(video);
    expect(FakeSocket.created).toBe(socketCount);
  });

  it("restores a previously saved grid selection, dropping cameras the user can no longer see", async () => {
    stubBrowserAPIs();
    localStorage.setItem(
      liveSelectionKey("t1", "u1"),
      serializeSelection(2, [{ camera_id: "cam-1", quality: "sub" }, null, { camera_id: "cam-gone", quality: "sub" }, null]),
    );
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
          ...emptyCatalogs,
        }),
      ),
    );

    renderPage(Live);

    // Once in the camera list, once in the placed tile's overlay.
    expect(await screen.findAllByText("Puerta norte")).toHaveLength(2);
    // 4 slots (2x2), 1 filled, cam-gone silently dropped instead of erroring.
    expect(screen.getAllByText("Vacío")).toHaveLength(3);
  });

  it("persists the grid selection to localStorage, keyed by tenant and user, when a camera is placed", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
          ...emptyCatalogs,
        }),
      ),
    );

    renderPage(Live);

    fireEvent.click(await screen.findByText("Puerta norte"));

    await waitFor(() => {
      const raw = localStorage.getItem(liveSelectionKey("t1", "u1"));
      const saved = parseSelection(raw, new Set(["cam-1"]));
      expect(saved?.tiles[0]).toEqual({ camera_id: "cam-1", quality: "sub" });
    });
  });

  it("places a camera using its default live quality", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () =>
            json({ items: [camera("cam-1", "Puerta norte", { default_live_quality: "main" }), camera("cam-2", "Patio")] }),
          ...emptyCatalogs,
        }),
      ),
    );

    renderPage(Live);

    fireEvent.click(await screen.findByText("Puerta norte"));
    fireEvent.click(await screen.findByText("Patio"));

    await waitFor(() => {
      const saved = parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-1", "cam-2"]));
      expect(saved?.tiles[0]).toEqual({ camera_id: "cam-1", quality: "main" });
      expect(saved?.tiles[1]).toEqual({ camera_id: "cam-2", quality: "sub" });
    });
  });

  it("offers the standard 3×3 layout as nine tiles in three columns", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(stubApi({
        "/api/v1/me": meResponse,
        "/api/v1/cameras": () => json({ items: [camera("cam-1", "North gate")] }),
        ...emptyCatalogs,
      })),
    );

    renderPage(Live);
    fireEvent.click(await screen.findByText("North gate"));
    const video = await waitFor(() => {
      const element = screen.getByLabelText("Cuadro 1").querySelector("video");
      expect(element).not.toBeNull();
      return element;
    });
    const socketCount = FakeSocket.created;
    expect(screen.getByRole("status", { name: "Status: online" })).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("button", { name: "Presentación" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "3×3" }));

    expect(screen.getAllByLabelText(/^Cuadro \d+$/)).toHaveLength(9);
    expect(screen.getByLabelText("Grilla de video")).toHaveStyle({ gridTemplateColumns: "repeat(3, minmax(0, 1fr))" });
    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBe(video);
    expect(FakeSocket.created).toBe(socketCount);
  });

  it("links each visible camera to its own recordings when recordings.view is granted", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(stubApi({
        "/api/v1/me": () => meResponse([
          { permission: "live.view", effect: "allow", scope_type: "platform" },
          { permission: "recordings.view", effect: "allow", scope_type: "platform" },
        ]),
        "/api/v1/cameras": () => json({ items: [camera("cam-1", "North gate")] }),
        ...emptyCatalogs,
      })),
    );

    renderPage(Live);

    const link = await screen.findByRole("link", { name: "Grabaciones de North gate" });
    expect(link).toHaveAttribute("href", "/playback?camera=cam-1");
  });

  it("does not expose camera recordings links without recordings.view", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(stubApi({
        "/api/v1/me": meResponse,
        "/api/v1/cameras": () => json({ items: [camera("cam-1", "North gate")] }),
        ...emptyCatalogs,
      })),
    );

    renderPage(Live);

    await screen.findByRole("button", { name: /North gate/ });
    expect(screen.queryByRole("link", { name: "Grabaciones de North gate" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Ver grabaciones de North gate" })).not.toBeInTheDocument();
  });

  it("opens a camera-local recordings timeline and HLS player inside the Live workspace", async () => {
    stubBrowserAPIs();
    const fetchMock = vi.fn(stubApi({
      "/api/v1/me": () => meResponse([
        { permission: "live.view", effect: "allow", scope_type: "platform" },
        { permission: "recordings.view", effect: "allow", scope_type: "platform" },
      ]),
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "North gate")] }),
      "/api/v1/cameras/cam-1/recordings": () => json({ items: [{ start_time: "2025-06-15T10:00:00Z", end_time: "2025-06-15T11:00:00Z" }] }),
      "/api/v1/events": () => json({ items: [{ id: "event-1", start_time: "2025-06-15T10:30:00Z", severity: "alert", labels: ["person"] }] }),
      ...emptyCatalogs,
    }));
    vi.stubGlobal(
      "fetch",
      fetchMock,
    );

    renderPage(Live);
    fireEvent.click(await screen.findByRole("button", { name: "North gate" }));
    const liveVideo = await waitFor(() => {
      const video = screen.getByLabelText("Cuadro 1").querySelector("video");
      expect(video).not.toBeNull();
      return video;
    });
    const socketCount = FakeSocket.created;
    fireEvent.click(await screen.findByRole("button", { name: "Ver grabaciones de North gate" }));

    expect(await screen.findByRole("slider", { name: "Línea de tiempo del día" })).toBeInTheDocument();
    expect(screen.getByLabelText("Reproducción HLS de North gate")).toBeInTheDocument();
    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBe(liveVideo);
    expect(FakeSocket.created).toBe(socketCount);
    await waitFor(() => {
      const recordingRequest = fetchMock.mock.calls
        .map(([request]) => new URL(request.url))
        .find((url) => url.pathname === "/api/v1/cameras/cam-1/recordings");
      expect(recordingRequest?.searchParams.has("from")).toBe(true);
      expect(recordingRequest?.searchParams.has("to")).toBe(true);
      const eventRequest = fetchMock.mock.calls
        .map(([request]) => new URL(request.url))
        .find((url) => url.pathname === "/api/v1/events");
      expect(eventRequest?.searchParams.getAll("camera_id")).toContain("cam-1");
    });
    expect(screen.getByRole("link", { name: "Abrir página de grabaciones de North gate" })).toHaveAttribute(
      "href",
      "/playback?camera=cam-1",
    );
  });

  it("expands a tile on a touch double tap without toggling it back on the synthesized dblclick", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
          ...emptyCatalogs,
        }),
      ),
    );
    renderPage(Live);
    fireEvent.click(await screen.findByText("Puerta norte"));
    await waitFor(() => expect(document.querySelector('[aria-label="Cuadro 1"]')?.textContent).toContain("Puerta norte"));
    const tile = document.querySelector('[aria-label="Cuadro 1"]') as HTMLElement;
    const tap = (x: number) => {
      fireEvent.pointerDown(tile, { pointerId: 1, pointerType: "touch", isPrimary: true, clientX: x, clientY: 40 });
      fireEvent.pointerUp(tile, { pointerId: 1, pointerType: "touch", isPrimary: true, clientX: x, clientY: 40 });
    };
    tap(50);
    expect(screen.queryByRole("button", { name: "Volver a la grilla" })).not.toBeInTheDocument();
    tap(52);
    expect(await screen.findByRole("button", { name: "Volver a la grilla" })).toBeInTheDocument();
    fireEvent.doubleClick(tile);
    expect(screen.getByRole("button", { name: "Volver a la grilla" })).toBeInTheDocument();
  });

  it("keeps the mouse double click toggling the tile and lets touch-action stay off the whole tile", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
          ...emptyCatalogs,
        }),
      ),
    );
    renderPage(Live);
    fireEvent.click(await screen.findByText("Puerta norte"));
    await waitFor(() => expect(document.querySelector('[aria-label="Cuadro 1"]')?.textContent).toContain("Puerta norte"));
    const tile = document.querySelector('[aria-label="Cuadro 1"]') as HTMLElement;
    expect(tile.className).toContain("select-none");
    fireEvent.doubleClick(tile);
    expect(await screen.findByRole("button", { name: "Volver a la grilla" })).toBeInTheDocument();
  });

  it("reorders two tiles with the keyboard (dnd-kit's built-in accessibility)", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte"), camera("cam-2", "Porton sur")] }),
          ...emptyCatalogs,
        }),
      ),
    );

    renderPage(Live);
    fireEvent.click(await screen.findByText("Puerta norte"));
    fireEvent.click(await screen.findByText("Porton sur"));
    // Grid is 2x2 by default: tile 1 = Puerta norte, tile 2 = Porton sur.
    // querySelector stays cheap while the 220ms click delay is still pending; getByLabelText
    // builds an accessible-name dump of the whole page on each miss and stalls jsdom.
    await waitFor(() => {
      expect(document.querySelector('[aria-label="Cuadro 1"]')?.textContent).toContain("Puerta norte");
      expect(document.querySelector('[aria-label="Cuadro 2"]')?.textContent).toContain("Porton sur");
    });

    // jsdom never lays out elements, so dnd-kit's keyboard sensor (which picks a directional
    // neighbor by comparing real getBoundingClientRect() rects) has nothing to compare. Give the
    // tiles distinct, side-by-side rects matching the 2x2 grid's actual layout so ArrowRight has
    // a real neighbor to resolve to — the same technique dnd-kit's own test suite uses.
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (this: Element) {
      const index = /Cuadro (\d+)/.exec(this.getAttribute?.("aria-label") ?? "")?.[1];
      const col = index ? (Number(index) - 1) % 2 : 0;
      const row = index ? Math.floor((Number(index) - 1) / 2) : 0;
      const x = col * 200;
      const y = row * 150;
      return { x, y, top: y, left: x, right: x + 200, bottom: y + 150, width: 200, height: 150, toJSON: () => ({}) } as DOMRect;
    });

    // Tab to the drag handle dnd-kit exposes (role="button", tabIndex=0), pick it up with
    // Space, move right with the arrow key, and drop with Space again.
    const tile1 = screen.getByLabelText("Cuadro 1");
    tile1.focus();
    fireEvent.keyDown(tile1, { code: "Space" });
    await new Promise((r) => setTimeout(r, 0));
    fireEvent.keyDown(document.activeElement ?? tile1, { code: "ArrowRight" });
    await new Promise((r) => setTimeout(r, 0));
    fireEvent.keyDown(document.activeElement ?? tile1, { code: "Space" });

    await waitFor(() => {
      expect(document.querySelector('[aria-label="Cuadro 1"]')?.textContent).toContain("Porton sur");
      expect(document.querySelector('[aria-label="Cuadro 2"]')?.textContent).toContain("Puerta norte");
    });
  });

  it("hides Guardar vista when the user has neither views.create_private nor views.create_shared", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": () => meResponse(),
          "/api/v1/cameras": () => json({ items: [] }),
          ...emptyCatalogs,
        }),
      ),
    );

    renderPage(Live);

    await screen.findByText("No hay cámaras visibles.");
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 30));
    });
    fireEvent.contextMenu(screen.getByRole("button", { name: /^Cámaras/ }));
    expect(screen.queryByRole("menuitem", { name: "Guardar grilla como vista" })).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Nombre de la vista")).not.toBeInTheDocument();
    expect(screen.queryByText("Compartida con mi organización")).not.toBeInTheDocument();
  });

  it("shows Guardar vista when the user holds views.create_private", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": () => meResponse([{ permission: "views.create_private", effect: "allow", scope_type: "tenant" }]),
          "/api/v1/cameras": () => json({ items: [] }),
          ...emptyCatalogs,
        }),
      ),
    );

    renderPage(Live);

    await screen.findByText("No hay cámaras visibles.");
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    fireEvent.contextMenu(screen.getByRole("button", { name: /^Cámaras/ }));
    expect(screen.getByRole("menuitem", { name: "Guardar grilla como vista" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("menuitem", { name: "Guardar grilla como vista" }));
    expect(await screen.findByLabelText("Nombre de la vista")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Guardar" })).toBeInTheDocument();
  });

  it("shows the API error when saving a view fails despite the gate (server-side denial)", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/me") return meResponse([{ permission: "views.create_private", effect: "allow", scope_type: "tenant" }]);
        if (url.pathname === "/api/v1/cameras") return json({ items: [] });
        if (url.pathname === "/api/v1/sites") return json({ items: [] });
        if (url.pathname === "/api/v1/servers") return json({ items: [] });
        if (url.pathname === "/api/v1/views" && input.method === "GET") return json({ items: [] });
        if (url.pathname === "/api/v1/views" && input.method === "POST") {
          return json({ code: "forbidden", message: "No tenés permiso para guardar esta vista." }, 403);
        }
        return json({ code: "not_found", message: "not found" }, 404);
      }),
    );

    renderPage(Live);

    await screen.findByText("No hay cámaras visibles.");
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 50));
    });
    fireEvent.contextMenu(screen.getByRole("button", { name: /^Cámaras/ }));
    expect(screen.getByRole("menuitem", { name: "Guardar grilla como vista" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("menuitem", { name: "Guardar grilla como vista" }));
    fireEvent.change(await screen.findByLabelText("Nombre de la vista"), { target: { value: "Turno noche" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("No tenés permiso para guardar esta vista.");
  });
  const savedView = (id: string, name: string, shared: boolean, editable: boolean, ownerName?: string) => ({
    id,
    tenant_id: "t1",
    owner_id: "u1",
    owner_name: ownerName,
    name,
    shared,
    editable,
    layout: { columns: 2, cells: [{ quality: "sub" }, { quality: "sub" }, { quality: "sub" }, { quality: "sub" }] },
    created_at: "",
    updated_at: "",
  });

  const viewsApi = (views: unknown[]) => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": () => meResponse([{ permission: "views.create_private", effect: "allow", scope_type: "tenant" }]),
          "/api/v1/cameras": () => json({ items: [] }),
          "/api/v1/sites": () => json({ items: [] }),
          "/api/v1/servers": () => json({ items: [] }),
          "/api/v1/views": () => json({ items: views }),
        }),
      ),
    );
  };

  it("lists saved views inside the camera tree", async () => {
    viewsApi([savedView("v1", "Turno noche", false, true), savedView("v2", "Perímetro", true, false)]);

    renderPage(Live);

    const turno = await screen.findByRole("button", { name: "Turno noche" });
    const tree = screen.getByRole("navigation", { name: "Cámaras" });
    expect(tree).toContainElement(turno);
    expect(within(tree).getByRole("button", { name: /Perímetro/ })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Vistas guardadas" })).not.toBeInTheDocument();
  });

  it("announces the active view, its visibility and read-only state", async () => {
    viewsApi([savedView("v2", "Perímetro", true, false)]);

    renderPage(Live);

    const item = await screen.findByRole("button", { name: /Perímetro/ });
    expect(item).not.toHaveAttribute("aria-current", "true");
    fireEvent.click(item);
    expect(item).toHaveAttribute("aria-current", "true");
    expect(item).toHaveAttribute("title", expect.stringContaining("Compartida"));
    expect(item).toHaveAttribute("title", expect.stringContaining("Solo lectura"));
  });

  it("shows who owns a shared view in the list and the status line", async () => {
    viewsApi([savedView("v1", "Turno noche", false, true, "Ana"), savedView("v2", "Perímetro", true, false, "Marta Gómez")]);

    renderPage(Live);

    const shared = await screen.findByRole("button", { name: "Perímetro · Marta Gómez" });
    const privateView = screen.getByRole("button", { name: "Turno noche" });
    expect(within(shared).getByRole("img", { name: "Compartida" })).toBeInTheDocument();
    expect(within(privateView).queryByRole("img", { name: "Compartida" })).toBeNull();
    expect(shared).toHaveAttribute("title", expect.stringContaining("Compartida por Marta Gómez"));
  });

  it("filters saved views with the explorer search", async () => {
    viewsApi([savedView("v1", "Turno noche", false, true), savedView("v2", "Perímetro", true, false)]);

    renderPage(Live);

    await screen.findByRole("button", { name: /Turno noche/ });
    fireEvent.change(screen.getByLabelText("Buscar en el explorador"), { target: { value: "perí" } });
    expect(screen.queryByRole("button", { name: /Turno noche/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Perímetro/ })).toBeInTheDocument();
  });

  it("opens the edge explorer with a tap and closes it with a tap outside, ignoring touch hover", async () => {
    localStorage.setItem("openvms.live.sidebar.pinned", "0");
    viewsApi([]);

    renderPage(Live);
    const edge = await screen.findByTestId("live-edge");
    fireEvent.pointerEnter(edge, { pointerType: "touch" });
    expect(edge.querySelector("aside")).toHaveAttribute("hidden");
    fireEvent.click(screen.getByRole("button", { name: "Mostrar explorador" }));
    expect(edge.querySelector("aside")).not.toHaveAttribute("hidden");
    fireEvent.pointerLeave(edge, { pointerType: "touch" });
    await new Promise((resolve) => setTimeout(resolve, 350));
    expect(edge.querySelector("aside")).not.toHaveAttribute("hidden");
    fireEvent.pointerDown(edge.querySelector("aside") as HTMLElement, { pointerType: "touch" });
    expect(edge.querySelector("aside")).not.toHaveAttribute("hidden");
    fireEvent.pointerDown(document.body, { pointerType: "touch" });
    await waitFor(() => expect(edge.querySelector("aside")).toHaveAttribute("hidden"));
  });

  it("keeps the explorer on the left edge and remembers the pin", async () => {
    localStorage.setItem("openvms.live.sidebar.pinned", "0");
    viewsApi([]);

    const first = renderPage(Live);
    const edge = await screen.findByTestId("live-edge");
    expect(edge.querySelector("aside")).toHaveAttribute("hidden");
    fireEvent.click(screen.getByRole("button", { name: "Mostrar explorador" }));
    expect(edge.querySelector("aside")).not.toHaveAttribute("hidden");
    fireEvent.click(screen.getByRole("button", { name: "Anclar explorador" }));
    expect(localStorage.getItem("openvms.live.sidebar.pinned")).toBe("1");
    expect(screen.queryByRole("button", { name: "Ocultar explorador" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Desanclar explorador" }));
    expect(localStorage.getItem("openvms.live.sidebar.pinned")).toBe("0");
    fireEvent.pointerLeave(edge);
    await waitFor(() => expect(edge.querySelector("aside")).toHaveAttribute("hidden"));
    first.unmount();

    localStorage.setItem("openvms.live.sidebar.pinned", "1");
    renderPage(Live);
    const again = await screen.findByTestId("live-edge");
    expect(again.querySelector("aside")).not.toHaveAttribute("hidden");
    expect(screen.getByLabelText("Buscar en el explorador")).toBeInTheDocument();
  });

  it("offers folder management only on servers the caller can manage", async () => {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte", { folder_id: "f1", sort_order: 0 }), camera("cam-2", "Muelle", { server_id: "srv2", folder_id: null, sort_order: 0 })] }),
          "/api/v1/sites": () => json({ items: [{ id: "s1", tenant_id: "t1", name: "Campus" }] }),
          "/api/v1/servers": () =>
            json({
              items: [
                { id: "srv1", tenant_id: "t1", site_id: "s1", name: "Frigate A", status: "online" },
                { id: "srv2", tenant_id: "t1", site_id: "s1", name: "Frigate B", status: "online" },
              ],
            }),
          "/api/v1/camera-folders": () =>
            json({ items: [{ id: "f1", tenant_id: "t1", server_id: "srv1", name: "Accesos", sort_order: 0, created_at: "", updated_at: "" }], manageable_server_ids: ["srv1"] }),
          "/api/v1/views": () => json({ items: [] }),
        }),
      ),
    );

    renderPage(Live);

    expect(await screen.findByRole("button", { name: /^Accesos\d*$/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Puerta norte" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Nueva carpeta en Frigate A" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Nueva carpeta en Frigate B" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Renombrar carpeta Accesos" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Buscar en el explorador"), { target: { value: "acce" } });
    expect(screen.queryByRole("button", { name: "Muelle" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Puerta norte" })).toBeInTheDocument();
  });

  function foldersApi() {
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte", { folder_id: "f1", sort_order: 0 })] }),
          "/api/v1/sites": () => json({ items: [{ id: "s1", tenant_id: "t1", name: "Campus" }] }),
          "/api/v1/servers": () => json({ items: [{ id: "srv1", tenant_id: "t1", site_id: "s1", name: "Frigate A", status: "online" }] }),
          "/api/v1/camera-folders": () =>
            json({ items: [{ id: "f1", tenant_id: "t1", server_id: "srv1", name: "Accesos", sort_order: 0, created_at: "", updated_at: "" }], manageable_server_ids: ["srv1"] }),
          "/api/v1/views": () => json({ items: [] }),
        }),
      ),
    );
  }
  const touch = (type: "pointerDown" | "pointerMove" | "pointerUp", el: Element, x: number, y: number) =>
    fireEvent[type](el, { pointerId: 7, pointerType: "touch", isPrimary: true, clientX: x, clientY: y });

  it("opens the explorer context menu on a touch long press at the touch point and not on a short tap", async () => {
    foldersApi();
    renderPage(Live);
    const row = await screen.findByRole("button", { name: "Puerta norte" });
    expect(row).toHaveAttribute("data-longpress");
    touch("pointerDown", row, 30, 40);
    touch("pointerUp", row, 30, 40);
    await new Promise((r) => setTimeout(r, 650));
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    touch("pointerDown", row, 30, 40);
    const menu = await screen.findByRole("menu", {}, { timeout: 2000 });
    expect(menu).toBeInTheDocument();
    const anchor = menu.closest<HTMLElement>("[data-context-menu]");
    expect(anchor?.style.left).toBe("30px");
    expect(anchor?.style.top).toBe("40px");
  });

  it("cancels the long press when the finger moves", async () => {
    foldersApi();
    renderPage(Live);
    const row = await screen.findByRole("button", { name: "Puerta norte" });
    touch("pointerDown", row, 30, 40);
    touch("pointerMove", row, 30, 60);
    await new Promise((r) => setTimeout(r, 700));
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("keeps folder row actions visible on devices without hover", async () => {
    foldersApi();
    renderPage(Live);
    const rename = await screen.findByRole("button", { name: "Renombrar carpeta Accesos" });
    expect(rename.parentElement?.className).toContain("[@media(hover:none)]:opacity-100");
  });

  it("labels the layout picker as a group", async () => {
    viewsApi([]);

    renderPage(Live);

    const group = await screen.findByRole("group", { name: "Layout de la grilla" });
    expect(group).toContainElement(screen.getByRole("button", { name: "Presentación" }));
    expect(group).toContainElement(screen.getByRole("button", { name: "Pantalla completa" }));
    expect(screen.queryByText(/Doble clic agrega/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Presentación" }));
    expect(screen.getAllByRole("menuitem").map((item) => item.textContent?.trim()).slice(0, 8)).toEqual(["1", "3", "2×2", "3×3", "4×4", "5×5", "6×6", "8×8"]);
    expect(screen.getByRole("menuitem", { name: "Editar presentaciones..." })).toBeInTheDocument();
  });

  it("removes a grid line in the presentation editor and applies the new shape", async () => {
    viewsApi([]);
    renderPage(Live);

    fireEvent.click(await screen.findByRole("button", { name: "Presentación" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Editar presentaciones..." }));
    fireEvent.click(screen.getByRole("option", { name: "2×2" }));
    fireEvent.click(screen.getByRole("button", { name: "Nueva" }));
    fireEvent.click(screen.getAllByRole("button", { name: "Quitar línea" })[0]!);
    fireEvent.click(screen.getByRole("button", { name: "Aceptar" }));

    await waitFor(() => expect(screen.getAllByLabelText(/^Cuadro \d+$/)).toHaveLength(3));
    expect(screen.getByLabelText("Grilla de video")).toHaveStyle({ gridTemplateColumns: "repeat(2, minmax(0, 1fr))" });
  });
});

describe("Live with persistent players (P0 acceptance)", () => {
  const LiveWithSessions = () => (
    <PlayerSessionProvider userId="u1">
      <Live />
    </PlayerSessionProvider>
  );

  async function renderPersistent() {
    localStorage.setItem(FEATURES_OVERRIDE_KEY, JSON.stringify({ persistentPlayers: true }));
    playerMetrics.reset();
    stubBrowserAPIs();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte"), camera("cam-2", "Porton sur")] }),
          ...emptyCatalogs,
        }),
      ),
    );
    renderPage(LiveWithSessions);
    fireEvent.click(await screen.findByRole("button", { name: /Puerta norte/ }));
    fireEvent.click(await screen.findByRole("button", { name: /Porton sur/ }));
    // Both sessions open asynchronously: wait for both pictures and both sockets, or the
    // connection counts below race the second camera's connect.
    await waitFor(() => {
      expect(screen.getByLabelText("Cuadro 1").querySelector("video")).not.toBeNull();
      expect(screen.getByLabelText("Cuadro 2").querySelector("video")).not.toBeNull();
      expect(FakeSocket.created).toBe(2);
    });
  }

  it("moving a camera from cell 1 to cell 8 keeps the same session and does not reconnect", async () => {
    await renderPersistent();
    fireEvent.click(screen.getByRole("button", { name: "Presentación" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "3×3" }));
    const video = screen.getByLabelText("Cuadro 1").querySelector("video");
    expect(video).not.toBeNull();
    expect(FakeSocket.created).toBe(2);

    // jsdom has no layout: give the cells the rects of a 3x3 grid so keyboard dragging can navigate it.
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (this: Element) {
      const index = /Cuadro (\d+)/.exec(this.getAttribute?.("aria-label") ?? "")?.[1];
      const col = index ? (Number(index) - 1) % 3 : 0;
      const row = index ? Math.floor((Number(index) - 1) / 3) : 0;
      const x = col * 200;
      const y = row * 150;
      return { x, y, top: y, left: x, right: x + 200, bottom: y + 150, width: 200, height: 150, toJSON: () => ({}) } as DOMRect;
    });
    const tile1 = screen.getByLabelText("Cuadro 1");
    tile1.focus();
    fireEvent.keyDown(tile1, { code: "Space" });
    for (const code of ["ArrowDown", "ArrowDown", "ArrowRight"]) {
      await new Promise((r) => setTimeout(r, 0));
      fireEvent.keyDown(document.activeElement ?? tile1, { code });
    }
    await new Promise((r) => setTimeout(r, 0));
    fireEvent.keyDown(document.activeElement ?? tile1, { code: "Space" });

    await waitFor(() => expect(screen.getByLabelText("Cuadro 8").textContent).toContain("Puerta norte"));
    // Swap semantics: nothing else moved, nothing reconnected.
    expect(screen.getByLabelText("Cuadro 2").textContent).toContain("Porton sur");
    expect(screen.getByLabelText("Cuadro 8").querySelector("video")).toBe(video);
    expect(FakeSocket.created).toBe(2);
    expect(playerMetrics.get("cam-1", "sub")?.connectAttempts).toBe(1);
    expect(playerMetrics.get("cam-2", "sub")?.connectAttempts).toBe(1);
  });

  it("grid -> expand -> grid keeps every session and does not reconnect", async () => {
    await renderPersistent();
    const video1 = screen.getByLabelText("Cuadro 1").querySelector("video");
    const video2 = screen.getByLabelText("Cuadro 2").querySelector("video");

    fireEvent.click(screen.getAllByRole("button", { name: "Ampliar" })[0]!);
    // The other tile stays mounted (its session WARM) but is not displayed.
    expect(screen.getByLabelText("Cuadro 2")).toHaveClass("hidden");
    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBe(video1);

    fireEvent.click(screen.getByRole("button", { name: "Volver a la grilla" }));
    expect(screen.getByLabelText("Cuadro 2")).not.toHaveClass("hidden");
    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBe(video1);
    expect(screen.getByLabelText("Cuadro 2").querySelector("video")).toBe(video2);
    expect(FakeSocket.created).toBe(2);
    expect(playerMetrics.get("cam-1", "sub")?.connectAttempts).toBe(1);
    expect(playerMetrics.get("cam-1", "sub")?.reconnectCount).toBe(0);
    expect(playerMetrics.get("cam-2", "sub")?.reconnectCount).toBe(0);
  });

  it("changing the layout keeps the sessions of cameras that stay visible", async () => {
    await renderPersistent();
    const video = screen.getByLabelText("Cuadro 1").querySelector("video");
    fireEvent.click(screen.getByRole("button", { name: "Presentación" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "4×4" }));
    fireEvent.click(screen.getByRole("button", { name: "Presentación" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "3×3" }));
    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBe(video);
    expect(FakeSocket.created).toBe(2);
  });

  it("offers the 5×5, 6×6 and 8×8 presentations from the layout menu", async () => {
    await renderPersistent();
    fireEvent.click(screen.getByRole("button", { name: "Presentación" }));
    expect(screen.getByRole("menuitem", { name: "5×5" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "6×6" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "8×8" })).toBeInTheDocument();
  });

  describe("LIVE/REC mode", () => {
    const withRecordings = [
      { permission: "live.view", effect: "allow" as const, scope_type: "platform" },
      { permission: "recordings.view", effect: "allow" as const, scope_type: "platform" },
    ];
    const setup = (grants: Parameters<typeof meResponse>[0], recordings: () => Response = () => json({ items: [] })) => {
      stubBrowserAPIs();
      localStorage.setItem(liveSelectionKey("t1", "u1"), serializeSelection(2, [{ camera_id: "cam-1", quality: "sub" }, null, null, null]));
      vi.stubGlobal(
        "fetch",
        vi.fn(
          stubApi({
            "/api/v1/me": () => meResponse(grants),
            "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
            "/api/v1/cameras/cam-1/recordings": recordings,
            "/api/v1/events": () => json({ items: [] }),
            ...emptyCatalogs,
          }),
        ),
      );
    };

    it("hides the toggle without any recordings permission", async () => {
      setup(undefined);
      renderPage(Live);
      await screen.findByLabelText("Cuadro 1");
      expect(screen.queryByRole("group", { name: "Modo de reproducción" })).toBeNull();
    });

    it("keeps the toggle clear of the pinned explorer and flush when unpinned", async () => {
      setup(withRecordings);
      const WithShell = () => (
        <AppShell primaryNav={<nav aria-label="Primary navigation" />} contextSidebar={<div id="live-context-sidebar" />}>
          <TopBarActionsSlot />
          <Live />
        </AppShell>
      );
      const first = renderPage(WithShell);
      await screen.findByRole("button", { name: "Grabado" });
      expect(screen.getByTestId("live-mode-toggle-slot")).toHaveClass("md:ml-(--pinned-offset)");
      first.unmount();
      localStorage.setItem("openvms.live.sidebar.pinned", "0");
      renderPage(WithShell);
      await screen.findByRole("button", { name: "Grabado" });
      expect(screen.getByTestId("live-mode-toggle-slot")).not.toHaveClass("md:ml-(--pinned-offset)");
    });

    it("switches to REC from the toggle and back to LIVE, clearing the URL state", async () => {
      setup(withRecordings);
      const { router } = renderPage(Live);
      fireEvent.click(await screen.findByRole("button", { name: "Grabado" }));
      expect(await screen.findByRole("region", { name: "Controles de grabación" })).toBeInTheDocument();
      await waitFor(() => expect(router.state.location.search).toMatchObject({ mode: "rec" }));
      const started = new Date(String((router.state.location.search as { t?: string }).t)).getTime();
      expect(Math.abs(started - (Date.now() - 5 * 60 * 1000))).toBeLessThan(5_000);
      expect(screen.getByRole("button", { name: "Grabado" })).toHaveAttribute("aria-pressed", "true");

      fireEvent.click(screen.getByRole("button", { name: "Vivo" }));
      await waitFor(() => expect(screen.queryByRole("region", { name: "Controles de grabación" })).toBeNull());
      expect(router.state.location.search).not.toHaveProperty("mode");
      expect(router.state.location.search).not.toHaveProperty("t");
    });

    it("opens in REC from ?mode=rec&t= and shows tiles without recordings permission", async () => {
      setup(withRecordings, () => json({ code: "forbidden", message: "forbidden" }, 403));
      renderPage(Live, "/?mode=rec&t=2026-09-30T12:00:00.000Z");
      expect(await screen.findByRole("region", { name: "Controles de grabación" })).toBeInTheDocument();
      expect(await screen.findByText("Sin permiso de grabaciones")).toBeInTheDocument();
    });
  });

  it("switches the explorer to maps, detections and plates", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": () => meResponse([
        { permission: "live.view", effect: "allow", scope_type: "platform" },
        { permission: "maps.view", effect: "allow", scope_type: "platform" },
        { permission: "events.view", effect: "allow", scope_type: "platform" },
        { permission: "lpr.view", effect: "allow", scope_type: "platform" },
      ]),
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
      "/api/v1/sites": () => json({ items: [{ id: "s1", tenant_id: "t1", name: "Campus", timezone: "UTC", address: "" }] }),
      "/api/v1/servers": () => json({ items: [] }),
      "/api/v1/views": () => json({ items: [] }),
      "/api/v1/maps/sites/s1": () => json({
        id: "s1", name: "Campus", buildings: [{ id: "b", site_id: "s1", name: "Planta", floors: [{ id: "f", building_id: "b", name: "PB", ordinal: 0 }] }], zones: [],
      }),
      "/api/v1/events": () => json({ items: [{
        id: "e1", tenant_id: "t1", site_id: "s1", site_name: "Campus", server_id: "srv", server_name: "Frigate", camera_id: "cam-1", camera_name: "Puerta norte",
        remote_id: "r", severity: "detection", labels: ["person"], sub_labels: [], zones: [], plates: [], start_time: "2026-10-03T12:00:00.000Z", reviewed: false,
        has_thumbnail: true, has_snapshot: false, has_preview: false,
      }] }),
      "/api/v1/lpr/reads": () => json({ items: [{
        id: "r1", site_id: "s1", site_name: "Campus", server_id: "srv", server_name: "Frigate", camera_id: "cam-1", camera_name: "Puerta norte",
        plate: "AB123CD", plate_normalized: "AB123CD", label: "car", zones: [], seen_at: "2026-10-03T12:00:00.000Z",
      }] }),
    })));
    renderPage(Live);
    expect(await screen.findByRole("tab", { name: "Cámaras" })).toHaveAttribute("aria-selected", "true");
    fireEvent.click(screen.getByRole("tab", { name: "Mapas" }));
    expect(await screen.findByRole("button", { name: "Mapa Campus" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Mapa Planta / PB" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Mapa Campus" }));
    expect(await screen.findByText("No se pudo abrir el mapa.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: "Detección" }));
    expect(await screen.findByRole("button", { name: "Detección de Puerta norte" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: "LPR" }));
    expect(await screen.findByRole("img", { name: "Lectura de patente AB123CD" })).toBeInTheDocument();
  });

  it("reveals the explorer from the left edge in fullscreen and keeps it pinned", async () => {
    localStorage.setItem("openvms.live.sidebar.pinned", "0");
    let element: Element | null = null;
    const original = Object.getOwnPropertyDescriptor(Document.prototype, "fullscreenElement");
    Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => element });
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
      ...emptyCatalogs,
    })));
    renderPage(Live);
    await screen.findByLabelText("Buscar en el explorador");
    element = document.documentElement;
    try {
      fireEvent(document, new Event("fullscreenchange"));
      const edge = screen.getByTestId("live-edge");
      expect(edge.querySelector("aside")).toHaveAttribute("hidden");
      fireEvent.click(screen.getByRole("button", { name: "Mostrar explorador" }));
      expect(edge.querySelector("aside")).not.toHaveAttribute("hidden");
      expect(screen.getByLabelText("Buscar en el explorador")).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Anclar explorador" }));
      expect(localStorage.getItem("openvms.live.sidebar.pinned")).toBe("1");
      fireEvent.pointerLeave(edge);
      await new Promise((resolve) => setTimeout(resolve, 400));
      expect(edge.querySelector("aside")).not.toHaveAttribute("hidden");
    } finally {
      if (original) Object.defineProperty(Document.prototype, "fullscreenElement", original);
      else Reflect.deleteProperty(document, "fullscreenElement");
    }
  });
});
