import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Live } from "@/routes/Live";

vi.mock("@/components/MsePlayer", () => ({
  MsePlayer: ({ cameraId, quality, persistent }: { cameraId: string; quality?: string; persistent?: boolean }) => (
    <div data-testid="player" data-camera={cameraId} data-quality={quality} data-persistent={String(!!persistent)} />
  ),
}));

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
  id, tenant_id: "t1", site_id: site, server_id: "srv1", remote_name: name, display_name: name, enabled: true, zones: [], lpr: false,
  status: "online", fps: 5, group_ids: [], folder_id: null, sort_order: 0, default_live_quality: "sub", description: "", location: "", tags: [],
  created_at: "", updated_at: "",
});

function stubBackend(cameras: ReturnType<typeof camera>[]) {
  vi.stubGlobal("fetch", vi.fn(stubApi({
    "/api/v1/me": () => json({ id: "u1", username: "op", display_name: "Op", mfa_enabled: false, must_change_password: false, auth_method: "session", tenant_id: "t1", grants: [{ permission: "live.view", effect: "allow", scope_type: "platform" }] }),
    "/api/v1/cameras": () => json({ items: cameras }),
    "/api/v1/sites": () => json({ items: [{ id: "s1", tenant_id: "t1", name: "Alpha" }, { id: "s2", tenant_id: "t1", name: "Beta" }] }),
    "/api/v1/servers": () => json({ items: [] }),
    "/api/v1/views": () => json({ items: [] }),
  })));
}

beforeEach(() => {
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

  it("orders by site then name and filters by site chip", async () => {
    stubPhone(true);
    stubBackend([camera("c1", "Zed", "s1"), camera("c2", "Beta cam", "s2"), camera("c3", "Able", "s1")]);
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver Zed" });
    const names = () => screen.getAllByRole("button", { name: /^Ver / }).map((b) => b.getAttribute("aria-label"));
    expect(names()).toEqual(["Ver Able", "Ver Zed", "Ver Beta cam"]);
    const all = screen.getByRole("button", { name: "Todas" });
    expect(all).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "Beta" }));
    expect(names()).toEqual(["Ver Beta cam"]);
    expect(screen.getByRole("button", { name: "Beta" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(all);
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

  it("resets to page 1 when the site chip changes", async () => {
    stubPhone(true);
    const list = [...Array.from({ length: 5 }, (_, i) => camera(`a${i + 1}`, `A${i + 1}`, "s1")), ...Array.from({ length: 5 }, (_, i) => camera(`b${i + 1}`, `B${i + 1}`, "s2"))];
    stubBackend(list);
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver A1" });
    fireEvent.click(screen.getByRole("button", { name: "Página siguiente" }));
    expect(screen.getByTestId("phone-page-status")).toHaveTextContent("Página 2 de 3");
    fireEvent.click(screen.getByRole("button", { name: "Beta" }));
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
    fireEvent.click(screen.getByRole("button", { name: "Beta" }));
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
    expect(screen.getByRole("button", { name: "Beta" })).toHaveAttribute("aria-pressed", "true");
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
