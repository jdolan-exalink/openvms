import type {
  FisheyeCalibration,
  FisheyeLensType,
  FisheyeMountType,
  FisheyeProjectionModel,
} from "./types";

export interface ManufacturerFisheyeProfile {
  id: string;
  manufacturer: string;
  modelPattern: RegExp;
  lensType: FisheyeLensType;
  mountType: FisheyeMountType;
  projectionModel: FisheyeProjectionModel;
  calibration: FisheyeCalibration;
}

export const KNOWN_FISHEYE_PROFILES: ManufacturerFisheyeProfile[] = [
  {
    id: "provision_isr_f360",
    manufacturer: "Provision-ISR",
    modelPattern: /F-360|FE-.*|PR-.*360/i,
    lensType: "fisheye_360",
    mountType: "ceiling",
    projectionModel: "equidistant",
    calibration: {
      centerX: 0.5,
      centerY: 0.5,
      radius: 0.49,
      rotation: 0,
      fov: 360,
    },
  },
  {
    id: "hikvision_fisheye_63xx",
    manufacturer: "Hikvision",
    modelPattern: /DS-2CD63|DS-2CD29|DS-2DF.*FE|DS-2CD.*F/i,
    lensType: "fisheye_360",
    mountType: "ceiling",
    projectionModel: "equidistant",
    calibration: {
      centerX: 0.5,
      centerY: 0.5,
      radius: 0.485,
      rotation: 0,
      fov: 360,
    },
  },
  {
    id: "dahua_fisheye_ebw",
    manufacturer: "Dahua",
    modelPattern: /IPC-EBW|IPC-EW|DH-IPC-.*360/i,
    lensType: "fisheye_360",
    mountType: "ceiling",
    projectionModel: "equidistant",
    calibration: {
      centerX: 0.5,
      centerY: 0.5,
      radius: 0.49,
      rotation: 0,
      fov: 360,
    },
  },
  {
    id: "axis_fisheye_m30",
    manufacturer: "Axis",
    modelPattern: /M305[78]|M307[78]|M306[78]|P39.*-R/i,
    lensType: "fisheye_360",
    mountType: "ceiling",
    projectionModel: "equidistant",
    calibration: {
      centerX: 0.5,
      centerY: 0.5,
      radius: 0.48,
      rotation: 0,
      fov: 360,
    },
  },
  {
    id: "hanwha_fisheye_xnf",
    manufacturer: "Hanwha Vision",
    modelPattern: /XNF-90|XNF-80|QNF-90|QNF-80/i,
    lensType: "fisheye_360",
    mountType: "ceiling",
    projectionModel: "equidistant",
    calibration: {
      centerX: 0.5,
      centerY: 0.5,
      radius: 0.49,
      rotation: 0,
      fov: 360,
    },
  },
  {
    id: "generic_180_panoramic",
    manufacturer: "Generic Panoramic",
    modelPattern: /180|pano|panoramic/i,
    lensType: "fisheye_180",
    mountType: "wall",
    projectionModel: "equidistant",
    calibration: {
      centerX: 0.5,
      centerY: 0.5,
      radius: 0.5,
      rotation: 0,
      fov: 180,
    },
  },
];

/**
 * Attempt automatic detection of fisheye camera parameters from camera metadata,
 * model strings, display name, and tags.
 */
export function detectFisheyeProfile(camera: {
  display_name?: string;
  remote_name?: string;
  description?: string;
  tags?: string[];
  onvif_profile?: string | null;
}): ManufacturerFisheyeProfile | null {
  const textCorpus = [
    camera.display_name || "",
    camera.remote_name || "",
    camera.description || "",
    camera.onvif_profile || "",
    ...(camera.tags || []),
  ].join(" ");

  for (const profile of KNOWN_FISHEYE_PROFILES) {
    if (profile.modelPattern.test(textCorpus)) {
      return profile;
    }
  }

  // Fallback heuristic: check for explicit keywords
  if (
    /fisheye|ojo[-_ ]?de[-_ ]?pez|360[-_ ]?cam|360°/i.test(textCorpus)
  ) {
    return {
      id: "generic_360_fisheye",
      manufacturer: "Generic Fisheye",
      modelPattern: /fisheye/i,
      lensType: "fisheye_360",
      mountType: "ceiling",
      projectionModel: "equidistant",
      calibration: {
        centerX: 0.5,
        centerY: 0.5,
        radius: 0.48,
        rotation: 0,
        fov: 360,
      },
    };
  }

  return null;
}
