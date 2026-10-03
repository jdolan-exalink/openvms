import { beforeEach, describe, expect, it, vi, afterEach } from "vitest";
import { addCameraToLiveGrid } from "./liveGridHelper";
import { cameraIdOf, liveSelectionKey, parseSelection, serializeSelection } from "@/lib/liveGrid";

describe("addCameraToLiveGrid", () => {
  afterEach(() => vi.restoreAllMocks());

  beforeEach(() => {
    localStorage.clear();
  });

  it("adds camera to empty live grid in localStorage", () => {
    const ok = addCameraToLiveGrid("tenant-1", "user-1", "cam-100", "sub");
    expect(ok).toBe(true);

    const key = liveSelectionKey("tenant-1", "user-1");
    const raw = localStorage.getItem(key);
    expect(raw).toBeTruthy();

    const parsed = parseSelection(raw, new Set(["cam-100"]));
    expect(parsed).not.toBeNull();
    expect(cameraIdOf(parsed?.tiles[0] ?? null)).toBe("cam-100");
    expect(parsed?.tiles[0] && "quality" in parsed.tiles[0] ? parsed.tiles[0].quality : undefined).toBe("sub");
  });

  it("does not duplicate camera if already in grid", () => {
    addCameraToLiveGrid("t", "u", "cam-1", "sub");
    addCameraToLiveGrid("t", "u", "cam-2", "sub");
    addCameraToLiveGrid("t", "u", "cam-1", "main"); // re-adding cam-1 with main quality

    const key = liveSelectionKey("t", "u");
    const parsed = parseSelection(localStorage.getItem(key), new Set(["cam-1", "cam-2"]));
    const cam1Tiles = parsed?.tiles.filter((t) => cameraIdOf(t) === "cam-1");
    expect(cam1Tiles).toHaveLength(1);
    expect(cam1Tiles?.[0] && "quality" in cam1Tiles[0] ? cam1Tiles[0].quality : undefined).toBe("main");
  });
  it("preserves multiple stored cameras and rectangular dimensions", () => {
    const key = liveSelectionKey("t", "u");
    localStorage.setItem(key, serializeSelection(2, [
      { camera_id: "a", quality: "main" }, { camera_id: "b", quality: "sub" }, null, null, null, null,
    ], 3));
    expect(addCameraToLiveGrid("t", "u", "c")).toBe(true);
    const saved = parseSelection(localStorage.getItem(key), new Set(["a", "b", "c"]));
    expect(saved?.columns).toBe(2);
    expect(saved?.rows).toBe(3);
    expect(saved?.tiles.slice(0, 3).map(t => cameraIdOf(t))).toEqual(["a", "b", "c"]);
  });

  it("grows rows when a stored grid is full", () => {
    const key = liveSelectionKey("t", "u");
    localStorage.setItem(key, serializeSelection(2, [
      { camera_id: "a", quality: "sub" }, { camera_id: "b", quality: "sub" },
    ], 1));
    expect(addCameraToLiveGrid("t", "u", "c")).toBe(true);
    const saved = parseSelection(localStorage.getItem(key), new Set(["a", "b", "c"]));
    expect(saved?.columns).toBe(2);
    expect(saved?.rows).toBe(2);
    expect(saved?.tiles.map(t => cameraIdOf(t) ?? null)).toEqual(["a", "b", "c", null]);
  });

  it("updates a duplicate in place without growing dimensions", () => {
    const key = liveSelectionKey("t", "u");
    localStorage.setItem(key, serializeSelection(2, [
      { camera_id: "a", quality: "sub" }, { camera_id: "b", quality: "sub" },
    ], 1));
    expect(addCameraToLiveGrid("t", "u", "a", "main")).toBe(true);
    const saved = parseSelection(localStorage.getItem(key), new Set(["a", "b"]));
    expect(saved?.rows).toBe(1);
    expect(saved?.tiles).toEqual([{ camera_id: "a", quality: "main" }, { camera_id: "b", quality: "sub" }]);
  });

  it("returns false when reading or writing storage fails", () => {
    const read = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked"); });
    expect(addCameraToLiveGrid("t", "u", "a")).toBe(false);
    read.mockRestore();
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("quota"); });
    expect(addCameraToLiveGrid("t", "u", "a")).toBe(false);
  });

  it("returns false when access to localStorage itself is denied", () => {
    vi.spyOn(window, "localStorage", "get").mockImplementation(() => { throw new Error("denied"); });
    expect(addCameraToLiveGrid("t", "u", "a")).toBe(false);
  });

});
