import type { Schemas } from "@/api/client";

export type AnalyticsModule = Schemas["CameraConfig"]["analytics"]["modules"][number];
export type ModuleId = AnalyticsModule["id"];

type Draft = Record<string, unknown>;
const isRec = (v: unknown): v is Draft => typeof v === "object" && v !== null && !Array.isArray(v);

function draftOf(patch: Draft, id: string): Draft {
  const analytics = patch.analytics;
  const modules = isRec(analytics) ? analytics.modules : undefined;
  const m = isRec(modules) ? modules[id] : undefined;
  return isRec(m) ? m : {};
}

/** The switch as the user sees it: the unsaved draft wins over what the camera reports. */
export function draftEnabled(patch: Draft, m: AnalyticsModule): boolean {
  // LPR is also switched from the header (analytics.lpr): same switch, so the draft reads both.
  const analytics = isRec(patch.analytics) ? patch.analytics : {};
  const v = m.id === "lpr" && typeof analytics.lpr === "boolean" ? analytics.lpr : draftOf(patch, m.id).enabled;
  return typeof v === "boolean" ? v : m.enabled;
}

/** The smallest-area setting as the user sees it; undefined when nobody knows it. */
export function draftMinArea(patch: Draft, m: AnalyticsModule): number | undefined {
  const s = draftOf(patch, m.id).settings;
  const v = isRec(s) ? s.min_area : undefined;
  return typeof v === "number" ? v : m.settings.min_area;
}

/** Sparse change of one module, ready for `setPatch`. */
export const enabledPatch = (id: ModuleId, enabled: boolean): Draft =>
  // `analytics.lpr` is the LPR module's switch; using one spelling keeps the header and the card from disagreeing.
  id === "lpr" ? { analytics: { lpr: enabled } } : { analytics: { modules: { [id]: { enabled } } } };
export const minAreaPatch = (id: ModuleId, minArea: number): Draft => ({ analytics: { modules: { [id]: { settings: { min_area: minArea } } } } });
export const enhancementPatch = (id: ModuleId, level: number): Draft => ({ analytics: { modules: { [id]: { settings: { enhancement: level } } } } });

/** The LPR image-enhancement level (0 to 10) as the user sees it. */
export function draftEnhancement(patch: Draft, m: AnalyticsModule): number | undefined {
  const s = draftOf(patch, m.id).settings;
  const v = isRec(s) ? s.enhancement : undefined;
  return typeof v === "number" ? v : m.settings.enhancement;
}

/** Whether any module can be shown: the Analíticas tab stays hidden otherwise. */
export const anyModuleAvailable = (modules: readonly Pick<AnalyticsModule, "available">[] | undefined): boolean => !!modules?.some((m) => m.available);

export const engineLabel: Record<AnalyticsModule["engine"], string> = {
  frigate: "Frigate",
  provision: "Provision ISR",
  native: "Analítica nativa de la cámara",
  openvms: "OpenVMS",
  none: "Sin motor",
};
