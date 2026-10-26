import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { type ConfigureResult, ProvisionConfigureResult } from "@/components/provision/ProvisionConfigureResult";

const waiting: ConfigureResult = { success: false, registered: true, verified: false, method: "manual_instructions", message: "Ingresá estos valores en la interfaz web de la cámara." };

describe("ProvisionConfigureResult", () => {
  it("shows the registered camera as waiting for its manual setup, never as done", () => {
    render(<ProvisionConfigureResult result={waiting} />);
    expect(screen.getByText("Ingresá estos valores en la interfaz web de la cámara.")).toBeInTheDocument();
    expect(screen.getByText(/Cámara registrada en el agente/)).toBeInTheDocument();
    expect(screen.getByText(/no aplica estos valores en la cámara/)).toBeInTheDocument();
    expect(screen.queryByText(/ya reporta/)).not.toBeInTheDocument();
  });

  it("labels the manual steps as instructions and keeps the warning colour for registration problems only", () => {
    const { container, rerender } = render(<ProvisionConfigureResult result={waiting} />);
    expect(screen.getByText("Instrucciones manuales")).toBeInTheDocument();
    expect(container.firstElementChild?.className).not.toContain("text-warn");
    rerender(<ProvisionConfigureResult result={{ ...waiting, registered: false, registration_error: "agent_unreachable" }} />);
    expect(container.firstElementChild?.className).toContain("text-warn");
  });

  it("shows success, without the manual warning, once the camera reports in", () => {
    render(<ProvisionConfigureResult result={{ ...waiting, success: true, verified: true, method: "verified", message: "La cámara ya reporta a este agente: la integración está verificada." }} />);
    expect(screen.getByText(/integración está verificada/)).toBeInTheDocument();
    expect(screen.queryByText(/no aplica estos valores/)).not.toBeInTheDocument();
  });

  it.each([
    ["agent_outdated", /anterior al registro de cámaras/],
    ["agent_unreachable", /No se pudo contactar al agente/],
    ["another camera is registered with that address", /No se pudo registrar la cámara: another camera is registered/],
  ])("explains a registration problem (%s) and keeps the manual steps", (code, text) => {
    render(<ProvisionConfigureResult result={{ ...waiting, registered: false, registration_error: code }} />);
    expect(screen.getByText(text)).toBeInTheDocument();
    expect(screen.queryByText(/Cámara registrada en el agente/)).not.toBeInTheDocument();
    expect(screen.getByText(/no aplica estos valores/)).toBeInTheDocument();
  });

  it("falls back to its own text when the answer has no message", () => {
    render(<ProvisionConfigureResult result={{ success: false }} />);
    expect(screen.getByText("Falta configurar la cámara.")).toBeInTheDocument();
  });
});
