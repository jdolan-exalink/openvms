import { afterEach, describe, expect, it } from "vitest";
import { frameCameras, loadGeoView, loadPlanView, MAP_SIDEBAR_PADDING, saveGeoView, savePlanView } from "./mapView";

afterEach(() => localStorage.clear());

describe("frameCameras", () => {
  it("frames every camera up close and shifts them clear of the sidebar", () => {
    const view = frameCameras(
      [{ lng: 0, lat: 0 }, { lng: 0.01, lat: 0 }, { lng: 0.02, lat: 0.005 }],
      { width: 1280, height: 720 },
      MAP_SIDEBAR_PADDING,
    );
    expect(view).not.toBeNull();
    expect(view!.zoom).toBeGreaterThan(10);
    expect(view!.zoom).toBeLessThanOrEqual(16);
    expect(view!.center[0]).toBeLessThan(0.01);
  });

  it("returns null without cameras", () => {
    expect(frameCameras([], { width: 800, height: 600 }, MAP_SIDEBAR_PADDING)).toBeNull();
  });
});

describe("remembered map views", () => {
  it("keeps a site view and a floor view for the same operator", () => {
    saveGeoView("t", "u", "s", { center: [-58.4, -34.6], zoom: 15.5 });
    savePlanView("t", "u", "f", { scale: 2.2, x: 40, y: -12 });
    expect(loadGeoView("t", "u", "s")).toEqual({ center: [-58.4, -34.6], zoom: 15.5 });
    expect(loadPlanView("t", "u", "f")).toEqual({ scale: 2.2, x: 40, y: -12 });
    expect(loadGeoView("t", "other", "s")).toBeNull();
  });
});
