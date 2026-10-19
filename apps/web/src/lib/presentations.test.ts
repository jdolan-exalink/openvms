import { describe, expect, it } from "vitest";
import { DEFAULT_PRESENTATIONS, divisionName, exportPresentations, gridSegments, importPresentations, loadCatalog, panesCover, presentationForCount, saveCatalog, toggleSegment, uniformPanes, uniformPresentation, type Presentation } from "./presentations";

describe("presentations", () => {
  it("names one pane in the singular", () => {
    expect(divisionName(1)).toBe("1 división");
    expect(divisionName(4)).toBe("4 divisiones");
  });

  it("lists the standard grids first: 1, 3, then the layouts through 8×8, including the 48-camera 8×6", () => {
    expect(DEFAULT_PRESENTATIONS.map((item) => item.name)).toEqual(["1", "3", "2×2", "3×3", "4×4", "5×5", "6×6", "8×6", "8×8"]);
    expect(DEFAULT_PRESENTATIONS.every((item) => item.builtin)).toBe(true);
    const byName = new Map(DEFAULT_PRESENTATIONS.map((item) => [item.name, item]));
    expect(byName.get("1")?.panes).toEqual(uniformPanes(1, 1));
    expect(byName.get("2×2")?.panes).toEqual(uniformPanes(2, 2));
    expect(byName.get("8×8")?.panes).toHaveLength(64);
    expect(byName.get("8×6")).toMatchObject({ columns: 8, rows: 6, panes: uniformPanes(8, 6) });
    expect(byName.get("3")?.panes).toEqual([
      { col: 0, row: 0, colSpan: 1, rowSpan: 1 },
      { col: 1, row: 0, colSpan: 1, rowSpan: 1 },
      { col: 0, row: 1, colSpan: 2, rowSpan: 1 },
    ]);
    for (const item of DEFAULT_PRESENTATIONS) expect(panesCover(item.columns, item.rows, item.panes)).toBe(true);
  });

  it("keeps standards first and appends saved customs", () => {
    const custom = { id: "c1", name: "4 divisiones", columns: 2, rows: 2, panes: uniformPanes(2, 2), builtin: false as const };
    saveCatalog([...DEFAULT_PRESENTATIONS, custom, { ...DEFAULT_PRESENTATIONS[0]!, id: "p1" }]);
    const loaded = loadCatalog();
    expect(loaded.map((item) => item.name)).toEqual(["1", "3", "2×2", "3×3", "4×4", "5×5", "6×6", "8×6", "8×8", "4 divisiones"]);
    expect(loaded.filter((item) => item.builtin)).toHaveLength(9);
    expect(loaded.at(-1)?.builtin).toBe(false);
  });

  it("fits 37 to 48 cameras in the 8×6 videowall instead of jumping to 8×8", () => {
    expect(presentationForCount(DEFAULT_PRESENTATIONS, 37).name).toBe("8×6");
    expect(presentationForCount(DEFAULT_PRESENTATIONS, 48).name).toBe("8×6");
    expect(presentationForCount(DEFAULT_PRESENTATIONS, 49).name).toBe("8×8");
  });

  it("removes a line to merge two cells and restores it by splitting the pane", () => {
    const panes = uniformPanes(2, 2);
    const merged = toggleSegment(panes, { orientation: "v", col: 0, row: 1, solid: true });
    expect(merged).toHaveLength(3);
    const dashed = gridSegments(2, 2, merged).find((segment) => segment.orientation === "v" && segment.row === 1);
    expect(dashed?.solid).toBe(false);
    expect(toggleSegment(merged, dashed!)).toEqual(panes);
  });

  it("refuses a merge that would not stay a rectangle", () => {
    const panes = [
      { col: 0, row: 0, colSpan: 2, rowSpan: 1 },
      { col: 0, row: 1, colSpan: 1, rowSpan: 1 },
      { col: 1, row: 1, colSpan: 1, rowSpan: 1 },
    ];
    expect(toggleSegment(panes, { orientation: "h", col: 0, row: 0, solid: true })).toEqual(panes);
  });

  it("picks the smallest uniform presentation that fits", () => {
    expect(presentationForCount(DEFAULT_PRESENTATIONS, 5).name).toBe("3×3");
    expect(presentationForCount(DEFAULT_PRESENTATIONS, 10).name).toBe("4×4");
    expect(presentationForCount(DEFAULT_PRESENTATIONS, 30).name).toBe("6×6");
    expect(presentationForCount(DEFAULT_PRESENTATIONS, 40).name).toBe("8×6");
  });
});

describe("presentation export and import", () => {
  const custom = (id: string, name: string): Presentation => ({ ...uniformPresentation(2, 2, id), name, builtin: false });

  it("exports only the custom presentations", () => {
    const text = exportPresentations([...DEFAULT_PRESENTATIONS, custom("c-1", "Plaza")]);
    const parsed = JSON.parse(text) as { presentations: Presentation[] };
    expect(parsed.presentations.map((item) => item.id)).toEqual(["c-1"]);
    expect(parsed.presentations[0]).toMatchObject({ name: "Plaza", columns: 2, rows: 2 });
  });

  it("round-trips an export into an empty catalog", () => {
    const text = exportPresentations([custom("c-1", "Plaza"), custom("c-2", "Acceso")]);
    const result = importPresentations(DEFAULT_PRESENTATIONS, text);
    expect(result).toMatchObject({ imported: 2, rejected: 0 });
    expect(result?.catalog.filter((item) => !item.builtin).map((item) => item.name)).toEqual(["Plaza", "Acceso"]);
    expect(result?.catalog.slice(0, DEFAULT_PRESENTATIONS.length)).toEqual(DEFAULT_PRESENTATIONS);
  });

  it("replaces a custom presentation that has the same id", () => {
    const text = exportPresentations([custom("c-1", "Nueva")]);
    const result = importPresentations([...DEFAULT_PRESENTATIONS, custom("c-1", "Vieja"), custom("c-9", "Otra")], text);
    expect(result?.imported).toBe(1);
    expect(result?.catalog.filter((item) => !item.builtin).map((item) => item.name)).toEqual(["Nueva", "Otra"]);
  });

  it("rejects items that collide with a standard id or do not cover the grid, and keeps the valid ones", () => {
    const text = JSON.stringify({
      format: "openvms.presentations",
      version: 1,
      presentations: [
        { ...custom("p1", "Pisa estándar") },
        { id: "bad", name: "Rota", columns: 2, rows: 2, panes: [{ col: 0, row: 0, colSpan: 1, rowSpan: 1 }] },
        42,
        custom("ok", "Buena"),
      ],
    });
    const result = importPresentations(DEFAULT_PRESENTATIONS, text);
    expect(result).toMatchObject({ imported: 1, rejected: 3 });
    expect(result?.catalog.filter((item) => !item.builtin).map((item) => item.id)).toEqual(["ok"]);
  });

  it("accepts a bare array and returns null for text that is not a presentations file", () => {
    expect(importPresentations(DEFAULT_PRESENTATIONS, JSON.stringify([custom("c-1", "Plaza")]))?.imported).toBe(1);
    expect(importPresentations(DEFAULT_PRESENTATIONS, "not json")).toBeNull();
    expect(importPresentations(DEFAULT_PRESENTATIONS, JSON.stringify({ hello: 1 }))).toBeNull();
  });
});
