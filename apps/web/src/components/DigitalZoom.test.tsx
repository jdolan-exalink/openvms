import { fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ZoomFrame } from "@/components/DigitalZoom";
import type { DigitalZoom } from "@/lib/live/digitalZoom";

beforeEach(() => {
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockReturnValue({ x: 0, y: 0, top: 0, left: 0, right: 200, bottom: 100, width: 200, height: 100, toJSON: () => ({}) } as DOMRect);
});
afterEach(() => vi.restoreAllMocks());

function setup() {
  const zooms: DigitalZoom[] = [];
  const view = render(<ZoomFrame resetKey="a" onZoom={(z) => zooms.push(z)} />);
  const frame = view.container.firstElementChild as HTMLElement;
  const last = (): DigitalZoom => zooms[zooms.length - 1] ?? { scale: 1, x: 0, y: 0 };
  const touch = (type: "pointerDown" | "pointerMove" | "pointerUp", id: number, x: number, y: number) =>
    fireEvent[type](frame, { pointerId: id, pointerType: "touch", isPrimary: id === 1, clientX: x, clientY: y, button: 0 });
  return { frame, last, touch };
}

describe("ZoomFrame touch gestures", () => {
  it("lets the page scroll vertically until zoomed, then captures every gesture", () => {
    const { frame, touch } = setup();
    expect(frame.style.touchAction).toBe("pan-y");
    touch("pointerDown", 1, 80, 50);
    touch("pointerDown", 2, 120, 50);
    touch("pointerMove", 2, 160, 50);
    expect(frame.style.touchAction).toBe("none");
  });

  it("zooms with two fingers around their midpoint and clamps to the maximum", () => {
    const { last, touch } = setup();
    touch("pointerDown", 1, 80, 50);
    touch("pointerDown", 2, 120, 50);
    touch("pointerMove", 2, 160, 50);
    expect(last().scale).toBeCloseTo(2);
    touch("pointerMove", 2, 2000, 50);
    expect(last().scale).toBe(8);
  });

  it("pans with one finger once zoomed", () => {
    const { last, touch } = setup();
    touch("pointerDown", 1, 80, 50);
    touch("pointerDown", 2, 120, 50);
    touch("pointerMove", 2, 160, 50);
    touch("pointerUp", 2, 160, 50);
    const before = last();
    touch("pointerMove", 1, 70, 50);
    expect(last().x).toBeLessThan(before.x);
  });

  it("does not take a single finger while at 1x (the page scrolls and tiles drag)", () => {
    const { last, touch } = setup();
    touch("pointerDown", 1, 80, 50);
    touch("pointerMove", 1, 90, 50);
    expect(last().scale).toBe(1);
  });

  it("resets the zoom on a double tap and keeps the tap away from the tile", () => {
    const { last, touch } = setup();
    touch("pointerDown", 1, 80, 50);
    touch("pointerDown", 2, 120, 50);
    touch("pointerMove", 2, 160, 50);
    touch("pointerUp", 2, 160, 50);
    touch("pointerUp", 1, 80, 50);
    expect(last().scale).toBeGreaterThan(1);
    touch("pointerDown", 1, 60, 40);
    touch("pointerUp", 1, 60, 40);
    touch("pointerDown", 1, 62, 41);
    touch("pointerUp", 1, 62, 41);
    expect(last().scale).toBe(1);
  });
});
