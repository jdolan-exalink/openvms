import { describe, expect, it } from "vitest";
import { positionAt, trackWindow, trailUntil, zoneEntries, type TrackPath } from "./objectTracks";

const track: TrackPath = {
  path: [
    { x: 0.1, y: 0.5, t: 100 },
    { x: 0.3, y: 0.5, t: 102 },
    { x: 0.5, y: 0.5, t: 104 },
    { x: 0.9, y: 0.5, t: 108 },
  ],
};

describe("trailUntil", () => {
  it("returns the points observed up to the instant", () => {
    expect(trailUntil(track, 103).map((p) => p.t)).toEqual([100, 102]);
    expect(trailUntil(track, 104).map((p) => p.t)).toEqual([100, 102, 104]);
  });
  it("is empty before the first point and complete after the last", () => {
    expect(trailUntil(track, 99)).toEqual([]);
    expect(trailUntil(track, 500)).toHaveLength(4);
  });
});

describe("positionAt", () => {
  it("interpolates linearly between the surrounding points", () => {
    expect(positionAt(track, 101)).toEqual({ x: 0.2, y: 0.5 });
    const p = positionAt(track, 106)!;
    expect(p.x).toBeCloseTo(0.7);
  });
  it("returns the exact point on a sample", () => {
    expect(positionAt(track, 102)).toEqual({ x: 0.3, y: 0.5 });
  });
  it("is null before the first point", () => {
    expect(positionAt(track, 99.9)).toBeNull();
  });
  it("holds the last point for a short grace and then disappears", () => {
    expect(positionAt(track, 109)).toEqual({ x: 0.9, y: 0.5 });
    expect(positionAt(track, 109.6)).toBeNull();
  });
  it("is null for an empty path", () => {
    expect(positionAt({ path: [] }, 100)).toBeNull();
  });
});

describe("zoneEntries", () => {
  const zone = {
    name: "yard",
    points: [
      { x: 0.4, y: 0.4 },
      { x: 0.6, y: 0.4 },
      { x: 0.6, y: 0.6 },
      { x: 0.4, y: 0.6 },
    ],
  };
  it("emits one entry when the path goes from outside to inside", () => {
    expect(zoneEntries(track, [zone])).toEqual([{ zone: "yard", t: 104, x: 0.5, y: 0.5 }]);
  });
  it("counts a first point already inside as an entry at its time", () => {
    const t: TrackPath = { path: [{ x: 0.5, y: 0.5, t: 10 }, { x: 0.52, y: 0.5, t: 11 }] };
    expect(zoneEntries(t, [zone])).toEqual([{ zone: "yard", t: 10, x: 0.5, y: 0.5 }]);
  });
  it("emits again after the object leaves and re-enters", () => {
    const t: TrackPath = {
      path: [
        { x: 0.1, y: 0.1, t: 1 },
        { x: 0.5, y: 0.5, t: 2 },
        { x: 0.9, y: 0.9, t: 3 },
        { x: 0.5, y: 0.5, t: 4 },
      ],
    };
    expect(zoneEntries(t, [zone]).map((e) => e.t)).toEqual([2, 4]);
  });
  it("ignores degenerate zones and orders entries by time", () => {
    const other = { name: "gate", points: [{ x: 0.25, y: 0.4 }, { x: 0.35, y: 0.4 }, { x: 0.35, y: 0.6 }, { x: 0.25, y: 0.6 }] };
    const entries = zoneEntries(track, [zone, other, { name: "bad", points: [{ x: 0, y: 0 }] }]);
    expect(entries.map((e) => [e.zone, e.t])).toEqual([["gate", 102], ["yard", 104]]);
  });
  it("is empty without a path or zones", () => {
    expect(zoneEntries({ path: [] }, [zone])).toEqual([]);
    expect(zoneEntries(track, [])).toEqual([]);
  });
});

describe("trackWindow", () => {
  it("spans the path with the grace after the last point", () => {
    expect(trackWindow(track)).toEqual({ from: 100, to: 109.5 });
  });
  it("is null for an empty path", () => {
    expect(trackWindow({ path: [] })).toBeNull();
  });
});
