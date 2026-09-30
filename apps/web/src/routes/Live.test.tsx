import { fireEvent, screen, waitFor } from "@testing-library/react";
import { AppShell } from "@/components/AppShell";
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

    const cameraSearch = await screen.findByLabelText("Buscar cámara");
    const contextSidebar = screen.getByRole("complementary", { name: "Context Sidebar" });
    expect(contextSidebar).toContainElement(cameraSearch);
    expect(screen.getByRole("main", { name: "Main Workspace" })).not.toContainElement(cameraSearch);

    fireEvent.click(await screen.findByRole("button", { name: /Puerta norte/ }));
    const tile = await screen.findByLabelText("Cuadro 1");
    await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
    const video = tile.querySelector("video");
    const socketCount = FakeSocket.created;
    fireEvent.click(screen.getByRole("button", { name: "Campus" }));

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

  it("offers the standard 3×2 layout as six tiles in three columns", async () => {
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
    fireEvent.click(await screen.findByRole("button", { name: "Layout 3 by 2" }));

    expect(screen.getAllByLabelText(/^Cuadro \d+$/)).toHaveLength(6);
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
    expect(screen.getByLabelText("Cuadro 1").textContent).toContain("Puerta norte");
    expect(screen.getByLabelText("Cuadro 2").textContent).toContain("Porton sur");

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
      expect(screen.getByLabelText("Cuadro 1").textContent).toContain("Porton sur");
      expect(screen.getByLabelText("Cuadro 2").textContent).toContain("Puerta norte");
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

    await screen.findByLabelText("Nombre de la vista");
    expect(screen.queryByText("Guardar vista")).not.toBeInTheDocument();
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

    expect(await screen.findByText("Guardar vista")).toBeInTheDocument();
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

    fireEvent.change(await screen.findByLabelText("Nombre de la vista"), { target: { value: "Turno noche" } });
    fireEvent.click(screen.getByText("Guardar vista"));

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

  it("groups saved views into private and shared groups", async () => {
    viewsApi([savedView("v1", "Turno noche", false, true), savedView("v2", "Perímetro", true, false)]);

    renderPage(Live);

    const select = await screen.findByLabelText("Vista guardada");
    await screen.findByRole("option", { name: /Turno noche/ });
    expect(select.querySelector('optgroup[label="Privadas"]')).toHaveTextContent("Turno noche");
    expect(select.querySelector('optgroup[label="Compartidas"]')).toHaveTextContent("Perímetro");
  });

  it("announces the active view, its visibility and read-only state", async () => {
    viewsApi([savedView("v2", "Perímetro", true, false)]);

    renderPage(Live);

    await screen.findByRole("option", { name: /Perímetro/ });
    expect(screen.getByRole("status", { name: "Vista activa" })).toHaveTextContent("Vista sin guardar");
    fireEvent.change(screen.getByLabelText("Vista guardada"), { target: { value: "v2" } });
    const status = screen.getByRole("status", { name: "Vista activa" });
    expect(status).toHaveTextContent("Perímetro");
    expect(status).toHaveTextContent("Compartida");
    expect(status).toHaveTextContent("Solo lectura");
  });

  it("shows who owns a shared view in the selector and the status line", async () => {
    viewsApi([savedView("v1", "Turno noche", false, true, "Ana"), savedView("v2", "Perímetro", true, false, "Marta Gómez")]);

    renderPage(Live);

    expect(await screen.findByRole("option", { name: "Perímetro · Marta Gómez" })).toBeInTheDocument();
    // Private views are the caller's own: no owner suffix.
    expect(screen.getByRole("option", { name: "Turno noche" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Vista guardada"), { target: { value: "v2" } });
    expect(screen.getByRole("status", { name: "Vista activa" })).toHaveTextContent("Compartida por Marta Gómez");
  });

  it("labels the layout picker as a group", async () => {
    viewsApi([]);

    renderPage(Live);

    const group = await screen.findByRole("group", { name: "Layout de la grilla" });
    expect(group).toContainElement(screen.getByRole("button", { name: "Layout 2 by 2" }));
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
    await waitFor(() => expect(screen.getByLabelText("Cuadro 1").querySelector("video")).not.toBeNull());
  }

  it("moving a camera from cell 1 to cell 8 keeps the same session and does not reconnect", async () => {
    await renderPersistent();
    fireEvent.click(screen.getByRole("button", { name: "Layout 3 by 3" }));
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
    fireEvent.click(screen.getByRole("button", { name: "Layout 4 by 4" }));
    fireEvent.click(screen.getByRole("button", { name: "Layout 3 by 2" }));
    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBe(video);
    expect(FakeSocket.created).toBe(2);
  });

  it("offers the 25 and 32 camera walls only with persistent players", async () => {
    await renderPersistent();
    expect(screen.getByRole("button", { name: "Layout 5 by 5" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Layout 8 by 4" })).toBeInTheDocument();
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

    it("switches to REC from the toggle and back to LIVE, clearing the URL state", async () => {
      setup(withRecordings);
      const { router } = renderPage(Live);
      fireEvent.click(await screen.findByRole("button", { name: "Grabación" }));
      expect(await screen.findByRole("region", { name: "Controles de grabación" })).toBeInTheDocument();
      await waitFor(() => expect(router.state.location.search).toMatchObject({ mode: "rec" }));
      expect(screen.getByRole("button", { name: "Grabación" })).toHaveAttribute("aria-pressed", "true");

      fireEvent.click(screen.getByRole("button", { name: "En vivo" }));
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
});
