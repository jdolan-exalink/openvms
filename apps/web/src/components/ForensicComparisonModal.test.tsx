import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ForensicResult } from "@/lib/enhancement/forensic";
import { ForensicComparisonModal } from "./ForensicComparisonModal";

function createMockCanvas(width: number, height: number): HTMLCanvasElement {
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  return canvas;
}

const mockResult: ForensicResult = {
  originalCanvas: createMockCanvas(640, 480),
  enhancedCanvas: createMockCanvas(1280, 960),
  operationsApplied: ["analysis", "deblur", "super_resolution", "tone_mapping"],
  metrics: {
    blurScore: 42.5,
    needsDeblur: true,
    noiseScore: 8.2,
    needsDenoise: false,
    dynamicRange: 180,
    avgLuminance: 95,
  },
  executionTimeMs: 145,
  metadata: {
    cameraName: "Front Entrance",
    cameraId: "cam-1",
    timestamp: 1728765432,
    scaleFactor: 2,
  },
};

describe("ForensicComparisonModal", () => {
  it("does not render when isOpen is false", () => {
    const { container } = render(
      <ForensicComparisonModal isOpen={false} onClose={vi.fn()} result={mockResult} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("does not render when result is null and not loading", () => {
    const { container } = render(
      <ForensicComparisonModal isOpen={true} onClose={vi.fn()} result={null} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("renders loading state when isLoading is true", () => {
    render(
      <ForensicComparisonModal isOpen={true} onClose={vi.fn()} result={null} isLoading={true} />,
    );
    expect(screen.getByText(/Procesando detalle forense/i)).toBeInTheDocument();
  });

  it("renders modal header, camera metadata, and execution time", () => {
    render(
      <ForensicComparisonModal isOpen={true} onClose={vi.fn()} result={mockResult} />,
    );

    expect(screen.getByText("Mejora de Detalle Forense")).toBeInTheDocument();
    expect(screen.getByText(/Front Entrance/i)).toBeInTheDocument();
    expect(screen.getByText(/145 ms/i)).toBeInTheDocument();
    expect(screen.getByText(/1280x960/i)).toBeInTheDocument();
  });

  it("renders download buttons for original and enhanced images", () => {
    render(
      <ForensicComparisonModal isOpen={true} onClose={vi.fn()} result={mockResult} />,
    );

    const downloadOriginalBtn = screen.getByRole("button", { name: /descargar original/i });
    const downloadEnhancedBtn = screen.getByRole("button", { name: /descargar mejorada/i });

    expect(downloadOriginalBtn).toBeInTheDocument();
    expect(downloadEnhancedBtn).toBeInTheDocument();
  });

  it("triggers download on click without errors", () => {
    const originalToDataURL = HTMLCanvasElement.prototype.toDataURL;
    HTMLCanvasElement.prototype.toDataURL = vi.fn(() => "data:image/png;base64,mockdata");

    render(
      <ForensicComparisonModal isOpen={true} onClose={vi.fn()} result={mockResult} />,
    );

    const downloadEnhancedBtn = screen.getByRole("button", { name: /descargar mejorada/i });
    fireEvent.click(downloadEnhancedBtn);

    expect(HTMLCanvasElement.prototype.toDataURL).toHaveBeenCalled();
    HTMLCanvasElement.prototype.toDataURL = originalToDataURL;
  });

  it("names downloaded files with camera name and metadata timestamp", () => {
    const originalToDataURL = HTMLCanvasElement.prototype.toDataURL;
    HTMLCanvasElement.prototype.toDataURL = vi.fn(() => "data:image/png;base64,mockdata");
    const names: string[] = [];
    const clickSpy = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(function (this: HTMLAnchorElement) {
        names.push(this.download);
      });

    render(<ForensicComparisonModal isOpen={true} onClose={vi.fn()} result={mockResult} />);
    fireEvent.click(screen.getByRole("button", { name: /descargar original/i }));
    fireEvent.click(screen.getByRole("button", { name: /descargar mejorada/i }));

    expect(names).toEqual([
      "front_entrance_original_1728765432.png",
      "front_entrance_mejorada_1728765432.png",
    ]);
    clickSpy.mockRestore();
    HTMLCanvasElement.prototype.toDataURL = originalToDataURL;
  });

  it("toggles comparison view mode between split slider and side-by-side", () => {
    render(
      <ForensicComparisonModal isOpen={true} onClose={vi.fn()} result={mockResult} />,
    );

    const sideBySideBtn = screen.getByRole("button", { name: /lado a lado/i });
    fireEvent.click(sideBySideBtn);

    expect(screen.getByText("Original (640x480)")).toBeInTheDocument();
    expect(screen.getByText("Mejorada (1280x960)")).toBeInTheDocument();
  });

  it("calls onClose when close button or dismiss is clicked", () => {
    const handleClose = vi.fn();
    render(
      <ForensicComparisonModal isOpen={true} onClose={handleClose} result={mockResult} />,
    );

    const closeBtn = screen.getByRole("button", { name: /cerrar mejora forense/i });
    fireEvent.click(closeBtn);
    expect(handleClose).toHaveBeenCalledTimes(1);
  });

  it("supports mouse wheel zoom and displays updated scale", () => {
    render(
      <ForensicComparisonModal isOpen={true} onClose={vi.fn()} result={mockResult} />,
    );

    const viewer = screen.getByTestId("forensic-viewer");
    expect(viewer).toBeInTheDocument();

    // Wheel event with deltaY < 0 (zoom in)
    fireEvent.wheel(viewer, { deltaY: -120 });

    // Zoom level indicator should be higher than 100%
    const zoomText = screen.getByTestId("zoom-level-text");
    expect(parseInt(zoomText.textContent || "100", 10)).toBeGreaterThan(100);
  });

  it("resets zoom and pan on double click", () => {
    render(
      <ForensicComparisonModal isOpen={true} onClose={vi.fn()} result={mockResult} />,
    );

    const viewer = screen.getByTestId("forensic-viewer");
    expect(viewer).toBeInTheDocument();

    // Zoom in first with button
    const zoomInBtn = screen.getByRole("button", { name: /aumentar zoom/i });
    fireEvent.click(zoomInBtn);

    const zoomText = screen.getByTestId("zoom-level-text");
    expect(parseInt(zoomText.textContent || "100", 10)).toBeGreaterThan(100);

    // Double click to reset
    fireEvent.doubleClick(viewer);
    expect(screen.getByTestId("zoom-level-text").textContent).toBe("100%");
  });

  it("supports dragging to pan when zoomed", () => {
    render(
      <ForensicComparisonModal isOpen={true} onClose={vi.fn()} result={mockResult} />,
    );

    const viewer = screen.getByTestId("forensic-viewer");
    const zoomInBtn = screen.getByRole("button", { name: /aumentar zoom/i });
    fireEvent.click(zoomInBtn);
    fireEvent.click(zoomInBtn);

    // Drag to pan
    fireEvent.pointerDown(viewer, { clientX: 200, clientY: 200, button: 0, pointerId: 1 });
    fireEvent.pointerMove(viewer, { clientX: 150, clientY: 150, button: 0, pointerId: 1 });
    fireEvent.pointerUp(viewer, { pointerId: 1 });

    const transformStage = screen.getByTestId("enhanced-layer");
    expect(transformStage.getAttribute("style")).toContain("translate(");
  });
});
