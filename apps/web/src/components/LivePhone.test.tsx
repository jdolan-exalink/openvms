import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Live } from "@/routes/Live";

vi.mock("@/components/MsePlayer", () => ({
  MsePlayer: ({ cameraId, quality, persistent }: { cameraId: string; quality?: string; persistent?: boolean }) => (
    <div data-testid="player" data-camera={cameraId} data-quality={quality} data-persistent={String(!!persistent)} />
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

function stubPhone(phone: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: phone && query === "(pointer: coarse)", media: query, addEventListener() {}, removeEventListener() {} }));
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

beforeEach(() => {
  hls.onTime.clear();
  localStorage.clear();
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
