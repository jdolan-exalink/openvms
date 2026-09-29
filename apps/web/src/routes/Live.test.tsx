import { fireEvent, screen, waitFor } from "@testing-library/react";
import { AppShell } from "@/components/AppShell";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { liveSelectionKey, parseSelection, serializeSelection } from "@/lib/liveGrid";
import { json, renderPage, stubApi } from "@/test-utils";
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

const camera = (id: string, name: string) => ({
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
  created_at: "",
  updated_at: "",
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
});
