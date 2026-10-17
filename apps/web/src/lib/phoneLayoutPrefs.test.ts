import { afterEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_PHONE_LAYOUT, loadPhoneLayout, parsePhoneLayout, phoneLayoutKey, savePhoneLayout } from "./phoneLayoutPrefs";

afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe("phone layout preference", () => {
  it("is scoped per tenant and user", () => {
    expect(phoneLayoutKey("t1", "u1")).toBe("openvms.livephone.layout.v1:t1:u1");
    expect(phoneLayoutKey(null, "u1")).toBe("openvms.livephone.layout.v1:platform:u1");
  });

  it("defaults to the grid for missing, malformed or unknown values", () => {
    expect(DEFAULT_PHONE_LAYOUT).toBe("grid");
    for (const raw of [null, "", "not json", "{}", '{"layout":"carousel"}', "42"]) expect(parsePhoneLayout(raw)).toBe("grid");
  });

  it("round-trips every layout", () => {
    for (const layout of ["list", "grid", "focus"] as const) {
      savePhoneLayout("k", layout);
      expect(loadPhoneLayout("k")).toBe(layout);
    }
  });

  it("survives unavailable storage", () => {
    vi.stubGlobal("localStorage", { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("full"); } });
    expect(loadPhoneLayout("k")).toBe("grid");
    expect(() => savePhoneLayout("k", "list")).not.toThrow();
  });
});
