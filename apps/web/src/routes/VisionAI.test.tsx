import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { focusManager } from "@tanstack/react-query";
import { json, renderPage, stubApi } from "@/test-utils";
import { VISION_SLOTS_REFETCH_MS } from "@/lib/visionAi";
import { DEFAULT_VISION_PREFS, loadVisionPrefs, saveVisionPrefs, visionPrefsKey } from "@/lib/visionAiPrefs";
import { VisionAI } from "./VisionAI";

vi.mock("@/components/MsePlayer", () => ({
  MsePlayer: ({ cameraId, quality, className }: { cameraId: string; quality?: string; className?: string }) => (
    <div data-testid="mse-player" data-camera-id={cameraId} data-quality={quality} data-class={className} />
  ),
}));

const navigateMock = vi.fn();
vi.mock("@tanstack/react-router", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-router")>()),
  useNavigate: () => navigateMock,
}));

let mePermissions = ["live.view", "cameras.view", "events.view", "recordings.view"];
const meResponse = {
  id: "u1",
  username: "operator",
  display_name: "Operator",
  role: "admin",
  tenant_id: "t1",
  get grants() {
    return mePermissions.map((permission) => ({ permission, effect: "allow", scope: { kind: "global" } }));
  },
};

type Slot = Record<string, unknown>;

function slot(n: number, cam: string | null, extra: Slot = {}): Slot {
  if (!cam) return { slot: n, camera_id: null, pinned: false };
  return {
    slot: n,
    camera_id: cam,
    pinned: false,
    camera_name: `Cam ${cam}`,
    event_id: `ev-${cam}`,
    object: "person",
    confidence: 0.9,
    zone: "Perímetro",
    score: 94,
    display_seconds: 14,
    breakdown: { object: 40, zone: 30 },
    ...extra,
  };
}

let slotsBody: { slots: Slot[] };
/** How far the server clock runs ahead of the browser; generated_at follows it like a real server. */
let serverAheadMs: number;
/** When set, /api/v1/me answers only after it resolves (simulates permissions loading late). */
let meGate: Promise<void> | null = null;
let slotRequests: URL[];
let slotsStatus: number;
let platesVisible: boolean;
let eventsBody: unknown[];
let platesBody: unknown[];
let eventRequests: URL[];

beforeEach(() => {
  localStorage.clear();
  mePermissions = ["live.view", "cameras.view", "events.view", "recordings.view"];
  navigateMock.mockReset();
  slotRequests = [];
  slotsStatus = 200;
  platesVisible = true;
  eventsBody = [];
  platesBody = [];
  eventRequests = [];
  serverAheadMs = 0;
  slotsBody = { slots: [slot(0, "cam-1"), slot(1, "cam-2", { object: "car", plate: "AB123CD" }), slot(2, null), slot(3, null)] };
});

describe("VisionAI Page", () => {
  const setup = () => {
    const routes = stubApi({
      "/api/v1/me": () => json(meResponse),
      "/api/v1/events": () => json({ items: eventsBody }),
      "/api/v1/lpr/reads": () => json({ items: platesBody }),
      "/api/v1/vision-ai/slots": () =>
        slotsStatus === 200
          ? json({ source: "general", layout: "4", generated_at: new Date(Date.now() + serverAheadMs).toISOString(), plates_visible: platesVisible, ...slotsBody })
          : json({ code: "boom", message: "motor no disponible" }, slotsStatus),
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/vision-ai/slots") slotRequests.push(url);
        if (url.pathname === "/api/v1/events") eventRequests.push(url);
        if (url.pathname === "/api/v1/me" && meGate) await meGate;
        return routes(input);
      }),
    );
    return renderPage(() => <VisionAI />);
  };
  const lastQuery = () => slotRequests.at(-1)?.searchParams;
  const playerCams = () => screen.queryAllByTestId("mse-player").map((p) => p.getAttribute("data-camera-id"));
  const playerOf = (cam: string) => screen.getAllByTestId("mse-player").find((p) => p.getAttribute("data-camera-id") === cam);
  const tileOrder = (cam: string) => playerOf(cam)?.closest<HTMLElement>("[data-vision-tile]")?.style.order;

  it("orders matrix tiles by priority, empty cells last", async () => {
    slotsBody = { slots: [slot(0, "cam-1", { score: 20 }), slot(1, null), slot(2, "cam-3", { score: 90 }), slot(3, null)] };
    setup();
    await waitFor(() => expect(tileOrder("cam-3")).toBe("0"));
    expect(tileOrder("cam-1")).toBe("1");
    const emptyOrders = screen.getAllByText("Sin evento asignado").map((el) => Number(el.closest<HTMLElement>("[data-vision-tile]")?.style.order));
    expect(emptyOrders).toHaveLength(2);
    for (const order of emptyOrders) expect(order).toBeGreaterThan(1);
  });

  it("asks for the 30s inactivity timeout by default and remembers a new choice", async () => {
    setup();
    await waitFor(() => expect(lastQuery()?.get("inactivity")).toBe("30"));
    fireEvent.change(await screen.findByLabelText("Cerrar tras inactividad"), { target: { value: "60" } });
    await waitFor(() => expect(lastQuery()?.get("inactivity")).toBe("60"));
    expect(loadVisionPrefs(visionPrefsKey("t1", "u1")).inactivitySeconds).toBe(60);
  });

  it("measures the age against the server clock, not the browser's", async () => {
    // The browser runs 60s behind the server: generated_at is ahead of Date.now().
    vi.useFakeTimers({ shouldAdvanceTime: true, now: Date.parse("2026-10-21T10:00:00Z") });
    try {
      serverAheadMs = 60_000;
      slotsBody = { slots: [slot(0, "cam-1", { active: false, last_seen_at: "2026-10-21T10:00:55Z" }), slot(1, null), slot(2, null), slot(3, null)] };
      setup();
      expect(await screen.findByText("hace 5s")).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("shows EN VIVO while the detection is running", async () => {
    slotsBody = { slots: [slot(0, "cam-1", { active: true, last_seen_at: "2026-10-21T10:00:00Z" }), slot(1, null), slot(2, null), slot(3, null)] };
    setup();
    expect(await screen.findByText("EN VIVO", { selector: "[data-detection-age]" })).toBeInTheDocument();
    expect(screen.queryByText(/^hace /)).not.toBeInTheDocument();
  });

  it("counts the seconds since the last activity once the detection ended", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: Date.parse("2026-10-21T10:00:12Z") });
    try {
      slotsBody = { slots: [slot(0, "cam-1", { active: false, detected_at: "2026-10-21T09:59:00Z", last_seen_at: "2026-10-21T10:00:00Z" }), slot(1, null), slot(2, null), slot(3, null)] };
      setup();
      expect(await screen.findByText("hace 12s")).toBeInTheDocument();
      await vi.advanceTimersByTimeAsync(3000);
      expect(await screen.findByText("hace 15s")).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("renders a player per filled slot, sized to fill the slot", async () => {
    setup();
    expect(await screen.findByText("Cam cam-1")).toBeInTheDocument();
    expect(playerCams()).toEqual(["cam-1", "cam-2"]);
    for (const p of screen.getAllByTestId("mse-player")) expect(p.getAttribute("data-class")).toContain("size-full");
    expect(screen.getAllByText("Sin evento asignado")).toHaveLength(2);
  });

  it("requests the general source with the matrix layout by default", async () => {
    setup();
    await screen.findByText("Cam cam-1");
    expect(lastQuery()?.get("source")).toBe("general");
    expect(lastQuery()?.get("layout")).toBe("4");
  });

  it("restores the saved view for the same user and ignores it for another user", async () => {
    const first = setup();
    await screen.findByText("Cam cam-1");
    fireEvent.click(screen.getByRole("button", { name: "Vehículos" }));
    fireEvent.click(screen.getByRole("button", { name: "Dinámica" }));
    await waitFor(() => expect(lastQuery()?.get("layout")).toBe("16"));
    expect(lastQuery()?.get("source")).toBe("vehiculos");
    first.unmount();

    slotRequests = [];
    setup();
    await waitFor(() => expect(lastQuery()?.get("layout")).toBe("16"));
    expect(lastQuery()?.get("source")).toBe("vehiculos");
    expect(screen.getByRole("button", { name: "Dinámica" })).toHaveClass("text-primary");
    cleanup();

    meResponse.id = "u2";
    try {
      slotRequests = [];
      setup();
      await screen.findByText("Cam cam-1");
      expect(lastQuery()?.get("source")).toBe("general");
      expect(lastQuery()?.get("layout")).toBe("4");
    } finally {
      meResponse.id = "u1";
    }
  });

  it("loads the new user's saved view when the signed-in user changes without a reload", async () => {
    saveVisionPrefs(visionPrefsKey("t1", "u2"), { ...DEFAULT_VISION_PREFS, mode: "dynamic", presetSource: "personas" });
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      setup();
      await screen.findByText("Cam cam-1");
      fireEvent.click(screen.getByRole("button", { name: "Vehículos" }));
      await waitFor(() => expect(lastQuery()?.get("source")).toBe("vehiculos"));

      meResponse.id = "u2";
      vi.setSystemTime(Date.now() + 61_000);
      focusManager.setFocused(false);
      focusManager.setFocused(true);
      await waitFor(() => expect(lastQuery()?.get("source")).toBe("personas"));
      expect(lastQuery()?.get("layout")).toBe("16");
      expect(loadVisionPrefs(visionPrefsKey("t1", "u2")).presetSource).toBe("personas");
    } finally {
      focusManager.setFocused(undefined);
      meResponse.id = "u1";
      vi.useRealTimers();
    }
  });

  it("maps presets, modes and layout selectors to source and layout", async () => {
    setup();
    await screen.findByText("Cam cam-1");

    fireEvent.click(screen.getByRole("button", { name: "Personas" }));
    await waitFor(() => expect(lastQuery()?.get("source")).toBe("personas"));

    fireEvent.click(screen.getByRole("button", { name: "Perímetro" }));
    await waitFor(() => expect(lastQuery()?.get("source")).toBe("perimetro"));

    fireEvent.click(screen.getByRole("button", { name: "3×3" }));
    await waitFor(() => expect(lastQuery()?.get("layout")).toBe("9"));

    fireEvent.click(screen.getByRole("button", { name: "Focus" }));
    await waitFor(() => expect(lastQuery()?.get("layout")).toBe("focus"));
    expect(lastQuery()?.get("source")).toBe("perimetro");

    fireEvent.click(screen.getByRole("button", { name: "Modo LPR" }));
    await waitFor(() => expect(lastQuery()?.get("source")).toBe("lpr"));

    fireEvent.click(screen.getAllByRole("button", { name: "Alarmas" })[0]!);
    await waitFor(() => expect(lastQuery()?.get("source")).toBe("alarmas"));
  });

  it("shows the hero slot and the other slots in focus mode", async () => {
    slotsBody = { slots: [slot(0, "cam-1"), slot(1, "cam-2"), slot(2, null), slot(3, null), slot(4, null)] };
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "Focus" }));
    const hero = await screen.findByTestId("vision-hero-slot");
    await waitFor(() => expect(within(hero).getByTestId("mse-player")).toHaveAttribute("data-camera-id", "cam-1"));
    expect(within(screen.getByTestId("vision-sub-strip")).getByTestId("mse-player")).toHaveAttribute("data-camera-id", "cam-2");
  });

  it("shows an empty state when no slot has a camera", async () => {
    slotsBody = { slots: [slot(0, null), slot(1, null), slot(2, null), slot(3, null)] };
    setup();
    expect(await screen.findByText(/Sin eventos recientes/i)).toBeInTheDocument();
    expect(playerCams()).toEqual([]);
  });

  it("shows the error with a retry action", async () => {
    slotsStatus = 500;
    setup();
    expect(await screen.findByText(/motor no disponible/i)).toBeInTheDocument();
    const before = slotRequests.length;
    slotsStatus = 200;
    fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    expect(await screen.findByText("Cam cam-1")).toBeInTheDocument();
    expect(slotRequests.length).toBeGreaterThan(before);
  });

  it("hides slots whose object is filtered out", async () => {
    setup();
    await screen.findByText("Cam cam-1");
    fireEvent.click(screen.getByRole("button", { name: "Persona" }));
    await waitFor(() => expect(playerCams()).toEqual(["cam-2"]));
    expect(screen.getAllByText("Sin evento asignado")).toHaveLength(3);
  });

  it("keeps a pinned camera when the ranked slot changes, and resumes on unpin", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      setup();
      await screen.findByText("Cam cam-1");
      fireEvent.click(screen.getAllByRole("button", { name: "Fijar cámara" })[0]!);
      expect(screen.getAllByRole("button", { name: "Fijada" })[0]).toHaveAttribute("aria-pressed", "true");

      slotsBody = { slots: [slot(0, "cam-9"), slot(1, "cam-2"), slot(2, null), slot(3, null)] };
      await vi.advanceTimersByTimeAsync(2100);
      await waitFor(() => expect(playerCams()).toContain("cam-2"));
      expect(playerCams()).toEqual(["cam-1", "cam-2"]);

      fireEvent.click(screen.getByRole("button", { name: "Fijada" }));
      await waitFor(() => expect(playerCams()).toEqual(["cam-9", "cam-2"]));
    } finally {
      vi.useRealTimers();
    }
  });

  it("opens playback for the slot camera", async () => {
    setup();
    await screen.findByText("Cam cam-1");
    fireEvent.click(screen.getAllByRole("button", { name: "Abrir en Grabaciones" })[0]!);
    expect(navigateMock).toHaveBeenCalledWith({ to: "/playback", search: { camera: "cam-1" } });
  });

  it("toggles debug mode and reveals the score breakdown from the slot", async () => {
    setup();
    await screen.findByText("Cam cam-1");
    fireEvent.click(screen.getByRole("button", { name: "Debug Score" }));
    expect((await screen.findAllByTestId("vision-debug-overlay")).length).toBe(2);
    expect(screen.getAllByText(/Score: 94/i)[0]).toBeInTheDocument();
    expect(screen.getAllByText(/object: \+40/)[0]).toBeInTheDocument();
  });

  const plateRead = (id: string, plate: string, seen_at: string, extra: Record<string, unknown> = {}) => ({
    id, site_id: "s", site_name: "Sitio", server_id: "sv", server_name: "Srv", camera_id: `cam-${id}`, camera_name: `Garita ${id}`,
    plate, plate_normalized: plate, label: "car", zones: ["acceso"], seen_at, score: 0.93, ...extra,
  });

  it("lists every plate read in LPR mode, not only the ranked slots", async () => {
    platesBody = [
      plateRead("p1", "AB123CD", "2026-10-21T10:00:30Z"),
      plateRead("p2", "AE526KV", "2026-10-21T10:00:00Z"),
      plateRead("p3", "AA111BB", "2026-10-21T09:59:00Z"),
    ];
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "Modo LPR" }));
    const rows = await screen.findAllByTestId("vision-lpr-hud");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("AB123CD");
    expect(rows[0]).toHaveTextContent("Garita p1");
    expect(rows[2]).toHaveTextContent("AA111BB");
    expect(screen.getByText(/Patente Detectada/i)).toBeInTheDocument();
  });

  it("opens the vehicle photo popup when a plate is clicked", async () => {
    mePermissions = [...mePermissions, "snapshots.view"];
    platesBody = [plateRead("p1", "AB123CD", "2026-10-21T10:00:30Z")];
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "Modo LPR" }));
    fireEvent.click(await screen.findByTestId("vision-lpr-hud"));
    const popup = await screen.findByRole("dialog", { name: /Detalle del evento/i });
    expect(within(popup).getByLabelText("Patente AB123CD")).toBeInTheDocument();
    expect(popup).toHaveTextContent("Garita p1");
    expect(within(popup).getByRole("img", { name: /Garita p1/ })).toHaveAttribute("src", "/media/v1/lpr/reads/p1/snapshot.jpg");
    fireEvent.click(within(popup).getByRole("button", { name: /Cerrar evento/i }));
    expect(screen.queryByRole("dialog", { name: /Detalle del evento/i })).not.toBeInTheDocument();
  });

  it("asks for a long history of reads in LPR mode", async () => {
    const asked: URL[] = [];
    platesBody = [];
    setup();
    const wrapped = globalThis.fetch;
    vi.stubGlobal("fetch", vi.fn(async (input: Request) => {
      const url = new URL(input.url);
      if (url.pathname === "/api/v1/lpr/reads") asked.push(url);
      return wrapped(input);
    }));
    fireEvent.click(await screen.findByRole("button", { name: "Modo LPR" }));
    await waitFor(() => expect(asked.length).toBeGreaterThan(0));
    expect(Number(asked[0]!.searchParams.get("limit"))).toBeGreaterThanOrEqual(100);
  });

  it("shows the no-permission notice in LPR mode only when plates_visible is false", async () => {
    platesVisible = false;
    platesBody = [plateRead("p1", "AB123CD", "2026-10-21T10:00:30Z")];
    slotsBody = { slots: [slot(0, "cam-1"), slot(1, "cam-2"), slot(2, null), slot(3, null)] };
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "Modo LPR" }));
    expect(await screen.findByText(/lecturas de patente no disponibles/i)).toBeInTheDocument();
    expect(screen.queryByText(/sin lecturas de patente recientes/i)).not.toBeInTheDocument();
    expect(screen.queryByTestId("vision-lpr-hud")).not.toBeInTheDocument();
  });

  it("shows 'Sin lecturas de patente recientes' when plates are visible but there are no reads", async () => {
    platesBody = [];
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "Modo LPR" }));
    expect(await screen.findByText(/sin lecturas de patente recientes/i)).toBeInTheDocument();
    expect(screen.queryByText(/lecturas de patente no disponibles/i)).not.toBeInTheDocument();
  });

  it("lists active events in the sidebar", async () => {
    setup();
    fireEvent.click(await screen.findByRole("button", { name: /^Eventos/i }));
    const sidebar = await screen.findByTestId("vision-events-sidebar");
    expect(within(sidebar).getByText("Cam cam-1")).toBeInTheDocument();
    expect(within(sidebar).getByText("AB123CD")).toBeInTheDocument();
  });

  it("names the side bar Eventos and offers the four tabs", async () => {
    mePermissions = [...mePermissions, "lpr.view"];
    setup();
    fireEvent.click(await screen.findByRole("button", { name: /^Eventos/i }));
    const sidebar = within(await screen.findByTestId("vision-events-sidebar"));
    expect(sidebar.getByText(/^Eventos \(\d+\)$/)).toBeInTheDocument();
    expect(sidebar.queryByText(/Eventos Activos/)).not.toBeInTheDocument();
    expect(sidebar.getAllByRole("tab").map((t) => t.textContent)).toEqual(["Activos", "Históricos", "LPR", "Faciales"]);
    expect(sidebar.getByRole("tab", { name: "Activos" })).toHaveAttribute("aria-selected", "true");
  });

  it("hides the tabs the user has no permission for", async () => {
    mePermissions = ["live.view", "cameras.view"];
    setup();
    fireEvent.click(await screen.findByRole("button", { name: /^Eventos/i }));
    const sidebar = within(await screen.findByTestId("vision-events-sidebar"));
    expect(sidebar.getAllByRole("tab").map((t) => t.textContent)).toEqual(["Activos"]);
  });

  it("opens a popup above the grid with the event and its plate, and closes it", async () => {
    setup();
    fireEvent.click(await screen.findByRole("button", { name: /^Eventos/i }));
    const sidebar = within(await screen.findByTestId("vision-events-sidebar"));
    fireEvent.click(sidebar.getByText("Cam cam-2"));
    const popup = await screen.findByRole("dialog", { name: /Detalle del evento/i });
    expect(popup).toHaveTextContent("Cam cam-2");
    expect(within(popup).getByLabelText("Patente AB123CD")).toBeInTheDocument();
    // Selecting an event no longer switches to the focus layout on its own.
    expect(screen.queryByTestId("vision-hero-slot")).not.toBeInTheDocument();
    fireEvent.click(within(popup).getByRole("button", { name: /Cerrar evento/i }));
    expect(screen.queryByRole("dialog", { name: /Detalle del evento/i })).not.toBeInTheDocument();
  });

  it("pins the popup's active event in the focus view", async () => {
    setup();
    fireEvent.click(await screen.findByRole("button", { name: /^Eventos/i }));
    const sidebar = within(await screen.findByTestId("vision-events-sidebar"));
    fireEvent.click(sidebar.getByText("Cam cam-2"));
    fireEvent.click(within(await screen.findByRole("dialog", { name: /Detalle del evento/i })).getByRole("button", { name: /Fijar en foco/i }));
    expect(await screen.findByTestId("vision-hero-slot")).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: /Detalle del evento/i })).not.toBeInTheDocument();
  });

  it("lists historic events on its tab", async () => {
    eventsBody = [{
      id: "h-1", tenant_id: "t1", site_id: "s", site_name: "S", server_id: "sv", server_name: "Srv", camera_id: "c9", camera_name: "Garita Sur",
      remote_id: "r", severity: "detection", labels: ["car"], sub_labels: [], zones: ["acceso"], plates: [], start_time: "2026-10-21T09:00:00Z",
      reviewed: false, has_thumbnail: true, has_snapshot: false, has_preview: false, tracks: [],
    }];
    setup();
    fireEvent.click(await screen.findByRole("button", { name: /^Eventos/i }));
    const sidebar = within(await screen.findByTestId("vision-events-sidebar"));
    fireEvent.click(sidebar.getByRole("tab", { name: "Históricos" }));
    expect(await sidebar.findByText("Garita Sur")).toBeInTheDocument();
    expect(eventRequests.every((u) => !u.searchParams.has("label"))).toBe(true);
  });

  it("lists plate reads on the LPR tab and face events on the Faciales tab", async () => {
    mePermissions = [...mePermissions, "lpr.view"];
    platesBody = [{
      id: "pr-1", site_id: "s", site_name: "S", server_id: "sv", server_name: "Srv", camera_id: "c7", camera_name: "Ingreso Norte",
      plate: "AE526KV", plate_normalized: "AE526KV", label: "car", zones: [], seen_at: "2026-10-21T08:00:00Z",
    }];
    eventsBody = [{
      id: "f-1", tenant_id: "t1", site_id: "s", site_name: "S", server_id: "sv", server_name: "Srv", camera_id: "c8", camera_name: "Hall Principal",
      remote_id: "r", severity: "detection", labels: ["face"], sub_labels: ["Ana"], zones: [], plates: [], start_time: "2026-10-21T09:30:00Z",
      reviewed: false, has_thumbnail: true, has_snapshot: false, has_preview: false, tracks: [],
    }];
    setup();
    fireEvent.click(await screen.findByRole("button", { name: /^Eventos/i }));
    const sidebar = within(await screen.findByTestId("vision-events-sidebar"));
    fireEvent.click(sidebar.getByRole("tab", { name: "LPR" }));
    expect(await sidebar.findByText("Ingreso Norte")).toBeInTheDocument();
    expect(sidebar.getByLabelText("Patente AE526KV")).toBeInTheDocument();
    fireEvent.click(sidebar.getByRole("tab", { name: "Faciales" }));
    expect(await sidebar.findByText("Hall Principal")).toBeInTheDocument();
    expect(sidebar.getByText("Ana")).toBeInTheDocument();
    await waitFor(() => expect(eventRequests.some((u) => u.searchParams.getAll("label").includes("face"))).toBe(true));
  });

  it("shows an icon on every preset and object chip", async () => {
    setup();
    await screen.findByText("Cam cam-1");
    const presetIcons: Record<string, string> = {
      Todo: "lucide-layout-grid", Personas: "lucide-users", "Vehículos": "lucide-car", LPR: "lucide-rectangle-horizontal",
      "Perímetro": "lucide-fence", Alarmas: "lucide-bell",
    };
    for (const [name, cls] of Object.entries(presetIcons)) {
      const btn = screen.getAllByRole("button", { name })[0]!;
      const svg = btn.querySelector("svg");
      expect(svg, name).toHaveClass(cls);
      expect(svg).toHaveAttribute("aria-hidden", "true");
    }
    const chipIcons: Record<string, string> = {
      Persona: "lucide-user", Auto: "lucide-car", Moto: "lucide-bike", "Camión": "lucide-truck",
      Rostro: "lucide-scan-face",
    };
    for (const [name, cls] of Object.entries(chipIcons)) {
      expect(screen.getByRole("button", { name }).querySelector("svg"), name).toHaveClass(cls);
    }
    const lprChip = screen.getAllByRole("button", { name: "LPR" }).find((b) => b.hasAttribute("data-active"));
    expect(lprChip?.querySelector("svg")).toHaveClass("lucide-rectangle-horizontal");
  });

  it("shows the object icon in the slot badge", async () => {
    setup();
    await screen.findByText("Cam cam-1");
    const badge = screen.getByText(/^person 90%$/i);
    expect(badge.querySelector("svg")).toHaveClass("lucide-user");
    expect(badge.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
  });

  describe("active events snapshots", () => {
    const openSidebar = async () => {
      setup();
      fireEvent.click(await screen.findByRole("button", { name: /^Eventos/i }));
      return within(await screen.findByTestId("vision-events-sidebar"));
    };

    it("uses the snapshot when the user can view snapshots", async () => {
      mePermissions = [...mePermissions, "snapshots.view"];
      const sidebar = await openSidebar();
      const img = await sidebar.findByRole("img", { name: "Detección: person en Cam cam-1" });
      expect(img).toHaveAttribute("src", "/media/v1/events/ev-cam-1/snapshot.jpg");
      expect(img).toHaveAttribute("loading", "lazy");
    });

    it("uses the thumbnail without snapshot permission", async () => {
      const sidebar = await openSidebar();
      const img = await sidebar.findByRole("img", { name: "Detección: person en Cam cam-1" });
      expect(img).toHaveAttribute("src", "/api/v1/events/ev-cam-1/thumbnail");
    });

    it("requests the snapshot once permissions load, even if the early thumbnail failed", async () => {
      mePermissions = [...mePermissions, "snapshots.view"];
      let releaseMe = () => {};
      meGate = new Promise<void>((resolve) => (releaseMe = resolve));
      try {
        const sidebar = await openSidebar();
        const img = await sidebar.findByRole("img", { name: "Detección: person en Cam cam-1" });
        expect(img).toHaveAttribute("src", "/api/v1/events/ev-cam-1/thumbnail");
        fireEvent.error(img);
        await waitFor(() => expect(sidebar.getByTestId("vision-event-placeholder-ev-cam-1")).toBeInTheDocument());
        releaseMe();
        await waitFor(() =>
          expect(sidebar.getByRole("img", { name: "Detección: person en Cam cam-1" })).toHaveAttribute("src", "/media/v1/events/ev-cam-1/snapshot.jpg"),
        );
      } finally {
        meGate = null;
      }
    });

    it("loads a fresh snapshot when the slot's event changes after a failed image", async () => {
      mePermissions = [...mePermissions, "snapshots.view"];
      vi.useFakeTimers({ shouldAdvanceTime: true });
      try {
        const sidebar = await openSidebar();
        const name = "Detección: person en Cam cam-1";
        fireEvent.error(await sidebar.findByRole("img", { name }));
        fireEvent.error(await sidebar.findByRole("img", { name }));
        await waitFor(() => expect(sidebar.getByTestId("vision-event-placeholder-ev-cam-1")).toBeInTheDocument());

        slotsBody = { slots: [slot(0, "cam-1", { event_id: "ev-next" }), slot(1, null), slot(2, null), slot(3, null)] };
        await vi.advanceTimersByTimeAsync(VISION_SLOTS_REFETCH_MS + 100);
        await waitFor(() => expect(sidebar.getByRole("img", { name })).toHaveAttribute("src", "/media/v1/events/ev-next/snapshot.jpg"));
        expect(sidebar.queryByTestId("vision-event-placeholder-ev-next")).not.toBeInTheDocument();
        expect(sidebar.queryByTestId("vision-event-placeholder-ev-cam-1")).not.toBeInTheDocument();
      } finally {
        vi.useRealTimers();
      }
    });

    it("falls back to the thumbnail once, then to the icon placeholder", async () => {
      mePermissions = [...mePermissions, "snapshots.view"];
      const sidebar = await openSidebar();
      const img = await sidebar.findByRole("img", { name: "Detección: person en Cam cam-1" });
      fireEvent.error(img);
      await waitFor(() => expect(sidebar.getByRole("img", { name: "Detección: person en Cam cam-1" })).toHaveAttribute("src", "/api/v1/events/ev-cam-1/thumbnail"));
      fireEvent.error(sidebar.getByRole("img", { name: "Detección: person en Cam cam-1" }));
      await waitFor(() => expect(sidebar.queryByRole("img", { name: "Detección: person en Cam cam-1" })).not.toBeInTheDocument());
      expect(sidebar.getByTestId("vision-event-placeholder-ev-cam-1")).toBeInTheDocument();
    });
  });

  describe("dynamic mode", () => {
    const gridOf = () => screen.getByTestId("vision-dynamic-grid");
    const sparse = (cams: string[]) => ({
      slots: Array.from({ length: 16 }, (_, i) => (cams[i] ? slot(i, cams[i]!) : slot(i, null))),
    });

    it("requests layout 16 and hides the size picker", async () => {
      setup();
      await screen.findByText("Cam cam-1");
      fireEvent.click(screen.getByRole("button", { name: "Dinámica" }));
      await waitFor(() => expect(lastQuery()?.get("layout")).toBe("16"));
      expect(screen.queryByRole("button", { name: "3×3" })).not.toBeInTheDocument();
    });

    it("renders only filled slots in a 2x2 grid without placeholders", async () => {
      slotsBody = sparse(["cam-1", "cam-2", "cam-3"]);
      setup();
      fireEvent.click(await screen.findByRole("button", { name: "Dinámica" }));
      await waitFor(() => expect(playerCams()).toEqual(["cam-1", "cam-2", "cam-3"]));
      expect(screen.queryByText("Sin evento asignado")).not.toBeInTheDocument();
      expect(gridOf().style.gridTemplateColumns).toBe("repeat(2, minmax(0, 1fr))");
      expect(gridOf().style.gridTemplateRows).toBe("repeat(2, minmax(0, 1fr))");
    });

    it("re-shapes the grid when the active count grows", async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true });
      try {
        slotsBody = sparse(["cam-1", "cam-2", "cam-3"]);
        setup();
        fireEvent.click(await screen.findByRole("button", { name: "Dinámica" }));
        await waitFor(() => expect(playerCams()).toHaveLength(3));

        slotsBody = sparse(["cam-1", "cam-2", "cam-3", "cam-4", "cam-5"]);
        await vi.advanceTimersByTimeAsync(2100);
        await waitFor(() => expect(playerCams()).toHaveLength(5));
        expect(gridOf().style.gridTemplateColumns).toBe("repeat(3, minmax(0, 1fr))");
        expect(gridOf().style.gridTemplateRows).toBe("repeat(2, minmax(0, 1fr))");
      } finally {
        vi.useRealTimers();
      }
    });

    it("shrinks in slot order and keeps the remaining players mounted", async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true });
      try {
        slotsBody = sparse(["cam-1", "cam-2", "cam-3", "cam-4", "cam-5"]);
        setup();
        fireEvent.click(await screen.findByRole("button", { name: "Dinámica" }));
        await waitFor(() => expect(playerCams()).toHaveLength(5));
        const firstPlayer = screen.getAllByTestId("mse-player")[0];

        slotsBody = sparse(["cam-1", "cam-2", "", "cam-4", "cam-5"]);
        await vi.advanceTimersByTimeAsync(2100);
        await waitFor(() => expect(playerCams()).toEqual(["cam-1", "cam-2", "cam-4", "cam-5"]));
        expect(gridOf().style.gridTemplateColumns).toBe("repeat(2, minmax(0, 1fr))");
        expect(gridOf().style.gridTemplateRows).toBe("repeat(2, minmax(0, 1fr))");
        expect(screen.getAllByTestId("mse-player")[0]).toBe(firstPlayer);
      } finally {
        vi.useRealTimers();
      }
    });

    it("moves the highest-priority camera first without re-mounting players", async () => {
      vi.useFakeTimers({ shouldAdvanceTime: true });
      try {
        slotsBody = { slots: [slot(0, "cam-1", { score: 80 }), slot(1, "cam-2", { score: 40 })] };
        setup();
        fireEvent.click(await screen.findByRole("button", { name: "Dinámica" }));
        await waitFor(() => expect(tileOrder("cam-1")).toBe("0"));
        expect(tileOrder("cam-2")).toBe("1");
        const cam2Player = playerOf("cam-2");

        slotsBody = { slots: [slot(0, "cam-1", { score: 80 }), slot(1, "cam-2", { score: 120 })] };
        await vi.advanceTimersByTimeAsync(2100);
        await waitFor(() => expect(tileOrder("cam-2")).toBe("0"));
        expect(tileOrder("cam-1")).toBe("1");
        expect(playerOf("cam-2")).toBe(cam2Player);
      } finally {
        vi.useRealTimers();
      }
    });

    it("shows a camera only once even if it fills two slots", async () => {
      slotsBody = sparse(["cam-1", "cam-1", "cam-2"]);
      setup();
      fireEvent.click(await screen.findByRole("button", { name: "Dinámica" }));
      await waitFor(() => expect(playerCams()).toEqual(["cam-1", "cam-2"]));
      expect(gridOf().style.gridTemplateColumns).toBe("repeat(2, minmax(0, 1fr))");
    });

    it("never draws more tiles than the 4x4 grid holds", async () => {
      slotsBody = { slots: Array.from({ length: 18 }, (_, i) => slot(i, `cam-${i + 1}`)) };
      setup();
      fireEvent.click(await screen.findByRole("button", { name: "Dinámica" }));
      await waitFor(() => expect(playerCams()).toHaveLength(16));
    });

    it("applies the object chips to the displayed cameras", async () => {
      slotsBody = sparse(["cam-1", "cam-2"]);
      setup();
      fireEvent.click(await screen.findByRole("button", { name: "Dinámica" }));
      await waitFor(() => expect(playerCams()).toHaveLength(2));
      fireEvent.click(screen.getByRole("button", { name: "Persona" }));
      expect(await screen.findByText(/Sin eventos recientes/i)).toBeInTheDocument();
      expect(playerCams()).toEqual([]);
    });

    it("shows the empty state with no active cameras", async () => {
      slotsBody = sparse([]);
      setup();
      fireEvent.click(await screen.findByRole("button", { name: "Dinámica" }));
      expect(await screen.findByText(/Sin eventos recientes/i)).toBeInTheDocument();
      expect(screen.queryByTestId("vision-dynamic-grid")).not.toBeInTheDocument();
    });
  });

  it("renders header, filters and layout modes", async () => {
    setup();
    expect(await screen.findByText(/VISION AI/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Vehículos" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Matriz" })).toBeInTheDocument();
    const person = screen.getByRole("button", { name: "Persona" });
    expect(person).toHaveAttribute("data-active", "true");
  });

  describe("events bar on the left", () => {
    it("puts the Eventos toggle first in the header", async () => {
      setup();
      const toggle = await screen.findByRole("button", { name: /^Eventos/i });
      const header = toggle.closest("header")!;
      const controls = Array.from(header.querySelectorAll("button"));
      expect(controls[0]).toBe(toggle);
    });

    it("opens the bar before the grid, on its left", async () => {
      setup();
      fireEvent.click(await screen.findByRole("button", { name: /^Eventos/i }));
      const sidebar = await screen.findByTestId("vision-events-sidebar");
      const grid = screen.getAllByTestId("mse-player")[0]!.closest("[data-vision-tile]")!.parentElement!;
      expect(sidebar.compareDocumentPosition(grid) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });
  });

  describe("full screen", () => {
    const original = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "requestFullscreen");
    let request: ReturnType<typeof vi.fn>;
    let left: boolean;

    beforeEach(() => {
      left = false;
      request = vi.fn(() => Promise.resolve());
      Object.defineProperty(HTMLElement.prototype, "requestFullscreen", { configurable: true, writable: true, value: request });
      // The element that last asked for full screen is the full screen element until the user leaves.
      Object.defineProperty(document, "fullscreenElement", { configurable: true, get: () => (left ? null : (request.mock.contexts.at(-1) ?? null)) });
    });
    afterEach(() => {
      if (original) Object.defineProperty(HTMLElement.prototype, "requestFullscreen", original);
      else delete (HTMLElement.prototype as { requestFullscreen?: unknown }).requestFullscreen;
      delete (document as { fullscreenElement?: unknown }).fullscreenElement;
    });

    it("asks the browser for real full screen, like F11", async () => {
      setup();
      fireEvent.click(await screen.findByRole("button", { name: "Pantalla completa" }));
      expect(request).toHaveBeenCalledTimes(1);
      const root = request.mock.contexts[0] as HTMLElement;
      expect(root.contains(screen.getByText(/VISION AI/i))).toBe(true);
      expect(root).toHaveClass("fixed", "inset-0");
      expect(await screen.findByRole("button", { name: "Salir de pantalla completa" })).toBeInTheDocument();
    });

    it("leaves full screen when the browser does (Esc)", async () => {
      setup();
      fireEvent.click(await screen.findByRole("button", { name: "Pantalla completa" }));
      await screen.findByRole("button", { name: "Salir de pantalla completa" });
      left = true;
      document.dispatchEvent(new Event("fullscreenchange"));
      expect(await screen.findByRole("button", { name: "Pantalla completa" })).toBeInTheDocument();
    });

    it("falls back to filling the page when the browser refuses", async () => {
      request.mockImplementation(() => Promise.reject(new Error("denied")));
      setup();
      fireEvent.click(await screen.findByRole("button", { name: "Pantalla completa" }));
      expect(await screen.findByRole("button", { name: "Salir de pantalla completa" })).toBeInTheDocument();
      expect((request.mock.contexts[0] as HTMLElement)).toHaveClass("fixed", "inset-0");
    });
  });
});
