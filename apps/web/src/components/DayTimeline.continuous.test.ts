import { describe, expect, it } from "vitest";
import { CONTINUOUS_MIN_S, isContinuous } from "./DayTimeline";

describe("isContinuous", () => {
  it("takes a long recorded stretch as continuous recording and a short one as motion-only", () => {
    expect(isContinuous({ start: 1000, end: 1000 + 35 })).toBe(false);
    expect(isContinuous({ start: 1000, end: 1000 + CONTINUOUS_MIN_S - 1 })).toBe(false);
    expect(isContinuous({ start: 1000, end: 1000 + CONTINUOUS_MIN_S })).toBe(true);
    expect(isContinuous({ start: 0, end: 6 * 3600 })).toBe(true);
  });
});
