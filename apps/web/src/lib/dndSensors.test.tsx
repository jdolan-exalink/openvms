import { KeyboardSensor, MouseSensor, TouchSensor } from "@dnd-kit/core";
import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TOUCH_DRAG_DELAY_MS, TOUCH_DRAG_TOLERANCE_PX } from "./touch";
import { useLiveDragSensors } from "./dndSensors";

describe("useLiveDragSensors", () => {
  it("keeps the mouse distance activation, adds a press-and-hold touch sensor and keeps the keyboard", () => {
    const { result } = renderHook(() => useLiveDragSensors());
    const byType = (sensor: unknown) => result.current.find((s) => s.sensor === sensor);
    expect(byType(MouseSensor)?.options).toEqual({ activationConstraint: { distance: 4 } });
    expect(byType(TouchSensor)?.options).toEqual({ activationConstraint: { delay: TOUCH_DRAG_DELAY_MS, tolerance: TOUCH_DRAG_TOLERANCE_PX } });
    expect(byType(KeyboardSensor)).toBeDefined();
    expect(TOUCH_DRAG_DELAY_MS).toBeGreaterThanOrEqual(200);
    expect(TOUCH_DRAG_DELAY_MS).toBeLessThanOrEqual(250);
  });
});
