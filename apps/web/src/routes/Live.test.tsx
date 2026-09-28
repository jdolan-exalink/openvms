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
});
