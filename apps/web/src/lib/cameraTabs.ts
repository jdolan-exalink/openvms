/**
 * Tab layout of the camera editor. Pure: given what the camera has and the Frigate sections the
 * schema/config know, it returns the visible tabs and the sections each one owns, so no section
 * of the previous sidebar is lost.
 */

// Pseudo sections: panels that are not a Frigate schema section.
export const GENERAL = "__general";
export const HISTORY = "__history";
export const FISHEYE = "__fisheye";
export const PROVISION = "__provision";
export const CAMERA_WEB = "__camera_web";
export const ONVIF_INFO = "__onvif_info";
export const PTZ_CONTROL = "__ptz_control";
/** Analytics modules (switch + detail per analytic); the raw Frigate/Provision panels stay after it as "Avanzado". */
export const ANALYTICS_MODULES = "__analytics_modules";

export type CameraTabId =
  | "general" | "video" | "record" | "detect" | "analytics" | "zones" | "ptz" | "fisheye" | "integrations" | "advanced";

export type CameraTab = { id: CameraTabId; label: string; sections: string[] };

export type CameraTabsInput = {
  /** Frigate camera sections known to the schema or the effective config, in display order. */
  sections: string[];
  /** Sections the General tab edits itself; they only show elsewhere when a tab claims them. */
  generalSections?: string[];
  ptz: boolean;
  fisheye: boolean;
  /** The camera has LPR, face or Provision analytics (or advertises analytics over ONVIF). */
  analytics: boolean;
  provision: boolean;
  /** The user may open the camera's own web interface. */
  cameraWeb: boolean;
};

const VIDEO = ["ffmpeg", "live", "rtmp"];
const RECORD = ["record", "snapshots"];
const DETECT = ["detect", "objects", "motion"];
const ANALYTICS = ["lpr", "face_recognition"];
const ZONES = ["zones", "review"];
const PTZ = ["autotracking"];

export function buildCameraTabs(input: CameraTabsInput): CameraTab[] {
  const known = (list: string[]) => list.filter((s) => input.sections.includes(s));
  const tabs: CameraTab[] = [
    { id: "general", label: "General", sections: [GENERAL] },
    { id: "video", label: "Video", sections: known(VIDEO) },
    // The core Frigate sections were always listed, even when an older schema lacks them.
    { id: "record", label: "Grabación", sections: RECORD },
    { id: "detect", label: "Detección", sections: DETECT },
  ];
  if (input.analytics) tabs.push({ id: "analytics", label: "Analíticas", sections: [ANALYTICS_MODULES, ...(input.provision ? [PROVISION] : []), ...known(ANALYTICS)] });
  tabs.push({ id: "zones", label: "Zonas", sections: ZONES });
  if (input.ptz) tabs.push({ id: "ptz", label: "PTZ", sections: [PTZ_CONTROL, ...known(PTZ)] });
  if (input.fisheye) tabs.push({ id: "fisheye", label: "Fisheye", sections: [FISHEYE] });
  tabs.push({ id: "integrations", label: "Integraciones", sections: [...(input.cameraWeb ? [CAMERA_WEB] : []), ONVIF_INFO] });

  const owned = new Set([...tabs.flatMap((t) => t.sections), ...(input.generalSections ?? [])]);
  tabs.push({
    id: "advanced",
    label: "Avanzado",
    sections: [...input.sections.filter((s) => !owned.has(s)), ...(input.fisheye ? [] : [FISHEYE]), HISTORY],
  });
  return tabs;
}

/**
 * Whether the Analíticas tab is shown: some analytics module is available (positive evidence in the
 * normalized model), or the camera is a Provision one, or it advertises ONVIF analytics.
 */
export function analyticsTabVisible(input: { modules: readonly { available: boolean }[] | undefined; provision: boolean; advertised: boolean; lpr: boolean }): boolean {
  return input.provision || input.advertised || input.lpr || !!input.modules?.some((m) => m.available);
}

/** The tab that owns a section, if any. */
export const tabOfSection = (tabs: CameraTab[], section: string): CameraTab | undefined => tabs.find((t) => t.sections.includes(section));

/** Camera tags that mark a fisheye lens. */
export const isFisheyeTagged = (tags: string[] | undefined) => !!tags?.some((t) => t === "fisheye" || t === "ojo_de_pez");
