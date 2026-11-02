import { describe, expect, it } from "vitest";
import { audioNeedsInput, withAudioRole } from "@/lib/frigateAudio";

const input = (...roles: string[]) => ({ path: "rtsp://x", roles });

describe("audioNeedsInput", () => {
  it("flags audio detection with no audio stream", () => {
    expect(audioNeedsInput({ enabled: true }, { inputs: [input("detect", "record")] })).toBe(true);
  });
  it("accepts audio detection when any stream carries the role", () => {
    expect(audioNeedsInput({ enabled: true }, { inputs: [input("detect"), input("audio")] })).toBe(false);
  });
  it("ignores cameras with audio detection off or unset", () => {
    expect(audioNeedsInput({ enabled: false }, { inputs: [input("detect")] })).toBe(false);
    expect(audioNeedsInput(undefined, { inputs: [input("detect")] })).toBe(false);
  });
  it("leaves a camera with no listed inputs alone", () => {
    expect(audioNeedsInput({ enabled: true }, undefined)).toBe(false);
    expect(audioNeedsInput({ enabled: true }, { inputs: [] })).toBe(false);
  });
});

describe("withAudioRole", () => {
  it("adds the role to the only stream and keeps its other roles", () => {
    expect(withAudioRole({ inputs: [input("detect", "record")] })).toEqual({ inputs: [input("detect", "record", "audio")] });
  });
  it("does not guess among several streams", () => {
    const two = { inputs: [input("detect"), input("record")] };
    expect(withAudioRole(two)).toBe(two);
  });
  it("is a no-op when the stream already has the role", () => {
    const one = { inputs: [input("audio")] };
    expect(withAudioRole(one)).toBe(one);
  });
});
