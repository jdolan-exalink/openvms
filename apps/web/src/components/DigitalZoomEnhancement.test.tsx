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

    const badge = screen.getByRole("button", { name: /Mejora visual: AUTO/i });
    expect(badge).toBeInTheDocument();

    unmount();

    render(<ZoomFrame resetKey="test-cam-1" enhanceable={false} />);
    expect(screen.queryByRole("button", { name: /Mejora visual/i })).not.toBeInTheDocument();
  });

  it("toggles between AUTO and OFF updating state correctly", () => {
    render(<ZoomFrame resetKey="test-cam-2" />);

    const autoBadge = screen.getByRole("button", { name: /Mejora visual: AUTO/i });
    expect(autoBadge).toBeInTheDocument();

    // Toggle to OFF
    fireEvent.click(autoBadge);
    const offBadge = screen.getByRole("button", { name: /Mejora visual: OFF/i });
    expect(offBadge).toBeInTheDocument();

    // Toggle back to AUTO
    fireEvent.click(offBadge);
    expect(screen.getByRole("button", { name: /Mejora visual: AUTO/i })).toBeInTheDocument();
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
});
