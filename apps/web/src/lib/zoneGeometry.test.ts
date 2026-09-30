import { describe, expect, it } from "vitest";
import {
  hasSelfIntersection,
  hitEdge,
  hitVertex,
  insertVertex,
  parseCoordinates,
  parseMasks,
  pointInPolygon,
  polygonArea,
  removeVertex,
  serializeCoordinates,
  serializeMasks,
  translatePolygon,
  usesDictMasks,
} from "./zoneGeometry";

const square = [
  { x: 0.1, y: 0.1 },
  { x: 0.9, y: 0.1 },
  { x: 0.9, y: 0.9 },
  { x: 0.1, y: 0.9 },
];

describe("coordinates", () => {
  it("round-trips relative strings with 3 decimals", () => {
    expect(serializeCoordinates(parseCoordinates("0.1234,0.5,0.9,0.75,0.3,1.0"))).toBe("0.123,0.5,0.9,0.75,0.3,1");
  });
  it("converts legacy absolute pixels using the frame size", () => {
    expect(parseCoordinates("640,360,1280,720,0,720", { width: 1280, height: 720 })).toEqual([
      { x: 0.5, y: 0.5 },
      { x: 1, y: 1 },
      { x: 0, y: 1 },
    ]);
  });
});

describe("geometry", () => {
  it("computes area, containment and self-intersection", () => {
    expect(polygonArea(square)).toBeCloseTo(0.64);
    expect(pointInPolygon({ x: 0.5, y: 0.5 }, square)).toBe(true);
    expect(pointInPolygon({ x: 0.05, y: 0.5 }, square)).toBe(false);
    expect(hasSelfIntersection(square)).toBe(false);
    const bowtie = [square[0]!, square[2]!, square[1]!, square[3]!];
    expect(hasSelfIntersection(bowtie)).toBe(true);
  });
  it("hit-tests vertices and edges and edits vertices", () => {
    const size = { width: 1000, height: 500 };
    expect(hitVertex(square, { x: 0.102, y: 0.1 }, 10, size)).toBe(0);
    const e = hitEdge(square, { x: 0.5, y: 0.11 }, 10, size);
    expect(e?.index).toBe(0);
    expect(e?.point.y).toBeCloseTo(0.1);
    expect(insertVertex(square, 0, e!.point)).toHaveLength(5);
    expect(removeVertex(square.slice(0, 3), 0)).toHaveLength(3);
    const moved = translatePolygon(square, 0.5, 0);
    expect(Math.max(...moved.map((p) => p.x))).toBeCloseTo(1);
  });
});

describe("mask formats", () => {
  const coords = "0.1,0.1,0.9,0.1,0.9,0.9";
  it("preserves string and list formats", () => {
    expect(serializeMasks(parseMasks(coords))).toBe(coords);
    expect(serializeMasks(parseMasks([coords, coords]))).toEqual([coords, coords]);
    expect(serializeMasks(parseMasks(""))).toBe("");
  });
  it("preserves 0.18 dicts including extra keys", () => {
    const list = [{ id: "a", friendly_name: "Cielo", enabled: false, coordinates: coords, foo: 1 }];
    expect(serializeMasks(parseMasks(list))).toEqual(list);
    const map = { a: { friendly_name: "Cielo", enabled: true, coordinates: coords } };
    expect(serializeMasks(parseMasks(map))).toEqual(map);
  });
  it("picks the native format when the field is missing", () => {
    expect(usesDictMasks("0.18.0-abc")).toBe(true);
    expect(parseMasks(undefined, undefined, "0.16.1").format).toBe("list");
    expect(parseMasks(undefined, undefined, "0.18.0").format).toBe("dict-map");
  });
});
