import { describe, expect, it } from "vitest";
import { canPressEnd, canPressStart, escapeMarks, exportRange, pressEnd, pressStart, shownMarks } from "./exportMarks";

describe("export marks", () => {
  it("needs a fixed start before the end can be pressed, and never lets both be placed at once", () => {
    expect(canPressEnd({})).toBe(false);
    expect(canPressEnd({ placing: "start" })).toBe(false);
    expect(canPressEnd({ start: 100 })).toBe(true);
    expect(canPressStart({ start: 100, placing: "end" })).toBe(false);
    expect(canPressStart({})).toBe(true);
    expect(pressEnd({}, 500).marks).toEqual({});
    expect(pressStart({ start: 100, placing: "end" }, 500).marks).toEqual({ start: 100, placing: "end" });
  });

  it("places the start in two presses: the first follows the cursor, the second fixes it where the cursor is", () => {
    const first = pressStart({}, 100);
    expect(first.marks).toEqual({ placing: "start" });
    expect(shownMarks(first.marks, 250)).toEqual({ start: 250, end: undefined });
    expect(pressStart(first.marks, 300).marks).toEqual({ start: 300, end: undefined });
  });

  it("places the end the same way, after the start is fixed", () => {
    let m = pressStart(pressStart({}, 0).marks, 100).marks;
    m = pressEnd(m, 150).marks;
    expect(m).toEqual({ start: 100, placing: "end" });
    expect(shownMarks(m, 400)).toEqual({ start: 100, end: 400 });
    m = pressEnd(m, 400).marks;
    expect(m).toEqual({ start: 100, end: 400 });
    expect(exportRange(m)).toEqual({ start: 100, end: 400 });
  });

  it("only exports with both marks fixed and nothing being placed", () => {
    expect(exportRange({ start: 100 })).toBeUndefined();
    expect(exportRange({ start: 100, end: 200, placing: "start" })).toBeUndefined();
    expect(exportRange({ start: 100, end: 200, placing: "end" })).toBeUndefined();
    expect(exportRange({ start: 200, end: 100 })).toBeUndefined();
    expect(exportRange({ start: 100, end: 200 })).toEqual({ start: 100, end: 200 });
  });

  it("refuses to fix an end at or before the start, or a start at or after the end, and keeps placing", () => {
    const placingEnd = { start: 100, placing: "end" as const };
    expect(pressEnd(placingEnd, 100)).toEqual({ marks: placingEnd, refused: "end_before_start" });
    expect(pressEnd(placingEnd, 50)).toEqual({ marks: placingEnd, refused: "end_before_start" });
    const placingStart = { start: 100, end: 200, placing: "start" as const };
    expect(pressStart(placingStart, 200)).toEqual({ marks: placingStart, refused: "start_after_end" });
    expect(pressStart(placingStart, 150).marks).toEqual({ start: 150, end: 200 });
  });

  it("lets a fixed mark be moved again without losing the other one", () => {
    const ready = { start: 100, end: 200 };
    const moving = pressStart(ready, 0).marks;
    expect(moving).toEqual({ start: 100, end: 200, placing: "start" });
    expect(exportRange(moving)).toBeUndefined();
    expect(pressStart(moving, 120).marks).toEqual({ start: 120, end: 200 });
  });

  it("Escape stops placing and keeps the last fixed value; with nothing placing it clears both", () => {
    expect(escapeMarks({ start: 100, end: 200, placing: "start" })).toEqual({ start: 100, end: 200 });
    expect(escapeMarks({ placing: "start" })).toEqual({});
    expect(escapeMarks({ start: 100, end: 200 })).toEqual({});
  });
});
