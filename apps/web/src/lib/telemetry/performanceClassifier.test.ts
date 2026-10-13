import { describe, expect, it } from "vitest";
import { classifyPerformance, EXPECTED_FPS, type ClassifierSample } from "./performanceClassifier";

const ok = (over: Partial<ClassifierSample> = {}): ClassifierSample => ({
  fps: EXPECTED_FPS,
  droppedRatio: 0,
  playing: 10,
  connecting: 0,
  error: 0,
  total: 10,
  ...over,
});

describe("classifyPerformance", () => {
  it("is optimal with no streams or a healthy window", () => {
    expect(classifyPerformance([])).toBe("optimal");
    expect(classifyPerformance([ok({ playing: 0, total: 0, fps: 0 })])).toBe("optimal");
    expect(classifyPerformance([ok(), ok(), ok()])).toBe("optimal");
  });

  it.each([
    [0.02, "optimal"],
    [0.05, "acceptable"],
    [0.12, "limit"],
    [0.3, "saturated"],
  ] as const)("maps dropped ratio %f to %s", (droppedRatio, expected) => {
    expect(classifyPerformance([ok({ droppedRatio })])).toBe(expected);
  });

  it.each([
    [EXPECTED_FPS * 0.9, "optimal"],
    [EXPECTED_FPS * 0.7, "acceptable"],
    [EXPECTED_FPS * 0.5, "limit"],
    [EXPECTED_FPS * 0.2, "saturated"],
  ] as const)("maps presented fps %f to %s", (fps, expected) => {
    expect(classifyPerformance([ok({ fps })])).toBe(expected);
  });

  it("degrades with a share of errored streams", () => {
    expect(classifyPerformance([ok({ error: 3 })])).toBe("limit");
    expect(classifyPerformance([ok({ error: 6 })])).toBe("saturated");
  });

  it("treats mass connecting as acceptable only", () => {
    expect(classifyPerformance([ok({ connecting: 8, playing: 2 })])).toBe("acceptable");
  });

  it("uses the worst signal and averages only the recent window", () => {
    const old = Array.from({ length: 30 }, () => ok({ droppedRatio: 0.5 }));
    const recent = Array.from({ length: 5 }, () => ok());
    expect(classifyPerformance([...old, ...recent])).toBe("optimal");
    expect(classifyPerformance([ok(), ok({ droppedRatio: 0.3 })])).toBe("limit");
  });
});
