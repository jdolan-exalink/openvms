import { fireEvent, screen, waitFor, within } from "@testing-library/react";
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

const meResponse = {
  id: "u1",
  username: "operator",
  display_name: "Operator",
  role: "admin",
  tenant_id: "t1",
  permissions: ["live.view", "cameras.view", "events.view", "recordings.view"],
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
let slotRequests: URL[];
let slotsStatus: number;

beforeEach(() => {
  localStorage.clear();
  navigateMock.mockReset();
  slotRequests = [];
  slotsStatus = 200;
  slotsBody = { slots: [slot(0, "cam-1"), slot(1, "cam-2", { object: "car", plate: "AB123CD" }), slot(2, null), slot(3, null)] };
});

describe("VisionAI Page", () => {
  const setup = () => {
    const routes = stubApi({
      "/api/v1/me": () => json(meResponse),
      "/api/v1/vision-ai/slots": () =>
        slotsStatus === 200
          ? json({ source: "general", layout: "4", generated_at: "2026-10-15T10:00:00Z", ...slotsBody })
          : json({ code: "boom", message: "motor no disponible" }, slotsStatus),
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/vision-ai/slots") slotRequests.push(url);
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

  it("shows a no-permission notice in LPR mode when filled slots carry no plate", async () => {
    slotsBody = { slots: [slot(0, "cam-1"), slot(1, "cam-2"), slot(2, null), slot(3, null)] };
    setup();
    fireEvent.click(await screen.findByRole("button", { name: "Modo LPR" }));
    expect(await screen.findByText(/lecturas de patente no disponibles/i)).toBeInTheDocument();
    expect(screen.queryByTestId("vision-lpr-hud")).not.toBeInTheDocument();
  });

  it("lists active events in the sidebar", async () => {
    setup();
    fireEvent.click(await screen.findByRole("button", { name: /Eventos Activos/i }));
    const sidebar = await screen.findByTestId("vision-events-sidebar");
    expect(within(sidebar).getByText("Cam cam-1")).toBeInTheDocument();
    expect(within(sidebar).getByText("AB123CD")).toBeInTheDocument();
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
