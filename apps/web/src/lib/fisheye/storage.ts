import {
  DEFAULT_FISHEYE_CONFIG,
  type FisheyeConfig,
  type FisheyeMountType,
  type FisheyeViewMode,
  type VirtualFisheyeView,
} from "./types";

const CONFIG_PREFIX = "openvms.fisheye.config.";
const VIEW_MODE_PREFIX = "openvms.fisheye.mode.";

/**
 * Generates 3 fixed virtual views covering 360° with 120° FOV each.
 * User request: "en las virtuales podriamos crear una opcion que sea 3 fijas de 120"
 * In ceiling mount, pitch is oriented down (-46°) to prevent top rays from clipping
 * into the ceiling horizon, eliminating top black borders.
 */
export function generate3Fixed120Views(
  cameraId: string,
  mountType: FisheyeMountType = "ceiling",
): VirtualFisheyeView[] {
  const isCeiling = mountType === "ceiling";
  const pitch = isCeiling ? -46 : 0;

  return [
    {
      id: `${cameraId}-view-120-1`,
      cameraId,
      name: "Vista 1 (0°)",
      projection: "rectilinear",
      yaw: 0,
      pitch,
      roll: 0,
      fov: 120,
      presetType: "3_fixed_120",
    },
    {
      id: `${cameraId}-view-120-2`,
      cameraId,
      name: "Vista 2 (120°)",
      projection: "rectilinear",
      yaw: 120,
      pitch,
      roll: 0,
      fov: 120,
      presetType: "3_fixed_120",
    },
    {
      id: `${cameraId}-view-120-3`,
      cameraId,
      name: "Vista 3 (240°)",
      projection: "rectilinear",
      yaw: 240,
      pitch,
      roll: 0,
      fov: 120,
      presetType: "3_fixed_120",
    },
  ];
}

/**
 * Resolves effective virtual views for a camera config.
 * When custom views exist, returns them. Otherwise, automatically provides
 * the standard views based on defaultMode (3 fixed 120° views)
 * so fisheye cameras have ready-to-drag individual views in the tree.
 */
export function getEffectiveVirtualViews(config: FisheyeConfig): VirtualFisheyeView[] {
  if (!config.enabled || config.lensType === "normal") {
    return [];
  }
  if (config.views && config.views.length > 0) {
    return config.views;
  }
  if (config.defaultMode === "3_fixed_120") {
    return generate3Fixed120Views(config.cameraId, config.mountType);
  }
  return [];
}

const SUPPORTED_VIEW_MODES: readonly FisheyeViewMode[] = ["original", "360", "panorama", "virtual_ptz", "3_fixed_120"];

/** Maps legacy or unknown stored modes (e.g. the removed "4_quadrants") to the current default. */
function normalizeViewMode(mode: unknown): FisheyeViewMode {
  return SUPPORTED_VIEW_MODES.includes(mode as FisheyeViewMode) ? (mode as FisheyeViewMode) : "virtual_ptz";
}

/**
 * Retrieves the stored fisheye configuration for a given camera,
 * falling back to defaults if not yet configured.
 */
export function getFisheyeConfig(cameraId: string): FisheyeConfig {
  if (!cameraId) {
    return { ...DEFAULT_FISHEYE_CONFIG, cameraId: "" };
  }

  try {
    const raw = typeof localStorage !== "undefined" ? localStorage.getItem(`${CONFIG_PREFIX}${cameraId}`) : null;
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<FisheyeConfig>;
      return {
        ...DEFAULT_FISHEYE_CONFIG,
        ...parsed,
        cameraId,
        defaultMode: normalizeViewMode(parsed.defaultMode ?? DEFAULT_FISHEYE_CONFIG.defaultMode),
        calibration: {
          ...DEFAULT_FISHEYE_CONFIG.calibration,
          ...(parsed.calibration || {}),
        },
        views: Array.isArray(parsed.views) ? parsed.views : [],
      };
    }
  } catch {
    // If invalid JSON or storage disabled, return default
  }

  return {
    ...DEFAULT_FISHEYE_CONFIG,
    cameraId,
  };
}

/**
 * Persists the fisheye configuration for a camera.
 */
export function saveFisheyeConfig(config: FisheyeConfig): void {
  if (!config.cameraId || typeof localStorage === "undefined") return;

  try {
    localStorage.setItem(`${CONFIG_PREFIX}${config.cameraId}`, JSON.stringify(config));
    if (typeof window !== "undefined") {
      window.dispatchEvent(
        new CustomEvent("openvms:fisheye-config-changed", { detail: config }),
      );
    }
  } catch (err) {
    console.warn("Failed to persist fisheye config:", err);
  }
}

/**
 * Gets the active or last selected view mode for a camera.
 */
export function getFisheyeViewMode(cameraId: string): FisheyeViewMode {
  if (!cameraId || typeof localStorage === "undefined") return "virtual_ptz";
  try {
    const mode = localStorage.getItem(`${VIEW_MODE_PREFIX}${cameraId}`);
    if (SUPPORTED_VIEW_MODES.includes(mode as FisheyeViewMode)) {
      return mode as FisheyeViewMode;
    }
    const config = getFisheyeConfig(cameraId);
    if (config?.defaultMode) return config.defaultMode;
  } catch {}
  return "virtual_ptz";
}

/**
 * Saves the active view mode for a camera.
 */
export function saveFisheyeViewMode(cameraId: string, mode: FisheyeViewMode): void {
  if (!cameraId || typeof localStorage === "undefined") return;
  try {
    localStorage.setItem(`${VIEW_MODE_PREFIX}${cameraId}`, mode);
  } catch {}
}
