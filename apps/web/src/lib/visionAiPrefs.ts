import type { VisionAISource, VisionInactivity } from "@/lib/visionAi";

export type VisionMode = "matrix" | "focus" | "lpr" | "faces" | "alarm" | "dynamic";

export interface VisionPrefs {
  mode: VisionMode;
  presetSource: VisionAISource;
  layoutCount: number;
  activeObjects: Record<string, boolean>;
  /** Seconds without a detection before a camera leaves the grid (birdseye-style). */
  inactivitySeconds: VisionInactivity;
  /**
   * Servers left unchecked in the server selector. Storing the exclusions, not the selection, keeps
   * "every server" the default and shows a server added later without anyone checking it.
   */
  excludedServerIds: string[];
}

/** Far more than any deployment has; it only bounds a corrupted value. */
const MAX_EXCLUDED_SERVERS = 100;

const STORAGE_PREFIX = "openvms.visionai.view.v1";

const MODES: readonly VisionMode[] = ["matrix", "focus", "lpr", "faces", "alarm", "dynamic"];
const SOURCES: readonly VisionAISource[] = ["general", "personas", "vehiculos", "lpr", "perimetro", "alarmas"];
export const VISION_LAYOUT_COUNTS: readonly number[] = [1, 4, 9, 16];
export const VISION_INACTIVITY_OPTIONS: readonly VisionInactivity[] = [10, 30, 60, 120];

export const DEFAULT_VISION_PREFS: VisionPrefs = {
  mode: "matrix",
  presetSource: "general",
  layoutCount: 4,
  activeObjects: { person: true, car: true, motorcycle: true, truck: true, license_plate: true, face: true },
  inactivitySeconds: 30,
  excludedServerIds: [],
};

export function visionPrefsKey(tenantId: string | null, userId: string): string {
  return `${STORAGE_PREFIX}:${tenantId ?? "platform"}:${userId}`;
}

/** Validates each field on its own, so one stale value never discards the rest of the saved view. */
export function parseVisionPrefs(raw: string | null): VisionPrefs {
  const defaults = DEFAULT_VISION_PREFS;
  if (!raw) return { ...defaults, activeObjects: { ...defaults.activeObjects }, excludedServerIds: [] };
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    parsed = null;
  }
  const rec = parsed && typeof parsed === "object" ? (parsed as Record<string, unknown>) : {};
  const activeObjects = { ...defaults.activeObjects };
  if (rec.activeObjects && typeof rec.activeObjects === "object") {
    for (const [name, value] of Object.entries(rec.activeObjects)) {
      if (Object.hasOwn(defaults.activeObjects, name) && typeof value === "boolean") activeObjects[name] = value;
    }
  }
  const excluded = Array.isArray(rec.excludedServerIds)
    ? [...new Set(rec.excludedServerIds.filter((id): id is string => typeof id === "string" && id !== ""))].slice(0, MAX_EXCLUDED_SERVERS)
    : [];
  return {
    mode: MODES.find((m) => m === rec.mode) ?? defaults.mode,
    presetSource: SOURCES.find((s) => s === rec.presetSource) ?? defaults.presetSource,
    layoutCount: VISION_LAYOUT_COUNTS.find((n) => n === rec.layoutCount) ?? defaults.layoutCount,
    activeObjects,
    inactivitySeconds: VISION_INACTIVITY_OPTIONS.find((n) => n === rec.inactivitySeconds) ?? defaults.inactivitySeconds,
    excludedServerIds: excluded,
  };
}

export function loadVisionPrefs(key: string): VisionPrefs {
  try {
    return parseVisionPrefs(localStorage.getItem(key));
  } catch {
    return parseVisionPrefs(null);
  }
}

export function saveVisionPrefs(key: string, prefs: VisionPrefs): void {
  try {
    localStorage.setItem(key, JSON.stringify(prefs));
  } catch {
    // Storage may be unavailable or full; the view simply is not remembered.
  }
}
