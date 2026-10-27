import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { FisheyeSection } from "./FisheyeSection";

describe("FisheyeSection Component", () => {
  beforeEach(() => {
    localStorage.clear();
  });
  it("renders fisheye toggle and controls when enabled", () => {
    render(<FisheyeSection cameraId="cam-fe-1" cameraName="Provision F-360" />);

    expect(screen.getByText(/Modo Fisheye \/ 360° \/ 180°/i)).toBeInTheDocument();
    const toggle = screen.getByRole("switch");
    expect(toggle).toHaveAttribute("aria-checked", "false");

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-checked", "true");

    expect(screen.getAllByRole("combobox")).toHaveLength(4);
    expect(screen.getByRole("button", { name: /Autodetectar fabricante/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Calibrar círculo útil/i })).toBeInTheDocument();
  });

  it("applies '3 Fijas de 120°' preset on click", () => {
    const onConfigChange = vi.fn();
    render(
      <FisheyeSection
        cameraId="cam-fe-1"
        cameraName="Camera 360"
        onConfigChange={onConfigChange}
      />,
    );

    fireEvent.click(screen.getByRole("switch"));

    const btn120 = screen.getByRole("button", { name: /3 Fijas de 120°/i });
    fireEvent.click(btn120);

    expect(screen.getByText(/Vista 1 \(0°\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Vista 2 \(120°\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Vista 3 \(240°\)/i)).toBeInTheDocument();

    // Click Guardar to persist and invoke onConfigChange
    const saveBtn = screen.getByRole("button", { name: /^Guardar$/i });
    fireEvent.click(saveBtn);

    expect(onConfigChange).toHaveBeenCalledWith(
      expect.objectContaining({
        views: expect.arrayContaining([
          expect.objectContaining({ name: "Vista 1 (0°)", fov: 120 }),
        ]),
      }),
    );
  });

  it("applies '3 Fijas de 120°' preset on click and cancels when clicking Cancelar", () => {
    render(<FisheyeSection cameraId="cam-fe-1" cameraName="Camera 360" />);

    fireEvent.click(screen.getByRole("switch"));

    const btn120 = screen.getByRole("button", { name: /3 Fijas de 120°/i });
    fireEvent.click(btn120);

    expect(screen.getByText(/Vista 1 \(0°\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Vista 2 \(120°\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Vista 3 \(240°\)/i)).toBeInTheDocument();

    // Revert with Cancelar
    const cancelBtn = screen.getByRole("button", { name: /Cancelar/i });
    fireEvent.click(cancelBtn);

    expect(screen.queryByText(/Vista 1 \(0°\)/i)).not.toBeInTheDocument();
  });

  it("triggers auto-detection for known manufacturer camera", () => {
    render(<FisheyeSection cameraId="cam-fe-2" cameraName="Provision-ISR F-360 Panoramic" />);

    fireEvent.click(screen.getByRole("switch"));

    const autoDetectBtn = screen.getByRole("button", { name: /Autodetectar fabricante/i });
    fireEvent.click(autoDetectBtn);

    expect(screen.getByText(/Detectado: Provision-ISR/i)).toBeInTheDocument();
  });

  it("clears virtual views when switching default mode away from 3 fixed views", () => {
    render(<FisheyeSection cameraId="cam-fe-clear" cameraName="Camera 360" />);

    fireEvent.click(screen.getByRole("switch"));

    // First generate 3 fixed views
    fireEvent.click(screen.getByRole("button", { name: /3 Fijas de 120°/i }));
    expect(screen.getByText(/Vista 1 \(0°\)/i)).toBeInTheDocument();

    // Now change default mode dropdown to Original
    const selects = screen.getAllByRole("combobox");
    const modeSelect = selects[3]!;
    fireEvent.change(modeSelect, { target: { value: "original" } });

    // Views are cleared!
    expect(screen.queryByText(/Vista 1 \(0°\)/i)).not.toBeInTheDocument();
    expect(screen.getByText(/No hay vistas virtuales configuradas/i)).toBeInTheDocument();
  });

  it("lists 360° and 2×180° as view modes while retaining panoramic projection", () => {
    render(<FisheyeSection cameraId="cam-fe-panorama" cameraName="Camera 360" />);
    fireEvent.click(screen.getByRole("switch"));

    const selects = screen.getAllByRole("combobox");
    const lensSelect = selects[0]!;
    const modeSelect = selects[3]!;
    expect(within(modeSelect).queryByRole("option", { name: /Panorama 180°/i })).not.toBeInTheDocument();
    expect(within(modeSelect).queryByRole("option", { name: /4 Cuadrantes/i })).not.toBeInTheDocument();
    expect(within(modeSelect).getByRole("option", { name: "360°" })).toBeInTheDocument();
    expect(within(modeSelect).getByRole("option", { name: "2×180°" })).toHaveValue("panorama");
    expect(within(lensSelect).getByRole("option", { name: /Panorámica/i })).toBeInTheDocument();
  });

  it("clears virtual views when clicking 'Borrar vistas'", () => {
    render(<FisheyeSection cameraId="cam-fe-del" cameraName="Camera 360" />);

    fireEvent.click(screen.getByRole("switch"));

    fireEvent.click(screen.getByRole("button", { name: /3 Fijas de 120°/i }));
    expect(screen.getByText(/Vista 1 \(0°\)/i)).toBeInTheDocument();

    const clearBtn = screen.getByRole("button", { name: /Borrar vistas/i });
    fireEvent.click(clearBtn);

    expect(screen.queryByText(/Vista 1 \(0°\)/i)).not.toBeInTheDocument();
  });
});
