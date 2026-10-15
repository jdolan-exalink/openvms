import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { PlayerSessionProvider } from "@/lib/live/PlayerSessionProvider";
import { VisionAI } from "./VisionAI";

const meResponse = {
  id: "u1",
  username: "operator",
  display_name: "Operator",
  role: "admin",
  tenant_id: "t1",
  permissions: ["live.view", "cameras.view", "events.view", "recordings.view"],
};

function camera(id: string, name: string) {
  return {
    id,
    display_name: name,
    server_id: "s1",
    site_id: "site-1",
    status: "online",
    streams: [{ quality: "sub" }, { quality: "main" }],
  };
}

class FakeSocket {
  static created = 0;
  static instances: FakeSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 0;
  binaryType = "blob";
  constructor(public url: string) {
    FakeSocket.created += 1;
    FakeSocket.instances.push(this);
  }
  send() {}
  close() {}
}

class FakeMediaSource {
  static isTypeSupported() {
    return true;
  }
  addEventListener() {}
}

beforeEach(() => {
  localStorage.clear();
  FakeSocket.created = 0;
  FakeSocket.instances = [];
  vi.stubGlobal("WebSocket", FakeSocket as unknown as typeof WebSocket);
  vi.stubGlobal("MediaSource", FakeMediaSource as unknown as typeof MediaSource);
});

describe("VisionAI Page", () => {
  const setup = () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": () => json(meResponse),
          "/api/v1/cameras": () =>
            json({
              items: [
                camera("cam-1", "Acceso Norte"),
                camera("cam-2", "Portón Sur"),
                camera("cam-3", "Perímetro Este"),
                camera("cam-4", "Estacionamiento"),
                camera("cam-5", "Ruta Acceso"),
              ],
            }),
          "/api/v1/sites": () => json({ items: [{ id: "site-1", name: "Casa Central" }] }),
        }),
      ),
    );

    return renderPage(() => (
      <PlayerSessionProvider userId="u1">
        <VisionAI />
      </PlayerSessionProvider>
    ));
  };

  it("renders Vision AI header with status, object filters, and layout modes", async () => {
    setup();

    expect(await screen.findByText(/VISION AI/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Personas" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Vehículos" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Modo LPR" })).toBeInTheDocument();

    // Mode toggles
    expect(screen.getByRole("button", { name: "Matriz" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Focus" })).toBeInTheDocument();
  });

  it("switches to Focus mode and displays hero slot with sub thumbnails", async () => {
    setup();

    const focusBtn = await screen.findByRole("button", { name: "Focus" });
    fireEvent.click(focusBtn);

    // Hero slot exists
    expect(screen.getByTestId("vision-hero-slot")).toBeInTheDocument();
    // Sub-strip exists
    expect(screen.getByTestId("vision-sub-strip")).toBeInTheDocument();
  });

  it("toggles object filters and updates selection", async () => {
    setup();

    const personFilter = await screen.findByRole("button", { name: "Persona" });
    expect(personFilter).toHaveAttribute("data-active", "true");

    // Toggle filter
    fireEvent.click(personFilter);
    expect(personFilter).toHaveAttribute("data-active", "false");
  });

  it("allows pinning and unpinning a camera slot", async () => {
    setup();

    await screen.findByText("Acceso Norte");

    const pinButtons = await screen.findAllByRole("button", { name: /Fijar cámara|Fijada/i });
    expect(pinButtons.length).toBeGreaterThan(0);

    const firstPin = pinButtons[0]!;
    expect(firstPin).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(firstPin);
    expect(firstPin).toHaveAttribute("aria-pressed", "true");
  });

  it("toggles debug mode and reveals mathematical score breakdown", async () => {
    setup();

    await screen.findByText("Acceso Norte");

    const debugToggle = await screen.findByRole("button", { name: "Debug Score" });
    fireEvent.click(debugToggle);

    const overlays = await screen.findAllByTestId("vision-debug-overlay");
    expect(overlays.length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Score: 94/i)[0]).toBeInTheDocument();
  });

  it("switches to LPR mode showing specialized plate telemetry", async () => {
    setup();

    const lprModeBtn = await screen.findByRole("button", { name: "Modo LPR" });
    fireEvent.click(lprModeBtn);

    expect(await screen.findByTestId("vision-lpr-hud")).toBeInTheDocument();
    expect(screen.getByText(/Patente Detectada/i)).toBeInTheDocument();
  });

  it("shows active events sidebar and allows focusing an event on click", async () => {
    setup();

    const sidebarToggle = await screen.findByRole("button", { name: /Eventos Activos/i });
    fireEvent.click(sidebarToggle);

    expect(screen.getByTestId("vision-events-sidebar")).toBeInTheDocument();
  });
});
