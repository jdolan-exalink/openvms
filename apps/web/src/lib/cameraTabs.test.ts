import { describe, expect, it } from "vitest";
import { ADVANCED_PANELS, ANALYTICS_MODULES, analyticsTabVisible, buildCameraTabs, CAMERA_WEB, FISHEYE, GENERAL, groupCameraTabs, HISTORY, INTEGRATIONS_STATUS, isFisheyeTagged, LEGACY_TAB_IDS, ONVIF_INFO, PROVISION, PTZ_CONTROL, tabById, tabOfSection } from "./cameraTabs";

const base = { sections: [], ptz: false, fisheye: false, analytics: false, provision: false, cameraWeb: false };
const build = (input: Partial<Parameters<typeof buildCameraTabs>[0]>) => buildCameraTabs({ ...base, ...input });
const ids = (input: Partial<Parameters<typeof buildCameraTabs>[0]>) => build(input).map((t) => t.id);
const sectionsOf = (input: Partial<Parameters<typeof buildCameraTabs>[0]>, id: string) => build(input).find((t) => t.id === id)!.sections;

// Everything the schema of a full Frigate camera exposes, plus a few unknown ones.
const FULL = ["enabled", "ffmpeg", "detect", "objects", "motion", "record", "snapshots", "zones", "review", "audio", "lpr", "face_recognition", "semantic_search", "onvif", "live", "ui", "timestamp_style", "mqtt", "notifications", "genai", "type", "webui_url", "ffmpeg_cmds", "profiles", "autotracking", "friendly_name", "birdseye_x"];

describe("buildCameraTabs", () => {
  it("always lists the fixed tabs starting with Resumen", () => {
    const tabs = build({});
    expect(tabs.map((t) => t.label)).toEqual(["Resumen", "Video", "Grabación", "Detección y zonas", "Conexión", "Avanzado"]);
    expect(tabs[0]?.sections).toEqual([GENERAL]);
  });

  it("groups the navigation as Cámara, Inteligencia, Control and Sistema, in that order", () => {
    const groups = groupCameraTabs(build({ analytics: true, ptz: true }));
    expect(groups.map((g) => [g.label, g.tabs.map((t) => t.label)])).toEqual([
      ["Cámara", ["Resumen", "Video", "Grabación"]],
      ["Inteligencia", ["Detección y zonas", "Analíticas OpenVMS"]],
      ["Control", ["PTZ y fisheye"]],
      ["Sistema", ["Conexión", "Avanzado"]],
    ]);
  });

  it("drops the Control heading when the camera has neither PTZ nor fisheye", () => {
    expect(groupCameraTabs(build({})).map((g) => g.label)).toEqual(["Cámara", "Inteligencia", "Sistema"]);
  });

  it("shows Analíticas OpenVMS and PTZ y fisheye only when the camera has them", () => {
    expect(ids({})).not.toContain("analytics");
    expect(ids({})).not.toContain("ptz");
    expect(ids({ analytics: true })).toContain("analytics");
    expect(ids({ ptz: true })).toContain("ptz");
    expect(ids({ fisheye: true })).toContain("ptz");
    expect(ids({ analytics: true, ptz: true, fisheye: true })).toEqual(["summary", "video", "record", "detect", "analytics", "ptz", "connection", "advanced"]);
  });

  it("maps the Frigate sections to their tab", () => {
    const input = { analytics: true, provision: true, sections: FULL };
    expect(sectionsOf(input, "video")).toEqual(["ffmpeg", "live"]);
    expect(sectionsOf(input, "record")).toEqual(["record", "snapshots"]);
    expect(sectionsOf(input, "detect")).toEqual(["detect", "objects", "motion", "zones", "review"]);
    expect(sectionsOf(input, "analytics")).toEqual([ANALYTICS_MODULES, PROVISION, "lpr", "face_recognition", "semantic_search"]);
    expect(sectionsOf(input, "connection")).toEqual([ONVIF_INFO, "onvif", "webui_url"]);
  });

  it("puts the camera web view in Conexión, gated by permission", () => {
    expect(sectionsOf({}, "connection")).not.toContain(CAMERA_WEB);
    expect(sectionsOf({ cameraWeb: true }, "connection")).toEqual([ONVIF_INFO, CAMERA_WEB]);
    expect(sectionsOf({ cameraWeb: true }, "advanced")).not.toContain(CAMERA_WEB);
  });

  it("leaves in Avanzado the integrations, ffmpeg commands, interface, profiles, the engine panels and the history", () => {
    const advanced = sectionsOf({ sections: ["mqtt", "notifications", "ffmpeg_cmds", "ui", "profiles"] }, "advanced");
    expect(advanced).toEqual(["mqtt", "notifications", "ffmpeg_cmds", "ui", "profiles", ADVANCED_PANELS, FISHEYE, HISTORY]);
  });

  it("sends every remaining section to Avanzado and keeps the ones General edits out of it", () => {
    const advanced = sectionsOf({ sections: ["detect", "onvif", "genai", "audio", "birdseye_x"], generalSections: ["audio"] }, "advanced");
    expect(advanced).toEqual([ADVANCED_PANELS, "genai", "birdseye_x", FISHEYE, HISTORY]);
  });

  it("keeps Fisheye in one place: PTZ y fisheye for fisheye cameras, Avanzado for the rest", () => {
    const fisheye = build({ fisheye: true });
    expect(fisheye.find((t) => t.id === "ptz")!.sections).toEqual([FISHEYE]);
    expect(fisheye.find((t) => t.id === "advanced")!.sections).not.toContain(FISHEYE);
    expect(sectionsOf({ ptz: true }, "ptz")).not.toContain(FISHEYE);
    expect(sectionsOf({ ptz: true }, "advanced")).toContain(FISHEYE);
  });

  it("gives PTZ y fisheye the control panel, autotracking and then Fisheye", () => {
    expect(sectionsOf({ ptz: true, fisheye: true, sections: ["autotracking"] }, "ptz")).toEqual([PTZ_CONTROL, "autotracking", FISHEYE]);
  });

  it("lists every section exactly once and drops none", () => {
    for (const flags of [{}, { ptz: true }, { fisheye: true }, { analytics: true, provision: true, cameraWeb: true, ptz: true, fisheye: true }]) {
      const generalSections = ["enabled", "audio", "timestamp_style", "type", "friendly_name"];
      const tabs = build({ ...flags, sections: FULL, generalSections });
      const listed = tabs.flatMap((t) => t.sections);
      expect(new Set(listed).size, JSON.stringify(flags)).toBe(listed.length);
      for (const s of FULL) {
        if (generalSections.includes(s) && s !== "ffmpeg") expect(listed).not.toContain(s);
        else expect(tabOfSection(tabs, s), `${s} ${JSON.stringify(flags)}`).toBeDefined();
      }
    }
  });

  it("sends analytics sections to Avanzado when the Analíticas OpenVMS tab is hidden", () => {
    const tabs = build({ analytics: false, sections: ["lpr", "face_recognition", "semantic_search"] });
    expect(tabOfSection(tabs, "semantic_search")?.id).toBe("advanced");
    expect(tabOfSection(tabs, "lpr")?.id).toBe("advanced");
  });
});

describe("analyticsTabVisible", () => {
  it("shows Analíticas OpenVMS by the modules list: only with an available module (or Provision / ONVIF analytics)", () => {
    const none = { modules: [{ available: false }, { available: false }], provision: false, advertised: false, lpr: false };
    expect(analyticsTabVisible(none)).toBe(false);
    expect(analyticsTabVisible({ ...none, modules: undefined })).toBe(false);
    expect(analyticsTabVisible({ ...none, modules: [{ available: false }, { available: true }] })).toBe(true);
    expect(analyticsTabVisible({ ...none, provision: true })).toBe(true);
    expect(analyticsTabVisible({ ...none, advertised: true })).toBe(true);
    expect(analyticsTabVisible({ ...none, lpr: true })).toBe(true);
  });
});

describe("tabOfSection", () => {
  it("returns undefined for a section nobody owns", () => {
    expect(tabOfSection(build({}), "nope")).toBeUndefined();
  });

  it("maps INTEGRATIONS_STATUS to the Analíticas OpenVMS tab when present", () => {
    expect(tabOfSection(build({ analytics: true }), INTEGRATIONS_STATUS)?.id).toBe("analytics");
  });

  it("lands every old section id on its new group", () => {
    const tabs = build({ analytics: true, provision: true, cameraWeb: true, ptz: true, fisheye: true, sections: FULL, generalSections: ["enabled", "audio"] });
    const of = (s: string) => tabOfSection(tabs, s)?.id;
    expect(of("zones")).toBe("detect");
    expect(of("review")).toBe("detect");
    expect(of(FISHEYE)).toBe("ptz");
    expect(of(PTZ_CONTROL)).toBe("ptz");
    expect(of("onvif")).toBe("connection");
    expect(of(ONVIF_INFO)).toBe("connection");
    expect(of(CAMERA_WEB)).toBe("connection");
    expect(of("webui_url")).toBe("connection");
    expect(of("semantic_search")).toBe("analytics");
    expect(of("mqtt")).toBe("advanced");
    expect(of(HISTORY)).toBe("advanced");
    expect(of(ADVANCED_PANELS)).toBe("advanced");
  });
});

describe("tabById", () => {
  it("resolves the ids of the tabs that were merged away to the group that absorbed them", () => {
    const tabs = build({ analytics: true, fisheye: true });
    expect(Object.keys(LEGACY_TAB_IDS).sort()).toEqual(["fisheye", "general", "integrations", "zones"]);
    expect(tabById(tabs, "general")?.id).toBe("summary");
    expect(tabById(tabs, "zones")?.id).toBe("detect");
    expect(tabById(tabs, "fisheye")?.id).toBe("ptz");
    expect(tabById(tabs, "video")?.id).toBe("video");
    expect(tabById(tabs, "nope")).toBeUndefined();
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
