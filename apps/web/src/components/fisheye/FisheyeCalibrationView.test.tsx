import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FisheyeCalibrationView } from "./FisheyeCalibrationView";

describe("FisheyeCalibrationView Component", () => {
  it("renders circular viewport and control handles", () => {
    const onSave = vi.fn();
    render(<FisheyeCalibrationView onSave={onSave} />);

    expect(screen.getByTestId("calibration-viewport")).toBeInTheDocument();
    expect(screen.getByTestId("center-handle")).toBeInTheDocument();
    expect(screen.getByTestId("radius-handle")).toBeInTheDocument();
  });

  it("changes mounting type and lens model", () => {
    const onSave = vi.fn();
    render(<FisheyeCalibrationView onSave={onSave} />);

    // Select wall mounting
    const selects = screen.getAllByRole("combobox");
    const mountSelect = selects[0]!;
    fireEvent.change(mountSelect, { target: { value: "wall" } });
    expect(mountSelect).toHaveValue("wall");

    // Select equisolid model
    const projSelect = selects[2]!;
    fireEvent.change(projSelect, { target: { value: "equisolid" } });
    expect(projSelect).toHaveValue("equisolid");

    // Click Save
    const saveBtn = screen.getByRole("button", { name: /guardar/i });
    fireEvent.click(saveBtn);

    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        mountType: "wall",
        projectionModel: "equisolid",
      }),
    );
  });

  it("handles dragging optical center handle", () => {
    const onSave = vi.fn();
    render(<FisheyeCalibrationView onSave={onSave} />);

    const centerHandle = screen.getByTestId("center-handle");
    const viewport = screen.getByTestId("calibration-viewport");

    // Mock viewport bounding rect
    vi.spyOn(viewport, "getBoundingClientRect").mockReturnValue({
      left: 0,
      top: 0,
      width: 400,
      height: 400,
      right: 400,
      bottom: 400,
      x: 0,
      y: 0,
      toJSON: () => {},
    });

    fireEvent.pointerDown(centerHandle, { clientX: 200, clientY: 200, pointerId: 1 });
    fireEvent.pointerMove(viewport, { clientX: 240, clientY: 240, pointerId: 1 });
    fireEvent.pointerUp(viewport, { clientX: 240, clientY: 240, pointerId: 1 });

    const saveBtn = screen.getByRole("button", { name: /guardar/i });
    fireEvent.click(saveBtn);

    expect(onSave).toHaveBeenCalledWith(
      expect.objectContaining({
        calibration: expect.objectContaining({
          centerX: 0.6,
          centerY: 0.6,
        }),
      }),
    );
  });
});
