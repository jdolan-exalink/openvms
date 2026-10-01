import { describe, expect, it } from "vitest";
import { generatePerfEntities } from "./perfFixture";

describe("generatePerfEntities", () => {
  it("generates exactly the requested cameras on one site with unique ids", () => {
    const entities = generatePerfEntities(5000);
    expect(entities).toHaveLength(5000);
    expect(new Set(entities.map(e => e.id)).size).toBe(5000);
    expect(entities.every(e => e.t === "camera" && e.site === "perf-site")).toBe(true);
  });

  it("is deterministic for a seed and varies when the seed changes", () => {
    const a = generatePerfEntities(50, { seed: 7 });
    const b = generatePerfEntities(50, { seed: 7 });
    const c = generatePerfEntities(50, { seed: 8 });
    expect(a).toEqual(b);
    expect(a).not.toEqual(c);
  });

  it("spreads every camera inside the bbox around the centre", () => {
    const entities = generatePerfEntities(200, {
      center: { lat: -34.6, lng: -58.4 },
      spreadDeg: 0.05,
    });
    for (const e of entities) {
      expect(e.pos.k).toBe("geo");
      const pos = e.pos as { k: "geo"; lat: number; lng: number };
      expect(Math.abs(pos.lat - -34.6)).toBeLessThanOrEqual(0.05);
      expect(Math.abs(pos.lng - -58.4)).toBeLessThanOrEqual(0.05);
    }
  });

  it("mixes camera statuses like a real site", () => {
    const entities = generatePerfEntities(500);
    expect(new Set(entities.map(e => e.st))).toEqual(new Set(["online", "offline", "degraded"]));
  });

  it("carries the camera props the canvas FOV layer needs", () => {
    const [entity] = generatePerfEntities(1);
    expect(entity!.name).toContain("Perf");
    expect(entity!.cam).toMatchObject({ fov: expect.any(Number), range: expect.any(Number), type: "fixed", ptz: false, lpr: false });
    expect(entity!.rev).toBe(1);
  });
});
