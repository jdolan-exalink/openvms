import { describe, expect, it } from "vitest";
import {
  cameraToFeature,
  computeDisplayState,
  EntityIndex,
  STATE_COLORS,
  STATE_ICONS,
} from "./entityIndex";
import type { CameraEntity } from "./types";

describe("entityIndex", () => {
  it("picks the glyph by camera type while online, and the status glyph otherwise", () => {
    const index = new EntityIndex([
      { ...baseCamera, camera: { ...baseCamera.camera, cameraType: "dome" } },
      { ...baseCamera, id: "cam-2", camera: { ...baseCamera.camera, cameraType: "ptz" } },
      { ...baseCamera, id: "cam-3", camera: { ...baseCamera.camera, cameraType: "fixed" } },
      { ...baseCamera, id: "cam-4", activeAlarms: 2 }, // alarm wins over type
    ]);
    const icons = index.toFeatureCollection().features.map((f) => f.properties.icon);
    expect(icons).toEqual(["cam-dome", "cam-ptz", "cam-normal", "cam-alarm"]);
  });

  it("renders server-only metadata outages as unreachable", () => {
    const index = new EntityIndex([{ ...baseCamera, metadata: { serverOffline: true } }]);
    expect(index.toFeatureCollection().features[0]!.properties.display_state).toBe("UNREACHABLE");
  });
  const baseCamera: CameraEntity = {
    id: "cam-1",
    type: "camera",
    siteId: "site-1",
    serverId: "srv-1",
    name: "Cámara Acceso Principal",
    status: "online",
    activeAlarms: 0,
    position: {
      kind: "geo",
      lat: -34.6037,
      lng: -58.3816,
    },
    camera: {
      bearingDeg: 90,
      fovDeg: 75,
      rangeM: 30,
      cameraType: "fixed",
      ptz: false,
      lpr: true,
    },
    metadata: {},
  };

  describe("computeDisplayState", () => {
    it("prioritizes ALARM when active alarms > 0 regardless of camera status", () => {
      expect(computeDisplayState("online", 2, false)).toBe("ALARM");
      expect(computeDisplayState("offline", 1, false)).toBe("ALARM");
    });

    it("returns UNREACHABLE when server is offline", () => {
      expect(computeDisplayState("online", 0, true)).toBe("UNREACHABLE");
      expect(computeDisplayState("unknown", 0, true)).toBe("UNREACHABLE");
    });

    it("returns OFFLINE when status is offline and server is online", () => {
      expect(computeDisplayState("offline", 0, false)).toBe("OFFLINE");
    });

    it("returns NO_SIGNAL when status is unknown and server is online", () => {
      expect(computeDisplayState("unknown", 0, false)).toBe("NO_SIGNAL");
    });

    it("returns DEGRADED when status is degraded", () => {
      expect(computeDisplayState("degraded", 0, false)).toBe("DEGRADED");
    });

    it("returns ONLINE when status is online", () => {
      expect(computeDisplayState("online", 0, false)).toBe("ONLINE");
    });
  });

  describe("cameraToFeature", () => {
    it("converts a geo CameraEntity to a GeoJSON Point feature with encoded properties", () => {
      const feat = cameraToFeature(baseCamera);
      expect(feat).not.toBeNull();
      expect(feat?.type).toBe("Feature");
      expect(feat?.id).toBe("cam-1");
      expect(feat?.geometry.type).toBe("Point");
      expect(feat?.geometry.coordinates).toEqual([-58.3816, -34.6037]);
      expect(feat?.properties.st).toBe("online");
      expect(feat?.properties.display_state).toBe("ONLINE");
      expect(feat?.properties.color).toBe(STATE_COLORS.ONLINE);
      expect(feat?.properties.icon).toBe(STATE_ICONS.ONLINE);
      expect(feat?.properties.lpr).toBe(1);
    });

    it("returns null for floor placement", () => {
      const floorCamera: CameraEntity = {
        ...baseCamera,
        position: {
          kind: "floor",
          floorId: "fl-1",
          x: 0.5,
          y: 0.5,
        },
      };
      expect(cameraToFeature(floorCamera)).toBeNull();
    });
  });

  describe("EntityIndex class", () => {
    it("updates status and produces patched FeatureCollection", () => {
      const index = new EntityIndex([baseCamera]);
      expect(index.getAll()).toHaveLength(1);

      let fc = index.toFeatureCollection();
      expect(fc.features).toHaveLength(1);
      expect(fc.features[0]!.properties.display_state).toBe("ONLINE");

      // Patch status to degraded
      index.patchStatus("cam-1", "degraded");
      fc = index.toFeatureCollection();
      expect(fc.features[0]!.properties.display_state).toBe("DEGRADED");
      expect(fc.features[0]!.properties.color).toBe(STATE_COLORS.DEGRADED);

      // Patch active alarms
      index.patchAlarms("cam-1", 3);
      fc = index.toFeatureCollection();
      expect(fc.features[0]!.properties.display_state).toBe("ALARM");
      expect(fc.features[0]!.properties.color).toBe(STATE_COLORS.ALARM);

      // Server offline override
      index.patchAlarms("cam-1", 0);
      index.setServerOffline("srv-1", true);
      fc = index.toFeatureCollection();
      expect(fc.features[0]!.properties.display_state).toBe("UNREACHABLE");
    });
  });
});
