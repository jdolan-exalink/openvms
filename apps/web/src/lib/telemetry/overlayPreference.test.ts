import { afterEach, describe, expect, it, vi } from "vitest";
import { overlayVisibleOnLive, readOverlayPreference, writeOverlayPreference } from "./overlayPreference";

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

describe("overlay preference", () => {
  it("is off by default and only the exact value 1 turns it on", () => {
    expect(readOverlayPreference()).toBe(false);
    localStorage.setItem("openvms.live.telemetryOverlay", "true");
    expect(readOverlayPreference()).toBe(false);
    writeOverlayPreference(true);
    expect(localStorage.getItem("openvms.live.telemetryOverlay")).toBe("1");
    expect(readOverlayPreference()).toBe(true);
  });

  it("removes the key when turned off", () => {
    writeOverlayPreference(true);
    writeOverlayPreference(false);
    expect(localStorage.getItem("openvms.live.telemetryOverlay")).toBeNull();
  });

  it("is visible on Live with the preference or the debug key, but the page switch ignores the debug key", () => {
    expect(overlayVisibleOnLive()).toBe(false);
    localStorage.setItem("openvms.live.debug", "1");
    expect(overlayVisibleOnLive()).toBe(true);
    expect(readOverlayPreference()).toBe(false);
  });

  it("survives blocked storage", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(overlayVisibleOnLive()).toBe(false);
    expect(() => writeOverlayPreference(true)).not.toThrow();
  });
});
