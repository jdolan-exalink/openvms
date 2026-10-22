import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { applyReorder, buildTree, camerasOfServer, camerasOfSite, moveCamera, moveFolder, resolveTreeDrop, treeCameraDropId, treeFolderId, treeRootDropId, type Camera, type Folder } from "./explorer";

const cam = (id: string, server: string, folder: string | null, order: number, name = id): Camera =>
  ({ id, server_id: server, site_id: "s1", folder_id: folder, sort_order: order, display_name: name, enabled: true, status: "online" }) as Camera;
const folder = (id: string, server: string, order: number, name = id): Folder => ({ id, server_id: server, sort_order: order, name }) as Folder;

const cams = [cam("a", "A", null, 0), cam("b", "A", "f1", 0), cam("c", "A", "f1", 1), cam("x", "B", null, 0)];
const folders = [folder("f1", "A", 0), folder("f2", "A", 1), folder("g1", "B", 0)];

describe("moveCamera", () => {
  it("moves into a folder and renumbers only what changed", () => {
    const r = moveCamera(cams, folders, "a", { serverId: "A", folderId: "f1", beforeCameraId: "c" });
    expect(r?.cameras).toEqual([
      { camera_id: "a", folder_id: "f1", sort_order: 1 },
      { camera_id: "c", folder_id: "f1", sort_order: 2 },
    ]);
  });

  it("reorders inside a folder", () => {
    const r = moveCamera(cams, folders, "c", { serverId: "A", folderId: "f1", beforeCameraId: "b" });
    expect(r?.cameras).toEqual([
      { camera_id: "c", folder_id: "f1", sort_order: 0 },
      { camera_id: "b", folder_id: "f1", sort_order: 1 },
    ]);
  });

  it("moves to the server root", () => {
    const r = moveCamera(cams, folders, "b", { serverId: "A", folderId: null });
    expect(r?.cameras).toContainEqual({ camera_id: "b", folder_id: null, sort_order: 1 });
    expect(r?.cameras).toContainEqual({ camera_id: "c", folder_id: "f1", sort_order: 0 });
  });

  it("refuses another server's folder or root", () => {
    expect(moveCamera(cams, folders, "a", { serverId: "B", folderId: null })).toBeNull();
    expect(moveCamera(cams, folders, "a", { serverId: "A", folderId: "g1" })).toBeNull();
  });
});

describe("moveFolder", () => {
  it("reorders folders of one server", () => {
    expect(moveFolder(folders, "f2", "f1")?.folders).toEqual([
      { folder_id: "f2", sort_order: 0 },
      { folder_id: "f1", sort_order: 1 },
    ]);
  });
  it("refuses a folder of another server", () => {
    expect(moveFolder(folders, "f1", "g1")).toBeNull();
  });
});

describe("resolveTreeDrop", () => {
  it("ignores grid drops", () => {
    expect(resolveTreeDrop("camera:a", "tile:0", cams, folders)).toBeNull();
    expect(resolveTreeDrop("tile:0", "tile:1", cams, folders)).toBeNull();
  });
  it("rejects a camera dropped on another server", () => {
    expect(resolveTreeDrop("camera:a", treeFolderId("g1"), cams, folders)).toEqual({ rejected: true });
    expect(resolveTreeDrop("camera:a", treeRootDropId("B"), cams, folders)).toEqual({ rejected: true });
    expect(resolveTreeDrop("camera:a", treeCameraDropId("x"), cams, folders)).toEqual({ rejected: true });
  });
  it("moves a camera onto a folder header (end) and a folder onto a folder (reorder)", () => {
    const r = resolveTreeDrop("camera:a", treeFolderId("f1"), cams, folders);
    expect(r && "reorder" in r && r.reorder.cameras.find((c) => c.camera_id === "a")).toEqual({ camera_id: "a", folder_id: "f1", sort_order: 2 });
    const f = resolveTreeDrop("tfolder:f2", treeFolderId("f1"), cams, folders);
    expect(f && "reorder" in f && f.reorder.folders[0]).toEqual({ folder_id: "f2", sort_order: 0 });
  });
});

describe("applyReorder", () => {
  it("patches cameras and folders", () => {
    const out = applyReorder(cams, folders, { cameras: [{ camera_id: "a", folder_id: "f1", sort_order: 5 }], folders: [{ folder_id: "f2", sort_order: 9 }] });
    expect(out.cameras.find((c) => c.id === "a")).toMatchObject({ folder_id: "f1", sort_order: 5 });
    expect(out.folders.find((f) => f.id === "f2")?.sort_order).toBe(9);
  });
});

describe("buildTree", () => {
  const base = { cameras: cams, folders, sites: [{ id: "s1", name: "Campus" }] as never, servers: [{ id: "A", site_id: "s1", name: "Alpha", status: "online" }, { id: "B", site_id: "s1", name: "Beta", status: "online" }] as never };
  it("nests folders and root cameras with counts", () => {
    const site = buildTree({ ...base, manageable: new Set(), query: "" })[0]!;
    const alpha = site.servers.find((s) => s.id === "A")!;
    expect(site.count).toBe(4);
    expect(alpha.folders.map((f) => f.folder.id)).toEqual(["f1"]); // f2 is empty: hidden to non-managers
    expect(alpha.rootCameras.map((c) => c.id)).toEqual(["a"]);
  });
  it("keeps empty folders for managers", () => {
    const site = buildTree({ ...base, manageable: new Set(["A"]), query: "" })[0]!;
    expect(site.servers.find((s) => s.id === "A")!.folders.map((f) => f.folder.id)).toEqual(["f1", "f2"]);
  });
  it("filters by camera and folder name", () => {
    const byCam = buildTree({ ...base, manageable: new Set(), query: "c" });
    expect(byCam[0]!.servers.flatMap((s) => s.folders.flatMap((f) => f.cameras.map((c) => c.id)))).toEqual(["c"]);
    const byFolder = buildTree({ ...base, manageable: new Set(), query: "f1" });
    expect(byFolder[0]!.count).toBe(2);
  });
});

describe("camerasOfServer and camerasOfSite", () => {
  const cam = (id: string, server_id: string, extra: Partial<Camera> = {}): Camera =>
    ({ id, server_id, site_id: "site-1", display_name: id, enabled: true, sort_order: 0, folder_id: null, ...extra }) as Camera;
  const folder = (id: string, server_id: string, sort_order = 0): Folder => ({ id, server_id, name: id, sort_order }) as Folder;
  const servers = [
    { id: "srv-b", name: "Beta", site_id: "site-1" },
    { id: "srv-a", name: "Alfa", site_id: "site-1" },
    { id: "srv-z", name: "Otro sitio", site_id: "site-2" },
  ] as Schemas["Server"][];

  it("puts a server's folders first, in folder order, then its loose cameras", () => {
    const cams = [cam("loose", "srv-a"), cam("in-f2", "srv-a", { folder_id: "f2" }), cam("in-f1", "srv-a", { folder_id: "f1" })];
    const folders = [folder("f2", "srv-a", 2), folder("f1", "srv-a", 1)];
    expect(camerasOfServer("srv-a", cams, folders).map((c) => c.id)).toEqual(["in-f1", "in-f2", "loose"]);
  });

  it("skips disabled cameras and cameras of other servers", () => {
    const cams = [cam("on", "srv-a"), cam("off", "srv-a", { enabled: false }), cam("elsewhere", "srv-b")];
    expect(camerasOfServer("srv-a", cams, []).map((c) => c.id)).toEqual(["on"]);
  });

  it("a camera in a folder that no longer exists is shown with the loose ones", () => {
    expect(camerasOfServer("srv-a", [cam("orphan", "srv-a", { folder_id: "gone" })], []).map((c) => c.id)).toEqual(["orphan"]);
  });

  it("gathers every camera of every server of the site, servers by name", () => {
    const cams = [cam("b1", "srv-b"), cam("a1", "srv-a"), cam("a2", "srv-a", { sort_order: 1 }), cam("z1", "srv-z")];
    expect(camerasOfSite("site-1", servers, cams, []).map((c) => c.id)).toEqual(["a1", "a2", "b1"]);
  });

  it("returns nothing for a site without cameras or an unknown site", () => {
    expect(camerasOfSite("site-2", servers, [cam("a1", "srv-a")], [])).toEqual([]);
    expect(camerasOfSite("nope", servers, [cam("a1", "srv-a")], [])).toEqual([]);
  });
});
