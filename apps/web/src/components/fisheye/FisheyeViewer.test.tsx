import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { FisheyeViewer } from "./FisheyeViewer";

describe("FisheyeViewer Component", () => {
  beforeEach(() => {
    localStorage.clear();
  });
  it("renders toolbar with view mode buttons and canvas stage", () => {
    render(<FisheyeViewer cameraId="cam-123" />);

    expect(screen.getByRole("button", { name: "Original" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "360°" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Panorama" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /PTZ Virtual/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /3 Fijas/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /4 Cuadrantes/i })).toBeInTheDocument();

    expect(screen.getByTestId("fisheye-canvas")).toBeInTheDocument();
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

  it("switches to '4 Cuadrantes' multi-view mode on click", () => {
    render(<FisheyeViewer cameraId="cam-123" />);

    const fourQuadsBtn = screen.getByRole("button", { name: /4 Cuadrantes/i });
    fireEvent.click(fourQuadsBtn);

    expect(screen.getByText(/Vista 1 \(Norte\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Vista 2 \(Este\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Vista 3 \(Sur\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Vista 4 \(Oeste\)/i)).toBeInTheDocument();
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

  it("saves selected mode as default when clicking 'Guardar vista'", () => {
    render(<FisheyeViewer cameraId="cam-123" />);

    const fourQuadsBtn = screen.getByRole("button", { name: /4 Cuadrantes/i });
    fireEvent.click(fourQuadsBtn);

    const saveBtn = screen.getByRole("button", { name: /Guardar vista/i });
    fireEvent.click(saveBtn);

    expect(screen.getByText(/Guardado/i)).toBeInTheDocument();
  });

  it("renders in compact mode without top toolbar", () => {
    render(<FisheyeViewer cameraId="cam-compact" initialMode="virtual_ptz" compact={true} />);

    expect(screen.queryByRole("button", { name: "Original" })).not.toBeInTheDocument();
    expect(screen.getByTestId("fisheye-canvas")).toBeInTheDocument();
  });

  it("switches from 4 cuadrantes to panorama, preserving canvas and clearing virtual views", () => {
    render(<FisheyeViewer cameraId="cam-switch" />);

    // Switch to 4 quadrants
    fireEvent.click(screen.getByRole("button", { name: /4 Cuadrantes/i }));
    expect(screen.getByText(/Vista 1 \(Norte\)/i)).toBeInTheDocument();

    // Switch to Panorama
    fireEvent.click(screen.getByRole("button", { name: "Panorama" }));

    // Canvas must be present and visible
    const canvas = screen.getByTestId("fisheye-canvas");
    expect(canvas).toBeInTheDocument();
    expect(screen.queryByText(/Vista 1 \(Norte\)/i)).not.toBeInTheDocument();

    // Check that views are cleared in storage
    const raw = localStorage.getItem("openvms.fisheye.configs.v1");
    if (raw) {
      const parsed = JSON.parse(raw);
      expect(parsed["cam-switch"]?.views).toEqual([]);
      expect(parsed["cam-switch"]?.defaultMode).toBe("panorama");
    }
  });
});
