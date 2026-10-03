import { describe, expect, it } from "vitest";
import { detectionNames, fmtWatermarkTimestamp } from "./format";

describe("detectionNames", () => {
  it("collapses verified copies and hides the plate label", () => {
    expect(detectionNames(["motorcycle", "motorcycle-verified", "car", "car-verified", "license_plate"])).toEqual(["Moto", "Auto"]);
  });
});

describe("fmtWatermarkTimestamp", () => {
  it("renders UTC with an explicit +00:00 offset", () => {
    expect(fmtWatermarkTimestamp("2026-09-28T16:05:30Z", "UTC")).toBe("2026-09-28 16:05:30 +00:00");
  });

  // America/Argentina/Buenos_Aires has been a fixed -03:00 with no DST since 2009 — PDW-7's
  // own documented default zone.
  it("renders a fixed non-UTC offset (Buenos Aires, no DST)", () => {
    expect(fmtWatermarkTimestamp("2026-09-28T16:05:30Z", "America/Argentina/Buenos_Aires")).toBe(
      "2026-09-28 13:05:30 -03:00",
    );
  });

  // PDW-7's own explicit "prove the offset is computed, not hardcoded" requirement: the same
  // zone must render a different numeric offset in winter (CET, +01:00) vs. summer (CEST,
  // +02:00), which only happens if the offset is actually derived from the IANA tz database
  // for that specific instant, not a static string.
  it("computes the real seasonal offset for a DST zone (Europe/Madrid)", () => {
    expect(fmtWatermarkTimestamp("2026-01-15T12:00:00Z", "Europe/Madrid")).toBe("2026-01-15 13:00:00 +01:00");
    expect(fmtWatermarkTimestamp("2026-07-15T12:00:00Z", "Europe/Madrid")).toBe("2026-07-15 14:00:00 +02:00");
  });

  it("matches Go's watermark.Text format exactly for the same instant/zone/owner-less case", () => {
    // internal/watermark/photo_test.go's TestText "Buenos Aires (no DST)" case (owner name
    // aside — the date/time-only prefix is the part that must match byte for byte).
    const got = fmtWatermarkTimestamp("2026-09-28T16:05:30Z", "America/Argentina/Buenos_Aires");
    expect(got).toBe("2026-09-28 13:05:30 -03:00");
  });
});
