import { describe, expect, it, vi } from "vitest";
import { VirtualPtzController } from "./VirtualPtzController";

describe("VirtualPtzController", () => {
  it("initializes with default state", () => {
    const ptz = new VirtualPtzController();
    const state = ptz.getState();
    expect(state.yaw).toBe(0);
    expect(state.pitch).toBe(-30);
    expect(state.roll).toBe(0);
    expect(state.fov).toBe(90);
  });

  it("updates state and triggers change callback", () => {
    const onChange = vi.fn();
    const ptz = new VirtualPtzController({ yaw: 10, pitch: -20, fov: 90 }, onChange);

    ptz.setState({ yaw: 45, pitch: -10 });
    const state = ptz.getState();
    expect(state.yaw).toBe(45);
    expect(state.pitch).toBe(-10);
    expect(onChange).toHaveBeenCalledWith(state);
  });

  it("handles panning with normalized yaw wrap-around", () => {
    const ptz = new VirtualPtzController({ yaw: 175, pitch: 0, fov: 90 });
    // Drag horizontally to increase yaw past 180 degrees
    ptz.panBy(100, 0, 1000, 1000);
    const state = ptz.getState();
    expect(state.yaw).toBeLessThan(180);
    expect(state.yaw).toBeGreaterThan(-180);
  });

  it("zooms by wheel delta while respecting FOV limits", () => {
    const ptz = new VirtualPtzController({ fov: 90 });
    // Zoom in (deltaWheel < 0) decreases FOV
    ptz.zoomBy(-100);
    expect(ptz.getState().fov).toBeLessThan(90);

    // Zoom out (deltaWheel > 0) increases FOV
    ptz.zoomBy(200);
    expect(ptz.getState().fov).toBeGreaterThan(80);

    // Zooming out excessively is clamped to 130
    for (let i = 0; i < 20; i++) ptz.zoomBy(100);
    expect(ptz.getState().fov).toBe(130);

    // Zooming in excessively is clamped to 30
    for (let i = 0; i < 30; i++) ptz.zoomBy(-100);
    expect(ptz.getState().fov).toBe(30);
  });

  it("resets state to defaults", () => {
    const ptz = new VirtualPtzController({ yaw: 90, pitch: 40, fov: 50 });
    ptz.reset();
    const state = ptz.getState();
    expect(state.yaw).toBe(0);
    expect(state.pitch).toBe(-30);
    expect(state.fov).toBe(90);
  });
});
