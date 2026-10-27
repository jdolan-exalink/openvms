import { describe, expect, it } from "vitest";
import { serverDefaultsFromYaml } from "./useServerDefaults";

const yaml = `motion:
  threshold: 30
objects:
  filters:
    person:
      min_score: 0.5
      min_area: 1000
record:
  retain:
    days: 7
cameras:
  entrada:
    motion:
      threshold: 20
  patio:
    detect:
      enabled: true
`;

describe("serverDefaultsFromYaml", () => {
  it("reports the server value of every field the camera does not override", () => {
    expect(serverDefaultsFromYaml(yaml, "patio", ["person"])).toEqual({ motionThreshold: "30", minScore: "0.5", minArea: "1000", recordRetention: "7 días" });
  });
  it("hides a field the camera overrides", () => {
    const d = serverDefaultsFromYaml(yaml, "entrada", ["person"]);
    expect(d.motionThreshold).toBeUndefined();
    expect(d.minScore).toBe("0.5");
  });
  it("needs every tracked label to share the server value", () => {
    expect(serverDefaultsFromYaml(yaml, "patio", ["person", "car"]).minScore).toBeUndefined();
  });
  it("returns nothing for an unusable camera name or an empty config", () => {
    expect(serverDefaultsFromYaml(yaml, "a b", [])).toEqual({});
    expect(serverDefaultsFromYaml("", "patio", ["person"])).toEqual({});
  });
});
