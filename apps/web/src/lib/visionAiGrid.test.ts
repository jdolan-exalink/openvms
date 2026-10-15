import { describe, expect, it } from "vitest";
import { dynamicGridShape } from "./visionAiGrid";

describe("dynamicGridShape", () => {
  it.each([
    [0, 0, 0],
    [1, 1, 1],
    [2, 2, 1],
    [3, 2, 2],
    [4, 2, 2],
    [5, 3, 2],
    [6, 3, 2],
    [7, 3, 3],
    [9, 3, 3],
    [10, 4, 3],
    [12, 4, 3],
    [13, 4, 4],
    [16, 4, 4],
    [17, 4, 4],
    [40, 4, 4],
  ])("n=%i -> %i cols x %i rows", (n, cols, rows) => {
    expect(dynamicGridShape(n)).toEqual({ cols, rows });
  });

  it("treats negative or non-finite counts as empty", () => {
    expect(dynamicGridShape(-3)).toEqual({ cols: 0, rows: 0 });
    expect(dynamicGridShape(Number.NaN)).toEqual({ cols: 0, rows: 0 });
  });
});
