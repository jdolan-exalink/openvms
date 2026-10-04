import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Live } from "@/routes/Live";

vi.mock("@/components/MsePlayer", () => ({
  MsePlayer: ({ cameraId, quality }: { cameraId: string; quality?: string }) => <div data-testid="player" data-camera={cameraId} data-quality={quality} />,
}));

/** MockIO records its targets so a test decides which cards are on screen. */
class MockIO {
  static instances: MockIO[] = [];
  targets = new Set<Element>();
  constructor(public cb: (entries: unknown[]) => void) {
    MockIO.instances.push(this);
  }
  observe(el: Element) {
    this.targets.add(el);
  }
  unobserve(el: Element) {
    this.targets.delete(el);
  }
  disconnect() {
    this.targets.clear();
  }
  takeRecords() {
    return [];
  }
}

function showCards(ids: string[]) {
  act(() => {
    for (const io of MockIO.instances) {
      const entries = [...io.targets].map((el, index) => {
        const id = (el as HTMLElement).dataset.cameraId ?? "";
        const on = ids.includes(id);
        return { target: el, isIntersecting: on, intersectionRatio: on ? 1 : 0, boundingClientRect: { top: index * 100, width: 320, height: 180 } };
      });
      io.cb(entries);
    }
  });
}

function stubPhone(phone: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: phone && query === "(max-width: 767px)", media: query, addEventListener() {}, removeEventListener() {} }));
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
  MockIO.instances = [];
  localStorage.clear();
  vi.stubGlobal("IntersectionObserver", MockIO);
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

  it("plays only visible cards and shows snapshots for the rest", async () => {
    stubPhone(true);
    stubBackend([camera("c1", "One"), camera("c2", "Two"), camera("c3", "Three")]);
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver One" });
    expect(screen.queryAllByTestId("player")).toHaveLength(0);
    expect(document.querySelector('img[src="/media/v1/cameras/c1/snapshot.jpg?h=360"]')).not.toBeNull();
    showCards(["c2"]);
    const players = screen.getAllByTestId("player");
    expect(players).toHaveLength(1);
    expect(players[0]).toHaveAttribute("data-camera", "c2");
    expect(players[0]).toHaveAttribute("data-quality", "sub");
    expect(document.querySelector('img[src="/media/v1/cameras/c2/snapshot.jpg?h=360"]')).toBeNull();
  });

  it("caps simultaneous live players at four", async () => {
    stubPhone(true);
    const list = Array.from({ length: 6 }, (_, i) => camera(`c${i + 1}`, `Cam ${i + 1}`));
    stubBackend(list);
    renderPage(Live);
    await screen.findByRole("button", { name: "Ver Cam 1" });
    showCards(list.map((c) => c.id));
    expect(screen.getAllByTestId("player").map((p) => p.getAttribute("data-camera"))).toEqual(["c1", "c2", "c3", "c4"]);
  });

  it("opens a single main-quality view and returns to the list keeping filter and scroll", async () => {
    stubPhone(true);
    stubBackend([camera("c1", "North", "s1"), camera("c2", "East", "s2")]);
    const { router } = renderPage(Live);
    await screen.findByRole("button", { name: "Ver North" });
    fireEvent.click(screen.getByRole("button", { name: "Beta" }));
    const list = screen.getByTestId("live-phone-list");
    list.scrollTop = 120;
    fireEvent.scroll(list);
    fireEvent.click(screen.getByRole("button", { name: "Ver East" }));
    await waitFor(() => expect(router.state.location.search).toMatchObject({ camera: "c2" }));
    const single = await screen.findByTestId("live-phone-single");
    const player = within(single).getByTestId("player");
    expect(player).toHaveAttribute("data-camera", "c2");
    expect(player).toHaveAttribute("data-quality", "main");
    expect(within(single).getByText("East")).toBeInTheDocument();
    expect(screen.getAllByTestId("player")).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Volver" }));
    await waitFor(() => expect(screen.queryByTestId("live-phone-single")).toBeNull());
    expect(router.state.location.search).not.toHaveProperty("camera");
    expect(screen.getByRole("button", { name: "Beta" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("live-phone-list").scrollTop).toBe(120);
  });
});
