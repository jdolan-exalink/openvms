import { renderHook } from "@testing-library/react";
import type { PointerEvent } from "react";
import { describe, expect, it, vi } from "vitest";
import { useHorizontalSwipe } from "./useHorizontalSwipe";

const ev = (pointerId: number, clientX: number, clientY = 100, isPrimary = true) => ({ pointerId, clientX, clientY, isPrimary }) as PointerEvent;

describe("useHorizontalSwipe", () => {
  it("navigates on a one-finger horizontal swipe", () => {
    const onSwipe = vi.fn();
    const { result } = renderHook(() => useHorizontalSwipe(onSwipe, () => false));
    result.current.onPointerDown(ev(1, 200));
    result.current.onPointerUp(ev(1, 100));
    expect(onSwipe).toHaveBeenCalledWith(1);
  });

  it("recovers when a previous pointer never reported its end", () => {
    const onSwipe = vi.fn();
    const { result } = renderHook(() => useHorizontalSwipe(onSwipe, () => false));
    result.current.onPointerDown(ev(1, 200)); // released outside the element: no up/cancel ever arrives
    result.current.onPointerDown(ev(2, 200));
    result.current.onPointerUp(ev(2, 100));
    expect(onSwipe).toHaveBeenCalledWith(1);
  });

  it("still ignores a real pinch (second, non-primary finger)", () => {
    const onSwipe = vi.fn();
    const { result } = renderHook(() => useHorizontalSwipe(onSwipe, () => false));
    result.current.onPointerDown(ev(1, 200));
    result.current.onPointerDown(ev(2, 220, 100, false));
    result.current.onPointerUp(ev(2, 100, 100, false));
    result.current.onPointerUp(ev(1, 100));
    expect(onSwipe).not.toHaveBeenCalled();
  });
});
