import { describe, expect, it } from "vitest";
import { isH265, streamCodec } from "./cameraCodec";

describe("isH265", () => {
  it("recognizes both spellings, ignoring case", () => {
    for (const codec of ["h265", "H265", "hevc", "HEVC", " h265 "]) expect(isH265(codec)).toBe(true);
  });
  it("is false for anything else, including an unknown codec", () => {
    for (const codec of ["h264", "jpeg", "", null, undefined]) expect(isH265(codec)).toBe(false);
  });
});

describe("streamCodec", () => {
  const camera = { live_codec: "h264", hq_codec: "h265" };
  it("follows the quality the tile is playing", () => {
    expect(streamCodec(camera, "sub")).toBe("h264");
    expect(streamCodec(camera, "main")).toBe("h265");
  });
  it("is undefined when the camera or its codec is unknown", () => {
    expect(streamCodec(undefined, "sub")).toBeUndefined();
    expect(streamCodec({}, "main")).toBeUndefined();
    expect(streamCodec({ live_codec: null, hq_codec: null }, "sub")).toBeUndefined();
  });
});
