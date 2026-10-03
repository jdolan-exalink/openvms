import { describe, expect, it } from "vitest";
import { DEFAULT_PRESENTATIONS, divisionName, gridSegments, loadCatalog, panesCover, presentationForCount, saveCatalog, toggleSegment, uniformPanes } from "./presentations";

describe("presentations", () => {
  it("names one pane in the singular", () => {
    expect(divisionName(1)).toBe("1 división");
    expect(divisionName(4)).toBe("4 divisiones");
  });

  it("lists the standard grids first: 1, 3, then the square layouts through 8×8", () => {
    expect(DEFAULT_PRESENTATIONS.map((item) => item.name)).toEqual(["1", "3", "2×2", "3×3", "4×4", "5×5", "6×6", "8×8"]);
    expect(DEFAULT_PRESENTATIONS.every((item) => item.builtin)).toBe(true);
    const byName = new Map(DEFAULT_PRESENTATIONS.map((item) => [item.name, item]));
    expect(byName.get("1")?.panes).toEqual(uniformPanes(1, 1));
    expect(byName.get("2×2")?.panes).toEqual(uniformPanes(2, 2));
    expect(byName.get("8×8")?.panes).toHaveLength(64);
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
    expect(loaded.map((item) => item.name)).toEqual(["1", "3", "2×2", "3×3", "4×4", "5×5", "6×6", "8×8", "4 divisiones"]);
    expect(loaded.filter((item) => item.builtin)).toHaveLength(8);
    expect(loaded.at(-1)?.builtin).toBe(false);
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
    expect(presentationForCount(DEFAULT_PRESENTATIONS, 40).name).toBe("8×8");
  });
});
