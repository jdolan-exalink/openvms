import { useRef, type PointerEvent } from "react";

/** Horizontal travel (px) a drag needs to count as a swipe. */
export const SWIPE_THRESHOLD = 50;

/** True when a drag is long and flat enough to be a horizontal swipe. */
export const isHorizontalSwipe = (dx: number, dy: number) => Math.abs(dx) >= SWIPE_THRESHOLD && Math.abs(dx) >= Math.abs(dy) * 1.5;

/**
 * useHorizontalSwipe recognises a one-finger horizontal swipe on bubbling pointer events and reports
 * its direction (1 = towards the next item, i.e. a leftward drag). It ignores gestures that involve
 * a second finger (pinch) and any gesture while `blocked()` is true (a zoomed picture pans instead).
 */
export function useHorizontalSwipe(onSwipe: (direction: 1 | -1) => void, blocked: () => boolean) {
  const pointers = useRef(new Set<number>());
  const start = useRef<{ x: number; y: number } | null>(null);
  return {
    onPointerDown: (event: PointerEvent) => {
      // A primary pointer is the first contact of a gesture. Any id still tracked is stale (its up or
      // cancel never reached us, e.g. released outside the element), so start from a clean slate.
      if (event.isPrimary !== false) pointers.current.clear();
      pointers.current.add(event.pointerId);
      start.current = pointers.current.size === 1 && !blocked() ? { x: event.clientX, y: event.clientY } : null;
    },
    onPointerUp: (event: PointerEvent) => {
      pointers.current.delete(event.pointerId);
      const origin = start.current;
      if (pointers.current.size > 0 || !origin) return;
      start.current = null;
      if (blocked()) return;
      const dx = event.clientX - origin.x;
      if (isHorizontalSwipe(dx, event.clientY - origin.y)) onSwipe(dx < 0 ? 1 : -1);
    },
    onPointerCancel: () => {
      pointers.current.clear();
      start.current = null;
    },
  };
}
