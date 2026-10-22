import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import type { Camera } from "./explorer";
import { cameraIdsOfServers, serverFilter, serverOptions, toggleServer } from "./serverSelector";

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
const cameras = [cam("a1", "srv-a"), cam("a2", "srv-a"), cam("b1", "srv-b"), cam("x1", "srv-x"), cam("off", "srv-a", { enabled: false })];

describe("serverOptions", () => {
  it("lists the servers that have cameras, by site name and then server name", () => {
    expect(serverOptions(servers, sites, cameras, []).map((o) => o.id)).toEqual(["srv-x", "srv-a", "srv-b"]);
  });

  it("is checked unless the server was left out, and counts its enabled cameras", () => {
    const by = Object.fromEntries(serverOptions(servers, sites, cameras, ["srv-b"]).map((o) => [o.id, [o.checked, o.cameras]]));
    expect(by).toEqual({ "srv-a": [true, 2], "srv-b": [false, 1], "srv-x": [true, 1] });
  });

  it("carries the site name and skips servers without enabled cameras", () => {
    const options = serverOptions(servers, sites, cameras, []);
    expect(options.find((o) => o.id === "srv-x")?.siteName).toBe("Anexo");
    expect(options.some((o) => o.id === "srv-empty")).toBe(false);
  });

  it("ignores exclusions of servers that no longer exist", () => {
    expect(serverOptions(servers, sites, cameras, ["gone"]).every((o) => o.checked)).toBe(true);
  });
});

describe("serverFilter", () => {
  it("is undefined while every server is checked, so nothing is filtered", () => {
    expect(serverFilter(serverOptions(servers, sites, cameras, []))).toBeUndefined();
  });

  it("lists the checked servers once some are left out", () => {
    expect(serverFilter(serverOptions(servers, sites, cameras, ["srv-b"]))).toEqual(["srv-x", "srv-a"]);
  });

  it("is undefined when there are no servers to choose from", () => {
    expect(serverFilter([])).toBeUndefined();
  });
});

describe("toggleServer", () => {
  const options = serverOptions(servers, sites, cameras, []);

  it("unchecking adds the server to the exclusions and checking removes it", () => {
    expect(toggleServer([], options, "srv-a", false)).toEqual(["srv-a"]);
    expect(toggleServer(["srv-a", "srv-b"], serverOptions(servers, sites, cameras, ["srv-a", "srv-b"]), "srv-a", true)).toEqual(["srv-b"]);
  });

  it("never leaves nothing checked: unchecking the last server is ignored", () => {
    const onlyX = serverOptions(servers, sites, cameras, ["srv-a", "srv-b"]);
    expect(toggleServer(["srv-a", "srv-b"], onlyX, "srv-x", false)).toEqual(["srv-a", "srv-b"]);
  });

  it("keeps exclusions that do not match any option and does not repeat ids", () => {
    expect(toggleServer(["gone"], options, "srv-a", false)).toEqual(["gone", "srv-a"]);
    expect(toggleServer(["srv-a"], serverOptions(servers, sites, cameras, ["srv-a"]), "srv-a", false)).toEqual(["srv-a"]);
  });
});

describe("cameraIdsOfServers", () => {
  it("returns the enabled cameras of those servers", () => {
    expect(cameraIdsOfServers(cameras, ["srv-b", "srv-x"]).sort()).toEqual(["b1", "x1"]);
  });
  it("is empty for no servers", () => {
    expect(cameraIdsOfServers(cameras, [])).toEqual([]);
  });
});
