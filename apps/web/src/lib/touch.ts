/** Touch gesture helpers shared by the desktop UI so tablets are fully usable. Framework-free. */

/** Press-and-hold before a drag starts; a normal swipe still scrolls. */
export const TOUCH_DRAG_DELAY_MS = 220;
export const TOUCH_DRAG_TOLERANCE_PX = 6;
export const LONG_PRESS_MS = 500;
export const LONG_PRESS_TOLERANCE_PX = 8;
export const DOUBLE_TAP_MS = 300;
export const DOUBLE_TAP_DISTANCE_PX = 24;
/** A touch that moves farther than this (or lasts longer than DOUBLE_TAP_MS) is not a tap. */
export const TAP_MAX_MOVE_PX = 10;

/** LongPressTracker fires once when one pointer stays put for the hold time. */
export class LongPressTracker {
  private timer: ReturnType<typeof setTimeout> | undefined;
  private current: { id: number; x: number; y: number } | null = null;
  private fired = false;

  begin(id: number, x: number, y: number, fire: () => void, holdMs = LONG_PRESS_MS): void {
    this.cancel();
    this.fired = false;
    this.current = { id, x, y };
    this.timer = setTimeout(() => {
      this.timer = undefined;
      this.current = null;
      this.fired = true;
      fire();
    }, holdMs);
  }

  move(id: number, x: number, y: number): void {
    const start = this.current;
    if (!start || start.id !== id) return;
    if (Math.hypot(x - start.x, y - start.y) > LONG_PRESS_TOLERANCE_PX) this.cancel();
  }

  end(id: number): void {
    if (this.current?.id === id) this.cancel();
  }

  cancel(): void {
    if (this.timer !== undefined) clearTimeout(this.timer);
    this.timer = undefined;
    this.current = null;
  }

  /** True once after a long press fired: the click that follows the release must be swallowed. */
  consumeFired(): boolean {
    const fired = this.fired;
    this.fired = false;
    return fired;
  }
}

/** DoubleTapDetector turns two close taps (time and distance) into one double tap. */
export class DoubleTapDetector {
  private last: { x: number; y: number; at: number } | null = null;

  tap(x: number, y: number, at: number): boolean {
    const last = this.last;
    if (last && at - last.at < DOUBLE_TAP_MS && Math.hypot(x - last.x, y - last.y) <= DOUBLE_TAP_DISTANCE_PX) {
      this.last = null;
      return true;
    }
    this.last = { x, y, at };
    return false;
  }

  reset(): void {
    this.last = null;
  }
}

type PointerLike = { pointerType: string; pointerId: number; isPrimary: boolean; clientX: number; clientY: number };

/** Minimal shape of the React pointer handlers the helpers below produce. */
export type TouchTapHandlers<E extends PointerLike> = {
  onPointerDown: (event: E) => void;
  onPointerUp: (event: E) => void;
  onPointerCancel: (event: E) => void;
};

/**
 * touchTapHandlers detects double taps from touch pointers only (mouse keeps onDoubleClick).
 * A tap is a short touch that barely moved. `onDoubleTap` fires on the second tap's release.
 */
export function touchTapHandlers<E extends PointerLike>(detector: DoubleTapDetector, onDoubleTap: (event: E) => void): TouchTapHandlers<E> {
  let down: { id: number; x: number; y: number; at: number } | null = null;
  return {
    onPointerDown: (event) => {
      if (event.pointerType !== "touch" || !event.isPrimary) return;
      down = { id: event.pointerId, x: event.clientX, y: event.clientY, at: performance.now() };
    },
    onPointerUp: (event) => {
      const start = down;
      down = null;
      if (!start || start.id !== event.pointerId || event.pointerType !== "touch") return;
      const now = performance.now();
      if (now - start.at > DOUBLE_TAP_MS || Math.hypot(event.clientX - start.x, event.clientY - start.y) > TAP_MAX_MOVE_PX) {
        detector.reset();
        return;
      }
      if (detector.tap(event.clientX, event.clientY, now)) onDoubleTap(event);
    },
    onPointerCancel: () => {
      down = null;
      detector.reset();
    },
  };
}

/** TouchDoubleTap bundles the touch tap handlers with the "just handled" window used to ignore a synthesized dblclick/click. */
export class TouchDoubleTap<E extends PointerLike> {
  private onDoubleTap: (event: E) => void = () => {};
  readonly handlers: TouchTapHandlers<E>;
  private handledAt = Number.NEGATIVE_INFINITY;

  constructor() {
    this.handlers = touchTapHandlers<E>(new DoubleTapDetector(), (event) => {
      this.handledAt = performance.now();
      this.onDoubleTap(event);
    });
  }

  /** The owner sets the callback after every render so the latest one runs. */
  listen(onDoubleTap: (event: E) => void): void {
    this.onDoubleTap = onDoubleTap;
  }

  justHandled(): boolean {
    return performance.now() - this.handledAt < 600;
  }
}

/** trackPointerKind remembers whether the latest pointer on `el` was a finger (hover is emulated then). */
export function trackPointerKind(el: HTMLElement): { isTouch: () => boolean; dispose: () => void } {
  let touch = false;
  const onPointer = (event: Event) => {
    touch = (event as PointerEvent).pointerType === "touch";
  };
  el.addEventListener("pointerdown", onPointer, true);
  el.addEventListener("pointermove", onPointer, true);
  return {
    isTouch: () => touch,
    dispose: () => {
      el.removeEventListener("pointerdown", onPointer, true);
      el.removeEventListener("pointermove", onPointer, true);
      touch = false;
    },
  };
}

/** bindLongPress opens something on a touch long press over a DOM element (mouse uses right click). */
export function bindLongPress(el: HTMLElement, onLongPress: (point: { x: number; y: number }) => void): { consumeFired: () => boolean; dispose: () => void } {
  const tracker = new LongPressTracker();
  const down = (event: Event) => {
    const e = event as PointerEvent;
    if (e.pointerType !== "touch" || !e.isPrimary) return;
    const point = { x: e.clientX, y: e.clientY };
    tracker.begin(e.pointerId, point.x, point.y, () => onLongPress(point));
  };
  const move = (event: Event) => {
    const e = event as PointerEvent;
    tracker.move(e.pointerId, e.clientX, e.clientY);
  };
  const end = (event: Event) => tracker.end((event as PointerEvent).pointerId);
  el.addEventListener("pointerdown", down);
  el.addEventListener("pointermove", move);
  el.addEventListener("pointerup", end);
  el.addEventListener("pointercancel", end);
  return {
    consumeFired: () => tracker.consumeFired(),
    dispose: () => {
      tracker.cancel();
      el.removeEventListener("pointerdown", down);
      el.removeEventListener("pointermove", move);
      el.removeEventListener("pointerup", end);
      el.removeEventListener("pointercancel", end);
    },
  };
}
