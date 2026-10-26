import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { IntegrationsTab } from "./IntegrationsTab";

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
});
