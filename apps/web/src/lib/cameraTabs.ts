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
/** Status of the backend integrations (Frigate, ONVIF, Provision ISR); first section of Integraciones. */
export const INTEGRATIONS_STATUS = "__integrations_status";
export const PTZ_CONTROL = "__ptz_control";
/** Analytics modules (switch + detail per analytic); the raw Frigate/Provision panels stay after it as "Avanzado". */
/** Per-backend technical panels (Frigate YAML, ONVIF, Provision) and the change history; first section of Avanzado. */
export const ADVANCED_PANELS = "__advanced";
export const ANALYTICS_MODULES = "__analytics_modules";

export type CameraTabId = "summary" | "video" | "record" | "detect" | "analytics" | "ptz" | "connection" | "advanced";

/** Heading of the left navigation a tab lives under. */
export type CameraTabGroup = "camera" | "intelligence" | "control" | "system";

export const GROUP_LABELS: Record<CameraTabGroup, string> = { camera: "Cámara", intelligence: "Inteligencia", control: "Control", system: "Sistema" };
const GROUP_ORDER: CameraTabGroup[] = ["camera", "intelligence", "control", "system"];

export type CameraTab = { id: CameraTabId; label: string; group: CameraTabGroup; sections: string[] };

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
// Detection and zones share one entry: zones are drawn on the detection area.
const DETECT = ["detect", "objects", "motion", "zones", "review"];
const ANALYTICS = ["lpr", "face_recognition", "semantic_search"];
const PTZ = ["autotracking"];
// ONVIF / PTZ connection settings and the camera's own web address.
const CONNECTION = ["onvif", "webui_url"];
// Advanced accordions, in display order; whatever else no group owns follows them.
const ADVANCED_FIRST = ["mqtt", "notifications", "ffmpeg_cmds", "ui", "profiles"];

/** Tabs that used to exist and the group that absorbed them, so old links and bookmarks still land. */
export const LEGACY_TAB_IDS: Record<string, CameraTabId> = { general: "summary", zones: "detect", fisheye: "ptz", integrations: "analytics" };

export function buildCameraTabs(input: CameraTabsInput): CameraTab[] {
  const known = (list: string[]) => list.filter((s) => input.sections.includes(s));
  const tabs: CameraTab[] = [
    { id: "summary", label: "Resumen", group: "camera", sections: [GENERAL] },
    { id: "video", label: "Video", group: "camera", sections: known(VIDEO) },
    // The core Frigate sections were always listed, even when an older schema lacks them.
    { id: "record", label: "Grabación", group: "camera", sections: RECORD },
    { id: "detect", label: "Detección y zonas", group: "intelligence", sections: DETECT },
  ];
  if (input.analytics) tabs.push({ id: "analytics", label: "Analíticas OpenVMS", group: "intelligence", sections: [ANALYTICS_MODULES, ...(input.provision ? [PROVISION] : []), ...known(ANALYTICS)] });
  if (input.ptz || input.fisheye) {
    tabs.push({ id: "ptz", label: "PTZ y fisheye", group: "control", sections: [...(input.ptz ? [PTZ_CONTROL, ...known(PTZ)] : []), ...(input.fisheye ? [FISHEYE] : [])] });
  }
  tabs.push({ id: "connection", label: "Conexión", group: "system", sections: [ONVIF_INFO, ...known(CONNECTION), ...(input.cameraWeb ? [CAMERA_WEB] : [])] });

  const owned = new Set([...tabs.flatMap((t) => t.sections), ...(input.generalSections ?? [])]);
  const rest = input.sections.filter((s) => !owned.has(s));
  const first = ADVANCED_FIRST.filter((s) => rest.includes(s));
  tabs.push({
    id: "advanced",
    label: "Avanzado",
    group: "system",
    sections: [
      ...first,
      ADVANCED_PANELS,
      ...rest.filter((s) => !first.includes(s)),
      ...(input.fisheye ? [] : [FISHEYE]),
      HISTORY,
    ],
  });
  return tabs;
}

/** The tabs per navigation heading, in heading order; headings without tabs are dropped. */
export function groupCameraTabs(tabs: CameraTab[]): { id: CameraTabGroup; label: string; tabs: CameraTab[] }[] {
  return GROUP_ORDER.map((id) => ({ id, label: GROUP_LABELS[id], tabs: tabs.filter((t) => t.group === id) })).filter((g) => g.tabs.length > 0);
}

/**
 * Whether the Analíticas OpenVMS tab is shown: some analytics module is available (positive evidence in the
 * normalized model), or the camera is a Provision one, or it advertises ONVIF analytics.
 */
export function analyticsTabVisible(input: { modules: readonly { available: boolean }[] | undefined; provision: boolean; advertised: boolean; lpr: boolean }): boolean {
  return input.provision || input.advertised || input.lpr || !!input.modules?.some((m) => m.available);
}

/** The tab that owns a section, if any. */
export const tabOfSection = (tabs: CameraTab[], section: string): CameraTab | undefined =>
  tabs.find((t) => t.sections.includes(section)) ?? (section === INTEGRATIONS_STATUS ? tabs.find((t) => t.id === "analytics") : undefined);

/** A tab by id; ids of the tabs that were merged away resolve to the group that absorbed them. */
export const tabById = (tabs: CameraTab[], id: string): CameraTab | undefined =>
  tabs.find((t) => t.id === id) ?? tabs.find((t) => t.id === LEGACY_TAB_IDS[id]);

/** Camera tags that mark a fisheye lens. */
export const isFisheyeTagged = (tags: string[] | undefined) => !!tags?.some((t) => t === "fisheye" || t === "ojo_de_pez");
