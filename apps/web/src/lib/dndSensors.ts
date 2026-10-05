import { KeyboardSensor, MouseSensor, TouchSensor, useSensor, useSensors, type KeyboardCoordinateGetter } from "@dnd-kit/core";
import { TOUCH_DRAG_DELAY_MS, TOUCH_DRAG_TOLERANCE_PX } from "@/lib/touch";

/**
 * Sensors for the Live drag and drop: the mouse starts after 4px, a finger after a press-and-hold
 * (so a normal swipe still scrolls the page) and the keyboard keeps its own coordinate getter.
 * MouseSensor + TouchSensor replace PointerSensor, which would also start on the first touch move.
 */
export function useLiveDragSensors(coordinateGetter?: KeyboardCoordinateGetter) {
  return useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 4 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: TOUCH_DRAG_DELAY_MS, tolerance: TOUCH_DRAG_TOLERANCE_PX } }),
    useSensor(KeyboardSensor, coordinateGetter ? { coordinateGetter } : undefined),
  );
}
