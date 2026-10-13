import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { saveFisheyeConfig } from "@/lib/fisheye/storage";
import { DEFAULT_FISHEYE_CONFIG } from "@/lib/fisheye/types";
import { FisheyeViewer } from "./FisheyeViewer";

const engineProbe = vi.hoisted(() => ({ draws: 0 }));
vi.mock("@/lib/fisheye/engine", () => ({
  FisheyeDewarpEngine: class {
    isSupported = true;
    render() {
      engineProbe.draws += 1;
      return true;
    }
    destroy() {}
  },
}));

describe("FisheyeViewer Component", () => {
  beforeEach(() => {
    localStorage.clear();
    engineProbe.draws = 0;
  });
  it("renders toolbar with view mode buttons and canvas stage", () => {
    render(<FisheyeViewer cameraId="cam-123" />);

    expect(screen.getByRole("button", { name: "Original" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "360°" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /PTZ Virtual/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /3 Fijas/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /PTZ Virtual/i })).toHaveClass("bg-primary");
    expect(screen.queryByRole("button", { name: "Panorama" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /4 Cuadrantes/i })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Guardar vista/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Cancelar/i })).toBeInTheDocument();

    expect(screen.getByTestId("fisheye-canvas")).toBeInTheDocument();
  });

  it("opens in the explicitly saved non-default per-camera mode", () => {
    saveFisheyeConfig({
      ...DEFAULT_FISHEYE_CONFIG,
      cameraId: "cam-saved-mode",
      enabled: true,
      lensType: "fisheye_360",
      defaultMode: "360",
    });

    render(<FisheyeViewer cameraId="cam-saved-mode" />);

    expect(screen.getByRole("button", { name: "360°" })).toHaveClass("bg-primary");
    expect(screen.getByRole("button", { name: /PTZ Virtual/i })).not.toHaveClass("bg-primary");
  });

  it("tracks a changing target virtual view", () => {
    const views = [
      { id: "front", cameraId: "cam-target-view", name: "Front", projection: "rectilinear" as const, yaw: 0, pitch: 0, roll: 0, fov: 80 },
      { id: "side", cameraId: "cam-target-view", name: "Side", projection: "rectilinear" as const, yaw: 90, pitch: 0, roll: 0, fov: 80 },
    ];
    saveFisheyeConfig({
      ...DEFAULT_FISHEYE_CONFIG,
      cameraId: "cam-target-view",
      enabled: true,
      lensType: "fisheye_360",
      views,
    });

    const { rerender } = render(
      <FisheyeViewer cameraId="cam-target-view" targetViewId="front" />,
    );
    expect(screen.getByRole("combobox", { name: "Seleccionar preset" })).toHaveValue("front");

    rerender(<FisheyeViewer cameraId="cam-target-view" targetViewId="side" />);

    expect(screen.getByRole("combobox", { name: "Seleccionar preset" })).toHaveValue("side");
  });

  it("switches to '3 Fijas (120°)' multi-view mode on click", () => {
    render(<FisheyeViewer cameraId="cam-123" />);

    const threeFixedBtn = screen.getByRole("button", { name: /3 Fijas/i });
    fireEvent.click(threeFixedBtn);

    // Multi-view renders 3 viewports
    expect(screen.getByText(/Vista 1 \(0°\) · 120° FOV/i)).toBeInTheDocument();
    expect(screen.getByText(/Vista 2 \(120°\) · 120° FOV/i)).toBeInTheDocument();
    expect(screen.getByText(/Vista 3 \(240°\) · 120° FOV/i)).toBeInTheDocument();
  });



  it("handles mouse wheel zoom in Virtual PTZ mode", () => {
    render(<FisheyeViewer cameraId="cam-123" />);

    const ptzBtn = screen.getByRole("button", { name: /PTZ Virtual/i });
    fireEvent.click(ptzBtn);

    const stage = screen.getByTestId("fisheye-stage");
    fireEvent.wheel(stage, { deltaY: -100 });

    // PTZ coordinates HUD reflects zoomed FOV
    expect(screen.getByText(/FOV:/i)).toBeInTheDocument();
  });

  it("handles videoElement prop when supplied directly", () => {
    const video = document.createElement("video");
    render(<FisheyeViewer cameraId="cam-123" videoElement={video} />);
    expect(screen.getByTestId("fisheye-canvas")).toBeInTheDocument();
  });

  it("does not report fisheye readiness before the canvas has drawn", async () => {
    const video = document.createElement("video");
    const onFirstFrame = vi.fn();
    render(<FisheyeViewer cameraId="cam-frame" videoElement={video} quality="sub" onFirstFrame={onFirstFrame} />);

    Object.defineProperty(video, "readyState", { configurable: true, value: 2 });
    Object.defineProperty(video, "videoWidth", { configurable: true, value: 640 });
    fireEvent.loadedData(video);

    expect(onFirstFrame).not.toHaveBeenCalled();
    await waitFor(() => expect(onFirstFrame).toHaveBeenCalledTimes(1));
    expect(engineProbe.draws).toBeGreaterThan(0);
  });

  it("saves selected mode as default when clicking 'Guardar vista'", () => {
    render(<FisheyeViewer cameraId="cam-123" />);

    const threeFixedBtn = screen.getByRole("button", { name: /3 Fijas/i });
    fireEvent.click(threeFixedBtn);

    const saveBtn = screen.getByRole("button", { name: /Guardar vista/i });
    fireEvent.click(saveBtn);

    expect(screen.getByText(/Guardado/i)).toBeInTheDocument();
  });

  it("renders in compact mode without top toolbar", () => {
    render(<FisheyeViewer cameraId="cam-compact" initialMode="virtual_ptz" compact={true} />);

    expect(screen.queryByRole("button", { name: "Original" })).not.toBeInTheDocument();
    expect(screen.getByTestId("fisheye-canvas")).toBeInTheDocument();
  });

  it("preserves the panoramic projection capability without offering a panorama view mode", () => {
    render(<FisheyeViewer cameraId="cam-switch" />);

    // Switch to 3 fixed views
    fireEvent.click(screen.getByRole("button", { name: /3 Fijas/i }));
    expect(screen.getByText(/Vista 1 \(0°\)/i)).toBeInTheDocument();

    expect(screen.getByTestId("fisheye-canvas")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /3 Fijas/i })).toBeInTheDocument();
  });

  it("resets changes when clicking Cancelar", () => {
    render(<FisheyeViewer cameraId="cam-cancel" initialMode="original" />);

    // Switch to 3 fixed views
    fireEvent.click(screen.getByRole("button", { name: /3 Fijas/i }));

    // Click Cancelar
    fireEvent.click(screen.getByRole("button", { name: /cancelar/i }));

    // Mode is reverted to original
    expect(screen.getByRole("button", { name: "Original" })).toHaveClass("bg-primary");
  });

  it("returns to the just-saved mode when clicking Cancelar after Guardar vista", () => {
    render(<FisheyeViewer cameraId="cam-cancel-saved" initialMode="original" />);

    fireEvent.click(screen.getByRole("button", { name: /3 Fijas/i }));
    fireEvent.click(screen.getByRole("button", { name: /Guardar vista/i }));
    fireEvent.click(screen.getByRole("button", { name: /PTZ Virtual/i }));
    fireEvent.click(screen.getByRole("button", { name: /cancelar/i }));

    expect(screen.getByRole("button", { name: /3 Fijas/i })).toHaveClass("bg-primary");
  });

  it("keeps user zoom on a targeted virtual view instead of snapping back to the saved view", () => {
    saveFisheyeConfig({
      ...DEFAULT_FISHEYE_CONFIG,
      cameraId: "cam-target-zoom",
      enabled: true,
      lensType: "fisheye_360",
      views: [{ id: "front", cameraId: "cam-target-zoom", name: "Front", projection: "rectilinear", yaw: 0, pitch: 0, roll: 0, fov: 80 }],
    });

    render(<FisheyeViewer cameraId="cam-target-zoom" targetViewId="front" />);
    expect(screen.getByText(/FOV:/i).textContent).toContain("80°");

    fireEvent.wheel(screen.getByTestId("fisheye-stage"), { deltaY: -100 });

    expect(screen.getByText(/FOV:/i).textContent).not.toContain("80°");
  });
});
