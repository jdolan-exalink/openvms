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
  inactivitySeconds: 60,
  excludedServerIds: ["srv-b"],
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

  it("defaults the inactivity timeout to 30s and only accepts the offered values", () => {
    expect(parseVisionPrefs(null).inactivitySeconds).toBe(30);
    // 0 is not offered: it would still release at max display, so "never" would be misleading.
    expect(parseVisionPrefs(JSON.stringify({ inactivitySeconds: 0 })).inactivitySeconds).toBe(30);
    expect(parseVisionPrefs(JSON.stringify({ inactivitySeconds: 120 })).inactivitySeconds).toBe(120);
    expect(parseVisionPrefs(JSON.stringify({ inactivitySeconds: 45 })).inactivitySeconds).toBe(30);
    expect(parseVisionPrefs(JSON.stringify({ inactivitySeconds: "60" })).inactivitySeconds).toBe(30);
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

describe("excluded servers", () => {
  it("starts with no server excluded and remembers servers left unchecked", () => {
    expect(DEFAULT_VISION_PREFS.excludedServerIds).toEqual([]);
    expect(parseVisionPrefs(null).excludedServerIds).toEqual([]);
    expect(parseVisionPrefs("{}").excludedServerIds).toEqual([]);
    expect(parseVisionPrefs(JSON.stringify({ excludedServerIds: ["srv-b", "srv-c"] })).excludedServerIds).toEqual(["srv-b", "srv-c"]);
  });

  it("keeps unique nonempty string ids and caps corrupt values", () => {
    expect(parseVisionPrefs(JSON.stringify({ excludedServerIds: ["srv-b", "srv-b", "", 7, null, "srv-c"] })).excludedServerIds).toEqual(["srv-b", "srv-c"]);
    expect(parseVisionPrefs(JSON.stringify({ excludedServerIds: "srv-b" })).excludedServerIds).toEqual([]);
    const many = Array.from({ length: 500 }, (_, i) => `srv-${i}`);
    expect(parseVisionPrefs(JSON.stringify({ excludedServerIds: many })).excludedServerIds.length).toBeLessThanOrEqual(100);
  });

  it("round-trips the exclusions through storage", () => {
    saveVisionPrefs("k", custom);
    expect(loadVisionPrefs("k").excludedServerIds).toEqual(["srv-b"]);
  });
});
