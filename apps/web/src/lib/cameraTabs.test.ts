import { describe, expect, it } from "vitest";
import { ANALYTICS_MODULES, analyticsTabVisible, buildCameraTabs, CAMERA_WEB, FISHEYE, GENERAL, HISTORY, isFisheyeTagged, ONVIF_INFO, PROVISION, PTZ_CONTROL, tabOfSection } from "./cameraTabs";

const base = { sections: [], ptz: false, fisheye: false, analytics: false, provision: false, cameraWeb: false };
const ids = (input: Partial<Parameters<typeof buildCameraTabs>[0]>) => buildCameraTabs({ ...base, ...input }).map((t) => t.id);

describe("buildCameraTabs", () => {
  it("always lists the fixed tabs in PRD order and starts with General", () => {
    const tabs = buildCameraTabs(base);
    expect(tabs.map((t) => t.label)).toEqual(["General", "Video", "Grabación", "Detección", "Zonas", "Integraciones", "Avanzado"]);
    expect(tabs[0]?.sections).toEqual([GENERAL]);
  });

  it("shows Analíticas, PTZ and Fisheye only when the camera has them", () => {
    expect(ids({})).not.toContain("analytics");
    expect(ids({})).not.toContain("ptz");
    expect(ids({})).not.toContain("fisheye");
    expect(ids({ analytics: true, ptz: true, fisheye: true })).toEqual([
      "general", "video", "record", "detect", "analytics", "zones", "ptz", "fisheye", "integrations", "advanced",
    ]);
  });

  it("maps the Frigate sections to their tab", () => {
    const tabs = buildCameraTabs({ ...base, analytics: true, provision: true, sections: ["ffmpeg", "live", "lpr", "face_recognition", "detect", "objects", "motion", "record", "snapshots", "zones", "review"] });
    const of = (id: string) => tabs.find((t) => t.id === id)!.sections;
    expect(of("video")).toEqual(["ffmpeg", "live"]);
    expect(of("record")).toEqual(["record", "snapshots"]);
    expect(of("detect")).toEqual(["detect", "objects", "motion"]);
    expect(of("analytics")).toEqual([ANALYTICS_MODULES, PROVISION, "lpr", "face_recognition"]);
    expect(of("zones")).toEqual(["zones", "review"]);
  });

  it("shows Analíticas by the modules list: only with an available module (or Provision / ONVIF analytics)", () => {
    const none = { modules: [{ available: false }, { available: false }], provision: false, advertised: false, lpr: false };
    expect(analyticsTabVisible(none)).toBe(false);
    expect(analyticsTabVisible({ ...none, modules: undefined })).toBe(false);
    expect(analyticsTabVisible({ ...none, modules: [{ available: false }, { available: true }] })).toBe(true);
    expect(analyticsTabVisible({ ...none, provision: true })).toBe(true);
    expect(analyticsTabVisible({ ...none, advertised: true })).toBe(true);
  });

  it("gates the camera web view and keeps the ONVIF information in Integraciones", () => {
    expect(buildCameraTabs(base).find((t) => t.id === "integrations")!.sections).toEqual([ONVIF_INFO]);
    expect(buildCameraTabs({ ...base, cameraWeb: true }).find((t) => t.id === "integrations")!.sections).toEqual([CAMERA_WEB, ONVIF_INFO]);
  });

  it("sends every remaining section, Fisheye for non-fisheye cameras and the history to Avanzado", () => {
    const tabs = buildCameraTabs({ ...base, sections: ["detect", "onvif", "genai", "audio", "birdseye_x"], generalSections: ["audio"] });
    expect(tabs.find((t) => t.id === "advanced")!.sections).toEqual(["onvif", "genai", "birdseye_x", FISHEYE, HISTORY]);
  });

  it("moves Fisheye out of Avanzado when the camera is fisheye", () => {
    const tabs = buildCameraTabs({ ...base, fisheye: true });
    expect(tabs.find((t) => t.id === "advanced")!.sections).toEqual([HISTORY]);
    expect(tabs.find((t) => t.id === "fisheye")!.sections).toEqual([FISHEYE]);
  });

  it("gives the PTZ tab its control panel and autotracking", () => {
    const tab = buildCameraTabs({ ...base, ptz: true, sections: ["autotracking"] }).find((t) => t.id === "ptz")!;
    expect(tab.sections).toEqual([PTZ_CONTROL, "autotracking"]);
  });

  it("never loses a section: every known one is owned by a tab or by General", () => {
    const sections = ["ffmpeg", "live", "detect", "objects", "motion", "record", "snapshots", "zones", "review", "onvif", "autotracking", "lpr", "genai", "mqtt"];
    const tabs = buildCameraTabs({ ...base, sections, generalSections: ["lpr"] });
    for (const s of sections) expect(tabOfSection(tabs, s) ?? (s === "lpr" ? "general" : undefined), s).toBeDefined();
  });
});

describe("tabOfSection", () => {
  it("returns undefined for a section nobody owns", () => {
    expect(tabOfSection(buildCameraTabs(base), "nope")).toBeUndefined();
  });
});

describe("isFisheyeTagged", () => {
  it("recognizes both tags", () => {
    expect(isFisheyeTagged(["fisheye"])).toBe(true);
    expect(isFisheyeTagged(["ojo_de_pez"])).toBe(true);
    expect(isFisheyeTagged(["ptz"])).toBe(false);
    expect(isFisheyeTagged(undefined)).toBe(false);
  });
});
