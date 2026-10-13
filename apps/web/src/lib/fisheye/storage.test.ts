import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  generate3Fixed120Views,
  getEffectiveVirtualViews,
  getFisheyeConfig,
  getFisheyeViewMode,
  saveFisheyeConfig,
  saveFisheyeViewMode,
} from "./storage";
import { DEFAULT_FISHEYE_CONFIG, type FisheyeConfig } from "./types";

describe("Fisheye Storage Module", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("returns default config when camera is unconfigured", () => {
    const config = getFisheyeConfig("cam-unconfigured");
    expect(config.cameraId).toBe("cam-unconfigured");
    expect(config.enabled).toBe(false);
    expect(config.lensType).toBe("normal");
    expect(config.mountType).toBe("ceiling");
    expect(config.views).toHaveLength(0);
  });

  it("persists and restores fisheye configuration", () => {
    const customConfig: FisheyeConfig = {
      ...DEFAULT_FISHEYE_CONFIG,
      cameraId: "cam-lobby-360",
      mountType: "wall",
      projectionModel: "equisolid",
      calibration: {
        centerX: 0.52,
        centerY: 0.49,
        radius: 0.47,
        rotation: 90,
        fov: 360,
      },
      views: [
        {
          id: "v1",
          cameraId: "cam-lobby-360",
          name: "Entrada Principal",
          projection: "rectilinear",
          yaw: 45,
          pitch: -15,
          roll: 0,
          fov: 85,
        },
      ],
      defaultMode: "360",
    };

    saveFisheyeConfig(customConfig);
    const restored = getFisheyeConfig("cam-lobby-360");

    expect(restored.mountType).toBe("wall");
    expect(restored.projectionModel).toBe("equisolid");
    expect(restored.calibration.centerX).toBe(0.52);
    expect(restored.calibration.rotation).toBe(90);
    expect(restored.views).toHaveLength(1);
    expect(restored.views[0]?.name).toBe("Entrada Principal");
  });

  it("generates 3 fixed 120-degree virtual views covering 360 degrees", () => {
    const views = generate3Fixed120Views("cam-store-360");
    expect(views).toHaveLength(3);

    // Verify 120-degree FOV per view
    expect(views.every((v) => v.fov === 120)).toBe(true);

    // Verify yaw spacing: 0, 120, 240
    expect(views[0]?.yaw).toBe(0);
    expect(views[1]?.yaw).toBe(120);
    expect(views[2]?.yaw).toBe(240);

    // Verify presetType tag
    expect(views.every((v) => v.presetType === "3_fixed_120")).toBe(true);
  });

  it("stores and retrieves last active view mode", () => {
    expect(getFisheyeViewMode("cam-test")).toBe("virtual_ptz");
    saveFisheyeViewMode("cam-test", "3_fixed_120");
    expect(getFisheyeViewMode("cam-test")).toBe("3_fixed_120");
  });

  it("honors a saved non-default mode when no transient mode is saved", () => {
    saveFisheyeConfig({ ...DEFAULT_FISHEYE_CONFIG, cameraId: "cam-saved-mode", enabled: true, lensType: "fisheye_360", defaultMode: "360" });
    expect(getFisheyeViewMode("cam-saved-mode")).toBe("360");
  });

  it("resolves effective virtual views automatically", () => {
    const unconfigured = getFisheyeConfig("cam-unconfigured");
    // By default, no camera is fisheye, so effective views is empty
    expect(getEffectiveVirtualViews(unconfigured)).toHaveLength(0);

    // Camera explicitly configured as fisheye with 3 fixed 120 views mode
    const configuredFisheye = {
      ...unconfigured,
      enabled: true,
      lensType: "fisheye_360" as const,
      defaultMode: "3_fixed_120" as const,
    };
    const effective = getEffectiveVirtualViews(configuredFisheye);
    expect(effective).toHaveLength(3);
    expect(effective[0]?.name).toBe("Vista 1 (0°)");
    expect(effective[0]?.pitch).toBe(-46); // Optimal ceiling pitch eliminating top black borders

    // Wall mount pitch adapts to 0
    const viewsWall = generate3Fixed120Views("cam-wall", "wall");
    expect(viewsWall[0]?.pitch).toBe(0);
  });

  it("migrates a legacy 4_quadrants default mode to virtual_ptz", () => {
    localStorage.setItem(
      "openvms.fisheye.config.cam-legacy",
      JSON.stringify({ ...DEFAULT_FISHEYE_CONFIG, cameraId: "cam-legacy", enabled: true, lensType: "fisheye_360", defaultMode: "4_quadrants" }),
    );
    localStorage.setItem("openvms.fisheye.mode.cam-legacy", "4_quadrants");

    expect(getFisheyeConfig("cam-legacy").defaultMode).toBe("virtual_ptz");
    expect(getFisheyeViewMode("cam-legacy")).toBe("virtual_ptz");
  });
});
