import { describe, expect, it } from "vitest";
import { liveSelectionKey, parseSelection, resizeTiles, serializeSelection, type Tile } from "./liveGrid";

describe("resizeTiles", () => {
  it("pads with null up to n*n", () => {
    expect(resizeTiles([], 2)).toEqual([null, null, null, null]);
  });

  it("truncates to n*n, keeping the first ones", () => {
    const tiles: Tile[] = [{ camera_id: "a", quality: "sub" }, { camera_id: "b", quality: "sub" }, { camera_id: "c", quality: "sub" }];
    expect(resizeTiles(tiles, 1)).toEqual([{ camera_id: "a", quality: "sub" }]);
  });
});

describe("liveSelectionKey", () => {
  it("scopes the key by tenant and user id", () => {
    expect(liveSelectionKey("t1", "u1")).toBe("openvms.live.selection.v1:t1:u1");
  });

  it("uses a platform placeholder for platform users (null tenant)", () => {
    expect(liveSelectionKey(null, "u1")).toBe("openvms.live.selection.v1:platform:u1");
  });
});

describe("serializeSelection / parseSelection", () => {
  const validIds = new Set(["cam-1", "cam-2"]);

  it("round-trips a valid selection", () => {
    const tiles: Tile[] = [{ camera_id: "cam-1", quality: "main" }, null, { camera_id: "cam-2", quality: "sub" }, null];
    const raw = serializeSelection(2, tiles);
    expect(parseSelection(raw, validIds)).toEqual({ columns: 2, tiles });
  });

  it("drops cameras the user can no longer see, keeping their slot empty", () => {
    const raw = serializeSelection(1, [{ camera_id: "gone", quality: "sub" }]);
    expect(parseSelection(raw, validIds)).toEqual({ columns: 1, tiles: [null] });
  });

  it("returns null for missing input", () => {
    expect(parseSelection(null, validIds)).toBeNull();
  });

  it("returns null for malformed JSON instead of throwing", () => {
    expect(parseSelection("{not json", validIds)).toBeNull();
  });

  it("returns null when columns is missing or not a positive integer", () => {
    expect(parseSelection(JSON.stringify({ tiles: [] }), validIds)).toBeNull();
    expect(parseSelection(JSON.stringify({ columns: 0, tiles: [] }), validIds)).toBeNull();
    expect(parseSelection(JSON.stringify({ columns: 1.5, tiles: [] }), validIds)).toBeNull();
  });
});
