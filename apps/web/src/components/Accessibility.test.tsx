import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { CameraSettingsDrawer } from "./CameraSettingsDrawer";
import { ErrorNote, Summary } from "./ui";

describe("Accessibility and ARIA roles", () => {
  it("renders ErrorNote with role='alert'", () => {
    render(<ErrorNote error={new Error("Fallo de conexión")} />);
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("Fallo de conexión");
  });

  it("renders Summary with role='status'", () => {
    render(<Summary>10 de 20 cámaras</Summary>);
    const status = screen.getByRole("status");
    expect(status).toHaveTextContent("10 de 20 cámaras");
  });

  it("renders validation error with role='alert' in CameraSettingsDrawer", () => {
    const qc = new QueryClient();
    const cam = {
      id: "c1",
      name: "cam-front",
      display_name: "Front Camera",
      server_id: "s1",
      enabled: true,
      labels: [],
      zones: [],
      lpr_enabled: false,
      tags: [],
    };
    render(
      <QueryClientProvider client={qc}>
        <CameraSettingsDrawer
          camera={cam as unknown as Schemas["Camera"]}
          canManage={true}
          onClose={() => {}}
        />
      </QueryClientProvider>
    );
    const input = screen.getByLabelText("Nombre");
    fireEvent.change(input, { target: { value: "   " } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(screen.getByRole("alert")).toHaveTextContent("El nombre no puede estar vacío.");
  });

  it("includes prefers-reduced-motion in global stylesheet", async () => {
    const fs = await import("node:fs");
    const path = await import("node:path");
    const css = fs.readFileSync(path.resolve(__dirname, "../index.css"), "utf-8");
    expect(css).toContain("prefers-reduced-motion: reduce");
    expect(css).toContain("animation-duration: 0.01ms !important");
    expect(css).toContain("transition-duration: 0.01ms !important");
  });
});
