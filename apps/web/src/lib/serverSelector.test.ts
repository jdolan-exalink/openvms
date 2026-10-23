import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import type { Camera, Folder } from "./explorer";
import type { Tile } from "./liveGrid";
import {
  cameraIdsOfServers,
  removeServerTiles,
  serverChoices,
  serverFilter,
  serverOptions,
  toggleServer,
} from "./serverSelector";

const cam = (id: string, server_id: string, extra: Partial<Camera> = {}): Camera =>
  ({ id, server_id, site_id: "site-1", display_name: id, enabled: true, sort_order: 0, folder_id: null, ...extra }) as Camera;
const sites = [
  { id: "site-1", name: "Campus" },
  { id: "site-2", name: "Anexo" },
] as Schemas["Site"][];
const servers = [
  { id: "srv-b", name: "Beta", site_id: "site-1" },
  { id: "srv-a", name: "Alfa", site_id: "site-1" },
  { id: "srv-x", name: "Anexo uno", site_id: "site-2" },
  { id: "srv-empty", name: "Sin camaras", site_id: "site-1" },
] as Schemas["Server"][];
const folders: Folder[] = [];
const tile = (camera_id: string): Tile => ({ camera_id, quality: "sub" });
const selectorCameras = [
  cam("a1", "srv-a"), cam("a2", "srv-a"), cam("b1", "srv-b"), cam("x1", "srv-x"), cam("off", "srv-a", { enabled: false }),
];

describe("serverOptions", () => {
  it("lists servers with enabled cameras by site and server name", () => {
    expect(serverOptions(servers, sites, selectorCameras, []).map((o) => o.id)).toEqual(["srv-x", "srv-a", "srv-b"]);
  });

  it("checks servers unless excluded and counts enabled cameras", () => {
    const by = Object.fromEntries(serverOptions(servers, sites, selectorCameras, ["srv-b"]).map((o) => [o.id, [o.checked, o.cameras]]));
    expect(by).toEqual({ "srv-a": [true, 2], "srv-b": [false, 1], "srv-x": [true, 1] });
  });

  it("ignores exclusions that no longer match an offered server", () => {
    expect(serverOptions(servers, sites, selectorCameras, ["gone"]).every((o) => o.checked)).toBe(true);
  });
});

describe("serverFilter", () => {
  it("returns no filter when all offered servers are checked or none are offered", () => {
    expect(serverFilter(serverOptions(servers, sites, selectorCameras, []))).toBeUndefined();
    expect(serverFilter([])).toBeUndefined();
  });

  it("returns the checked servers after an exclusion", () => {
    expect(serverFilter(serverOptions(servers, sites, selectorCameras, ["srv-b"]))).toEqual(["srv-x", "srv-a"]);
  });
});

describe("toggleServer", () => {
  it("adds and removes exclusions while keeping at least one server checked", () => {
    const options = serverOptions(servers, sites, selectorCameras, []);
    expect(toggleServer([], options, "srv-a", false)).toEqual(["srv-a"]);
    expect(toggleServer(["srv-a", "srv-b"], serverOptions(servers, sites, selectorCameras, ["srv-a", "srv-b"]), "srv-x", false)).toEqual(["srv-a", "srv-b"]);
    expect(toggleServer(["srv-a", "srv-b"], serverOptions(servers, sites, selectorCameras, ["srv-a", "srv-b"]), "srv-a", true)).toEqual(["srv-b"]);
  });
});

describe("cameraIdsOfServers", () => {
  it("returns enabled cameras of selected servers", () => {
    expect(cameraIdsOfServers(selectorCameras, ["srv-b", "srv-x"]).sort()).toEqual(["b1", "x1"]);
    expect(cameraIdsOfServers(selectorCameras, [])).toEqual([]);
  });
});

describe("serverChoices", () => {
  const cameras = [cam("a1", "srv-a"), cam("a2", "srv-a"), cam("b1", "srv-b"), cam("x1", "srv-x", { site_id: "site-2" }), cam("off", "srv-a", { enabled: false })];

  it("lists the servers that have cameras, by site name and then server name", () => {
    expect(serverChoices(servers, sites, cameras, folders, []).map((c) => c.id)).toEqual(["srv-x", "srv-a", "srv-b"]);
  });

  it("counts how many cameras of each server are in the grid out of how many it has", () => {
    const choices = serverChoices(servers, sites, cameras, folders, [tile("a1"), null, tile("b1"), tile("a2")]);
    const by = Object.fromEntries(choices.map((c) => [c.id, [c.inGrid, c.total, c.checked]]));
    expect(by).toEqual({ "srv-a": [2, 2, true], "srv-b": [1, 1, true], "srv-x": [0, 1, false] });
  });

  it("does not offer a server without enabled cameras, and carries the site name", () => {
    const choices = serverChoices(servers, sites, cameras, folders, []);
    expect(choices.some((c) => c.id === "srv-empty")).toBe(false);
    expect(choices.find((c) => c.id === "srv-x")?.siteName).toBe("Anexo");
  });

  it("ignores non-camera tiles when counting", () => {
    const tiles: Tile[] = [{ map: { site_id: "site-1", name: "Planta" } }, { vision: { id: "v", name: "Vision" } }, tile("a1")];
    expect(serverChoices(servers, sites, cameras, folders, tiles).find((c) => c.id === "srv-a")?.inGrid).toBe(1);
  });
});

describe("removeServerTiles", () => {
  const cameras = [cam("a1", "srv-a"), cam("b1", "srv-b")];

  it("empties the cells of that server's cameras and keeps the rest where they are", () => {
    const tiles: Tile[] = [tile("a1"), tile("b1"), null, tile("a1")];
    expect(removeServerTiles(tiles, cameras, "srv-a")).toEqual([null, tile("b1"), null, null]);
  });

  it("empties the virtual fisheye views of those cameras too", () => {
    const tiles: Tile[] = [{ camera_id: "a1", quality: "sub", fisheye_view_id: "v1" }, tile("b1")];
    expect(removeServerTiles(tiles, cameras, "srv-a")).toEqual([null, tile("b1")]);
  });

  it("leaves maps, vision tiles and unknown cameras alone", () => {
    const map: Tile = { map: { site_id: "site-1", name: "Planta" } };
    const tiles: Tile[] = [map, tile("ghost"), tile("a1")];
    expect(removeServerTiles(tiles, cameras, "srv-a")).toEqual([map, tile("ghost"), null]);
  });

  it("does not modify its input", () => {
    const tiles: Tile[] = [tile("a1")];
    removeServerTiles(tiles, cameras, "srv-a");
    expect(tiles).toEqual([tile("a1")]);
  });
});
