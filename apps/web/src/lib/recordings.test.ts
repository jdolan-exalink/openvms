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

describe("clampToNow", () => {
  it("leaves a past instant alone and pulls a future one back to just before now", async () => {
    const { clampToNow } = await import("./recordings");
    expect(clampToNow(1_000, 5_000)).toBe(1_000);
    expect(clampToNow(9_000, 5_000)).toBe(4_995);
    expect(clampToNow(5_000, 5_000)).toBe(4_995);
  });

  it("keeps the window it feeds valid: a click in the future no longer ends before it starts", async () => {
    const { clampToNow, vodWindowForInstant } = await import("./recordings");
    const now = 100_000;
    const day = 80_000;
    const { start, end } = vodWindowForInstant(clampToNow(now + 3 * 3600, now), day, now);
    expect(end).toBeGreaterThan(start);
  });
});

describe("isRecordableCamera", () => {
  it("filters out synthetic birdseye cameras by remote name or display name", async () => {
    const { isRecordableCamera } = await import("./recordings");
    expect(isRecordableCamera({ remote_name: "birdseye", display_name: "BirdEye" })).toBe(false);
    expect(isRecordableCamera({ remote_name: "birdseye", display_name: "Vista general" })).toBe(false);
    expect(isRecordableCamera({ remote_name: "cam_01", display_name: "BirdEye" })).toBe(false);
    expect(isRecordableCamera({ remote_name: "cam_01", display_name: "bireye" })).toBe(false);
  });

  it("keeps standard physical cameras and other cameras", async () => {
    const { isRecordableCamera } = await import("./recordings");
    expect(isRecordableCamera({ remote_name: "cam_01", display_name: "Entrada" })).toBe(true);
    expect(isRecordableCamera({ remote_name: "pajaros", display_name: "Jaula Pájaros" })).toBe(true);
    expect(isRecordableCamera({})).toBe(true);
  });
});
