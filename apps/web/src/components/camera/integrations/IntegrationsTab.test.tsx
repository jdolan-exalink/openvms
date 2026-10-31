import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { IntegrationsTab } from "./IntegrationsTab";

function renderWithClient(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe("IntegrationsTab", () => {
  it("shows the status of each backend integration", () => {
    render(
      <IntegrationsTab
        integrations={[
          { name: "lpr", enabled: true } as never,
          { id: "frigate", label: "Frigate", status: "connected" },
          { id: "onvif", label: "ONVIF", status: "connected" },
          { id: "provision_isr", label: "Provision ISR", status: "not_configured" },
        ]}
      />,
    );
    expect(within(screen.getByTestId("integration-frigate")).getByText("Conectado")).toBeTruthy();
    expect(within(screen.getByTestId("integration-onvif")).getByText("Conectado")).toBeTruthy();
    expect(within(screen.getByTestId("integration-provision_isr")).getByText("No configurado")).toBeTruthy();
    // The plain feature flag is not a backend.
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
  });

  it("shows error and unknown states with their detail", () => {
    render(
      <IntegrationsTab
        integrations={[
          { id: "provision_isr", label: "Provision ISR", status: "error", detail: "Sin keepalive reciente" },
          { id: "onvif", label: "ONVIF", status: "unknown", detail: "Sin verificar" },
        ]}
      />,
    );
    expect(within(screen.getByTestId("integration-provision_isr")).getByText("Error")).toBeTruthy();
    expect(screen.getByText("Sin keepalive reciente")).toBeTruthy();
    expect(within(screen.getByTestId("integration-onvif")).getAllByText("Sin verificar").length).toBeGreaterThan(0);
  });

  it("handles loading and a model without integration data", () => {
    const { rerender } = render(<IntegrationsTab integrations={undefined} />);
    expect(screen.getByText("Cargando integraciones…")).toBeTruthy();
    rerender(<IntegrationsTab integrations={[]} />);
    expect(screen.getByText("Sin información de integraciones.")).toBeTruthy();
  });

  it("renders the 4 granular service controls and offers testing buttons", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          target: "lpr_provision",
          engine: "provision",
          passed: true,
          checks: [
            { id: "agent_reachable", status: "pass", message: "Agente alcanzable", detail: "OK" },
            { id: "keepalive_recent", status: "pass", message: "Keepalive reciente", detail: "hace 5s" },
          ],
          hints: [],
          observed: { keepalive_count: 12 },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );

    renderWithClient(
      <IntegrationsTab
        cameraId="c1"
        integrations={[
          { id: "lpr_provision", label: "LPR Provision-ISR", status: "connected", detail: "En línea con el agente" },
          { id: "analytics_provision", label: "Analíticas Provision", status: "connected", detail: "Analítica inteligente Profile M soportada" },
          { id: "analytics_frigate", label: "Analíticas Frigate", status: "connected", detail: "Detección activa en Frigate" },
          { id: "lpr_frigate", label: "LPR Frigate", status: "not_configured", detail: "No configurado" },
        ]}
      />,
    );

    expect(screen.getByText("LPR Provision-ISR")).toBeInTheDocument();
    expect(screen.getByText("Analíticas Provision")).toBeInTheDocument();
    expect(screen.getByText("Analíticas Frigate")).toBeInTheDocument();
    expect(screen.getByText("LPR Frigate")).toBeInTheDocument();

    const provBtn = within(screen.getByTestId("integration-lpr_provision")).getByRole("button", { name: /Probar/i });
    expect(provBtn).toBeInTheDocument();

    fireEvent.click(provBtn);

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalled();
    });
    expect(await screen.findByText("Agente alcanzable")).toBeInTheDocument();
    expect(screen.getByText("Keepalive reciente")).toBeInTheDocument();
  });
});
