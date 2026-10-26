import { describe, expect, it } from "vitest";
import { cameraTypeOf, tagsForType } from "./cameraType";

describe("cameraTypeOf", () => {
  it("is generic without lpr signals", () => {
    expect(cameraTypeOf([])).toBe("generic");
    expect(cameraTypeOf(["ptz"], { looksProvision: true })).toBe("generic");
  });
  it("is lpr_provision with provision tags and lpr", () => {
    expect(cameraTypeOf(["lpr", "provision"])).toBe("lpr_provision");
    expect(cameraTypeOf(["LPR", "provision_isr"])).toBe("lpr_provision");
  });
  it("is lpr_frigate with the lpr tag or Frigate lpr.enabled", () => {
    expect(cameraTypeOf(["lpr"])).toBe("lpr_frigate");
    expect(cameraTypeOf([], { frigateLpr: true })).toBe("lpr_frigate");
    expect(cameraTypeOf(["lpr"], { looksProvision: true, frigateLpr: true })).toBe("lpr_frigate");
  });
  it("treats a Provision-looking lpr camera without Frigate lpr as provision", () => {
    expect(cameraTypeOf(["lpr"], { looksProvision: true })).toBe("lpr_provision");
  });
});

describe("tagsForType", () => {
  it("keeps unrelated tags and swaps the managed ones", () => {
    expect(tagsForType(["ptz", "lpr", "provision", "provision_isr", "norte"], "generic")).toEqual(["ptz", "norte"]);
    expect(tagsForType(["ptz", "provision"], "lpr_frigate")).toEqual(["ptz", "lpr"]);
    expect(tagsForType(["ptz"], "lpr_provision")).toEqual(["ptz", "provision", "lpr"]);
  });
});
