import { describe, expect, it } from "vitest";
import { pickProvenance } from "./useProvenance";

const map = {
  "detection.enabled": { source: "engine" as const },
  "recording.enabled": { source: "profile" as const, profile_id: "p1", profile_name: "Exterior" },
  "streams.primary.fps": { source: "override" as const },
};

describe("pickProvenance", () => {
  it("returns the source of a single path", () => {
    expect(pickProvenance(map, "recording.enabled")?.profile_name).toBe("Exterior");
  });
  it("lets a local override win over profile and engine", () => {
    expect(pickProvenance(map, ["detection.enabled", "recording.enabled", "streams.primary.fps"])?.source).toBe("override");
    expect(pickProvenance(map, ["detection.enabled", "recording.enabled"])?.source).toBe("profile");
  });
  it("is undefined without map or unknown path", () => {
    expect(pickProvenance(undefined, "x")).toBeUndefined();
    expect(pickProvenance(map, "nope")).toBeUndefined();
  });
});
