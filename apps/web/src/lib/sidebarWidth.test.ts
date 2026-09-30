import { beforeEach, describe, expect, it } from "vitest";
import { clampSidebarWidth, loadSidebarWidth, maxSidebarWidth, saveSidebarWidth, SIDEBAR_DEFAULT_WIDTH, SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_WIDTH } from "./sidebarWidth";

describe("sidebar width", () => {
  beforeEach(() => localStorage.clear());

  it("caps the maximum at 40% of the viewport and 480 px, never below the minimum", () => {
    expect(maxSidebarWidth(1000)).toBe(400);
    expect(maxSidebarWidth(2560)).toBe(SIDEBAR_MAX_WIDTH);
    expect(maxSidebarWidth(500)).toBe(SIDEBAR_MIN_WIDTH);
  });

  it("clamps widths into [min, max] and rejects non-finite values", () => {
    expect(clampSidebarWidth(100, 1600)).toBe(SIDEBAR_MIN_WIDTH);
    expect(clampSidebarWidth(9999, 1600)).toBe(SIDEBAR_MAX_WIDTH);
    expect(clampSidebarWidth(300.4, 1600)).toBe(300);
    expect(clampSidebarWidth(Number.NaN, 1600)).toBe(SIDEBAR_DEFAULT_WIDTH);
  });

  it("persists per browser and falls back to the default for missing or invalid values", () => {
    expect(loadSidebarWidth()).toBe(SIDEBAR_DEFAULT_WIDTH);
    saveSidebarWidth(320);
    expect(loadSidebarWidth()).toBe(320);
    localStorage.setItem("openvms.live.sidebar.width", "garbage");
    expect(loadSidebarWidth()).toBe(SIDEBAR_DEFAULT_WIDTH);
  });
});
