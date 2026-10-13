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
 * Generates 4 quadrant virtual views covering 360° (0°, 90°, 180°, 270°) with 88° FOV each.
 * In ceiling mount, pitch is oriented down (-48°) so the entire upper frustum remains
 * within the lens circle, eliminating dead black borders at the top.
 */
export function generate4QuadrantsViews(
  cameraId: string,
  mountType: FisheyeMountType = "ceiling",
): VirtualFisheyeView[] {
  const isCeiling = mountType === "ceiling";
  const pitch = isCeiling ? -48 : 0;
  const fov = 88;

  return [
    {
      id: `${cameraId}-quad-1`,
      cameraId,
      name: "Vista 1 (Norte)",
      projection: "rectilinear",
      yaw: 0,
      pitch,
      roll: 0,
      fov,
      presetType: "4_quadrants",
    },
    {
      id: `${cameraId}-quad-2`,
      cameraId,
      name: "Vista 2 (Este)",
      projection: "rectilinear",
      yaw: 90,
      pitch,
      roll: 0,
      fov,
      presetType: "4_quadrants",
    },
    {
      id: `${cameraId}-quad-3`,
      cameraId,
      name: "Vista 3 (Sur)",
      projection: "rectilinear",
      yaw: 180,
      pitch,
      roll: 0,
      fov,
      presetType: "4_quadrants",
    },
    {
      id: `${cameraId}-quad-4`,
      cameraId,
      name: "Vista 4 (Oeste)",
      projection: "rectilinear",
      yaw: -90,
      pitch,
      roll: 0,
      fov,
      presetType: "4_quadrants",
    },
  ];
}

/**
 * Resolves effective virtual views for a camera config.
 * When custom views exist, returns them. Otherwise, automatically provides
 * the standard views based on defaultMode (3 fixed 120° views or 4 quadrants)
 * so fisheye cameras always have ready-to-drag individual views in the tree.
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
  return generate4QuadrantsViews(config.cameraId, config.mountType);
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
    if (
      mode === "original" ||
      mode === "360" ||
      mode === "panorama" ||
      mode === "virtual_ptz" ||
      mode === "3_fixed_120" ||
      mode === "4_quadrants"
    ) {
      return mode;
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
