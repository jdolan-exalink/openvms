import { afterEach, describe, expect, it } from "vitest";
import {
  clampPinnedOrigin,
  defaultPinnedOrigin,
  loadPinnedWindows,
  arrangeWindows,
  moveWindow,
  parsePinnedWindows,
  pinWindow,
  pinnedWindowsKey,
  savePinnedWindows,
  unpinWindow,
} from "./pinnedWindows";

const stage = { width: 1000, height: 800 };

afterEach(() => localStorage.clear());

describe("pinned map windows", () => {
  it("scopes storage to the user and the map", () => {
    expect(pinnedWindowsKey("t", "u", "geo:s")).toBe("openvms.maps.windows.v1:t:u:geo:s");
    expect(pinnedWindowsKey(null, "u", "floor:f")).toBe("openvms.maps.windows.v1:platform:u:floor:f");
  });

  it("drops malformed entries and keeps four unique windows", () => {
    const raw = JSON.stringify([
      { id: "a", x: 1, y: 2 },
      { id: "a", x: 9, y: 9 },
      { nope: true },
      { id: "b", x: "no", y: 1 },
      { id: "c", x: 3, y: 4 },
      { id: "d", x: 5, y: 6 },
      { id: "e", x: 7, y: 8 },
    ]);
    expect(parsePinnedWindows(raw).map((window) => window.id)).toEqual(["a", "c", "d", "e"]);
    expect(parsePinnedWindows("not-json")).toEqual([]);
  });

  it("pins the newest four and leaves an open window where it was", () => {
    let windows = pinWindow([], "a", stage);
    windows = pinWindow(windows, "b", stage);
    const kept = pinWindow(windows, "a", stage);
    expect(kept).toBe(windows);
    for (const id of ["c", "d", "e"]) windows = pinWindow(windows, id, stage);
    expect(windows.map((window) => window.id)).toEqual(["b", "c", "d", "e"]);
    expect(windows[0]).toMatchObject({ x: expect.any(Number), y: expect.any(Number) });
  });

  it("remembers a dragged place inside the map", () => {
    const pinned = pinWindow([], "a", stage);
    const moved = moveWindow(pinned, "a", 4000, -20, stage);
    expect(moved[0]).toMatchObject({ id: "a", x: 952, y: 0 });
    expect(unpinWindow(moved, "a")).toEqual([]);
    const arranged = arrangeWindows([{ id: "a", x: 1, y: 1 }, { id: "b", x: 400, y: 400 }], stage);
    expect(arranged[0]).toEqual({ id: "a", ...defaultPinnedOrigin(0, stage) });
    expect(arranged[1].y).toBeGreaterThan(arranged[0].y);
    expect(arranged[0].x).toBe(arranged[1].x);
    expect(defaultPinnedOrigin(0, stage).x).toBe(1000 - 288 - 16);
    expect(clampPinnedOrigin(10, 10, { width: 0, height: 0 })).toEqual({ x: 0, y: 0 });
  });

  it("round-trips through localStorage", () => {
    const key = pinnedWindowsKey("t", "u", "geo:s");
    savePinnedWindows(key, [{ id: "cam", x: 12, y: 24 }]);
    expect(loadPinnedWindows(key)).toEqual([{ id: "cam", x: 12, y: 24 }]);
  });
});
