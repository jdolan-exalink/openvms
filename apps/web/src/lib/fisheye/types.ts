/**
 * Core type definitions for Fisheye / 360° / 180° camera management and WebGL dewarping.
 */

export type FisheyeLensType =
  | "auto"
  | "normal"
  | "fisheye_360"
  | "fisheye_180"
  | "panoramic";

export type FisheyeMountType = "ceiling" | "wall" | "floor";

export type FisheyeProjectionModel =
  | "equidistant"
  | "equisolid"
  | "stereographic"
  | "orthographic";

export type FisheyeViewMode =
  | "original"
  | "360"
  | "panorama"
  | "virtual_ptz"
  | "3_fixed_120"
  | "4_quadrants";

export interface FisheyeCalibration {
  /** Normalized optical center X in [0, 1] range (default: 0.5) */
  centerX: number;
  /** Normalized optical center Y in [0, 1] range (default: 0.5) */
  centerY: number;
  /** Useful lens radius normalized relative to image width/height (default: 0.5) */
  radius: number;
  /** Roll rotation of the sensor in degrees (-180 to 180, default: 0) */
  rotation: number;
  /** Total Lens Field Of View in degrees (e.g., 180, 220, 360; default: 360) */
  fov: number;
}

export interface VirtualFisheyeView {
  id: string;
  cameraId: string;
  name: string;
  projection: "rectilinear" | "panorama" | "cylindrical" | "original";
  /** Horizontal angle in degrees (-180 to 180) */
  yaw: number;
  /** Vertical tilt angle in degrees (-90 to 90) */
  pitch: number;
  /** Sensor roll angle in degrees (default: 0) */
  roll: number;
  /** Virtual Camera Field of View in degrees (e.g. 60 to 120, default: 90) */
  fov: number;
  /** Preset type identifier if created from a standard template */
  presetType?: "custom" | "3_fixed_120" | "4_quadrants";
}

export interface FisheyeConfig {
  cameraId: string;
  enabled: boolean;
  lensType: FisheyeLensType;
  mountType: FisheyeMountType;
  projectionModel: FisheyeProjectionModel;
  calibration: FisheyeCalibration;
  views: VirtualFisheyeView[];
  activeViewId?: string;
  defaultMode: FisheyeViewMode;
}

export const DEFAULT_FISHEYE_CALIBRATION: FisheyeCalibration = {
  centerX: 0.5,
  centerY: 0.5,
  radius: 0.48,
  rotation: 0,
  fov: 185,
};

export const DEFAULT_FISHEYE_CONFIG: Omit<FisheyeConfig, "cameraId"> = {
  enabled: true,
  lensType: "fisheye_360",
  mountType: "ceiling",
  projectionModel: "equidistant",
  calibration: DEFAULT_FISHEYE_CALIBRATION,
  views: [],
  defaultMode: "virtual_ptz",
};
