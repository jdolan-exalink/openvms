import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { bindLongPress, DOUBLE_TAP_DISTANCE_PX, DOUBLE_TAP_MS, DoubleTapDetector, LONG_PRESS_MS, LongPressTracker, trackPointerKind } from "./touch";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("LongPressTracker", () => {
  it("fires once after the hold time", () => {
    const fire = vi.fn();
    const tracker = new LongPressTracker();
    tracker.begin(1, 10, 10, fire);
    vi.advanceTimersByTime(LONG_PRESS_MS - 1);
    expect(fire).not.toHaveBeenCalled();
    vi.advanceTimersByTime(2);
    expect(fire).toHaveBeenCalledTimes(1);
    expect(tracker.consumeFired()).toBe(true);
    expect(tracker.consumeFired()).toBe(false);
  });

  it("cancels when the finger moves past the tolerance but not within it", () => {
    const fire = vi.fn();
    const tracker = new LongPressTracker();
    tracker.begin(1, 10, 10, fire);
    tracker.move(1, 14, 14);
    tracker.move(1, 10, 17);
    vi.advanceTimersByTime(LONG_PRESS_MS + 1);
    expect(fire).toHaveBeenCalledTimes(1);

    const second = vi.fn();
    tracker.begin(2, 0, 0, second);
    tracker.move(2, 0, 9);
    vi.advanceTimersByTime(LONG_PRESS_MS + 1);
    expect(second).not.toHaveBeenCalled();
  });

  it("cancels on release and ignores other pointers", () => {
    const fire = vi.fn();
    const tracker = new LongPressTracker();
    tracker.begin(1, 0, 0, fire);
    tracker.move(2, 500, 500);
    tracker.end(1);
    vi.advanceTimersByTime(LONG_PRESS_MS + 1);
    expect(fire).not.toHaveBeenCalled();
  });
});

describe("DoubleTapDetector", () => {
  it("reports the second close tap and resets afterwards", () => {
    const detector = new DoubleTapDetector();
    expect(detector.tap(100, 100, 0)).toBe(false);
    expect(detector.tap(110, 105, DOUBLE_TAP_MS - 10)).toBe(true);
    expect(detector.tap(110, 105, DOUBLE_TAP_MS)).toBe(false);
  });

  it("ignores slow or distant second taps", () => {
    const detector = new DoubleTapDetector();
    detector.tap(0, 0, 0);
    expect(detector.tap(0, 0, DOUBLE_TAP_MS + 1)).toBe(false);
    expect(detector.tap(DOUBLE_TAP_DISTANCE_PX + 1, 0, DOUBLE_TAP_MS + 50)).toBe(false);
  });
});

describe("trackPointerKind", () => {
  it("follows the last pointer type seen on the element", () => {
    const el = document.createElement("div");
    const kind = trackPointerKind(el);
    expect(kind.isTouch()).toBe(false);
    el.dispatchEvent(new PointerEvent("pointerdown", { pointerType: "touch" }));
    expect(kind.isTouch()).toBe(true);
    el.dispatchEvent(new PointerEvent("pointermove", { pointerType: "mouse" }));
    expect(kind.isTouch()).toBe(false);
    kind.dispose();
    el.dispatchEvent(new PointerEvent("pointerdown", { pointerType: "touch" }));
    expect(kind.isTouch()).toBe(false);
  });
});

describe("bindLongPress", () => {
  const down = (el: HTMLElement, pointerType: string, x = 5, y = 6) => el.dispatchEvent(new PointerEvent("pointerdown", { pointerType, pointerId: 1, isPrimary: true, clientX: x, clientY: y }));

  it("reports the touch point after the hold, only for touch, and cancels on a move", () => {
    const el = document.createElement("div");
    const onLongPress = vi.fn();
    const bound = bindLongPress(el, onLongPress);
    down(el, "mouse");
    vi.advanceTimersByTime(LONG_PRESS_MS + 1);
    expect(onLongPress).not.toHaveBeenCalled();

    down(el, "touch", 30, 40);
    el.dispatchEvent(new PointerEvent("pointermove", { pointerType: "touch", pointerId: 1, isPrimary: true, clientX: 30, clientY: 60 }));
    vi.advanceTimersByTime(LONG_PRESS_MS + 1);
    expect(onLongPress).not.toHaveBeenCalled();

    down(el, "touch", 30, 40);
    vi.advanceTimersByTime(LONG_PRESS_MS + 1);
    expect(onLongPress).toHaveBeenCalledWith({ x: 30, y: 40 });
    expect(bound.consumeFired()).toBe(true);
    bound.dispose();
    down(el, "touch");
    vi.advanceTimersByTime(LONG_PRESS_MS + 1);
    expect(onLongPress).toHaveBeenCalledTimes(1);
  });
});
