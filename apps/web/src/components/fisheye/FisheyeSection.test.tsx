import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FisheyeSection } from "./FisheyeSection";

describe("FisheyeSection Component", () => {
  it("renders fisheye toggle and controls", () => {
    render(<FisheyeSection cameraId="cam-fe-1" cameraName="Provision F-360" />);

    expect(screen.getByText(/Modo Fisheye \/ 360° \/ 180°/i)).toBeInTheDocument();
    expect(screen.getAllByRole("combobox")).toHaveLength(3);
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

    const btn120 = screen.getByRole("button", { name: /3 Fijas de 120°/i });
    fireEvent.click(btn120);

    expect(screen.getByText(/Vista 1 \(0°\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Vista 2 \(120°\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Vista 3 \(240°\)/i)).toBeInTheDocument();

    expect(onConfigChange).toHaveBeenCalledWith(
      expect.objectContaining({
        views: expect.arrayContaining([
          expect.objectContaining({ name: "Vista 1 (0°)", fov: 120 }),
        ]),
      }),
    );
  });

  it("applies '4 Cuadrantes' preset on click", () => {
    const onConfigChange = vi.fn();
    render(
      <FisheyeSection
        cameraId="cam-fe-1"
        cameraName="Camera 360"
        onConfigChange={onConfigChange}
      />,
    );

    const btnQuad = screen.getByRole("button", { name: /4 Cuadrantes/i });
    fireEvent.click(btnQuad);

    expect(screen.getByText(/Cuadrante Norte \(0°\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Cuadrante Este \(90°\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Cuadrante Sur \(180°\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Cuadrante Oeste \(270°\)/i)).toBeInTheDocument();
  });

  it("triggers auto-detection for known manufacturer camera", () => {
    render(<FisheyeSection cameraId="cam-fe-2" cameraName="Provision-ISR F-360 Panoramic" />);

    const autoDetectBtn = screen.getByRole("button", { name: /Autodetectar fabricante/i });
    fireEvent.click(autoDetectBtn);

    expect(screen.getByText(/Detectado: Provision-ISR/i)).toBeInTheDocument();
  });
});
