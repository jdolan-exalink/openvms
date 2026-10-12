import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { FisheyeViewer } from "./FisheyeViewer";

describe("FisheyeViewer Component", () => {
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

    expect(screen.getByText(/Cuadrante Norte \(0°\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Cuadrante Este \(90°\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Cuadrante Sur \(180°\)/i)).toBeInTheDocument();
    expect(screen.getByText(/Cuadrante Oeste \(270°\)/i)).toBeInTheDocument();
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
});
