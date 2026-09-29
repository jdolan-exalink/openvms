import { describe, expect, it } from "vitest";
import {
  cameraDragId, liveSelectionKey, parseSelection, duplicateTileIndexes, placeCameraUnique, reorderTiles, resizeTiles, resolveDragEnd, serializeSelection, swapTiles, tileDragId, type Tile,
} from "./liveGrid";

describe("resizeTiles", () => {
  it("pads with null up to n*n", () => {
    expect(resizeTiles([], 2)).toEqual([null, null, null, null]);
  });

  it("truncates to n*n, keeping the first ones", () => {
    const tiles: Tile[] = [{ camera_id: "a", quality: "sub" }, { camera_id: "b", quality: "sub" }, { camera_id: "c", quality: "sub" }];
    expect(resizeTiles(tiles, 1)).toEqual([{ camera_id: "a", quality: "sub" }]);
  });

  it("supports a rectangular layout without dropping camera order", () => {
    const tiles: Tile[] = [{ camera_id: "a", quality: "sub" }, { camera_id: "b", quality: "main" }];
    expect(resizeTiles(tiles, 2, 1)).toEqual(tiles);
    expect(resizeTiles(tiles, 3, 2)).toEqual([...tiles, null, null, null, null]);
  });
});

describe("reorderTiles", () => {
  const tiles: Tile[] = [
    { camera_id: "a", quality: "sub" },
    { camera_id: "b", quality: "sub" },
    { camera_id: "c", quality: "sub" },
    { camera_id: "d", quality: "sub" },
  ];

  it("moves a tile forward, shifting the ones in between back one slot", () => {
    expect(reorderTiles(tiles, 0, 2)).toEqual([
      { camera_id: "b", quality: "sub" },
      { camera_id: "c", quality: "sub" },
      { camera_id: "a", quality: "sub" },
      { camera_id: "d", quality: "sub" },
    ]);
  });

  it("moves a tile backward, shifting the ones in between forward one slot", () => {
    expect(reorderTiles(tiles, 3, 1)).toEqual([
      { camera_id: "a", quality: "sub" },
      { camera_id: "d", quality: "sub" },
      { camera_id: "b", quality: "sub" },
      { camera_id: "c", quality: "sub" },
    ]);
  });

  it("swaps two adjacent tiles", () => {
    expect(reorderTiles(tiles, 1, 2)).toEqual([
      { camera_id: "a", quality: "sub" },
      { camera_id: "c", quality: "sub" },
      { camera_id: "b", quality: "sub" },
      { camera_id: "d", quality: "sub" },
    ]);
  });

  it("is a no-op moving a tile to its own slot", () => {
    expect(reorderTiles(tiles, 2, 2)).toEqual(tiles);
  });

  it("is a no-op for an out-of-range index", () => {
    expect(reorderTiles(tiles, 0, 99)).toEqual(tiles);
    expect(reorderTiles(tiles, -1, 2)).toEqual(tiles);
  });
});

describe("resolveDragEnd", () => {
  it("resolves dropping a camera list item onto a tile as a placement", () => {
    expect(resolveDragEnd(cameraDragId("cam-1"), tileDragId(2))).toEqual({ type: "place", index: 2, cameraId: "cam-1" });
  });

  it("resolves dropping a tile onto another tile as a reorder", () => {
    expect(resolveDragEnd(tileDragId(0), tileDragId(3))).toEqual({ type: "reorder", from: 0, to: 3 });
  });

  it("is a no-op dropping a tile onto itself", () => {
    expect(resolveDragEnd(tileDragId(1), tileDragId(1))).toBeNull();
  });

  it("is a no-op when dropped outside any droppable", () => {
    expect(resolveDragEnd(tileDragId(1), null)).toBeNull();
  });

  it("is a no-op when dropped onto something that isn't a tile", () => {
    expect(resolveDragEnd(cameraDragId("cam-1"), "not-a-tile")).toBeNull();
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
    expect(parseSelection(raw, validIds)).toEqual({ columns: 2, rows: 2, tiles });
  });

  it("restores rectangular selections while accepting legacy square selections", () => {
    const rectangular = serializeSelection(3, [{ camera_id: "cam-1", quality: "sub" }, null, null, null, null, null], 2);
    expect(parseSelection(rectangular, validIds)).toEqual({ columns: 3, rows: 2, tiles: [{ camera_id: "cam-1", quality: "sub" }, null, null, null, null, null] });
    expect(parseSelection(JSON.stringify({ columns: 2, tiles: [null] }), validIds)).toEqual({ columns: 2, rows: 2, tiles: [null, null, null, null] });
  });

  it("drops cameras the user can no longer see, keeping their slot empty", () => {
    const raw = serializeSelection(1, [{ camera_id: "gone", quality: "sub" }]);
    expect(parseSelection(raw, validIds)).toEqual({ columns: 1, rows: 1, tiles: [null] });
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

describe("swapTiles / placeCameraUnique / duplicateTileIndexes", () => {
  const a: Tile = { camera_id: "a", quality: "sub" };
  const b: Tile = { camera_id: "b", quality: "sub" };
  const c: Tile = { camera_id: "c", quality: "sub" };

  it("swaps two cells without shifting the ones in between", () => {
    expect(swapTiles([a, b, c, null], 0, 3)).toEqual([null, b, c, a]);
    expect(swapTiles([a, b, c], 0, 2)).toEqual([c, b, a]);
  });

  it("ignores identical or out-of-range indexes", () => {
    const tiles = [a, b];
    expect(swapTiles(tiles, 1, 1)).toBe(tiles);
    expect(swapTiles(tiles, 0, 5)).toBe(tiles);
  });

  it("moves a camera that is already on the grid instead of duplicating it", () => {
    expect(placeCameraUnique([a, b, null], 2, "a", "main")).toEqual([null, b, a]);
    expect(placeCameraUnique([a, b, null], 2, "z", "main")).toEqual([a, b, { camera_id: "z", quality: "main" }]);
  });

  it("finds cells that repeat an earlier camera", () => {
    expect([...duplicateTileIndexes([a, b, a, null, b])]).toEqual([2, 4]);
  });
});
