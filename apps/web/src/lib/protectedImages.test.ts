import { describe, expect, it } from "vitest";
import { protectedMedia } from "./protectedImages";

describe("protectedMedia", () => {
  it("recovers the full frame and the clip from an older plate copy", () => {
    expect(protectedMedia({ id: "plate:r1", kind: "plate" })).toEqual({
      fullUrl: "/media/v1/lpr/reads/r1/snapshot.jpg",
      clipUrl: "/media/v1/lpr/reads/r1/clip.mp4",
    });
  });

  it("keeps the urls stored with the copy", () => {
    expect(protectedMedia({ id: "plate:r1", kind: "plate", fullUrl: "/full.jpg", clipUrl: "/clip.mp4" })).toEqual({
      fullUrl: "/full.jpg",
      clipUrl: "/clip.mp4",
    });
  });

  it("has no clip for an alarm", () => {
    expect(protectedMedia({ id: "alarm:a1", kind: "alarm" })).toEqual({});
  });
});
