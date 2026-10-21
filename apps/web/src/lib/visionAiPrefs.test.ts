import { afterEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_VISION_PREFS,
  loadVisionPrefs,
  parseVisionPrefs,
  saveVisionPrefs,
  visionPrefsKey,
  type VisionPrefs,
} from "./visionAiPrefs";

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

const custom: VisionPrefs = {
  mode: "dynamic",
  presetSource: "vehiculos",
  layoutCount: 9,
  activeObjects: { person: false, car: true, motorcycle: true, truck: true, license_plate: true, face: false },
};

describe("visionPrefsKey", () => {
  it("scopes by tenant and user, with a platform fallback", () => {
    expect(visionPrefsKey("t1", "u1")).toBe("openvms.visionai.view.v1:t1:u1");
    expect(visionPrefsKey(null, "u1")).toBe("openvms.visionai.view.v1:platform:u1");
  });
});

describe("parseVisionPrefs", () => {
  it("enables every smart detection for a first visit", () => {
    expect(parseVisionPrefs(null).activeObjects).toEqual({
      person: true, car: true, motorcycle: true, truck: true, license_plate: true, face: true,
    });
  });

  it("returns defaults for missing or corrupt input", () => {
    expect(parseVisionPrefs(null)).toEqual(DEFAULT_VISION_PREFS);
    expect(parseVisionPrefs("{not json")).toEqual(DEFAULT_VISION_PREFS);
    expect(parseVisionPrefs("42")).toEqual(DEFAULT_VISION_PREFS);
  });

  it("ignores inherited object names such as toString or constructor", () => {
    const raw = JSON.stringify({ activeObjects: { toString: true, constructor: false, person: false } });
    const parsed = parseVisionPrefs(raw);
    expect(Object.keys(parsed.activeObjects).sort()).toEqual(Object.keys(DEFAULT_VISION_PREFS.activeObjects).sort());
    expect(parsed.activeObjects.person).toBe(false);
  });

  it("drops unknown values per field and keeps the valid ones", () => {
    const raw = JSON.stringify({ mode: "bogus", presetSource: "vehiculos", layoutCount: 7, activeObjects: { person: false, car: "yes", ghost: true } });
    expect(parseVisionPrefs(raw)).toEqual({
      ...DEFAULT_VISION_PREFS,
      presetSource: "vehiculos",
      activeObjects: { ...DEFAULT_VISION_PREFS.activeObjects, person: false },
    });
  });
});

describe("load/save", () => {
  it("round-trips preferences", () => {
    saveVisionPrefs("k", custom);
    expect(loadVisionPrefs("k")).toEqual(custom);
  });

  it("falls back to defaults when storage throws", () => {
    vi.stubGlobal("localStorage", {
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
    });
    expect(loadVisionPrefs("k")).toEqual(DEFAULT_VISION_PREFS);
    expect(() => saveVisionPrefs("k", custom)).not.toThrow();
  });
});
