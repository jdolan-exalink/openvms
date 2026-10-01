import { describe, expect, it, vi } from "vitest";
import { bindCameraPointerDrag, spreadCameraPositions, canvasDropPoint } from "./editorInteractions";

describe("provisional camera distribution", () => {
  it("spreads stable camera IDs around an existing anchor without overlap", () => {
    const positions = spreadCameraPositions(["b", "a", "c", "d"], { lat: -31.1, lng: -60.09 });
    expect(positions.map(p => p.id)).toEqual(["a", "b", "c", "d"]);
    expect(new Set(positions.map(p => `${p.lat},${p.lng}`)).size).toBe(4);
    expect(positions.every(p => Math.abs(p.lat + 31.1) < 0.001)).toBe(true);
    expect(spreadCameraPositions([], { lat: 0, lng: 0 })).toEqual([]);
  });
  it("rejects drops outside the canvas or onto an overlay", () => {
    const canvas = document.createElement("canvas");
    const overlay = document.createElement("button");
    const rect = { left: 20, top: 30, width: 100, height: 80 };
    expect(canvasDropPoint(canvas, rect, canvas, 25, 40)).toEqual([5, 10]);
    expect(canvasDropPoint(canvas, rect, overlay, 25, 40)).toBeUndefined();
    expect(canvasDropPoint(canvas, rect, canvas, 150, 40)).toBeUndefined();
  });
});

describe("camera pointer dragging", () => {
  function fixture() {
    const canvas = document.createElement("canvas");
    canvas.getBoundingClientRect = () => ({ left: 0, top: 0, width: 100, height: 100 } as DOMRect);
    canvas.setPointerCapture = vi.fn(); canvas.releasePointerCapture = vi.fn();
    const callbacks = { start: vi.fn(), move: vi.fn(), end: vi.fn() };
    const pan = { isEnabled: () => true, disable: vi.fn(), enable: vi.fn() };
    const binding = bindCameraPointerDrag(canvas, { pick: () => "a", enabled: () => true,
      unproject: ([lng, lat]) => ({ lng, lat }), pan, ...callbacks });
    const pointer = (type: string, x: number, y: number, pointerType = "touch") => {
      const event = new Event(type, { bubbles: true, cancelable: true });
      Object.assign(event, { clientX: x, clientY: y, pointerId: 1, pointerType, button: 0 });
      canvas.dispatchEvent(event);
    };
    return { canvas, callbacks, pan, binding, pointer };
  }
  it("does not stage a simple tap or movement below threshold", () => {
    const f = fixture(); f.pointer("pointerdown", 10, 10); f.pointer("pointermove", 11, 11); f.pointer("pointerup", 11, 11);
    expect(f.callbacks.start).not.toHaveBeenCalled(); expect(f.pan.enable).toHaveBeenCalled(); f.binding.dispose();
  });
  it("moves with touch and restores panning after release", () => {
    const f = fixture(); f.pointer("pointerdown", 10, 10); f.pointer("pointermove", 20, 20); f.pointer("pointerup", 25, 25);
    expect(f.callbacks.start).toHaveBeenCalledWith("a");
    expect(f.callbacks.end).toHaveBeenCalledWith("a", { lng: 25, lat: 25 });
    expect(f.binding.consumeClick()).toBe(true); expect(f.binding.consumeClick()).toBe(false);
    expect(f.pan.enable).toHaveBeenCalled(); f.binding.dispose();
  });
  it("restores pan on cancellation and cleanup without an outside-coordinate write", () => {
    const f = fixture(); f.pointer("pointerdown", 10, 10, "mouse"); f.pointer("pointermove", 20, 20, "mouse"); f.pointer("pointercancel", 150, 150, "mouse");
    expect(f.callbacks.end).toHaveBeenCalledWith("a", { lng: 20, lat: 20 });
    expect(f.pan.enable).toHaveBeenCalled(); f.binding.dispose();
  });
});
