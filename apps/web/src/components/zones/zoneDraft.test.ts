import { describe, expect, it } from "vitest";
import { buildDraft, draftToValue, type ZoneEditorValue } from "./zoneDraft";

const c = "0.1,0.1,0.9,0.1,0.9,0.9";

describe("zone draft round-trip", () => {
  it("keeps zone config, unknown keys and each mask format", () => {
    const value: ZoneEditorValue = {
      zones: { patio: { coordinates: c, objects: ["person"], inertia: 3, filters: { person: { min_area: 10 } } } },
      motionMask: c,
      objectMask: [c],
      objectFilterMasks: { car: { a: { friendly_name: "x", enabled: true, coordinates: c } } },
    };
    expect(draftToValue(buildDraft(value, undefined, "0.18.0"), "0.18.0")).toEqual(value);
  });

  it("converts legacy absolute zone coordinates and writes relative ones", () => {
    const value: ZoneEditorValue = { zones: { z: { coordinates: "640,360,1280,360,1280,720" } }, motionMask: [], objectMask: [], objectFilterMasks: {} };
    const out = draftToValue(buildDraft(value, { width: 1280, height: 720 }, "0.16.0"), "0.16.0");
    expect(out.zones.z?.coordinates).toBe("0.5,0.5,1,0.5,1,1");
  });
});
