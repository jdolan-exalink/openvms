import { describe, expect, it, vi } from "vitest";
import { LAYER_GROUPS, applyLayerVisibility, builtLayerIds, ownedLayerIds } from "./visibility";

describe("layer visibility groups", () => {
  it("groups every layer the builders declare exactly once", () => {
    const owned = ownedLayerIds();
    expect(new Set(owned).size).toBe(owned.length);
    expect([...owned].sort()).toEqual([...builtLayerIds()].sort());
  });

  it("shows everything by default", () => {
    const setLayoutProperty = vi.fn();
    applyLayerVisibility({ getLayer: () => true, setLayoutProperty } as never, {});
    expect(setLayoutProperty).not.toHaveBeenCalled();
  });

  it("hides only the groups that were turned off", () => {
    const setLayoutProperty = vi.fn();
    applyLayerVisibility({ getLayer: () => true, setLayoutProperty } as never, { cameras: false, sites: true });
    const valueOf = (call: unknown[], wanted: string) => call[2] === wanted;
    expect(setLayoutProperty.mock.calls.filter((c) => valueOf(c, "none")).map(([id]) => id))
      .toEqual([...LAYER_GROUPS.cameras]);
    expect(setLayoutProperty.mock.calls.filter((c) => valueOf(c, "visible")).map(([id]) => id))
      .toEqual([...LAYER_GROUPS.sites]);
  });

  it("skips layers that are not on the style yet", () => {
    const setLayoutProperty = vi.fn();
    applyLayerVisibility({ getLayer: (id: string) => id === "cam-label", setLayoutProperty } as never, { cameras: false });
    expect(setLayoutProperty).toHaveBeenCalledTimes(1);
    expect(setLayoutProperty).toHaveBeenCalledWith("cam-label", "visibility", "none");
  });
});
