import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { DEFAULT_ADJUSTMENTS } from "@/lib/enhancement/types";
import { EnhancementControl } from "./EnhancementControl";

describe("EnhancementControl Component", () => {
  it("renders with AUTO badge by default and toggles to OFF on click", () => {
    const onModeChange = vi.fn();
    const onReset = vi.fn();

    render(
      <EnhancementControl
        mode="auto"
        onModeChange={onModeChange}
        adjustments={DEFAULT_ADJUSTMENTS}
        onReset={onReset}
      />,
    );

    const toggleBtn = screen.getByRole("button", { name: /Mejora visual: AUTO/i });
    expect(toggleBtn).toBeInTheDocument();

    fireEvent.click(toggleBtn);
    expect(onModeChange).toHaveBeenCalledWith("off");
  });

  it("toggles from OFF to AUTO when clicked in OFF state", () => {
    const onModeChange = vi.fn();
    const onReset = vi.fn();

    render(
      <EnhancementControl
        mode="off"
        onModeChange={onModeChange}
        adjustments={DEFAULT_ADJUSTMENTS}
        onReset={onReset}
      />,
    );

    const toggleBtn = screen.getByRole("button", { name: /Mejora visual: OFF/i });
    expect(toggleBtn).toBeInTheDocument();

    fireEvent.click(toggleBtn);
    expect(onModeChange).toHaveBeenCalledWith("auto");
  });

  it("opens advanced manual panel and updates adjustments", () => {
    const onModeChange = vi.fn();
    const onAdjustmentsChange = vi.fn();
    const onReset = vi.fn();

    render(
      <EnhancementControl
        mode="auto"
        onModeChange={onModeChange}
        adjustments={DEFAULT_ADJUSTMENTS}
        onAdjustmentsChange={onAdjustmentsChange}
        onReset={onReset}
      />,
    );

    // Open options popover
    const optionsBtn = screen.getByRole("button", { name: /Ajustes de mejora visual/i });
    fireEvent.click(optionsBtn);

    expect(screen.getByText(/Control manual de imagen/i)).toBeInTheDocument();

    // Select Manual tab
    const manualBtn = screen.getByRole("button", { name: /Manual/i });
    fireEvent.click(manualBtn);
    expect(onModeChange).toHaveBeenCalledWith("manual");

    // Adjust brightness slider
    const brightnessSlider = screen.getByLabelText(/Brillo/i);
    fireEvent.change(brightnessSlider, { target: { value: "25" } });
    expect(onAdjustmentsChange).toHaveBeenCalledWith(expect.objectContaining({ brightness: 25 }));
  });

  it("triggers reset when 'Restablecer imagen' is clicked", () => {
    const onModeChange = vi.fn();
    const onReset = vi.fn();

    render(
      <EnhancementControl
        mode="manual"
        onModeChange={onModeChange}
        adjustments={{ ...DEFAULT_ADJUSTMENTS, brightness: 30, contrast: 20 }}
        onReset={onReset}
      />,
    );

    // Open options popover
    const optionsBtn = screen.getByRole("button", { name: /Ajustes de mejora visual/i });
    fireEvent.click(optionsBtn);

    const resetBtn = screen.getByRole("button", { name: /Restablecer imagen/i });
    fireEvent.click(resetBtn);

    expect(onReset).toHaveBeenCalled();
  });
});
