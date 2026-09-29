import { describe, expect, it } from "vitest";
import { MAX_VOD_WINDOW_SECONDS, vodWindowForInstant } from "./recordings";

describe("vodWindowForInstant", () => {
  it("keeps a selected VOD request within the existing one-hour maximum", () => {
    const dayStart = 1_750_000_000;
    const window = vodWindowForInstant(dayStart + 7_200, dayStart, dayStart + 20_000);

    expect(window.start).toBe(dayStart + 7_140);
    expect(window.end - window.start).toBe(MAX_VOD_WINDOW_SECONDS);
  });

  it("caps the range at the current time near the live edge", () => {
    const dayStart = 1_750_000_000;
    const window = vodWindowForInstant(dayStart + 550, dayStart, dayStart + 600);

    expect(window).toEqual({ start: dayStart + 490, end: dayStart + 600 });
  });
});
