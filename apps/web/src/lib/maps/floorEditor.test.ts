import { describe, it, expect, vi, afterEach } from "vitest";
import { floorPoint, emptyFloorDraft, stageFloor, undoFloor, redoFloor, saveFloorPlacement, floorEntitiesQuery } from "./floorEditor";
import { json, stubApi } from "@/test-utils";
afterEach(() => vi.unstubAllGlobals());
describe("independent floor placements", () => {
  it("projects top-left coordinates through zoom/pan and rejects outside points", () => {
    const rect = { left: 10, top: 20, width: 400, height: 200 };
    expect(floorPoint(rect, { scale: 2, x: 30, y: 10 }, 240, 130)).toEqual({ x: .25, y: .25 });
    expect(floorPoint(rect, { scale: 2, x: 30, y: 10 }, 20, 30)).toBeUndefined();
  });
  it("undoes/redoes without mixing revisions or geographic coordinates", () => {
    let d = stageFloor(emptyFloorDraft(), "c", { x: .2, y: .3 }, 7);
    d = stageFloor(d, "c", { x: .4, y: .5 }, 99);
    expect(d.entries.c).toEqual({ x: .4, y: .5, revision: 7 });
    expect(undoFloor(d).entries.c?.x).toBe(.2);
    expect(redoFloor(undoFloor(d))).toEqual(d);
  });
  it("reads only this floor and saves normalized position without lat/lng", async () => {
    const fetch = vi.fn(stubApi({
      "/api/v1/maps/sites/s/entities": () => json({ revision: 1, entities: [] }),
      "/api/v1/maps/placements/camera/c": () => json({ revision: 8 }),
    }));
    vi.stubGlobal("fetch", fetch);
    await floorEntitiesQuery("s", "f").queryFn!({ signal: new AbortController().signal } as never);
    expect(new URL((fetch.mock.calls[0]![0] as Request).url).searchParams.get("floor_id")).toBe("f");
    await saveFloorPlacement("s", "f", "c", { x: .3, y: .5, revision: 7 });
    const request = fetch.mock.calls[1]![0] as Request;
    expect(request.headers.get("If-Match")).toBe('"7"');
    expect(await request.json()).toEqual({ site_id: "s", floor_id: "f", x: .3, y: .5 });
  });
});
