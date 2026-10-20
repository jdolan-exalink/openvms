import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ZoomFrame } from "@/components/DigitalZoom";
import * as analysis from "@/lib/enhancement/analysis";

describe("DigitalZoom Enhancement Integration", () => {
  beforeEach(() => {
    vi.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue({
      x: 0,
      y: 0,
      top: 0,
      left: 0,
      right: 200,
      bottom: 100,
      width: 200,
      height: 100,
      toJSON: () => ({}),
    } as DOMRect);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("mounts EnhancementControl by default, and hides it when enhanceable is false", () => {
    const { unmount } = render(<ZoomFrame resetKey="test-cam-1" />);

    const badge = screen.getByRole("button", { name: /Ajustes de mejora visual/i });
    expect(badge).toBeInTheDocument();

    unmount();

    render(<ZoomFrame resetKey="test-cam-1" enhanceable={false} />);
    expect(screen.queryByRole("button", { name: /Mejora visual/i })).not.toBeInTheDocument();
  });

  it("offers mode changes inside the settings panel", () => {
    render(<ZoomFrame resetKey="test-cam-2" />);

    fireEvent.click(screen.getByRole("button", { name: /Ajustes de mejora visual/i }));
    fireEvent.click(screen.getByRole("button", { name: "Off" }));
    fireEvent.click(screen.getByRole("button", { name: "Auto" }));
    expect(screen.getByRole("button", { name: "Auto" })).toBeInTheDocument();
  });

  it("triggers ROI recalculation when zoom changes", () => {
    const extractRoiRectSpy = vi.spyOn(analysis, "extractRoiRect");

    const { container } = render(<ZoomFrame resetKey="test-cam-3" />);
    const frame = container.firstElementChild as HTMLElement;

    // Check initial ROI calculation on mount
    expect(extractRoiRectSpy).toHaveBeenCalled();
    extractRoiRectSpy.mockClear();

    // Trigger wheel zoom (zoom into point)
    fireEvent.wheel(frame, { clientX: 100, clientY: 50, deltaY: -120, deltaMode: 0 });

    expect(extractRoiRectSpy).toHaveBeenCalled();
    const lastCall = extractRoiRectSpy.mock.calls[extractRoiRectSpy.mock.calls.length - 1]!;
    const [calledZoom, calledWidth, calledHeight] = lastCall;
    expect(calledZoom.scale).toBeGreaterThan(1);
    expect(calledWidth).toBe(200);
    expect(calledHeight).toBe(100);
  });

  it("correctly reflects CSS filter on the video stage container", () => {
    const { container } = render(
      <ZoomFrame resetKey="test-cam-4">
        <video data-testid="video-element" />
      </ZoomFrame>,
    );

    const stage = container.querySelector(".absolute.inset-0") as HTMLElement;
    expect(stage).toBeInTheDocument();

    // Initially with DEFAULT_ADJUSTMENTS, filter is empty / none
    expect(stage.style.filter).toBe("");

    // Open options popover
    const optionsBtn = screen.getByRole("button", { name: /Ajustes de mejora visual/i });
    fireEvent.click(optionsBtn);

    // Switch to manual mode
    const manualBtn = screen.getByRole("button", { name: /Manual/i });
    fireEvent.click(manualBtn);

    // Adjust brightness slider to +30
    const brightnessSlider = screen.getByLabelText(/Brillo/i);
    fireEvent.change(brightnessSlider, { target: { value: "30" } });

    // CSS filter should now be applied to the stage container
    expect(stage.style.filter).toContain("brightness(130%)");

    // Adjust contrast slider to +20
    const contrastSlider = screen.getByLabelText(/Contraste/i);
    fireEvent.change(contrastSlider, { target: { value: "20" } });
    expect(stage.style.filter).toContain("contrast(120%)");

    // Reset image adjustments
    const resetBtn = screen.getByRole("button", { name: /Restablecer imagen/i });
    fireEvent.click(resetBtn);

    // In auto mode with default adjustments, filter is none
    expect(stage.style.filter).toBe("");
  });

  describe("when the picture lives outside the frame (surface layer)", () => {
    const dark = {
      minLuma: 0, maxLuma: 60, avgLuma: 20, p5Luma: 2, p95Luma: 50, avgR: 20, avgG: 20, avgB: 20,
      isDark: true, isOverexposed: false, isLowContrast: true,
    };

    it("samples the supplied media element over the visible zoomed region and reports the filter", async () => {
      const sample = vi.spyOn(analysis, "sampleFrameStats").mockReturnValue(dark);
      vi.spyOn(performance, "now").mockReturnValue(10_000);
      const video = document.createElement("video");
      const filters: string[] = [];
      render(
        <ZoomFrame
          resetKey="surface-cam"
          scalePicture={false}
          zoom={{ scale: 2, x: -100, y: -50 }}
          mediaSource={() => video}
          onEnhance={(filter) => filters.push(filter)}
        />,
      );
      expect(sample).toHaveBeenCalled();
      const [source, roi] = sample.mock.calls.at(-1)!;
      expect(source).toBe(video);
      expect(roi?.width).toBeCloseTo(0.5);
      expect(roi?.height).toBeCloseTo(0.5);
      expect(filters.at(-1)).not.toBe("none");
    });

    it("clears the reported filter when the frame goes away", () => {
      vi.spyOn(analysis, "sampleFrameStats").mockReturnValue(dark);
      vi.spyOn(performance, "now").mockReturnValue(10_000);
      const filters: string[] = [];
      const view = render(<ZoomFrame resetKey="surface-cam" scalePicture={false} mediaSource={() => document.createElement("video")} onEnhance={(f) => filters.push(f)} />);
      view.unmount();
      expect(filters.at(-1)).toBe("none");
    });
  });
});
