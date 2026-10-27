import { describe, expect, it } from "vitest";
import { draftErrors, draftToPatch, emptyDraft, leafCount, patchToDraft, sectionsOf, summarizePatch } from "./patchForm";

describe("patchForm", () => {
  it("round-trips a profile patch and keeps fields the form does not edit", () => {
    const patch = { detection: { enabled: true, objects: ["person", "car"] }, recording: { enabled: false }, streams: { primary: { fps: 15, codec: "h265", resolution: { width: 1920, height: 1080 }, extra: 1 } }, analytics: { modules: { lpr: { enabled: true } } } };
    const d = patchToDraft(patch);
    expect(d.detectionObjects).toBe("person, car");
    expect(d.primary.resolution).toBe("1920x1080");
    expect(draftToPatch(d)).toEqual(patch);
  });

  it("drops a field that was blanked", () => {
    const d = patchToDraft({ recording: { enabled: true }, detection: { enabled: true } });
    d.recordingEnabled = "";
    expect(draftToPatch(d)).toEqual({ detection: { enabled: true } });
  });

  it("omits blank fields and builds typed values", () => {
    const d = emptyDraft();
    d.lpr = "on";
    d.detection.fps = "7";
    d.detection.resolution = "1280x720";
    expect(draftToPatch(d)).toEqual({ analytics: { lpr: true }, streams: { detection: { fps: 7, resolution: { width: 1280, height: 720 } } } });
  });

  it("keeps exactly one section when asked", () => {
    const d = emptyDraft();
    d.detectionEnabled = "on";
    d.recordingEnabled = "on";
    expect(draftToPatch(d, "detection")).toEqual({ detection: { enabled: true } });
    expect(draftToPatch(d, "analytics")).toEqual({});
  });

  it("reports invalid stream values", () => {
    const d = emptyDraft();
    d.primary.fps = "abc";
    d.primary.resolution = "grande";
    expect(Object.keys(draftErrors(d)).sort()).toEqual(["primary.fps", "primary.resolution"]);
  });

  it("counts, lists and summarizes", () => {
    const p = { detection: { enabled: true, objects: ["car"] }, streams: { primary: { fps: 5 } } };
    expect(leafCount(p)).toBe(3);
    expect(sectionsOf(p)).toEqual(["detection", "streams"]);
    expect(summarizePatch(p)).toBe("Detección activada · Objetos: car · Video: 1 valor");
  });
});
