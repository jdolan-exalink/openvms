import { describe, expect, it } from "vitest";
import { buildPatch, diffValues, isLiveSection, isSecretPath, type JSchema, kindOf, orderSections, resolve, schemaAt, validateTree } from "./frigateSchema";

const root: JSchema = {
  $ref: "#/$defs/CameraConfig",
  $defs: {
    CameraConfig: { type: "object", properties: { detect: { $ref: "#/$defs/Detect" }, objects: { $ref: "#/$defs/Objects" } } },
    Detect: {
      type: "object",
      properties: {
        enabled: { type: "boolean", default: true },
        fps: { anyOf: [{ type: "integer", minimum: 1, maximum: 30 }, { type: "null" }], default: 5 },
        mode: { $ref: "#/$defs/Mode" },
      },
    },
    Mode: { type: "string", enum: ["a", "b"] },
    Objects: {
      type: "object",
      properties: {
        track: { type: "array", items: { type: "string" } },
        filters: { type: "object", additionalProperties: { $ref: "#/$defs/Filter" } },
      },
    },
    Filter: { type: "object", properties: { min_area: { type: "integer" } } },
  },
};

describe("resolve/kindOf", () => {
  it("follows $ref and unwraps nullable anyOf", () => {
    const detect = resolve({ $ref: "#/$defs/Detect" }, root).node;
    const fps = resolve(detect.properties?.fps, root);
    expect(fps.nullable).toBe(true);
    expect(fps.node).toMatchObject({ type: "integer", minimum: 1, default: 5 });
    expect(kindOf(fps.node, root)).toBe("integer");
    expect(kindOf(resolve(detect.properties?.mode, root).node, root)).toBe("enum");
  });

  it("classifies arrays of primitives as tags and additionalProperties objects as maps", () => {
    const objects = resolve({ $ref: "#/$defs/Objects" }, root).node;
    expect(kindOf(resolve(objects.properties?.track, root).node, root)).toBe("tags");
    expect(kindOf(resolve(objects.properties?.filters, root).node, root)).toBe("map");
  });

  it("falls back to json for unions of several shapes", () => {
    expect(kindOf(resolve({ anyOf: [{ type: "string" }, { type: "integer" }] }, root).node, root)).toBe("json");
  });
});

describe("validateTree", () => {
  it("reports range and enum errors by path", () => {
    const errs = validateTree(root, { $ref: "#/$defs/Detect" }, { fps: 99, mode: "z" }, ["detect"]);
    expect(errs["detect.fps"]).toMatch(/como máximo 30/);
    expect(errs["detect.mode"]).toBeDefined();
    expect(validateTree(root, { $ref: "#/$defs/Detect" }, { fps: 10 }, ["detect"])).toEqual({});
  });
});

describe("diff and patch", () => {
  it("lists leaf changes and sends only changed keys", () => {
    const before = { enabled: true, fps: 5, nested: { a: 1, b: 2 }, list: ["x"] };
    const after = { enabled: true, fps: 8, nested: { a: 1, b: 3 }, list: ["x", "y"] };
    expect(diffValues(before, after, ["detect"]).map((c) => c.path.join("."))).toEqual(["detect.fps", "detect.nested.b", "detect.list"]);
    expect(buildPatch(before, after, () => false)).toEqual({ fps: 8, nested: { b: 3 }, list: ["x", "y"] });
  });

  it("nulls keys removed from maps only", () => {
    const isMap = (p: string[]) => p.join(".") === "filters";
    expect(buildPatch({ filters: { car: { min_area: 1 }, dog: {} }, x: 1 }, { filters: { car: { min_area: 1 } } }, isMap)).toEqual({ filters: { dog: null } });
  });

  it("schemaAt walks properties and map values", () => {
    expect(schemaAt(root, { $ref: "#/$defs/Objects" }, ["filters", "car", "min_area"])).toMatchObject({ type: "integer" });
  });
});

describe("sections, live and secrets", () => {
  it("orders known sections first and hides internal fields", () => {
    expect(orderSections(["zones", "zzz", "detect", "name", "enabled_in_config", "ffmpeg", "aaa"])).toEqual(["ffmpeg", "detect", "zones", "aaa", "zzz"]);
  });

  it("treats everything as restart-required before Frigate 0.17", () => {
    expect(isLiveSection("detect", "0.16.2")).toBe(false);
    expect(isLiveSection("detect", "0.17.0-abc")).toBe(true);
    expect(isLiveSection("ffmpeg", "0.17.0")).toBe(false);
  });

  it("flags credential paths", () => {
    expect(isSecretPath(["ffmpeg", "inputs", 0, "path"])).toBe(true);
    expect(isSecretPath(["onvif", "password"])).toBe(true);
    expect(isSecretPath(["onvif", "host"])).toBe(false);
  });
});
