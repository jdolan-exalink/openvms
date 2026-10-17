import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Live } from "@/routes/Live";

vi.mock("@/components/MsePlayer", () => ({
  MsePlayer: ({ cameraId, quality, persistent, objectFit }: { cameraId: string; quality?: string; persistent?: boolean; objectFit?: string }) => (
    <div data-testid="player" data-camera={cameraId} data-quality={quality} data-persistent={String(!!persistent)} data-fit={objectFit} />
  ),
}));

/** Recorded players: expose the window props and the master clock callback the transport drives. */
const hls = vi.hoisted(() => ({ onTime: new Map<string, (unix: number) => void>() }));
vi.mock("@/components/HlsPlayer", async () => {
  const React = await import("react");
  return {
    HlsPlayer: React.forwardRef(function FakeHls({ cameraId, start, startOffset, onTime }: { cameraId: string; start: number; startOffset?: number; onTime?: (unix: number) => void }, ref: React.Ref<{ video: null }>) {
      React.useImperativeHandle(ref, () => ({ video: null }));
      React.useEffect(() => {
        if (onTime) hls.onTime.set(cameraId, onTime);
        return () => void hls.onTime.delete(cameraId);
      });
      return <div data-testid="rec-player" data-camera={cameraId} data-at={start + (startOffset ?? 0)} />;
    }),
  };
});

/** A stand-in session: only what the main layer of the single view touches. */
const sessions = vi.hoisted(() => ({ main: null as unknown }));
vi.mock("@/lib/live/PlayerSessionProvider", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/live/PlayerSessionProvider")>()),
  usePlayerSession: vi.fn((cameraId: string, quality: string) => (quality === "main" ? ({ ...(sessions.main as object), cameraId, quality } as never) : null)),
}));

function fakeMainSession() {
  const video = document.createElement("video");
  const attach = vi.fn((el: HTMLElement) => el.appendChild(video));
  sessions.main = { video, attach, detach: vi.fn(() => video.remove()), setMuted: vi.fn(), setObjectFit: vi.fn() };
  return { video, attach };
}

function stubPhone(phone: boolean, opts: { landscape?: boolean } = {}) {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: (phone && query === "(pointer: coarse)") || (!!opts.landscape && query === "(orientation: landscape)"),
    media: query,
    addEventListener() {},
    removeEventListener() {},
  }));
  vi.stubGlobal("screen", { width: phone ? 390 : 1920, height: phone ? 844 : 1080 });
}

const camera = (id: string, name: string, site = "s1") => ({
  id, tenant_id: "t1", site_id: site, server_id: site === "s1" ? "srv1" : `srv-${site}`, remote_name: name, display_name: name, enabled: true, zones: [], lpr: false,
  status: "online", fps: 5, group_ids: [], folder_id: null, sort_order: 0, default_live_quality: "sub", description: "", location: "", tags: [],
  created_at: "", updated_at: "",
});

const REC_T = Date.parse("2026-09-30T12:00:00.000Z") / 1000;
const REC_URL = "/?mode=rec&t=2026-09-30T12:00:00.000Z";
const at = () => screen.getAllByTestId("rec-player").map((p) => [p.getAttribute("data-camera"), Number(p.getAttribute("data-at"))]);

function stubBackend(cameras: ReturnType<typeof camera>[], opts: { recordings?: boolean; recordingsStatus?: number } = {}) {
  const grants = [{ permission: "live.view", effect: "allow", scope_type: "platform" }, ...(opts.recordings ? [{ permission: "recordings.view", effect: "allow", scope_type: "platform" }] : [])];
  const recordingRoutes = Object.fromEntries(
    cameras.map((c) => [`/api/v1/cameras/${c.id}/recordings`, () => opts.recordingsStatus ? json({ code: "forbidden", message: "forbidden" }, opts.recordingsStatus) : json({ items: [{ start_time: "2026-09-30T00:00:00.000Z", end_time: "2026-10-01T00:00:00.000Z" }] })]),
  );
  vi.stubGlobal("fetch", vi.fn(stubApi({
    ...recordingRoutes,
    "/api/v1/events": () => json({ items: [] }),
    "/api/v1/me": () => json({ id: "u1", username: "op", display_name: "Op", mfa_enabled: false, must_change_password: false, auth_method: "session", tenant_id: "t1", grants }),
    "/api/v1/cameras": () => json({ items: cameras }),
    "/api/v1/sites": () => json({ items: [{ id: "s1", tenant_id: "t1", name: "Alpha" }, { id: "s2", tenant_id: "t1", name: "Beta" }] }),
    "/api/v1/servers": () => json({ items: [] }),
    "/api/v1/views": () => json({ items: [] }),
  })));
}

const scopeButton = () => screen.getByRole("button", { name: /^Filtrar por sitio/ });
const sheet = () => screen.getByRole("dialog", { name: "Cámaras" });
const openSheet = () => fireEvent.click(scopeButton());
/** Opens the scope sheet and picks a row by its accessible name. */
const pickScope = (name: string) => {
  openSheet();
  fireEvent.click(within(sheet()).getByRole("button", { name }));
};

/** The remembered phone layout is per user (tenant t1, user u1 in stubBackend). */
const LAYOUT_KEY = "openvms.livephone.layout.v1:t1:u1";
const seedLayout = (layout: string, key = LAYOUT_KEY) => localStorage.setItem(key, JSON.stringify({ layout }));

beforeEach(() => {
  hls.onTime.clear();
  localStorage.clear();
  // The default layout is the snapshot grid; the original suites exercise the list.
  seedLayout("list");
  fakeMainSession();
});
afterEach(() => vi.unstubAllGlobals());

describe("Live on a phone", () => {
  it("shows the camera list instead of the grid", async () => {
    stubPhone(true);
    stubBackend([camera("c1", "North"), camera("c2", "East")]);
    renderPage(Live);
    expect(await screen.findByRole("button", { name: "Ver North" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Ver East" })).toBeInTheDocument();
    expect(screen.queryByLabelText("Cuadro 1")).toBeNull();
    expect(screen.queryByRole("group", { name: "Video grid" })).toBeNull();
    expect(screen.queryByLabelText("Live explorer")).toBeNull();
  });

  it("keeps the grid on larger screens", async () => {
    stubPhone(false);
    stubBackend([camera("c1", "North")]);
    renderPage(Live);
    expect(await screen.findByLabelText("Cuadro 1")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Ver North" })).toBeNull();
  });

  it("orders by site then name and filters by site from the scope sheet", async () => {
    stubPhone(true);
    stubBackend([camera("c1", "Zed", "s1"), camera("c2", "Beta cam", "s2"), camera("c3", "Able", "s1")]);
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver Zed" });
    const names = () => screen.getAllByRole("button", { name: /^Ver / }).map((b) => b.getAttribute("aria-label"));
    expect(names()).toEqual(["Ver Able", "Ver Zed", "Ver Beta cam"]);
    expect(scopeButton()).toHaveTextContent("Todas");
    pickScope("Beta");
    expect(names()).toEqual(["Ver Beta cam"]);
    expect(scopeButton()).toHaveTextContent("Beta");
    pickScope("Todas");
    expect(names()).toHaveLength(3);
  });

  it("shows 4 cards per page and plays only those 4 on the sub stream through persistent sessions", async () => {
    stubPhone(true);
    const list = Array.from({ length: 6 }, (_, i) => camera(`c${i + 1}`, `Cam ${i + 1}`));
    stubBackend(list);
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver Cam 1" });
    expect(screen.getAllByRole("button", { name: /^Ver / })).toHaveLength(4);
    expect(screen.queryByRole("button", { name: "Ver Cam 5" })).toBeNull();
    const players = screen.getAllByTestId("player");
    expect(players.map((p) => p.getAttribute("data-camera"))).toEqual(["c1", "c2", "c3", "c4"]);
    for (const player of players) {
      expect(player).toHaveAttribute("data-quality", "sub");
      expect(player).toHaveAttribute("data-persistent", "true");
    }
  });

  it("changes page with the prev/next buttons, announces it and re-mounts the same sessions on return", async () => {
    stubPhone(true);
    stubBackend(Array.from({ length: 6 }, (_, i) => camera(`c${i + 1}`, `Cam ${i + 1}`)));
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver Cam 1" });
    const ids = () => screen.getAllByTestId("player").map((p) => p.getAttribute("data-camera"));
    const status = screen.getByTestId("phone-page-status");
    expect(status).toHaveAttribute("aria-live", "polite");
    expect(status).toHaveTextContent("Página 1 de 2");
    expect(screen.getByRole("button", { name: "Página anterior" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Página siguiente" }));
    expect(ids()).toEqual(["c5", "c6"]);
    expect(status).toHaveTextContent("Página 2 de 2");
    expect(screen.getByRole("button", { name: "Página siguiente" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Página anterior" }));
    expect(ids()).toEqual(["c1", "c2", "c3", "c4"]);
  });

  it("changes page with a horizontal swipe but ignores short or vertical drags", async () => {
    stubPhone(true);
    stubBackend(Array.from({ length: 6 }, (_, i) => camera(`c${i + 1}`, `Cam ${i + 1}`)));
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver Cam 1" });
    const area = screen.getByTestId("live-phone-pages");
    const drag = (dx: number, dy: number) => {
      fireEvent.pointerDown(area, { pointerId: 1, clientX: 200, clientY: 300 });
      fireEvent.pointerUp(area, { pointerId: 1, clientX: 200 + dx, clientY: 300 + dy });
    };
    const first = () => screen.getAllByTestId("player")[0]?.getAttribute("data-camera");
    drag(-20, 0);
    drag(-80, 90);
    expect(first()).toBe("c1");
    drag(80, 0); // swipe right on the first page: nowhere to go
    expect(first()).toBe("c1");
    drag(-80, 5); // swipe left: next page
    expect(first()).toBe("c5");
    drag(80, 5); // swipe right: previous page
    expect(first()).toBe("c1");
  });

  it("changes page with the arrow keys when the page area is focused", async () => {
    stubPhone(true);
    stubBackend(Array.from({ length: 6 }, (_, i) => camera(`c${i + 1}`, `Cam ${i + 1}`)));
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver Cam 1" });
    const area = screen.getByTestId("live-phone-pages");
    fireEvent.keyDown(area, { key: "ArrowRight" });
    expect(screen.getAllByTestId("player")[0]).toHaveAttribute("data-camera", "c5");
    fireEvent.keyDown(area, { key: "ArrowLeft" });
    expect(screen.getAllByTestId("player")[0]).toHaveAttribute("data-camera", "c1");
  });

  it("resets to page 1 when the scope site changes", async () => {
    stubPhone(true);
    const list = [...Array.from({ length: 5 }, (_, i) => camera(`a${i + 1}`, `A${i + 1}`, "s1")), ...Array.from({ length: 5 }, (_, i) => camera(`b${i + 1}`, `B${i + 1}`, "s2"))];
    stubBackend(list);
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver A1" });
    fireEvent.click(screen.getByRole("button", { name: "Página siguiente" }));
    expect(screen.getByTestId("phone-page-status")).toHaveTextContent("Página 2 de 3");
    pickScope("Beta");
    expect(screen.getByTestId("phone-page-status")).toHaveTextContent("Página 1 de 2");
    expect(screen.getAllByTestId("player")[0]).toHaveAttribute("data-camera", "b1");
  });

  it("keeps the camera snapshot under every card as the poster until the first frame", async () => {
    stubPhone(true);
    stubBackend([camera("c1", "One"), camera("c2", "Two")]);
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver One" });
    for (const id of ["c1", "c2"]) {
      expect(document.querySelector(`img[src="/media/v1/cameras/${id}/snapshot.jpg?h=360"]`)).not.toBeNull();
    }
  });

  it("opens a single main-quality view and returns to the same page keeping filter", async () => {
    stubPhone(true);
    stubBackend([camera("c1", "North", "s1"), ...Array.from({ length: 5 }, (_, i) => camera(`e${i + 1}`, `East ${i + 1}`, "s2"))]);
    const { router } = renderPage(Live);
    await screen.findByRole("button", { name: "Ver North" });
    pickScope("Beta");
    fireEvent.click(screen.getByRole("button", { name: "Página siguiente" }));
    fireEvent.click(screen.getByRole("button", { name: "Ver East 5" }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ camera: "e5" }));
    const single = await screen.findByTestId("live-phone-single");
    const player = within(single).getByTestId("player");
    expect(player).toHaveAttribute("data-camera", "e5");
    expect(player).toHaveAttribute("data-quality", "sub");
    expect(within(single).getByTestId("phone-main-layer")).toBeInTheDocument();
    expect(within(single).getByText("East 5")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Volver" }));
    await waitFor(() => expect(screen.queryByTestId("live-phone-single")).toBeNull());
    expect(router.state.location.search).not.toHaveProperty("camera");
    expect(scopeButton()).toHaveTextContent("Beta");
    expect(screen.getByTestId("phone-page-status")).toHaveTextContent("Página 2 de 2");
    expect(screen.getByRole("button", { name: "Ver East 5" })).toBeInTheDocument();
  });

  it("shows the playing sub picture on open and layers main on top of it", async () => {
    stubPhone(true);
    const { video, attach } = fakeMainSession();
    stubBackend([camera("c1", "North")]);
    renderPage(Live, "/?camera=c1");
    const single = await screen.findByTestId("live-phone-single");
    const { usePlayerSession } = await import("@/lib/live/PlayerSessionProvider");
    expect(usePlayerSession).toHaveBeenCalledWith("c1", "main", "srv1");
    // Sub stays visible and main is attached on top while it has no picture yet.
    expect(within(single).getByTestId("player")).toHaveAttribute("data-quality", "sub");
    expect(attach).toHaveBeenCalled();
    expect(within(single).getByTestId("phone-main-layer")).toContainElement(video);
    // The sub session stays mounted under main so going back to the list is instant.
    expect(within(single).getByTestId("player")).toHaveAttribute("data-camera", "c1");
  });
});

describe("Live phone scope sheet", () => {
  const five = () => [camera("c1", "North", "s1"), camera("c2", "Zed", "s1"), camera("c3", "Beta cam", "s2")];

  it("replaces the site chips with one scope control that opens a modal sheet with search and the tree", async () => {
    stubPhone(true);
    stubBackend(five());
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver North" });
    expect(screen.queryByRole("group", { name: "Filtrar por sitio" })).toBeNull();
    expect(scopeButton()).toHaveAttribute("aria-haspopup", "dialog");
    expect(screen.queryByRole("dialog")).toBeNull();
    openSheet();
    const dialog = sheet();
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(within(dialog).getByRole("searchbox", { name: "Buscar en el explorador" })).toBeInTheDocument();
    for (const name of ["Todas", "Alpha", "Beta", "North", "Zed", "Beta cam"]) expect(within(dialog).getByRole("button", { name })).toBeInTheDocument();
  });

  it("filters the tree with the search box", async () => {
    stubPhone(true);
    stubBackend(five());
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver North" });
    openSheet();
    fireEvent.change(within(sheet()).getByRole("searchbox"), { target: { value: "zed" } });
    expect(within(sheet()).getByRole("button", { name: "Zed" })).toBeInTheDocument();
    expect(within(sheet()).queryByRole("button", { name: "North" })).toBeNull();
  });

  it("closes after choosing a site and the pages show only that site from page 1", async () => {
    stubPhone(true);
    stubBackend(five());
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver North" });
    pickScope("Beta");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.getAllByRole("button", { name: /^Ver / }).map((b) => b.getAttribute("aria-label"))).toEqual(["Ver Beta cam"]);
  });

  it("opens the single view of a camera picked in the tree and closes the sheet", async () => {
    stubPhone(true);
    stubBackend(five());
    const { router } = renderPage(Live);
    await screen.findByRole("button", { name: "Ver North" });
    pickScope("Beta cam");
    expect(screen.queryByRole("dialog")).toBeNull();
    await waitFor(() => expect(router.state.location.search).toMatchObject({ camera: "c3" }));
    expect(await screen.findByTestId("live-phone-single")).toBeInTheDocument();
  });

  it("closes with Escape and returns focus to the scope control", async () => {
    stubPhone(true);
    stubBackend(five());
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver North" });
    scopeButton().focus();
    openSheet();
    expect(sheet().contains(document.activeElement)).toBe(true);
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(scopeButton()).toHaveFocus();
  });

  it("closes from the scrim", async () => {
    stubPhone(true);
    stubBackend(five());
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver North" });
    openSheet();
    fireEvent.mouseDown(screen.getByTestId("phone-sheet-scrim"));
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("Live phone GRABADO", () => {
  const cams = (n: number) => Array.from({ length: n }, (_, i) => camera(`c${i + 1}`, `Cam ${i + 1}`));

  it("hides the mode toggle without the recordings permission", async () => {
    stubPhone(true);
    stubBackend(cams(2));
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver Cam 1" });
    expect(screen.queryByRole("group", { name: "Modo de reproducción" })).toBeNull();
  });

  it("switches the page cards to recorded tiles at a shared time and back to live", async () => {
    stubPhone(true);
    stubBackend(cams(6), { recordings: true });
    const { router } = renderPage(Live);
    await screen.findByRole("button", { name: "Ver Cam 1" });
    expect(screen.getAllByTestId("player")).toHaveLength(4);
    fireEvent.click(screen.getByRole("button", { name: "Grabado" }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ mode: "rec" }));
    expect(await screen.findByRole("region", { name: "Controles de grabación" })).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByTestId("rec-player")).toHaveLength(4));
    expect(screen.queryAllByTestId("player")).toHaveLength(0);
    const times = new Set(at().map(([, t]) => t));
    expect(times.size).toBe(1);
    fireEvent.click(screen.getByRole("button", { name: "Vivo" }));
    await waitFor(() => expect(screen.getAllByTestId("player")).toHaveLength(4));
    expect(screen.queryAllByTestId("rec-player")).toHaveLength(0);
    expect(router.state.location.search).not.toHaveProperty("mode");
  });

  it("keeps the playback time when the page changes", async () => {
    stubPhone(true);
    stubBackend(cams(6), { recordings: true });
    renderPage(Live, REC_URL);
    await waitFor(() => expect(screen.getAllByTestId("rec-player")).toHaveLength(4));
    expect(at()[0]![1]).toBe(REC_T);
    act(() => hls.onTime.get("c1")!(REC_T + 300));
    fireEvent.click(screen.getByRole("button", { name: "Página siguiente" }));
    await waitFor(() => expect(at().map(([id]) => id)).toEqual(["c5", "c6"]));
    expect(at().map(([, t]) => t)).toEqual([REC_T + 300, REC_T + 300]);
  });

  it("shows the recording of one camera in the single view at the same time and keeps time and page on back", async () => {
    stubPhone(true);
    stubBackend(cams(6), { recordings: true });
    renderPage(Live, REC_URL);
    await waitFor(() => expect(screen.getAllByTestId("rec-player")).toHaveLength(4));
    fireEvent.click(screen.getByRole("button", { name: "Página siguiente" }));
    await waitFor(() => expect(at().map(([id]) => id)).toEqual(["c5", "c6"]));
    act(() => hls.onTime.get("c5")!(REC_T + 120));
    fireEvent.click(screen.getByRole("button", { name: "Ver Cam 6" }));
    const single = await screen.findByTestId("live-phone-single");
    await waitFor(() => expect(within(single).getAllByTestId("rec-player")).toHaveLength(1));
    expect(within(single).getByTestId("rec-player")).toHaveAttribute("data-camera", "c6");
    expect(within(single).getByTestId("rec-player")).toHaveAttribute("data-at", String(REC_T + 120));
    expect(within(single).queryByTestId("phone-main-layer")).toBeNull();
    expect(screen.getByRole("region", { name: "Controles de grabación" })).toBeInTheDocument();
    act(() => hls.onTime.get("c6")!(REC_T + 200));
    fireEvent.click(screen.getByRole("button", { name: "Volver" }));
    await waitFor(() => expect(screen.queryByTestId("live-phone-single")).toBeNull());
    await waitFor(() => expect(at().map(([id]) => id)).toEqual(["c5", "c6"]));
    expect(at().map(([, t]) => t)).toEqual([REC_T + 200, REC_T + 200]);
    expect(screen.getByTestId("phone-page-status")).toHaveTextContent("Página 2 de 2");
  });

  it("shows the desktop notices for cameras without permission", async () => {
    stubPhone(true);
    stubBackend(cams(2), { recordings: true, recordingsStatus: 403 });
    renderPage(Live, REC_URL);
    expect(await screen.findAllByText("Sin permiso de grabaciones")).toHaveLength(2);
  });

  it("opens a camera picked in the sheet in the current mode (GRABADO)", async () => {
    stubPhone(true);
    stubBackend(cams(2), { recordings: true });
    renderPage(Live, REC_URL);
    await waitFor(() => expect(screen.getAllByTestId("rec-player")).toHaveLength(2));
    pickScope("Cam 2");
    const single = await screen.findByTestId("live-phone-single");
    await waitFor(() => expect(within(single).getByTestId("rec-player")).toHaveAttribute("data-camera", "c2"));
  });
});

describe("Live phone immersive single camera", () => {
  const fs = { element: null as Element | null };
  const request = vi.fn(function (this: Element) {
    fs.element = this;
    return Promise.resolve();
  });
  const exit = vi.fn(() => {
    fs.element = null;
    document.dispatchEvent(new Event("fullscreenchange"));
    return Promise.resolve();
  });

  beforeEach(() => {
    fs.element = null;
    request.mockClear();
    exit.mockClear();
    Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => fs.element });
    Object.defineProperty(document, "exitFullscreen", { configurable: true, value: exit });
    Object.defineProperty(HTMLElement.prototype, "requestFullscreen", { configurable: true, value: request });
  });
  afterEach(() => {
    delete (document as unknown as Record<string, unknown>).fullscreenElement;
    delete (document as unknown as Record<string, unknown>).exitFullscreen;
    delete (HTMLElement.prototype as unknown as Record<string, unknown>).requestFullscreen;
  });

  async function openSingle(opts: { landscape?: boolean } = {}) {
    stubPhone(true, opts);
    stubBackend([camera("c1", "North")]);
    renderPage(Live, "/?camera=c1");
    return screen.findByTestId("live-phone-single");
  }

  it("stays inline in portrait and offers a fullscreen button", async () => {
    const single = await openSingle();
    expect(single).not.toHaveClass("fixed");
    expect(within(single).getByRole("button", { name: "Pantalla completa" })).toHaveAttribute("aria-pressed", "false");
    expect(within(single).getByTestId("phone-stage")).toHaveClass("aspect-video");
  });

  it("requests native fullscreen on the container and covers the app chrome safely", async () => {
    const single = await openSingle();
    fireEvent.click(within(single).getByRole("button", { name: "Pantalla completa" }));
    expect(request).toHaveBeenCalledTimes(1);
    expect(request.mock.contexts[0]).toBe(single);
    expect(single).toHaveClass("fixed", "inset-0", "pt-[env(safe-area-inset-top)]", "pl-[env(safe-area-inset-left)]", "pr-[env(safe-area-inset-right)]", "pb-[env(safe-area-inset-bottom)]");
    expect(within(single).getByRole("button", { name: "Salir de pantalla completa" })).toHaveAttribute("aria-pressed", "true");
    expect(within(single).getByTestId("phone-stage")).toHaveClass("size-full");
  });

  it("returns to the previous state when the system leaves fullscreen", async () => {
    const single = await openSingle();
    fireEvent.click(within(single).getByRole("button", { name: "Pantalla completa" }));
    fs.element = null;
    act(() => void document.dispatchEvent(new Event("fullscreenchange")));
    expect(single).not.toHaveClass("fixed");
    expect(exit).not.toHaveBeenCalled();
  });

  it("exits through the toggle", async () => {
    const single = await openSingle();
    fireEvent.click(within(single).getByRole("button", { name: "Pantalla completa" }));
    fireEvent.click(within(single).getByRole("button", { name: "Salir de pantalla completa" }));
    expect(exit).toHaveBeenCalledTimes(1);
    expect(single).not.toHaveClass("fixed");
  });

  it("falls back to CSS immersive mode where the Fullscreen API does not exist", async () => {
    delete (HTMLElement.prototype as unknown as Record<string, unknown>).requestFullscreen;
    const single = await openSingle();
    fireEvent.click(within(single).getByRole("button", { name: "Pantalla completa" }));
    expect(single).toHaveClass("fixed", "inset-0");
    fireEvent.click(within(single).getByRole("button", { name: "Salir de pantalla completa" }));
    expect(single).not.toHaveClass("fixed");
  });

  it("keeps the CSS immersive mode when native fullscreen is refused", async () => {
    request.mockImplementationOnce(() => Promise.reject(new Error("denied")));
    const single = await openSingle();
    fireEvent.click(within(single).getByRole("button", { name: "Pantalla completa" }));
    await Promise.resolve();
    expect(single).toHaveClass("fixed");
  });

  it("fills the screen in landscape without any tap and contains the picture", async () => {
    const { attach } = fakeMainSession();
    const single = await openSingle({ landscape: true });
    expect(attach).toHaveBeenCalled();
    expect(single).toHaveClass("fixed", "inset-0");
    expect(request).not.toHaveBeenCalled();
    expect((sessions.main as { setObjectFit: ReturnType<typeof vi.fn> }).setObjectFit).toHaveBeenCalledWith("contain");
  });

  it("leaves fullscreen when the back button closes the camera", async () => {
    const single = await openSingle();
    fireEvent.click(within(single).getByRole("button", { name: "Pantalla completa" }));
    fireEvent.click(within(single).getByRole("button", { name: "Volver" }));
    await waitFor(() => expect(screen.queryByTestId("live-phone-single")).toBeNull());
    expect(exit).toHaveBeenCalled();
  });
});

describe("Live phone pinch-zoom in the single camera", () => {
  beforeEach(() => {
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue({ x: 0, y: 0, top: 0, left: 0, right: 200, bottom: 100, width: 200, height: 100, toJSON: () => ({}) } as DOMRect);
  });
  afterEach(() => vi.restoreAllMocks());

  const zoomLayer = (single: HTMLElement) => within(single).getByTestId("phone-main-layer").parentElement as HTMLElement;
  const touch = (frame: HTMLElement, type: "pointerDown" | "pointerMove" | "pointerUp", id: number, x: number, y: number) =>
    fireEvent[type](frame, { pointerId: id, pointerType: "touch", isPrimary: id === 1, clientX: x, clientY: y, button: 0 });
  const frameOf = (single: HTMLElement) => zoomLayer(single).parentElement as HTMLElement;
  const pinchOut = (frame: HTMLElement) => {
    touch(frame, "pointerDown", 1, 80, 50);
    touch(frame, "pointerDown", 2, 120, 50);
    touch(frame, "pointerMove", 2, 160, 50);
    touch(frame, "pointerUp", 2, 160, 50);
    touch(frame, "pointerUp", 1, 80, 50);
  };

  async function openSingle(path = "/?camera=c1") {
    stubPhone(true);
    stubBackend([camera("c1", "North"), camera("c2", "East")]);
    const view = renderPage(Live, path);
    return { ...view, single: await screen.findByTestId("live-phone-single") };
  }

  it("zooms the picture with two fingers and shows the reset chip", async () => {
    const { single } = await openSingle();
    expect(zoomLayer(single).style.transform).toBe("");
    pinchOut(frameOf(single));
    expect(zoomLayer(single).style.transform).toMatch(/scale\(2\)/);
    expect(within(single).getByRole("button", { name: /Restablecer/ })).toBeInTheDocument();
  });

  it("pans with one finger once zoomed", async () => {
    const { single } = await openSingle();
    const frame = frameOf(single);
    pinchOut(frame);
    const before = zoomLayer(single).style.transform;
    touch(frame, "pointerDown", 1, 100, 50);
    touch(frame, "pointerMove", 1, 80, 50);
    expect(zoomLayer(single).style.transform).not.toBe(before);
  });

  it("toggles with a double tap", async () => {
    const { single } = await openSingle();
    const frame = frameOf(single);
    const tap = () => {
      touch(frame, "pointerDown", 1, 100, 50);
      touch(frame, "pointerUp", 1, 100, 50);
    };
    tap();
    tap();
    expect(zoomLayer(single).style.transform).toMatch(/scale\(2\.5\)/);
    tap();
    tap();
    expect(zoomLayer(single).style.transform).toBe("");
  });

  it("resets when the camera changes", async () => {
    const { single, router } = await openSingle();
    pinchOut(frameOf(single));
    expect(zoomLayer(single).style.transform).not.toBe("");
    await act(async () => void (await router.navigate({ to: "/", search: { camera: "c2" } as never })));
    const next = await screen.findByTestId("live-phone-single");
    expect(zoomLayer(next).style.transform).toBe("");
  });

  it("starts at 1x again after leaving and reopening the camera", async () => {
    const { single } = await openSingle();
    pinchOut(frameOf(single));
    fireEvent.click(screen.getByRole("button", { name: "Volver" }));
    await waitFor(() => expect(screen.queryByTestId("live-phone-single")).toBeNull());
    fireEvent.click(await screen.findByRole("button", { name: "Ver North" }));
    const again = await screen.findByTestId("live-phone-single");
    expect(zoomLayer(again).style.transform).toBe("");
  });

  it("does not add zoom handling to the multi-camera list", async () => {
    stubPhone(true);
    stubBackend([camera("c1", "North")]);
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver North" });
    expect(screen.getByTestId("live-phone-pages").querySelector('[style*="touch-action"]')).toBeNull();
  });
});

describe("Live phone layouts", () => {
  const cams = (n: number, status = "online") => Array.from({ length: n }, (_, i) => ({ ...camera(`c${i + 1}`, `Cam ${i + 1}`), status }));
  const toggle = (name: string) => screen.getByRole("button", { name });

  afterEach(() => vi.useRealTimers());

  it("defaults to the snapshot grid: tiles with snapshots and no live player", async () => {
    localStorage.clear();
    stubPhone(true);
    stubBackend(cams(6));
    renderPage(Live);
    const grid = await screen.findByTestId("phone-grid");
    expect(within(grid).getAllByRole("button", { name: /^Ver / })).toHaveLength(6);
    expect(screen.queryAllByTestId("player")).toHaveLength(0);
    expect(grid.querySelector('img[src^="/media/v1/cameras/c1/snapshot.jpg?h=360"]')).not.toBeNull();
    expect(toggle("Cuadrícula")).toHaveAttribute("aria-pressed", "true");
    expect(screen.queryByTestId("phone-page-status")).toBeNull();
  });

  it("marks offline cameras on their tile", async () => {
    localStorage.clear();
    stubPhone(true);
    stubBackend(cams(2, "offline"));
    renderPage(Live);
    const grid = await screen.findByTestId("phone-grid");
    expect(within(grid).getAllByText("Sin conexión")).toHaveLength(2);
  });

  it("opens the single camera from a tile", async () => {
    localStorage.clear();
    stubPhone(true);
    stubBackend(cams(2));
    const { router } = renderPage(Live);
    fireEvent.click(await screen.findByRole("button", { name: "Ver Cam 2" }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ camera: "c2" }));
    expect(await screen.findByTestId("live-phone-single")).toBeInTheDocument();
  });

  it("switches layout from the header toggle and remembers it for the user", async () => {
    localStorage.clear();
    stubPhone(true);
    stubBackend(cams(6));
    renderPage(Live);
    await screen.findByTestId("phone-grid");
    fireEvent.click(toggle("Lista"));
    expect(screen.queryByTestId("phone-grid")).toBeNull();
    expect(screen.getAllByTestId("player")).toHaveLength(4);
    expect(toggle("Lista")).toHaveAttribute("aria-pressed", "true");
    expect(JSON.parse(localStorage.getItem(LAYOUT_KEY)!)).toEqual({ layout: "list" });
    fireEvent.click(toggle("Enfoque"));
    expect(JSON.parse(localStorage.getItem(LAYOUT_KEY)!)).toEqual({ layout: "focus" });
  });

  it("restores the remembered layout and ignores another user's choice", async () => {
    localStorage.clear();
    seedLayout("focus");
    seedLayout("list", "openvms.livephone.layout.v1:t1:u2");
    stubPhone(true);
    stubBackend(cams(3));
    renderPage(Live);
    expect(await screen.findByTestId("phone-focus-strip")).toBeInTheDocument();
    expect(toggle("Enfoque")).toHaveAttribute("aria-pressed", "true");
  });

  it("falls back to the grid for an unknown stored layout", async () => {
    localStorage.clear();
    seedLayout("carousel");
    stubPhone(true);
    stubBackend(cams(2));
    renderPage(Live);
    expect(await screen.findByTestId("phone-grid")).toBeInTheDocument();
  });

  it("offers site chips as a quick switcher next to the full scope sheet", async () => {
    localStorage.clear();
    stubPhone(true);
    stubBackend([camera("c1", "North", "s1"), camera("c2", "East", "s2")]);
    renderPage(Live);
    const chips = await screen.findByRole("group", { name: "Sitios" });
    expect(within(chips).getByRole("button", { name: "Todas" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(within(chips).getByRole("button", { name: "Beta" }));
    expect(within(chips).getByRole("button", { name: "Beta" })).toHaveAttribute("aria-pressed", "true");
    expect(within(screen.getByTestId("phone-grid")).getAllByRole("button", { name: /^Ver / }).map((b) => b.getAttribute("aria-label"))).toEqual(["Ver East"]);
    expect(scopeButton()).toHaveTextContent("Beta");
    pickScope("Todas");
    expect(within(chips).getByRole("button", { name: "Todas" })).toHaveAttribute("aria-pressed", "true");
  });

  describe("snapshot refresh", () => {
    type Observer = { callback: IntersectionObserverCallback; target: Element | null };
    const observers: Observer[] = [];
    beforeEach(() => {
      observers.length = 0;
      class FakeObserver {
        entry: Observer;
        constructor(callback: IntersectionObserverCallback) {
          this.entry = { callback, target: null };
          observers.push(this.entry);
        }
        observe(target: Element) { this.entry.target = target; }
        unobserve() {}
        disconnect() {}
      }
      vi.stubGlobal("IntersectionObserver", FakeObserver);
    });
    const setVisible = (id: string, visible: boolean) => {
      const img = document.querySelector(`[data-testid="phone-grid"] img[src*="/cameras/${id}/"]`)!;
      const entry = observers.find((o) => o.target?.contains(img))!;
      act(() => entry.callback([{ isIntersecting: visible, target: entry.target! } as IntersectionObserverEntry], {} as IntersectionObserver));
    };
    const src = (id: string) => document.querySelector(`[data-testid="phone-grid"] img[src*="/cameras/${id}/"]`)!.getAttribute("src");
    const setHidden = (hidden: boolean) => {
      Object.defineProperty(document, "visibilityState", { configurable: true, get: () => (hidden ? "hidden" : "visible") });
      act(() => void document.dispatchEvent(new Event("visibilitychange")));
    };
    afterEach(() => setHidden(false));

    async function mountGrid() {
      localStorage.clear();
      stubPhone(true);
      stubBackend(cams(3));
      renderPage(Live);
      await screen.findByTestId("phone-grid");
      vi.useFakeTimers();
    }

    it("refreshes about every 10 s only the tiles on screen", async () => {
      await mountGrid();
      const first = src("c1");
      setVisible("c1", true);
      act(() => void vi.advanceTimersByTime(9_000));
      expect(src("c1")).toBe(first);
      act(() => void vi.advanceTimersByTime(1_500));
      expect(src("c1")).not.toBe(first);
      expect(src("c1")).toMatch(/^\/media\/v1\/cameras\/c1\/snapshot\.jpg\?h=360&_=\d+$/);
      expect(src("c2")).toBe("/media/v1/cameras/c2/snapshot.jpg?h=360");
      act(() => void vi.advanceTimersByTime(30_000));
      expect(src("c3")).toBe("/media/v1/cameras/c3/snapshot.jpg?h=360");
    });

    it("stops refreshing a tile that scrolls out of view", async () => {
      await mountGrid();
      setVisible("c1", true);
      act(() => void vi.advanceTimersByTime(10_500));
      const refreshed = src("c1");
      setVisible("c1", false);
      act(() => void vi.advanceTimersByTime(60_000));
      expect(src("c1")).toBe(refreshed);
    });

    it("pauses while the page is hidden and resumes when it returns", async () => {
      await mountGrid();
      setVisible("c1", true);
      setHidden(true);
      const before = src("c1");
      act(() => void vi.advanceTimersByTime(60_000));
      expect(src("c1")).toBe(before);
      setHidden(false);
      act(() => void vi.advanceTimersByTime(10_500));
      expect(src("c1")).not.toBe(before);
    });
  });

  it("uses more columns in landscape", async () => {
    localStorage.clear();
    stubPhone(true, { landscape: true });
    stubBackend(cams(4));
    renderPage(Live);
    expect((await screen.findByTestId("phone-grid-list")).className).toMatch(/grid-cols-4/);
  });

  it("uses two columns in portrait", async () => {
    localStorage.clear();
    stubPhone(true);
    stubBackend(cams(4));
    renderPage(Live);
    expect((await screen.findByTestId("phone-grid-list")).className).toMatch(/grid-cols-2/);
  });

  describe("focus", () => {
    it("plays exactly one live camera and a snapshot strip switches it", async () => {
      localStorage.clear();
      seedLayout("focus");
      stubPhone(true);
      stubBackend(cams(3));
      renderPage(Live);
      const strip = await screen.findByTestId("phone-focus-strip");
      expect(screen.getAllByTestId("player")).toHaveLength(1);
      expect(screen.getByTestId("player")).toHaveAttribute("data-camera", "c1");
      const { usePlayerSession } = await import("@/lib/live/PlayerSessionProvider");
      expect(usePlayerSession).toHaveBeenCalledWith("c1", "main", "srv1");
      expect(strip.querySelector('img[src^="/media/v1/cameras/c3/snapshot.jpg"]')).not.toBeNull();
      expect(within(strip).getByRole("button", { name: "Mostrar Cam 1" })).toHaveAttribute("aria-current", "true");
      fireEvent.click(within(strip).getByRole("button", { name: "Mostrar Cam 3" }));
      expect(screen.getAllByTestId("player")).toHaveLength(1);
      expect(screen.getByTestId("player")).toHaveAttribute("data-camera", "c3");
      expect(usePlayerSession).toHaveBeenCalledWith("c3", "main", "srv1");
      expect(within(strip).getByRole("button", { name: "Mostrar Cam 3" })).toHaveAttribute("aria-current", "true");
    });

    it("scrolls the strip sideways in portrait and vertically beside the picture in landscape", async () => {
      localStorage.clear();
      seedLayout("focus");
      stubPhone(true);
      stubBackend(cams(3));
      const { unmount } = renderPage(Live);
      expect((await screen.findByTestId("phone-focus-strip")).className).toMatch(/overflow-x-auto/);
      unmount();
      stubPhone(true, { landscape: true });
      renderPage(Live);
      expect((await screen.findByTestId("phone-focus-strip")).className).toMatch(/overflow-y-auto/);
    });

    it("opens the single view of the live camera", async () => {
      localStorage.clear();
      seedLayout("focus");
      stubPhone(true);
      stubBackend(cams(3));
      const { router } = renderPage(Live);
      await screen.findByTestId("phone-focus-strip");
      fireEvent.click(screen.getByRole("button", { name: "Mostrar Cam 2" }));
      fireEvent.click(screen.getByRole("button", { name: "Ver Cam 2" }));
      await waitFor(() => expect(router.state.location.search).toMatchObject({ camera: "c2" }));
      await screen.findByTestId("live-phone-single");
      expect(screen.getAllByTestId("player")).toHaveLength(1);
    });
  });

  it("keeps GRABADO on recorded tiles whatever the layout", async () => {
    localStorage.clear();
    seedLayout("grid");
    stubPhone(true);
    stubBackend(cams(6), { recordings: true });
    renderPage(Live, REC_URL);
    await waitFor(() => expect(screen.getAllByTestId("rec-player")).toHaveLength(4));
    expect(screen.queryByTestId("phone-grid")).toBeNull();
    expect(screen.queryByRole("button", { name: "Cuadrícula" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Vivo" }));
    expect(await screen.findByTestId("phone-grid")).toBeInTheDocument();
  });
});

describe("Live phone single-view swipe between cameras", () => {
  beforeEach(() => {
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue({ x: 0, y: 0, top: 0, left: 0, right: 200, bottom: 100, width: 200, height: 100, toJSON: () => ({}) } as DOMRect);
  });
  afterEach(() => vi.restoreAllMocks());

  const touch = (frame: HTMLElement, type: "pointerDown" | "pointerMove" | "pointerUp", id: number, x: number, y: number) =>
    fireEvent[type](frame, { pointerId: id, pointerType: "touch", isPrimary: id === 1, clientX: x, clientY: y, button: 0 });
  const swipe = (frame: HTMLElement, dx: number, dy = 0) => {
    touch(frame, "pointerDown", 1, 200, 50);
    touch(frame, "pointerMove", 1, 200 + dx, 50 + dy);
    touch(frame, "pointerUp", 1, 200 + dx, 50 + dy);
  };
  const current = (router: { state: { location: { search: unknown } } }) => (router.state.location.search as { camera?: string }).camera;

  async function open(id = "c2") {
    localStorage.clear();
    seedLayout("grid");
    stubPhone(true);
    stubBackend([camera("c1", "Aaa", "s1"), camera("c2", "Bbb", "s1"), camera("c3", "Ccc", "s1"), camera("c4", "Ddd", "s2")]);
    const view = renderPage(Live, `/?camera=${id}`);
    const single = await screen.findByTestId("live-phone-single");
    return { ...view, single, frame: within(single).getByTestId("phone-main-layer").parentElement!.parentElement as HTMLElement };
  }

  it("moves to the next and previous camera in grid order", async () => {
    const { router, frame } = await open();
    swipe(frame, -120);
    await waitFor(() => expect(current(router)).toBe("c3"));
    swipe(frame, 120);
    await waitFor(() => expect(current(router)).toBe("c2"));
    swipe(frame, 120);
    await waitFor(() => expect(current(router)).toBe("c1"));
    swipe(frame, 120); // first camera: nowhere to go
    expect(current(router)).toBe("c1");
  });

  it("follows the current scope and stops at its last camera", async () => {
    const { router, frame } = await open("c3");
    // Grid order is by site: Alpha (c1..c3) then Beta (c4).
    swipe(frame, -120);
    await waitFor(() => expect(current(router)).toBe("c4"));
    swipe(frame, -120);
    expect(current(router)).toBe("c4");
  });

  it("ignores short and mostly vertical drags", async () => {
    const { router, frame } = await open();
    swipe(frame, -20);
    swipe(frame, -90, 100);
    expect(current(router)).toBe("c2");
  });

  it("keeps the back button returning to the grid after swiping", async () => {
    const { router, frame } = await open();
    swipe(frame, -120);
    await waitFor(() => expect(current(router)).toBe("c3"));
    fireEvent.click(screen.getByRole("button", { name: "Volver" }));
    await waitFor(() => expect(screen.queryByTestId("live-phone-single")).toBeNull());
    expect(current(router)).toBeUndefined();
  });

  it("does not navigate while zoomed: a one-finger drag pans", async () => {
    const { router, frame } = await open();
    touch(frame, "pointerDown", 1, 80, 50);
    touch(frame, "pointerDown", 2, 120, 50);
    touch(frame, "pointerMove", 2, 160, 50);
    touch(frame, "pointerUp", 2, 160, 50);
    touch(frame, "pointerUp", 1, 80, 50);
    swipe(frame, -120);
    expect(current(router)).toBe("c2");
  });

  it("does not navigate with a pinch", async () => {
    const { router, frame } = await open();
    touch(frame, "pointerDown", 1, 40, 50);
    touch(frame, "pointerDown", 2, 80, 50);
    touch(frame, "pointerMove", 1, 0, 50);
    touch(frame, "pointerUp", 2, 80, 50);
    touch(frame, "pointerUp", 1, 0, 50);
    expect(current(router)).toBe("c2");
  });
});

describe("Live phone immersive picture and recording controls", () => {
  const rec = "/?mode=rec&t=2026-09-30T12:00:00.000Z&camera=c1";

  it("letterboxes the playing sub picture with contain when immersive and fills inline", async () => {
    stubPhone(true);
    stubBackend([camera("c1", "North")]);
    const view = renderPage(Live, "/?camera=c1");
    const single = await screen.findByTestId("live-phone-single");
    expect(within(single).getByTestId("player")).toHaveAttribute("data-fit", "fill");
    view.unmount();
    stubPhone(true, { landscape: true });
    renderPage(Live, "/?camera=c1");
    expect(await screen.findByTestId("player")).toHaveAttribute("data-fit", "contain");
  });

  it("reveals the recording controls on demand in landscape GRABADO", async () => {
    stubPhone(true, { landscape: true });
    stubBackend([camera("c1", "North")], { recordings: true });
    renderPage(Live, rec);
    const single = await screen.findByTestId("live-phone-single");
    expect(screen.queryByRole("region", { name: "Controles de grabación" })).toBeNull();
    fireEvent.click(within(single).getByRole("button", { name: "Mostrar controles de grabación" }));
    expect(within(single).getByRole("region", { name: "Controles de grabación" })).toBeInTheDocument();
    expect(screen.getAllByRole("region", { name: "Controles de grabación" })).toHaveLength(1);
    fireEvent.click(within(single).getByRole("button", { name: "Ocultar controles de grabación" }));
    expect(screen.queryByRole("region", { name: "Controles de grabación" })).toBeNull();
  });

  it("keeps the dock inline in portrait GRABADO and offers no extra button", async () => {
    stubPhone(true);
    stubBackend([camera("c1", "North")], { recordings: true });
    renderPage(Live, rec);
    const single = await screen.findByTestId("live-phone-single");
    expect(await screen.findByRole("region", { name: "Controles de grabación" })).not.toBeNull();
    expect(within(single).queryByRole("button", { name: "Mostrar controles de grabación" })).toBeNull();
  });

  it("adds no recording button to immersive LIVE", async () => {
    stubPhone(true, { landscape: true });
    stubBackend([camera("c1", "North")], { recordings: true });
    renderPage(Live, "/?camera=c1");
    const single = await screen.findByTestId("live-phone-single");
    expect(within(single).queryByRole("button", { name: "Mostrar controles de grabación" })).toBeNull();
  });
});
