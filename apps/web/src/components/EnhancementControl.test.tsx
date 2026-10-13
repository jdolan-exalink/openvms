import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { DEFAULT_ADJUSTMENTS } from "@/lib/enhancement/types";
import { EnhancementControl } from "./EnhancementControl";

describe("EnhancementControl Component", () => {
  it("promotes the enhancement icon and keeps mode selection inside settings", () => {
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

    expect(screen.queryByText("AUTO")).not.toBeInTheDocument();
    const optionsBtn = screen.getByRole("button", { name: /Ajustes de mejora visual/i });
    expect(optionsBtn.querySelector("svg")).toBeInTheDocument();
    fireEvent.click(optionsBtn);
    fireEvent.click(screen.getByRole("button", { name: "Off" }));
    expect(onModeChange).toHaveBeenCalledWith("off");
  });

  it("does not expose a quick mode toggle when enhancement is off", () => {
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

    expect(screen.queryByRole("button", { name: /Mejora visual: OFF/i })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Ajustes de mejora visual/i }));
    fireEvent.click(screen.getByRole("button", { name: "Auto" }));
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
