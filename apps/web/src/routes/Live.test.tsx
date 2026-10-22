import { act, fireEvent, renderHook, screen, waitFor, within } from "@testing-library/react";
import { AppShell, TopBarActionsSlot } from "@/components/AppShell";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { liveSelectionKey, parseSelection, serializeSelection } from "@/lib/liveGrid";
import { json, renderPage, stubApi } from "@/test-utils";
import { FEATURES_OVERRIDE_KEY } from "@/lib/features";
import { PlayerSessionManager } from "@/lib/live/PlayerSessionManager";
import { PlayerSessionProvider } from "@/lib/live/PlayerSessionProvider";
import { VideoSurfaceLayer } from "@/lib/live/SurfaceLayer";
import { playerMetrics } from "@/lib/live/playerMetrics";
import { saveFisheyeConfig } from "@/lib/fisheye/storage";
import { DEFAULT_FISHEYE_CALIBRATION, DEFAULT_FISHEYE_CONFIG } from "@/lib/fisheye/types";
import { classifyHealth, telemetryRuntime } from "@/lib/telemetry";
import { Live, useTileOverlayVisibility } from "./Live";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
beforeEach(() => {
  localStorage.clear();
  localStorage.setItem("openvms.live.sidebar.pinned", "1");
  FakeSocket.created = 0;
  FakeSocket.closed = 0;
  FakeSocket.urls = [];
  FakeSocket.instances = [];
});

/** FakeSocket/FakeMediaSource let MsePlayer mount without a real browser MSE stack. */
class FakeSocket {
  static created = 0;
  static closed = 0;
  static urls: string[] = [];
  static instances: FakeSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 0;
  binaryType = "blob";
  constructor(public url: string) {
    FakeSocket.created += 1;
    FakeSocket.urls.push(url);
    FakeSocket.instances.push(this);
  }
  send() {}
  close() { FakeSocket.closed += 1; }
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
  it("hides tile overlays after three seconds and reveals them again on interaction", () => {
    vi.useFakeTimers();
    const { result } = renderHook(() => useTileOverlayVisibility());
    expect(result.current.visible).toBe(true);
    act(() => { vi.advanceTimersByTime(3000); });
    expect(result.current.visible).toBe(false);
    act(() => { result.current.reveal(); });
    expect(result.current.visible).toBe(true);
    act(() => { result.current.scheduleHide(); vi.advanceTimersByTime(3000); });
    expect(result.current.visible).toBe(false);
  });

  it("does not render a fisheye dewarping action for a regular camera", async () => {
    stubBrowserAPIs();
    localStorage.setItem(liveSelectionKey("t1", "u1"), serializeSelection(2, [
      { camera_id: "cam-plain", quality: "sub" }, null, null, null,
    ]));
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-plain", "Plain camera")] }),
      ...emptyCatalogs,
    })));
    renderPage(Live);
    await screen.findAllByText("Plain camera", { selector: ".truncate" });
    expect(screen.queryByRole("button", { name: "Fisheye 360°" })).not.toBeInTheDocument();
  });

  it("consolidates live fisheye actions behind the fish menu and preserves both flows", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-fish", "Fish camera", { tags: ["fisheye"] })] }),
      ...emptyCatalogs,
    })));
    renderPage(Live);
    fireEvent.click(await screen.findByRole("button", { name: /Fish camera/ }));

    const fishAction = await screen.findByRole("button", { name: "Acciones de Fisheye" });
    expect(fishAction.querySelector("svg")).toHaveClass("lucide-fish");
    expect(within(screen.getByLabelText("Cuadro 1")).queryByRole("button", { name: "Configuración de vista" })).not.toBeInTheDocument();
    fireEvent.click(fishAction);
    expect(screen.getByRole("menu")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("menuitem", { name: /Navegación y dewarping Fisheye 360°/ }));
    expect(await screen.findByRole("dialog", { name: /Fisheye 360° Dewarping/ })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Cerrar" }));
    fireEvent.click(screen.getByRole("button", { name: "Acciones de Fisheye" }));
    fireEvent.click(screen.getByRole("menuitem", { name: /Configuración de vista Fisheye/ }));
    expect(await screen.findByRole("dialog", { name: /Configuración de vista Fisheye/ })).toBeInTheDocument();
  });

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

  it("shows the plate list overlay only on LPR cameras that have reads", async () => {
    stubBrowserAPIs();
    const plateRead = { id: "r1", site_id: "s", site_name: "S", server_id: "sv", server_name: "Srv", camera_id: "cam-lpr", camera_name: "Acceso", plate: "AB123CD", plate_normalized: "AB123CD", label: "car", zones: [], seen_at: "2026-10-21T10:00:00Z" };
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-lpr", "Acceso", { lpr: true }), camera("cam-plain", "Patio")] }),
      "/api/v1/lpr/reads": () => json({ items: [plateRead] }),
      ...emptyCatalogs,
    })));
    renderPage(Live);
    fireEvent.click(await screen.findByRole("button", { name: /Acceso/ }));
    await waitFor(() => expect(within(screen.getByLabelText("Cuadro 1")).getByText("Acceso")).toBeInTheDocument());
    expect(await within(screen.getByLabelText("Cuadro 1")).findByTestId("detection-overlay")).toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("Cuadro 2"));
    fireEvent.click(await screen.findByRole("button", { name: /Patio/ }));
    await waitFor(() => expect(within(screen.getByLabelText("Cuadro 2")).getByText("Patio")).toBeInTheDocument());
    expect(within(screen.getByLabelText("Cuadro 2")).queryByTestId("detection-overlay")).not.toBeInTheDocument();
  });

  it("shows no plate list on an LPR camera that has no reads yet", async () => {
    stubBrowserAPIs();
    let asked = 0;
    vi.stubGlobal("fetch", vi.fn(async (input: Request) => {
      if (new URL(input.url).pathname === "/api/v1/lpr/reads") asked++;
      return stubApi({
        "/api/v1/me": meResponse,
        "/api/v1/cameras": () => json({ items: [camera("cam-lpr", "Acceso", { lpr: true })] }),
        "/api/v1/lpr/reads": () => json({ items: [] }),
        ...emptyCatalogs,
      })(input);
    }));
    renderPage(Live);
    fireEvent.click(await screen.findByRole("button", { name: /Acceso/ }));
    await waitFor(() => expect(within(screen.getByLabelText("Cuadro 1")).getByText("Acceso")).toBeInTheDocument());
    await waitFor(() => expect(asked).toBeGreaterThan(0));
    expect(within(screen.getByLabelText("Cuadro 1")).queryByTestId("detection-overlay")).not.toBeInTheDocument();
  });

  describe("face detections on the video", () => {
    const faceEvent = (id: string, name: string, camera_id: string) => ({
      id, tenant_id: "t1", site_id: "s", site_name: "S", server_id: "sv", server_name: "Srv", camera_id, camera_name: "cam",
      remote_id: id, severity: "detection", labels: ["face"], sub_labels: name ? [name] : [], zones: [], plates: [],
      start_time: "2026-10-21T10:05:00Z", reviewed: false, has_thumbnail: false, has_snapshot: false, has_preview: false, tracks: [],
    });
    const plateRead = (camera_id: string) => ({ id: "r1", site_id: "s", site_name: "S", server_id: "sv", server_name: "Srv", camera_id, camera_name: "cam", plate: "AB123CD", plate_normalized: "AB123CD", label: "car", zones: [], seen_at: "2026-10-21T10:00:00Z" });
    const meWithEvents = () => meResponse([
      { permission: "live.view", effect: "allow", scope_type: "platform" },
      { permission: "events.view", effect: "allow", scope_type: "platform" },
    ]);

    it("shows the face list on any camera that has face detections, LPR flag or not", async () => {
      stubBrowserAPIs();
      vi.stubGlobal("fetch", vi.fn(stubApi({
        "/api/v1/me": meWithEvents,
        "/api/v1/cameras": () => json({ items: [camera("cam-hall", "Hall"), camera("cam-yard", "Patio")] }),
        "/api/v1/events": () => json({ items: [faceEvent("e1", "Ana Pérez", "cam-hall")] }),
        ...emptyCatalogs,
      })));
      renderPage(Live);
      fireEvent.click(await screen.findByRole("button", { name: /Hall/ }));
      await waitFor(() => expect(within(screen.getByLabelText("Cuadro 1")).getByText("Hall")).toBeInTheDocument());
      const overlay = await within(screen.getByLabelText("Cuadro 1")).findByTestId("detection-overlay");
      expect(overlay).toHaveAttribute("data-kind", "faces");
      expect(overlay).toHaveTextContent("Ana Pérez");

      fireEvent.click(screen.getByLabelText("Cuadro 2"));
      fireEvent.click(await screen.findByRole("button", { name: /Patio/ }));
      await waitFor(() => expect(within(screen.getByLabelText("Cuadro 2")).getByText("Patio")).toBeInTheDocument());
      expect(within(screen.getByLabelText("Cuadro 2")).queryByTestId("detection-overlay")).not.toBeInTheDocument();
    });

    it("turns the panel into LPR and Rostros tabs on a camera that has both", async () => {
      stubBrowserAPIs();
      vi.stubGlobal("fetch", vi.fn(stubApi({
        "/api/v1/me": meWithEvents,
        "/api/v1/cameras": () => json({ items: [camera("cam-gate", "Acceso", { lpr: true })] }),
        "/api/v1/lpr/reads": () => json({ items: [plateRead("cam-gate")] }),
        "/api/v1/events": () => json({ items: [faceEvent("e1", "Ana Pérez", "cam-gate")] }),
        ...emptyCatalogs,
      })));
      renderPage(Live);
      fireEvent.click(await screen.findByRole("button", { name: /Acceso/ }));
      await waitFor(() => expect(within(screen.getByLabelText("Cuadro 1")).getByText("Acceso")).toBeInTheDocument());
      const cell = screen.getByLabelText("Cuadro 1");
      const tabs = await within(cell).findByRole("tablist", { name: /Tipo de detección/i });
      expect(within(tabs).getAllByRole("tab").map((t) => t.textContent)).toEqual(["LPR", "Rostros"]);
      fireEvent.click(within(tabs).getByRole("tab", { name: /Rostros/ }));
      expect(within(cell).getByRole("list", { name: /Detecciones faciales/i })).toHaveTextContent("Ana Pérez");
    });
  });

  it("flags a camera that streams H.265 right on its tile", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-hevc", "Cementerio", { live_codec: "h265", hq_codec: "h265" }), camera("cam-avc", "Banco", { live_codec: "h264", hq_codec: "h264" }), camera("cam-unknown", "Patio")] }),
      ...emptyCatalogs,
    })));
    renderPage(Live);
    fireEvent.click(await screen.findByRole("button", { name: /Cementerio/ }));
    await waitFor(() => expect(within(screen.getByLabelText("Cuadro 1")).getByText("Cementerio")).toBeInTheDocument());
    const badge = within(screen.getByLabelText("Cuadro 1")).getByTestId("codec-badge");
    expect(badge).toHaveTextContent("H.265");

    fireEvent.click(screen.getByLabelText("Cuadro 2"));
    fireEvent.click(await screen.findByRole("button", { name: /Banco/ }));
    await waitFor(() => expect(within(screen.getByLabelText("Cuadro 2")).getByText("Banco")).toBeInTheDocument());
    expect(within(screen.getByLabelText("Cuadro 2")).queryByTestId("codec-badge")).not.toBeInTheDocument();

    fireEvent.click(screen.getByLabelText("Cuadro 3"));
    fireEvent.click(await screen.findByRole("button", { name: /Patio/ }));
    await waitFor(() => expect(within(screen.getByLabelText("Cuadro 3")).getByText("Patio")).toBeInTheDocument());
    expect(within(screen.getByLabelText("Cuadro 3")).queryByTestId("codec-badge")).not.toBeInTheDocument();
  });

  it("lets the whole site be dragged onto the grid, with a hint saying so", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
      "/api/v1/sites": () => json({ items: [{ id: "s1", tenant_id: "t1", name: "Campus" }] }),
      "/api/v1/servers": () => json({ items: [{ id: "srv1", tenant_id: "t1", site_id: "s1", name: "Frigate A", status: "online" }] }),
      "/api/v1/views": () => json({ items: [] }),
    })));
    renderPage(Live);
    const site = await screen.findByRole("button", { name: /Campus/ });
    expect(site).toHaveAttribute("aria-roledescription", "draggable");
    expect(site).toHaveAttribute("title", expect.stringMatching(/Campus.*Arrastrá a la grilla.*todos sus servidores/));
    // Folding the site still works with a click.
    const open = site.getAttribute("aria-expanded");
    fireEvent.click(site);
    expect(site.getAttribute("aria-expanded")).not.toBe(open);
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

    await waitFor(() => expect(within(screen.getByLabelText("Cuadro 1")).getByText("North")).toBeInTheDocument());
    const placedTile = screen.getByLabelText("Cuadro 1");
    const nameEl = within(placedTile).getByText("North");
    expect(nameEl).toBeInTheDocument();
    const overlay = nameEl.closest(".absolute");
    expect(overlay?.className).toContain("z-[3]");

    const video = placedTile.querySelector("video");
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
    await waitFor(() => expect(screen.getByLabelText("Cuadro 1").querySelector("video")).not.toBeNull());
    const tile = screen.getByLabelText("Cuadro 1");
    const video = tile.querySelector("video");
    const socketCount = FakeSocket.created;
    fireEvent.click(screen.getByRole("button", { name: /^Campus/ }));

    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBe(video);
    expect(FakeSocket.created).toBe(socketCount);
  });

  it("marks cameras in the live grid bold and offline cameras gray in the sidebar", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-online", "Online camera"), camera("cam-offline", "Offline camera", { status: "offline" })] }),
      "/api/v1/sites": () => json({ items: [{ id: "s1", tenant_id: "t1", name: "Campus" }] }),
      "/api/v1/servers": () => json({ items: [{ id: "srv1", tenant_id: "t1", site_id: "s1", name: "Frigate A", status: "online" }] }),
      "/api/v1/views": () => json({ items: [] }),
    })));
    renderPage(Live);

    const online = await screen.findByRole("button", { name: /Online camera/ });
    const offline = await screen.findByRole("button", { name: /Offline camera/ });
    expect(online.querySelector("span.min-w-0.truncate")).not.toHaveClass("font-bold");
    expect(offline.querySelector("span.min-w-0.truncate")).toHaveClass("text-on-surface-variant");

    fireEvent.click(online);
    await waitFor(() => expect(online.querySelector("span.min-w-0.truncate")).toHaveClass("font-bold"));
    expect(offline.querySelector("span.min-w-0.truncate")).not.toHaveClass("font-bold");

    fireEvent.click(offline);
    await waitFor(() => expect(offline.querySelector("span.min-w-0.truncate")).toHaveClass("font-bold"));
    expect(offline.querySelector("span.min-w-0.truncate")).toHaveClass("text-on-surface-variant");
  });

  it("uses a fish icon for fisheye cameras and keeps view disclosure at the row's right edge", async () => {
    stubBrowserAPIs();
    saveFisheyeConfig({
      cameraId: "cam-fish",
      enabled: true,
      lensType: "fisheye_360",
      mountType: "ceiling",
      projectionModel: "equidistant",
      calibration: DEFAULT_FISHEYE_CALIBRATION,
      views: [],
      defaultMode: "3_fixed_120",
    });
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-fish", "Fisheye camera")] }),
      "/api/v1/sites": () => json({ items: [{ id: "s1", tenant_id: "t1", name: "Campus" }] }),
      "/api/v1/servers": () => json({ items: [{ id: "srv1", tenant_id: "t1", site_id: "s1", name: "Frigate A", status: "online" }] }),
      "/api/v1/views": () => json({ items: [] }),
    })));
    renderPage(Live);

    const cameraName = await screen.findByText("Fisheye camera");
    const rowButton = cameraName.closest("button");
    expect(rowButton).not.toBeNull();
    expect(rowButton?.querySelector("svg.lucide-fish")).toBeInTheDocument();
    expect(rowButton).toHaveAttribute("title");
    const disclosure = screen.getByRole("button", { name: "Vistas virtuales de Fisheye camera" });
    expect(disclosure).toHaveAttribute("aria-expanded", "true");
    expect(disclosure).toHaveAttribute("title", "Ocultar vistas virtuales");
    expect(disclosure.parentElement?.lastElementChild).toBe(disclosure);
    fireEvent.click(disclosure);
    expect(disclosure).toHaveAttribute("aria-expanded", "false");
  });

  it("updates an explorer row when its fisheye config is saved after render", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-late", "Late fisheye"), camera("cam-other", "Other camera")] }),
      "/api/v1/sites": () => json({ items: [{ id: "s1", tenant_id: "t1", name: "Campus" }] }),
      "/api/v1/servers": () => json({ items: [{ id: "srv1", tenant_id: "t1", site_id: "s1", name: "Frigate A", status: "online" }] }),
      "/api/v1/views": () => json({ items: [] }),
    })));
    renderPage(Live);

    const row = (await screen.findByText("Late fisheye")).closest("button");
    expect(row?.querySelector("svg.lucide-fish")).toBeNull();
    expect(screen.queryByRole("button", { name: "Vistas virtuales de Late fisheye" })).toBeNull();

    act(() => {
      saveFisheyeConfig({
        cameraId: "cam-late",
        enabled: true,
        lensType: "fisheye_360",
        mountType: "ceiling",
        projectionModel: "equidistant",
        calibration: DEFAULT_FISHEYE_CALIBRATION,
        views: [],
        defaultMode: "3_fixed_120",
      });
    });

    await waitFor(() => expect(screen.getByText("Late fisheye").closest("button")?.querySelector("svg.lucide-fish")).toBeInTheDocument());
    expect(screen.getByRole("button", { name: "Vistas virtuales de Late fisheye" })).toBeInTheDocument();
    expect(screen.getByText("Other camera").closest("button")?.querySelector("svg.lucide-fish")).toBeNull();
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

  it("empties restored fisheye tiles whose saved view no longer exists", async () => {
    stubBrowserAPIs();
    saveFisheyeConfig({
      ...DEFAULT_FISHEYE_CONFIG,
      cameraId: "cam-fe-stale",
      enabled: true,
      lensType: "fisheye_360",
      views: [{ id: "v-live", cameraId: "cam-fe-stale", name: "Vista viva", projection: "rectilinear", yaw: 0, pitch: 0, roll: 0, fov: 100 }],
    });
    localStorage.setItem(
      liveSelectionKey("t1", "u1"),
      serializeSelection(2, [
        { camera_id: "cam-fe-stale", quality: "sub", fisheye_view_id: "v-live" },
        { camera_id: "cam-fe-stale", quality: "sub", fisheye_view_id: "v-removed-quadrant" },
        null,
        null,
      ]),
    );
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-fe-stale", "Fish stale")] }),
      ...emptyCatalogs,
    })));

    renderPage(Live);

    await screen.findAllByText(/Vista viva/);
    expect(screen.getAllByText("Vacío")).toHaveLength(3);
    await waitFor(() => {
      const saved = parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-fe-stale"]));
      expect(saved?.tiles[1]).toBeNull();
    });
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

  it("exposes a quality control beside a camera that is already in the grid", async () => {
    stubBrowserAPIs();
    localStorage.setItem(liveSelectionKey("t1", "u1"), serializeSelection(2, [
      { camera_id: "cam-1", quality: "sub" }, null, null, null,
    ]));
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
      ...emptyCatalogs,
    })));

    renderPage(Live);

    expect(await screen.findByRole("button", { name: "Calidad de Puerta norte" })).toBeInTheDocument();
  });

  it("keeps the old frame through a temporary main-quality handoff and restores each saved tile quality", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
      ...emptyCatalogs,
    })));

    renderPage(Live);
    fireEvent.click(await screen.findByText("Puerta norte"));
    await screen.findByRole("button", { name: "Calidad de Puerta norte" });
    const tile = await screen.findByLabelText("Cuadro 1");
    await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
    vi.useFakeTimers();

    fireEvent.click(screen.getByRole("button", { name: "Calidad de Puerta norte" }));
    fireEvent.click(screen.getByRole("button", { name: "Principal por 30 s" }));

    const targetSurface = tile.querySelector('[data-testid="quality-target-surface"]');
    expect(targetSurface).toBeInTheDocument();
    expect(targetSurface).toHaveClass("opacity-0");
    expect(targetSurface?.querySelector('[data-testid="player-status"]')).not.toBeInTheDocument();
    const videos = tile.querySelectorAll("video");
    expect(videos).toHaveLength(2);
    expect(FakeSocket.urls.some((url) => url.includes("quality=sub"))).toBe(true);
    expect(FakeSocket.urls.some((url) => url.includes("quality=main"))).toBe(true);

    Object.defineProperty(videos[0], "videoWidth", { configurable: true, value: 640 });
    fireEvent.loadedData(videos[0]!);
    expect(videos[0]).toHaveStyle({ opacity: "1" });
    expect(videos[1]).toHaveStyle({ opacity: "0" });
    Object.defineProperty(videos[1], "readyState", { configurable: true, value: 2 });
    Object.defineProperty(videos[1], "videoWidth", { configurable: true, value: 640 });
    fireEvent.loadedData(videos[1]!);
    expect(targetSurface).toHaveClass("opacity-100");
    expect(videos[0]).toHaveStyle({ opacity: "1" });
    expect(videos[1]).toHaveStyle({ opacity: "1" });

    const savedDuringOverride = parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-1"]));
    expect(savedDuringOverride?.tiles[0]).toEqual({ camera_id: "cam-1", quality: "sub" });
    act(() => { vi.advanceTimersByTime(29_999); });
    expect(tile.querySelectorAll("video")).toHaveLength(2);
    act(() => { vi.advanceTimersByTime(1); });
    expect(tile.querySelectorAll("video")).toHaveLength(1);
    const savedAfterRestore = parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-1"]));
    expect(savedAfterRestore?.tiles[0]).toEqual({ camera_id: "cam-1", quality: "sub" });
  });

  it("keeps the external target surface hidden until its first drawable frame", async () => {
    localStorage.setItem(FEATURES_OVERRIDE_KEY, JSON.stringify({ persistentPlayers: true, videoSurfaceLayer: true }));
    stubBrowserAPIs();
    const originalRect = HTMLElement.prototype.getBoundingClientRect;
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      if (this.hasAttribute("data-surface-slot")) {
        return { x: 0, y: 0, top: 0, left: 0, right: 640, bottom: 360, width: 640, height: 360, toJSON: () => ({}) } as DOMRect;
      }
      return originalRect.call(this);
    });
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
      ...emptyCatalogs,
    })));
    const LiveWithSurfaceSessions = () => (
      <PlayerSessionProvider userId="u1">
        <VideoSurfaceLayer><Live /></VideoSurfaceLayer>
      </PlayerSessionProvider>
    );

    renderPage(LiveWithSurfaceSessions);
    fireEvent.click(await screen.findByText("Puerta norte"));
    await screen.findByRole("button", { name: "Calidad de Puerta norte" });
    await waitFor(() => expect(FakeSocket.instances).toHaveLength(1));
    act(() => FakeSocket.instances[0]?.onmessage?.({ data: new ArrayBuffer(1) }));
    fireEvent.click(screen.getByRole("button", { name: "Calidad de Puerta norte" }));
    fireEvent.click(screen.getByRole("button", { name: "Principal por 30 s" }));
    await waitFor(() => expect(FakeSocket.instances).toHaveLength(2));
    act(() => FakeSocket.instances[1]?.onmessage?.({ data: new ArrayBuffer(1) }));

    const wrappers = await waitFor(() => {
      const nodes = screen.getByTestId("video-surface-layer").querySelectorAll('[data-video-surface="cam-1"]');
      expect(nodes).toHaveLength(2);
      return nodes;
    });
    expect((wrappers[1] as HTMLElement).style.visibility).toBe("hidden");

    const targetVideo = wrappers[1]?.querySelector("video");
    expect(targetVideo).not.toBeNull();
    Object.defineProperty(targetVideo, "readyState", { configurable: true, value: 2 });
    Object.defineProperty(targetVideo, "videoWidth", { configurable: true, value: 640 });
    fireEvent.loadedData(targetVideo!);
    expect((wrappers[1] as HTMLElement).style.visibility).toBe("visible");
  });

  it("promotes all active fisheye grid identities and keeps their saved qualities unchanged", async () => {
    stubBrowserAPIs();
    saveFisheyeConfig({
      cameraId: "cam-fe-quality",
      enabled: true,
      lensType: "fisheye_360",
      mountType: "ceiling",
      projectionModel: "equidistant",
      calibration: DEFAULT_FISHEYE_CALIBRATION,
      views: [
        { id: "v-a", cameraId: "cam-fe-quality", name: "View A", projection: "rectilinear", yaw: 0, pitch: 0, roll: 0, fov: 120 },
        { id: "v-b", cameraId: "cam-fe-quality", name: "View B", projection: "rectilinear", yaw: 120, pitch: 0, roll: 0, fov: 120 },
      ],
      defaultMode: "3_fixed_120",
    });
    localStorage.setItem(liveSelectionKey("t1", "u1"), serializeSelection(2, [
      { camera_id: "cam-fe-quality", quality: "sub" },
      { camera_id: "cam-fe-quality", quality: "sub", fisheye_view_id: "v-a" },
      { camera_id: "cam-fe-quality", quality: "main", fisheye_view_id: "v-b" },
      null,
    ]));
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-fe-quality", "Fish quality")] }),
      ...emptyCatalogs,
    })));

    renderPage(Live);
    await screen.findByRole("button", { name: "Calidad de Fish quality" });
    vi.useFakeTimers();

    const mainSessionsBefore = FakeSocket.urls.filter((url) => url.includes("quality=main")).length;
    fireEvent.click(screen.getByRole("button", { name: "Calidad de Fish quality" }));
    fireEvent.click(screen.getByRole("button", { name: "Principal por 30 s" }));

    const mainSessionsAfter = FakeSocket.urls.filter((url) => url.includes("quality=main")).length;
    expect(mainSessionsAfter - mainSessionsBefore).toBe(2);
    expect(FakeSocket.urls.some((url) => url.includes("quality=sub"))).toBe(true);
    const saved = parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-fe-quality"]));
    expect(saved?.tiles.slice(0, 3).map((tile) => tile && "quality" in tile ? tile.quality : null)).toEqual(["sub", "sub", "main"]);
  });

  it("promotes to main immediately on maximize and restores sub on unmaximize", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
      ...emptyCatalogs,
    })));

    renderPage(Live);
    fireEvent.click(await screen.findByText("Puerta norte"));
    await screen.findByRole("button", { name: "Calidad de Puerta norte" });
    const tile = await screen.findByLabelText("Cuadro 1");
    await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
    vi.useFakeTimers();
    fireEvent.doubleClick(tile);

    expect(FakeSocket.urls.some((url) => url.includes("quality=main"))).toBe(true);

    fireEvent.doubleClick(tile);
    expect(tile.querySelectorAll("video")).toHaveLength(1);
    expect(parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-1"]))?.tiles[0]).toEqual({ camera_id: "cam-1", quality: "sub" });
    act(() => { vi.advanceTimersByTime(10_000); });
    expect(FakeSocket.urls.filter((url) => url.includes("quality=main"))).toHaveLength(1);
  });

  it("keeps a tile saved on main at a single main session after maximize ends", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte", { default_live_quality: "main" })] }),
      ...emptyCatalogs,
    })));

    renderPage(Live);
    fireEvent.click(await screen.findByText("Puerta norte"));
    await screen.findByRole("button", { name: "Calidad de Puerta norte" });
    const tile = await screen.findByLabelText("Cuadro 1");
    await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
    vi.useFakeTimers();

    fireEvent.doubleClick(tile);
    fireEvent.doubleClick(tile);
    act(() => { vi.advanceTimersByTime(10_000); });

    expect(tile.querySelectorAll("video")).toHaveLength(1);
    expect(parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-1"]))?.tiles[0]).toEqual({ camera_id: "cam-1", quality: "main" });
  });

  describe.each([{ persistent: false }, { persistent: true }])("quality handoff keeps the base player mounted (persistentPlayers=$persistent)", ({ persistent }) => {
    it("never closes or reopens the base sub socket across maximize promotion and unmaximize, and releases main afterwards", async () => {
      if (persistent) localStorage.setItem(FEATURES_OVERRIDE_KEY, JSON.stringify({ persistentPlayers: true }));
      stubBrowserAPIs();
      vi.stubGlobal("fetch", vi.fn(stubApi({
        "/api/v1/me": meResponse,
        "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
        ...emptyCatalogs,
      })));

      renderPage(() => (
        <PlayerSessionProvider userId="u1">
          <Live />
        </PlayerSessionProvider>
      ));
      fireEvent.click(await screen.findByText("Puerta norte"));
      await screen.findByRole("button", { name: "Calidad de Puerta norte" });
      const tile = await screen.findByLabelText("Cuadro 1");
      await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
      await waitFor(() => expect(FakeSocket.urls.filter((url) => url.includes("quality=sub"))).toHaveLength(1));
      const subSocket = FakeSocket.instances.find((socket) => socket.url.includes("quality=sub"))!;
      const closeSub = vi.spyOn(subSocket, "close");
      const baseVideo = tile.querySelector("video")!;
      // A persistent manager can keep the same socket and <video> alive across a React remount,
      // so count sub-session acquisitions and releases to observe the base player mount itself.
      const subLifecycle = (method: unknown[][]) => method.filter(([cameraId, quality]) => cameraId === "cam-1" && quality === "sub").length;
      const acquire = vi.spyOn(PlayerSessionManager.prototype, "acquire");
      const release = vi.spyOn(PlayerSessionManager.prototype, "release");
      vi.useFakeTimers();

      fireEvent.doubleClick(tile);
      expect(subLifecycle(acquire.mock.calls)).toBe(0);
      expect(subLifecycle(release.mock.calls)).toBe(0);
      expect(baseVideo.isConnected).toBe(true);
      expect(tile.querySelector('[data-testid="quality-target-surface"]')).toHaveAttribute("data-quality", "main");
      expect(closeSub).not.toHaveBeenCalled();
      expect(FakeSocket.urls.filter((url) => url.includes("quality=sub"))).toHaveLength(1);
      const mainSocket = FakeSocket.instances.find((socket) => socket.url.includes("quality=main"));
      expect(mainSocket).toBeDefined();
      const closeMain = vi.spyOn(mainSocket!, "close");

      fireEvent.doubleClick(tile);
      expect(tile.querySelector('[data-testid="quality-target-surface"]')).toBeNull();
      expect(tile.querySelectorAll("video")).toHaveLength(1);
      expect(baseVideo.isConnected).toBe(true);
      expect(subLifecycle(acquire.mock.calls)).toBe(0);
      expect(subLifecycle(release.mock.calls)).toBe(0);
      expect(closeSub).not.toHaveBeenCalled();
      expect(FakeSocket.urls.filter((url) => url.includes("quality=sub"))).toHaveLength(1);

      // The main session is discarded and closed immediately on unmaximize in both modes (spec §10).
      expect(closeMain).toHaveBeenCalledTimes(1);
      expect(closeSub).not.toHaveBeenCalled();
      expect(FakeSocket.urls.filter((url) => url.includes("quality=sub"))).toHaveLength(1);
      expect(FakeSocket.urls.filter((url) => url.includes("quality=main"))).toHaveLength(1);
      expect(parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-1"]))?.tiles[0]).toEqual({ camera_id: "cam-1", quality: "sub" });
    });
  });

  describe.each([{ persistent: false }, { persistent: true }])("tile quality badge (persistentPlayers=$persistent)", ({ persistent }) => {
    it("shows SUB, then the handoff, then MAIN once the target has a drawable frame, then SUB again", async () => {
      if (persistent) localStorage.setItem(FEATURES_OVERRIDE_KEY, JSON.stringify({ persistentPlayers: true }));
      stubBrowserAPIs();
      vi.stubGlobal("fetch", vi.fn(stubApi({
        "/api/v1/me": meResponse,
        "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
        ...emptyCatalogs,
      })));
      renderPage(() => (
        <PlayerSessionProvider userId="u1">
          <Live />
        </PlayerSessionProvider>
      ));
      fireEvent.click(await screen.findByText("Puerta norte"));
      await screen.findByRole("button", { name: "Calidad de Puerta norte" });
      const tile = await screen.findByLabelText("Cuadro 1");
      await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
      const badge = () => within(tile).getByTestId("quality-badge");
      expect(badge()).toHaveTextContent("SUB");
      expect(badge()).toHaveAttribute("aria-label", "Calidad: secundaria");

      fireEvent.doubleClick(tile);
      expect(badge()).toHaveTextContent("SUB → MAIN");
      expect(badge()).toHaveAttribute("data-quality", "switching");

      const targetVideo = tile.querySelector('[data-testid="quality-target-surface"] video') as HTMLVideoElement;
      Object.defineProperty(targetVideo, "readyState", { value: 4, configurable: true });
      Object.defineProperty(targetVideo, "videoWidth", { value: 1280, configurable: true });
      act(() => { targetVideo.dispatchEvent(new Event("loadeddata")); });
      await waitFor(() => expect(badge()).toHaveTextContent(/^MAIN$/));
      expect(badge()).toHaveAttribute("aria-label", "Calidad: principal");

      fireEvent.doubleClick(tile);
      expect(badge()).toHaveTextContent(/^SUB$/);
    });
  });

  it("shows the quality badge on a fisheye tile and the handoff while main is requested", async () => {
    stubBrowserAPIs();
    localStorage.setItem(liveSelectionKey("t1", "u1"), serializeSelection(2, [
      { camera_id: "cam-fish", quality: "sub" }, null, null, null,
    ]));
    saveFisheyeConfig({
      cameraId: "cam-fish",
      enabled: true,
      lensType: "fisheye_360",
      mountType: "ceiling",
      projectionModel: "equidistant",
      calibration: DEFAULT_FISHEYE_CALIBRATION,
      views: [],
      defaultMode: "virtual_ptz",
    });
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-fish", "Fisheye camera")] }),
      ...emptyCatalogs,
    })));
    renderPage(Live);
    await screen.findAllByText("Fisheye camera", { selector: ".truncate" });
    const tile = await screen.findByLabelText("Cuadro 1");
    expect(await within(tile).findByTestId("quality-badge")).toHaveTextContent("SUB");
    fireEvent.doubleClick(tile);
    expect(within(tile).getByTestId("quality-badge")).toHaveTextContent("SUB → MAIN");
  });

  describe.each([{ persistent: false }, { persistent: true }])("digital zoom belongs to the tile across the quality handoff (persistentPlayers=$persistent)", ({ persistent }) => {
    it("applies the same zoom to the main target layer as to the zoomed sub layer", async () => {
      if (persistent) localStorage.setItem(FEATURES_OVERRIDE_KEY, JSON.stringify({ persistentPlayers: true }));
      stubBrowserAPIs();
      vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
        x: 0, y: 0, left: 0, top: 0, right: 400, bottom: 300, width: 400, height: 300, toJSON: () => ({}),
      } as DOMRect);
      vi.stubGlobal("fetch", vi.fn(stubApi({
        "/api/v1/me": meResponse,
        "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
        ...emptyCatalogs,
      })));
      renderPage(() => (
        <PlayerSessionProvider userId="u1">
          <Live />
        </PlayerSessionProvider>
      ));
      fireEvent.click(await screen.findByText("Puerta norte"));
      await screen.findByRole("button", { name: "Calidad de Puerta norte" });
      const tile = await screen.findByLabelText("Cuadro 1");
      await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
      const baseStage = tile.querySelector("video")!.parentElement!;
      const baseFrame = baseStage.parentElement!;

      fireEvent.wheel(baseFrame, { deltaY: -300, clientX: 200, clientY: 150 });
      await waitFor(() => expect(baseStage.style.transform).toContain("scale("));
      const zoomed = baseStage.style.transform;

      fireEvent.doubleClick(tile);
      const target = tile.querySelector('[data-testid="quality-target-surface"]')!;
      await waitFor(() => expect(target.querySelector("video")).not.toBeNull());
      expect((target.querySelector("video")!.parentElement as HTMLElement).style.transform).toBe(zoomed);

      // Zooming further while the target is still loading moves both layers together.
      fireEvent.wheel(baseFrame, { deltaY: -300, clientX: 200, clientY: 150 });
      await waitFor(() => expect(baseStage.style.transform).not.toBe(zoomed));
      expect((target.querySelector("video")!.parentElement as HTMLElement).style.transform).toBe(baseStage.style.transform);

      // Back to the grid: the sub layer keeps the tile zoom (it is not reset by the handoff).
      const expected = baseStage.style.transform;
      fireEvent.doubleClick(tile);
      expect(tile.querySelector('[data-testid="quality-target-surface"]')).toBeNull();
      expect(baseStage.style.transform).toBe(expected);
    });
  });

  it("keeps a manual thirty-second override alive when maximize ends", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
      ...emptyCatalogs,
    })));

    renderPage(Live);
    fireEvent.click(await screen.findByText("Puerta norte"));
    await screen.findByRole("button", { name: "Calidad de Puerta norte" });
    const tile = await screen.findByLabelText("Cuadro 1");
    await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
    vi.useFakeTimers();

    fireEvent.doubleClick(tile);
    fireEvent.click(screen.getByRole("button", { name: "Calidad de Puerta norte" }));
    fireEvent.click(screen.getByRole("button", { name: "Principal por 30 s" }));
    fireEvent.doubleClick(tile);
    act(() => { vi.advanceTimersByTime(30_000); });

    expect(FakeSocket.urls.some((url) => url.includes("quality=main"))).toBe(true);
    expect(tile.querySelectorAll("video")).toHaveLength(1);
  });

  it("keeps main after a manual window expires while the camera remains maximized", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
      ...emptyCatalogs,
    })));

    renderPage(Live);
    fireEvent.click(await screen.findByText("Puerta norte"));
    await screen.findByRole("button", { name: "Calidad de Puerta norte" });
    const tile = await screen.findByLabelText("Cuadro 1");
    await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
    vi.useFakeTimers();

    fireEvent.doubleClick(tile);
    fireEvent.click(screen.getByRole("button", { name: "Calidad de Puerta norte" }));
    fireEvent.click(screen.getByRole("button", { name: "Principal por 30 s" }));
    act(() => { vi.advanceTimersByTime(30_000); });

    expect(tile.querySelectorAll("video")).toHaveLength(2);
    fireEvent.doubleClick(tile);
    expect(tile.querySelectorAll("video")).toHaveLength(1);
  });

  it("drops a temporary main override when its tile is closed and keeps the replacement camera on its own quality", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte"), camera("cam-2", "Porton sur")] }),
      ...emptyCatalogs,
    })));

    renderPage(Live);
    fireEvent.click(await screen.findByText("Puerta norte"));
    await screen.findByRole("button", { name: "Calidad de Puerta norte" });
    const tile = await screen.findByLabelText("Cuadro 1");
    await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
    vi.useFakeTimers();

    fireEvent.click(screen.getByRole("button", { name: "Calidad de Puerta norte" }));
    fireEvent.click(screen.getByRole("button", { name: "Principal por 30 s" }));
    expect(tile.querySelectorAll("video")).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: "Quitar Puerta norte" }));
    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBeNull();
    fireEvent.click(screen.getByLabelText("Cuadro 1"));
    fireEvent.click(screen.getByText("Porton sur"));
    act(() => { vi.advanceTimersByTime(500); });
    const mainBefore = FakeSocket.urls.filter((url) => url.includes("quality=main")).length;
    const replaced = screen.getByLabelText("Cuadro 1");
    expect(replaced.textContent).toContain("Porton sur");
    expect(replaced.querySelectorAll("video")).toHaveLength(1);

    act(() => { vi.advanceTimersByTime(60_000); });
    expect(FakeSocket.urls.filter((url) => url.includes("quality=main"))).toHaveLength(mainBefore);
    expect(screen.getByLabelText("Cuadro 1").querySelectorAll("video")).toHaveLength(1);
    expect(parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-1", "cam-2"]))?.tiles[0]).toEqual({ camera_id: "cam-2", quality: "sub" });
  });

  it("still promotes to main on maximize after a manually overridden camera was removed and placed again", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
      ...emptyCatalogs,
    })));

    renderPage(Live);
    fireEvent.click(await screen.findByText("Puerta norte"));
    await screen.findByRole("button", { name: "Calidad de Puerta norte" });
    const tile = await screen.findByLabelText("Cuadro 1");
    await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
    vi.useFakeTimers();

    fireEvent.click(screen.getByRole("button", { name: "Calidad de Puerta norte" }));
    fireEvent.click(screen.getByRole("button", { name: "Principal por 30 s" }));
    fireEvent.click(screen.getByRole("button", { name: "Quitar Puerta norte" }));
    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBeNull();
    act(() => { vi.advanceTimersByTime(500); });

    fireEvent.click(screen.getByLabelText("Cuadro 1"));
    fireEvent.click(screen.getByText("Puerta norte"));
    act(() => { vi.advanceTimersByTime(500); });
    const replaced = screen.getByLabelText("Cuadro 1");
    expect(replaced.querySelectorAll("video")).toHaveLength(1);

    fireEvent.doubleClick(replaced);
    expect(screen.getByLabelText("Cuadro 1").querySelectorAll("video")).toHaveLength(2);
  });

  it("leaves no pending quality timers after unmounting with a manual override and a maximize dwell", async () => {
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
      ...emptyCatalogs,
    })));
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    const { unmount } = renderPage(Live);
    fireEvent.click(await screen.findByText("Puerta norte"));
    await screen.findByRole("button", { name: "Calidad de Puerta norte" });
    const tile = await screen.findByLabelText("Cuadro 1");
    await waitFor(() => expect(tile.querySelector("video")).not.toBeNull());
    vi.useFakeTimers();
    const baseline = vi.getTimerCount();

    fireEvent.click(screen.getByRole("button", { name: "Calidad de Puerta norte" }));
    fireEvent.click(screen.getByRole("button", { name: "Principal por 30 s" }));
    fireEvent.doubleClick(tile);
    expect(vi.getTimerCount()).toBeGreaterThan(baseline);

    unmount();
    const urlsAfterUnmount = FakeSocket.urls.length;
    act(() => { vi.advanceTimersByTime(60_000); });
    expect(FakeSocket.urls).toHaveLength(urlsAfterUnmount);
    expect(errorSpy).not.toHaveBeenCalled();
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

    const item = await screen.findByRole("button", { name: /^Perímetro$/ });
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
    expect(screen.getByRole("button", { name: /^Perímetro$/ })).toBeInTheDocument();
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
    expect(screen.getAllByRole("menuitem").map((item) => item.textContent?.trim()).slice(0, 9)).toEqual(["1", "3", "2×2", "3×3", "4×4", "5×5", "6×6", "8×6", "8×8"]);
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
    const view = renderPage(LiveWithSessions);
    fireEvent.click(await screen.findByRole("button", { name: /Puerta norte/ }));
    fireEvent.click(await screen.findByRole("button", { name: /Porton sur/ }));
    // Both sessions open asynchronously: wait for both pictures and both sockets, or the
    // connection counts below race the second camera's connect.
    await waitFor(() => {
      expect(screen.getByLabelText("Cuadro 1").querySelector("video")).not.toBeNull();
      expect(screen.getByLabelText("Cuadro 2").querySelector("video")).not.toBeNull();
      expect(FakeSocket.created).toBe(2);
    });
    return view;
  }

  it("keeps the telemetry overlay out of the grid by default and registers the sidebar meter consumer", async () => {
    const { unmount } = await renderPersistent();
    expect(screen.queryByRole("group", { name: "Métricas del videowall" })).not.toBeInTheDocument();
    expect(telemetryRuntime.consumers()).toBe(1);
    unmount();
    expect(telemetryRuntime.consumers()).toBe(0);
  });

  it("shows the telemetry overlay beside the grid, without taking a tile", async () => {
    localStorage.setItem("openvms.live.telemetryOverlay", "1");
    const { unmount } = await renderPersistent();
    const overlay = screen.getByRole("group", { name: "Métricas del videowall" });
    const grid = screen.getByRole("group", { name: "Grilla de video" });
    expect(grid.contains(overlay)).toBe(false);
    expect(overlay.parentElement).toBe(grid.parentElement);
    expect(screen.getAllByLabelText(/^Cuadro \d+$/)).toHaveLength(4);
    expect(telemetryRuntime.consumers()).toBe(2);
    unmount();
    expect(telemetryRuntime.consumers()).toBe(0);
  });

  it("reports the visible cameras to the telemetry runtime while mounted", async () => {
    const spy = vi.spyOn(telemetryRuntime, "setVisibleCameras");
    const { unmount } = await renderPersistent();
    expect(spy).toHaveBeenLastCalledWith(2);
    unmount();
    expect(spy).toHaveBeenLastCalledWith(0);
  });

  it("closes all streams without changing the selected grid dimensions", async () => {
    await renderPersistent();
    const closedBefore = FakeSocket.closed;
    const grid = screen.getByRole("group", { name: "Grilla de video" });
    const initialColumns = getComputedStyle(grid).gridTemplateColumns;
    const initialRows = grid.style.getPropertyValue("--grid-rows");

    fireEvent.click(screen.getByRole("button", { name: "Cerrar todas las cámaras" }));

    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBeNull();
    expect(screen.getByLabelText("Cuadro 2").querySelector("video")).toBeNull();
    await waitFor(() => expect(FakeSocket.closed).toBeGreaterThan(closedBefore));
    expect(getComputedStyle(grid).gridTemplateColumns).toBe(initialColumns);
    expect(grid.style.getPropertyValue("--grid-rows")).toBe(initialRows);
    await waitFor(() => {
      const saved = parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-1", "cam-2"]));
      expect(saved?.tiles).toEqual([null, null, null, null]);
    });
  });

  it("keeps the final tile closed in the persisted selection", async () => {
    localStorage.setItem(FEATURES_OVERRIDE_KEY, JSON.stringify({ persistentPlayers: true }));
    localStorage.setItem(liveSelectionKey("t1", "u1"), serializeSelection(2, [
      { camera_id: "cam-1", quality: "sub" }, null, null, null,
    ]));
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte"), camera("cam-2", "Porton sur")] }),
      ...emptyCatalogs,
    })));
    renderPage(LiveWithSessions);
    fireEvent.click(await screen.findByRole("button", { name: "Quitar Puerta norte" }));

    await waitFor(() => {
      const saved = parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-1", "cam-2"]));
      expect(saved?.tiles).toEqual([null, null, null, null]);
    });
    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBeNull();
  });

  it("does not restore camera A after replacing it with B and closing B", async () => {
    localStorage.setItem(liveSelectionKey("t1", "u1"), serializeSelection(2, [
      { camera_id: "cam-1", quality: "sub" }, null, null, null,
    ]));
    playerMetrics.reset();
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte"), camera("cam-2", "Porton sur")] }),
      ...emptyCatalogs,
    })));
    const { unmount } = renderPage(LiveWithSessions);
    await screen.findByRole("button", { name: "Quitar Puerta norte" });
    const closedBeforeReplace = FakeSocket.closed;
    fireEvent.click(within(screen.getByTestId("live-edge")).getByRole("button", { name: /Porton sur/ }));
    await waitFor(() => expect(screen.getByLabelText("Cuadro 1").textContent).toContain("Porton sur"));
    await waitFor(() => expect(FakeSocket.closed).toBeGreaterThan(closedBeforeReplace));
    fireEvent.click(screen.getByRole("button", { name: "Quitar Porton sur" }));
    await waitFor(() => {
      const saved = parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-1", "cam-2"]));
      expect(saved?.tiles).toEqual([null, null, null, null]);
    });

    unmount();
    renderPage(LiveWithSessions);
    await waitFor(() => expect(screen.getByLabelText("Cuadro 1").textContent).toContain("Vacío"));
    expect(screen.getByLabelText("Cuadro 1").querySelector("video")).toBeNull();
  });

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
    // Maximize now promotes to main immediately: exactly one extra (main) socket, no sub reconnects.
    expect(FakeSocket.created).toBe(3);
    expect(FakeSocket.urls.filter((url) => url.includes("quality=sub"))).toHaveLength(2);
    expect(FakeSocket.urls.filter((url) => url.includes("quality=main"))).toHaveLength(1);
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

  it("offers the 5×5, 6×6, 8×6 and 8×8 presentations from the layout menu", async () => {
    await renderPersistent();
    fireEvent.click(screen.getByRole("button", { name: "Presentación" }));
    expect(screen.getByRole("menuitem", { name: "5×5" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "6×6" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "8×6" })).toBeInTheDocument();
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

  it("populates empty grid with real view and 3 fixed views when fisheye camera is placed", async () => {
    stubBrowserAPIs();
    saveFisheyeConfig({
      cameraId: "cam-fe-1",
      enabled: true,
      lensType: "fisheye_360",
      mountType: "ceiling",
      projectionModel: "equidistant",
      calibration: DEFAULT_FISHEYE_CALIBRATION,
      views: [],
      defaultMode: "3_fixed_120",
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () =>
            json({ items: [camera("cam-fe-1", "Fisheye Hall")] }),
          ...emptyCatalogs,
        }),
      ),
    );

    renderPage(Live);

    const camBtn = await screen.findByText("Fisheye Hall");
    fireEvent.doubleClick(camBtn);

    await waitFor(() => {
      const saved = parseSelection(localStorage.getItem(liveSelectionKey("t1", "u1")), new Set(["cam-fe-1"]));
      expect(saved?.tiles).toHaveLength(4);
      // Tile 0 is real view (no fisheye_view_id)
      expect(saved?.tiles[0]).toEqual({ camera_id: "cam-fe-1", quality: "sub" });
      // Tiles 1..3 are the 3 of 120°
      expect(saved?.tiles[1]).toEqual(expect.objectContaining({ camera_id: "cam-fe-1", fisheye_view_id: "cam-fe-1-view-120-1" }));
      expect(saved?.tiles[2]).toEqual(expect.objectContaining({ camera_id: "cam-fe-1", fisheye_view_id: "cam-fe-1-view-120-2" }));
      expect(saved?.tiles[3]).toEqual(expect.objectContaining({ camera_id: "cam-fe-1", fisheye_view_id: "cam-fe-1-view-120-3" }));
    });
  });

  it("opens fisheye view configuration modal on right-click menu in Live", async () => {
    stubBrowserAPIs();
    saveFisheyeConfig({
      cameraId: "cam-fe-2",
      enabled: true,
      lensType: "fisheye_360",
      mountType: "ceiling",
      projectionModel: "equidistant",
      calibration: DEFAULT_FISHEYE_CALIBRATION,
      views: [],
      defaultMode: "virtual_ptz",
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () =>
            json({ items: [camera("cam-fe-2", "Fisheye Lobby")] }),
          ...emptyCatalogs,
        }),
      ),
    );

    renderPage(Live);

    const camBtn = await screen.findByText("Fisheye Lobby");
    fireEvent.contextMenu(camBtn);

    const cfgOption = await screen.findByText("Configuración de vista");
    expect(cfgOption).toBeInTheDocument();
    fireEvent.click(cfgOption);

    expect(await screen.findByText(/Configuración de vista Fisheye · Fisheye Lobby/i)).toBeInTheDocument();
  });

  it("clears virtual views and resets grid to 1x1 when fisheye camera switches away from multi-view", async () => {
    stubBrowserAPIs();
    saveFisheyeConfig({
      cameraId: "cam-fe-3",
      enabled: true,
      lensType: "fisheye_360",
      mountType: "ceiling",
      projectionModel: "equidistant",
      calibration: DEFAULT_FISHEYE_CALIBRATION,
      views: [{ id: "v1", cameraId: "cam-fe-3", name: "V1", projection: "rectilinear", yaw: 0, pitch: -30, roll: 0, fov: 120 }],
      defaultMode: "3_fixed_120",
    });

    localStorage.setItem(
      liveSelectionKey("t1", "u1"),
      serializeSelection(2, [
        { camera_id: "cam-fe-3", quality: "main" },
        { camera_id: "cam-fe-3", quality: "sub", fisheye_view_id: "v1" },
        null,
        null,
      ]),
    );

    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () =>
            json({ items: [camera("cam-fe-3", "Fisheye Hall")] }),
          ...emptyCatalogs,
        }),
      ),
    );

    renderPage(Live);

    await screen.findByText("Fisheye Hall");

    // Camera leaves 3 or 4: switches to panorama and clears views
    saveFisheyeConfig({
      cameraId: "cam-fe-3",
      enabled: true,
      lensType: "fisheye_360",
      mountType: "ceiling",
      projectionModel: "equidistant",
      calibration: DEFAULT_FISHEYE_CALIBRATION,
      views: [],
      defaultMode: "panorama",
    });

    // Grid should clear the virtual view and reset to single 1x1 tile
    await waitFor(() => {
      const saved = parseSelection(
        localStorage.getItem(liveSelectionKey("t1", "u1")),
        new Set(["cam-fe-3"]),
      );
      expect(saved?.columns).toBe(1);
      expect(saved?.tiles).toEqual([{ camera_id: "cam-fe-3", quality: "main" }]);
    });
  });

  it("displays operator warning banner when station is saturated under adaptive streaming and allows dismissal", async () => {
    localStorage.setItem(FEATURES_OVERRIDE_KEY, JSON.stringify({ adaptiveStreaming: true }));
    stubBrowserAPIs();
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/cameras": () => json({ items: [camera("cam-1", "Puerta norte")] }),
      ...emptyCatalogs,
    })));

    const spy = vi.spyOn(telemetryRuntime, "getView").mockReturnValue({
      snapshot: { metrics: null, streams: [] },
      history: [],
      health: { ...classifyHealth([]), state: "saturated" },
      state: "saturated",
      bottleneck: { layer: "decode", confidence: "high", estimated: true, evidence: ["dropped frames"] },
    });

    renderPage(Live);
    fireEvent.click(await screen.findByText("Puerta norte"));
    await screen.findByRole("button", { name: "Calidad de Puerta norte" });

    const banner = await screen.findByText(/Rendimiento del videowall comprometido/);
    expect(banner.textContent).toContain("Decodificación de video");

    const dismissBtn = screen.getByRole("button", { name: "Descartar advertencia" });
    fireEvent.click(dismissBtn);

    expect(screen.queryByText(/Rendimiento del videowall comprometido/)).toBeNull();
    spy.mockRestore();
  });
});
