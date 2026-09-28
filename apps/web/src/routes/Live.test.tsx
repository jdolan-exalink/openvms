import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { liveSelectionKey, parseSelection, serializeSelection } from "@/lib/liveGrid";
import { json, renderPage, stubApi } from "@/test-utils";
import { Live } from "./Live";

afterEach(() => vi.unstubAllGlobals());
beforeEach(() => localStorage.clear());

/** FakeSocket/FakeMediaSource let MsePlayer mount without a real browser MSE stack. */
class FakeSocket {
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 0;
  binaryType = "blob";
  constructor(public url: string) {}
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

function meResponse() {
  return json({
    id: "u1",
    username: "operator",
    display_name: "Operator",
    mfa_enabled: false,
    must_change_password: false,
    auth_method: "session",
    tenant_id: "t1",
    grants: [{ permission: "live.view", effect: "allow" as const, scope_type: "platform" as const }],
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
});
