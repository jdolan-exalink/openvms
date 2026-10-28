import { describe, expect, it } from "vitest";
import { describeStreamError, streamError } from "./streamErrors";

describe("describeStreamError", () => {
  it("says a recorder's video is not available yet, and does not retry", () => {
    const err = describeStreamError({ code: "video_unavailable" });
    expect(err.label).toBe("El video de este NVR todavía no está disponible en OpenVMS");
    expect(err.retryable).toBe(false);
    expect(streamError("video_unavailable")).toBe(err);
  });

  it("keeps treating an unknown code as a retryable stream error", () => {
    expect(describeStreamError({ code: "something_new" }).retryable).toBe(true);
  });

  it("still maps the Frigate codes as before", () => {
    expect(describeStreamError({ code: "upstream_unreachable" }).label).toBe("Servidor no disponible");
    expect(describeStreamError({ code: "camera_no_signal" }).retryDelayMs).toBe(30_000);
  });
});
