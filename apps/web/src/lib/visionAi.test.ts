import { describe, expect, it } from "vitest";
import { detectionAgeSeconds, formatDetectionAge } from "./visionAi";

describe("detectionAgeSeconds", () => {
  const now = Date.parse("2026-10-21T10:00:30Z");

  it("counts whole seconds since the detection", () => {
    expect(detectionAgeSeconds("2026-10-21T10:00:00Z", now)).toBe(30);
    expect(detectionAgeSeconds("2026-10-21T10:00:29.400Z", now)).toBe(0);
  });

  it("never goes negative and ignores missing or invalid times", () => {
    expect(detectionAgeSeconds("2026-10-21T10:01:00Z", now)).toBe(0);
    expect(detectionAgeSeconds(undefined, now)).toBeNull();
    expect(detectionAgeSeconds("nope", now)).toBeNull();
  });
});

describe("formatDetectionAge", () => {
  it.each([
    [0, "hace 0s"],
    [9, "hace 9s"],
    [59, "hace 59s"],
    [60, "hace 1m 00s"],
    [125, "hace 2m 05s"],
    [3600, "hace 60m 00s"],
  ])("%is -> %s", (seconds, label) => {
    expect(formatDetectionAge(seconds)).toBe(label);
  });
});
