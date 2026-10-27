import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { can, canCamera } from "./perm";

const me = (...perms: string[]) => ({ id: "u", tenant_id: "t", grants: perms.map((permission) => ({ permission, effect: "allow" })) }) as unknown as Schemas["Me"];

describe("canCamera", () => {
  it("keeps legacy holders working and grants nothing to others", () => {
    expect(canCamera(me("cameras.view"), "cameras.config.view")).toBe(true);
    expect(canCamera(me("cameras.view"), "cameras.config.video")).toBe(false);
    expect(canCamera(me("cameras.manage"), "cameras.config.video")).toBe(false);
    expect(canCamera(me("cameras.manage"), "cameras.config.ptz")).toBe(true);
    expect(canCamera(me("servers.config"), "cameras.config.recording")).toBe(true);
    expect(canCamera(me("servers.config"), "cameras.web.access")).toBe(false);
    expect(canCamera(me("servers.manage", "servers.config.secrets"), "cameras.web.access")).toBe(true);
    expect(canCamera(undefined, "cameras.config.view")).toBe(false);
  });

  it("stops honoring legacy once the user has granular grants", () => {
    const granular = me("servers.config", "cameras.config.edit");
    expect(canCamera(granular, "cameras.config.edit")).toBe(true);
    expect(canCamera(granular, "cameras.config.video")).toBe(false);
    expect(canCamera(me("servers.manage", "servers.config.secrets", "cameras.config.edit"), "cameras.web.access")).toBe(true);
  });

  it("can() is unchanged", () => {
    expect(can(me("cameras.config.video"), "cameras.config.video")).toBe(true);
  });
});
