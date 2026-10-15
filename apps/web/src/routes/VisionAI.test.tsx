import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
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
/** When set, /api/v1/me answers only after it resolves (simulates permissions loading late). */
let meGate: Promise<void> | null = null;
let slotRequests: URL[];
let slotsStatus: number;
let platesVisible: boolean;

beforeEach(() => {
  localStorage.clear();
  mePermissions = ["live.view", "cameras.view", "events.view", "recordings.view"];
  navigateMock.mockReset();
  slotRequests = [];
  slotsStatus = 200;
  platesVisible = true;
  slotsBody = { slots: [slot(0, "cam-1"), slot(1, "cam-2", { object: "car", plate: "AB123CD" }), slot(2, null), slot(3, null)] };
});

describe("VisionAI Page", () => {
  const setup = () => {
    const routes = stubApi({
      "/api/v1/me": () => json(meResponse),
      "/api/v1/vision-ai/slots": () =>
        slotsStatus === 200
          ? json({ source: "general", layout: "4", generated_at: "2026-10-15T10:00:00Z", plates_visible: platesVisible, ...slotsBody })
          : json({ code: "boom", message: "motor no disponible" }, slotsStatus),
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/vision-ai/slots") slotRequests.push(url);
        if (url.pathname === "/api/v1/me" && meGate) await meGate;
        return routes(input);
      }),
    );
    return renderPage(() => <VisionAI />);
  };
  const lastQuery = () => slotRequests.at(-1)?.searchParams;
  const playerCams = () => screen.queryAllByTestId("mse-player").map((p) => p.getAttribute("data-camera-id"));

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

  it("shows plate telemetry in LPR mode only for slots with a plate", async () => {
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "Modo LPR" }));
    const huds = await screen.findAllByTestId("vision-lpr-hud");
    expect(huds).toHaveLength(1);
    expect(within(huds[0]!).getByText("AB123CD")).toBeInTheDocument();
    expect(screen.getByText(/Patente Detectada/i)).toBeInTheDocument();
  });

  it("shows the no-permission notice in LPR mode only when plates_visible is false", async () => {
    platesVisible = false;
    slotsBody = { slots: [slot(0, "cam-1"), slot(1, "cam-2"), slot(2, null), slot(3, null)] };
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "Modo LPR" }));
    expect(await screen.findByText(/lecturas de patente no disponibles/i)).toBeInTheDocument();
    expect(screen.queryByText(/sin lecturas de patente recientes/i)).not.toBeInTheDocument();
    expect(screen.queryByTestId("vision-lpr-hud")).not.toBeInTheDocument();
  });

  it("shows 'Sin lecturas de patente recientes' when plates are visible but no slot has one", async () => {
    slotsBody = { slots: [slot(0, "cam-1"), slot(1, "cam-2"), slot(2, null), slot(3, null)] };
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "Modo LPR" }));
    expect(await screen.findByText(/sin lecturas de patente recientes/i)).toBeInTheDocument();
    expect(screen.queryByText(/lecturas de patente no disponibles/i)).not.toBeInTheDocument();
  });

  it("lists active events in the sidebar", async () => {
    setup();
    fireEvent.click(await screen.findByRole("button", { name: /Eventos Activos/i }));
    const sidebar = await screen.findByTestId("vision-events-sidebar");
    expect(within(sidebar).getByText("Cam cam-1")).toBeInTheDocument();
    expect(within(sidebar).getByText("AB123CD")).toBeInTheDocument();
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
      fireEvent.click(await screen.findByRole("button", { name: /Eventos Activos/i }));
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
});
