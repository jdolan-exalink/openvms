import { describe, expect, it } from "vitest";
import {
  cameraDragId, layoutForCount, LIVE_GRID_DROP_ID, liveSelectionKey, mapDragId, parseSelection, duplicateTileIndexes, placeCameraUnique, placeInOpenCell, placeVisionInOpenCell, placeMapAt, reorderTiles, resizeTiles, resolveDragEnd, serializeSelection, swapTiles, tileDragId, tilesForCameras, type Tile, feviewDragId, feviewFromDragId, tilesForFisheyeMultiView, cleanFisheyeVirtualViewTiles, visionDragId, placeVisionAt, isVisionTile, tileIdentityKey,
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

  it("resolves dropping a map onto a tile as a placement", () => {
    const map = { site_id: "s", name: "Planta / PB", floor_id: "f" };
    expect(resolveDragEnd(mapDragId(map), tileDragId(1))).toEqual({ type: "place-map", index: 1, map });
    expect(resolveDragEnd("tmap:not-json", tileDragId(0))).toBeNull();
    expect(resolveDragEnd(mapDragId(map), LIVE_GRID_DROP_ID)).toBeNull();
  });

  it("places a map in a cell and swaps it when that map is already on the grid", () => {
    const map = { site_id: "s", name: "Planta", floor_id: "f" };
    const placed = placeMapAt([null, { camera_id: "cam-1", quality: "sub" }], 0, map);
    expect(placed[0]).toEqual({ map });
    const swapped = placeMapAt([{ map }, { camera_id: "cam-1", quality: "sub" }], 1, map);
    expect(swapped).toEqual([{ camera_id: "cam-1", quality: "sub" }, { map }]);
  });

  it("resolves dropping a vision instance onto a tile as a placement", () => {
    const vision = { id: "vis-1", name: "Vision AI - Personas" };
    expect(resolveDragEnd(visionDragId(vision), tileDragId(1))).toEqual({ type: "place-vision", index: 1, vision });
    expect(resolveDragEnd("tvision:not-json", tileDragId(0))).toBeNull();
    expect(resolveDragEnd(visionDragId(vision), LIVE_GRID_DROP_ID)).toBeNull();
  });

  it("places a vision instance in a cell", () => {
    const vision = { id: "vis-1", name: "Vision AI - Personas" };
    const placed = placeVisionAt([null, { camera_id: "cam-1", quality: "sub" }], 0, vision);
    expect(placed[0]).toEqual({ vision });
    expect(isVisionTile(placed[0]!)).toBe(true);
    expect(tileIdentityKey(placed[0]!)).toBe("vision:vis-1");
  });

  it("resolves dropping a server or a folder onto the grid as a fill", () => {
    expect(resolveDragEnd("tserver:srv-1", tileDragId(0))).toEqual({ type: "fill-server", serverId: "srv-1" });
    expect(resolveDragEnd("tfolder:fld-1", LIVE_GRID_DROP_ID)).toEqual({ type: "fill-folder", folderId: "fld-1" });
    expect(resolveDragEnd("tserver:srv-1", "not-a-tile")).toBeNull();
  });
});

describe("layoutForCount", () => {
  const layouts = [
    { columns: 1, rows: 1 },
    { columns: 2, rows: 2 },
    { columns: 4, rows: 4 },
  ];

  it("picks the smallest layout that fits every camera", () => {
    expect(layoutForCount(layouts, 1)).toEqual({ columns: 1, rows: 1 });
    expect(layoutForCount(layouts, 3)).toEqual({ columns: 2, rows: 2 });
    expect(layoutForCount(layouts, 5)).toEqual({ columns: 4, rows: 4 });
  });

  it("uses the largest layout when there are more cameras than cells", () => {
    expect(layoutForCount(layouts, 40)).toEqual({ columns: 4, rows: 4 });
    expect(tilesForCameras([{ id: "a", quality: "sub" }, { id: "b", quality: "sub" }, { id: "c", quality: "main" }], 2, 1)).toEqual({
      tiles: [{ camera_id: "a", quality: "sub" }, { camera_id: "b", quality: "sub" }],
      skipped: 1,
    });
  });
});

describe("placeInOpenCell", () => {
  const tiles: Tile[] = [{ camera_id: "cam-1", quality: "sub" }, null, { camera_id: "cam-2", quality: "sub" }];

  it("uses the first empty cell", () => {
    expect(placeInOpenCell(tiles, "cam-3", "sub")).toEqual({
      tiles: [{ camera_id: "cam-1", quality: "sub" }, { camera_id: "cam-3", quality: "sub" }, { camera_id: "cam-2", quality: "sub" }],
      index: 1,
    });
  });

  it("replaces the first cell when the grid is full", () => {
    const full: Tile[] = [{ camera_id: "cam-1", quality: "sub" }, { camera_id: "cam-2", quality: "sub" }];
    expect(placeInOpenCell(full, "cam-3", "main").index).toBe(0);
    expect(placeInOpenCell(full, "cam-3", "main").tiles[0]).toEqual({ camera_id: "cam-3", quality: "main" });
  });

  it("keeps a camera that is already on the grid", () => {
    expect(placeInOpenCell(tiles, "cam-2", "main")).toEqual({ tiles, index: 2 });
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

  it("keeps a named map tile beside authorized cameras", () => {
    const withMap = serializeSelection(2, [{ map: { site_id: "s", name: "Planta", floor_id: "f" } }, { camera_id: "cam-1", quality: "sub" }]);
    expect(parseSelection(withMap, validIds)?.tiles).toEqual([
      { map: { site_id: "s", name: "Planta", floor_id: "f" } },
      { camera_id: "cam-1", quality: "sub" },
      null,
      null,
    ]);
  });

  it("keeps a vision tile beside authorized cameras", () => {
    const withVision = serializeSelection(2, [{ vision: { id: "vis-1", name: "Vision AI" } }, { camera_id: "cam-1", quality: "sub" }]);
    expect(parseSelection(withVision, validIds)?.tiles).toEqual([
      { vision: { id: "vis-1", name: "Vision AI" } },
      { camera_id: "cam-1", quality: "sub" },
      null,
      null,
    ]);
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

  it("distinguishes different virtual views of the same camera", () => {
    const v1: Tile = { camera_id: "a", quality: "sub", fisheye_view_id: "view-1" };
    const v2: Tile = { camera_id: "a", quality: "sub", fisheye_view_id: "view-2" };
    expect([...duplicateTileIndexes([v1, v2, v1])]).toEqual([2]);

    const placed = placeCameraUnique([v1, null], 1, "a", "main", "view-2");
    expect(placed).toEqual([v1, { camera_id: "a", quality: "main", fisheye_view_id: "view-2" }]);
  });
});

describe("fisheye virtual view drag/drop resolution", () => {
  it("encodes and decodes feview drag ids correctly", () => {
    const dragId = feviewDragId("cam-360", "view-120-1");
    expect(feviewFromDragId(dragId)).toEqual({ cameraId: "cam-360", viewId: "view-120-1" });
    expect(feviewFromDragId("invalid-prefix:abc")).toBeNull();
  });

  it("resolves dropping a virtual view onto a tile slot", () => {
    const dragId = feviewDragId("cam-360", "quad-2");
    expect(resolveDragEnd(dragId, tileDragId(3))).toEqual({
      type: "place",
      index: 3,
      cameraId: "cam-360",
      fisheyeViewId: "quad-2",
    });
  });
});

describe("tilesForFisheyeMultiView", () => {
  it("populates 1 real view + 3 views of 120 on a 4-pane grid", () => {
    const views = [{ id: "v1" }, { id: "v2" }, { id: "v3" }];
    const tiles = tilesForFisheyeMultiView("cam-360", views, "sub", 4);
    expect(tiles).toHaveLength(4);
    expect(tiles[0]).toEqual({ camera_id: "cam-360", quality: "sub" });
    expect(tiles[1]).toEqual({ camera_id: "cam-360", quality: "sub", fisheye_view_id: "v1" });
    expect(tiles[2]).toEqual({ camera_id: "cam-360", quality: "sub", fisheye_view_id: "v2" });
    expect(tiles[3]).toEqual({ camera_id: "cam-360", quality: "sub", fisheye_view_id: "v3" });
  });

  it("populates 1 real view + 4 quadrant views on a 6-pane grid with trailing empty slot", () => {
    const views = [{ id: "q1" }, { id: "q2" }, { id: "q3" }, { id: "q4" }];
    const tiles = tilesForFisheyeMultiView("cam-360", views, "main", 6);
    expect(tiles).toHaveLength(6);
    expect(tiles[0]).toEqual({ camera_id: "cam-360", quality: "main" });
    expect(tiles[1]).toEqual({ camera_id: "cam-360", quality: "main", fisheye_view_id: "q1" });
    expect(tiles[2]).toEqual({ camera_id: "cam-360", quality: "main", fisheye_view_id: "q2" });
    expect(tiles[3]).toEqual({ camera_id: "cam-360", quality: "main", fisheye_view_id: "q3" });
    expect(tiles[4]).toEqual({ camera_id: "cam-360", quality: "main", fisheye_view_id: "q4" });
    expect(tiles[5]).toBeNull();
  });
});

describe("cleanFisheyeVirtualViewTiles", () => {
  it("does nothing when there are no virtual views for the camera", () => {
    const tiles: Tile[] = [
      { camera_id: "cam-1", quality: "sub" },
      { camera_id: "cam-2", quality: "sub" },
    ];
    const res = cleanFisheyeVirtualViewTiles(tiles, "cam-1");
    expect(res.changed).toBe(false);
    expect(res.shouldResetGridToSingle).toBe(false);
    expect(res.tiles).toEqual(tiles);
  });

  it("resets grid to 1x1 single tile when only this fisheye camera was in the grid", () => {
    const tiles: Tile[] = [
      { camera_id: "cam-fe", quality: "sub" },
      { camera_id: "cam-fe", quality: "sub", fisheye_view_id: "view-1" },
      { camera_id: "cam-fe", quality: "sub", fisheye_view_id: "view-2" },
      null,
    ];
    const res = cleanFisheyeVirtualViewTiles(tiles, "cam-fe", { resetIfOnlyThisCamera: true, defaultQuality: "main" });
    expect(res.changed).toBe(true);
    expect(res.shouldResetGridToSingle).toBe(true);
    expect(res.tiles).toEqual([{ camera_id: "cam-fe", quality: "main" }]);
  });

  it("clears virtual views with nulls when other cameras exist in the grid without resetting grid", () => {
    const tiles: Tile[] = [
      { camera_id: "cam-fe", quality: "sub" },
      { camera_id: "cam-fe", quality: "sub", fisheye_view_id: "view-1" },
      { camera_id: "cam-other", quality: "sub" },
      { camera_id: "cam-fe", quality: "sub", fisheye_view_id: "view-2" },
    ];
    const res = cleanFisheyeVirtualViewTiles(tiles, "cam-fe", { resetIfOnlyThisCamera: true });
    expect(res.changed).toBe(true);
    expect(res.shouldResetGridToSingle).toBe(false);
    expect(res.tiles).toEqual([
      { camera_id: "cam-fe", quality: "sub" },
      null,
      { camera_id: "cam-other", quality: "sub" },
      null,
    ]);
  });

  describe("placeVisionInOpenCell", () => {
    const v1 = { id: "vis-1", name: "Vision AI" };
    const v2 = { id: "vis-2", name: "Vision AI - LPR" };

    it("places vision in the first empty cell", () => {
      const tiles: Tile[] = [{ camera_id: "cam-1", quality: "sub" }, null, null, null];
      const res = placeVisionInOpenCell(tiles, v1);
      expect(res.index).toBe(1);
      expect(res.tiles[1]).toEqual({ vision: v1 });
    });

    it("returns existing index if vision is already on the grid without duplicating", () => {
      const tiles: Tile[] = [{ camera_id: "cam-1", quality: "sub" }, { vision: v1 }, null, null];
      const res = placeVisionInOpenCell(tiles, v1);
      expect(res.index).toBe(1);
      expect(res.tiles).toEqual(tiles);
    });

    it("replaces index 0 if grid is completely full", () => {
      const tiles: Tile[] = [
        { camera_id: "cam-1", quality: "sub" },
        { camera_id: "cam-2", quality: "sub" },
      ];
      const res = placeVisionInOpenCell(tiles, v2);
      expect(res.index).toBe(0);
      expect(res.tiles[0]).toEqual({ vision: v2 });
      expect(res.tiles[1]).toEqual({ camera_id: "cam-2", quality: "sub" });
    });
  });
});

