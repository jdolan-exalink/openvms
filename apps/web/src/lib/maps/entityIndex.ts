import type { Feature, FeatureCollection, Point } from "geojson";
import type { CameraDisplayState, CameraEntity, MapEntity } from "./types";

export interface CameraFeatureProperties {
  id: string;
  name: string;
  site_id: string;
  server_id?: string;
  st: string; // display state token for clusterProperties
  display_state: CameraDisplayState;
  alarms: number;
  bearing: number | null;
  fov: number | null;
  range: number | null;
  camera_type: string;
  ptz: number;
  lpr: number;
  icon: string;
  color: string;
}

export const STATE_COLORS: Record<CameraDisplayState, string> = {
  ONLINE: "#21b45b",
  DEGRADED: "#f59e0b",
  OFFLINE: "#7e8a9a",
  NO_SIGNAL: "#7e8a9a",
  RECORDING_ERROR: "#f59e0b",
  UNREACHABLE: "#7e8a9a",
  ALARM: "#ef3f46",
};

export const STATE_ICONS: Record<CameraDisplayState, string> = {
  ONLINE: "cam-normal",
  DEGRADED: "cam-warning",
  OFFLINE: "cam-offline",
  NO_SIGNAL: "cam-offline",
  RECORDING_ERROR: "cam-warning",
  UNREACHABLE: "cam-server-offline",
  ALARM: "cam-alarm",
};

export function computeDisplayState(
  status: MapEntity["status"],
  activeAlarms = 0,
  serverOffline = false,
): CameraDisplayState {
  if (activeAlarms > 0) return "ALARM";
  if (serverOffline) return "UNREACHABLE";
  switch (status) {
    case "online":
      return "ONLINE";
    case "degraded":
      return "DEGRADED";
    case "offline":
      return "OFFLINE";
    case "unknown":
    default:
      return "NO_SIGNAL";
  }
}

export function cameraToFeature(entity: CameraEntity, serverOffline = false): Feature<Point, CameraFeatureProperties> | null {
  if (entity.position.kind !== "geo") return null;

  const displayState = computeDisplayState(entity.status, entity.activeAlarms, serverOffline || !!entity.metadata.serverOffline);
  // Connectivity controls the base marker; alarms remain an independent badge.
  const connectivity = computeDisplayState(entity.status, 0, serverOffline || !!entity.metadata.serverOffline);
  const color = STATE_COLORS[connectivity];
  const icon = connectivity === "ONLINE"
    ? entity.camera.cameraType === "dome" ? "cam-dome"
      : entity.camera.cameraType === "ptz" ? "cam-ptz" : "cam-normal"
    : STATE_ICONS[connectivity];

  return {
    type: "Feature",
    id: entity.id,
    geometry: {
      type: "Point",
      coordinates: [entity.position.lng, entity.position.lat],
    },
    properties: {
      id: entity.id,
      name: entity.name,
      site_id: entity.siteId,
      server_id: entity.serverId,
      st: connectivity.toLowerCase(),
      display_state: displayState,
      alarms: entity.activeAlarms,
      bearing: entity.camera.bearingDeg,
      fov: entity.camera.fovDeg,
      range: entity.camera.rangeM,
      camera_type: entity.camera.cameraType,
      ptz: entity.camera.ptz ? 1 : 0,
      lpr: entity.camera.lpr ? 1 : 0,
      icon,
      color,
    },
  };
}

export class EntityIndex {
  private entities = new Map<string, CameraEntity>();
  private serverOfflineStatus = new Map<string, boolean>();
  private cachedCollection: FeatureCollection<Point, CameraFeatureProperties> | null = null;

  constructor(initialCameras: CameraEntity[] = []) {
    this.setAll(initialCameras);
  }

  public setAll(cameras: CameraEntity[]) {
    this.entities.clear();
    for (const cam of cameras) {
      this.entities.set(cam.id, cam);
    }
    this.cachedCollection = null;
  }

  public get(id: string): CameraEntity | undefined {
    return this.entities.get(id);
  }

  public getAll(): CameraEntity[] {
    return Array.from(this.entities.values());
  }

  public setServerOffline(serverId: string, offline: boolean) {
    const prev = this.serverOfflineStatus.get(serverId) ?? false;
    if (prev !== offline) {
      this.serverOfflineStatus.set(serverId, offline);
      this.cachedCollection = null;
    }
  }

  public patchStatus(id: string, status: MapEntity["status"]) {
    const existing = this.entities.get(id);
    if (!existing) return;
    if (existing.status !== status) {
      existing.status = status;
      this.cachedCollection = null;
    }
  }

  public patchAlarms(id: string, activeAlarms: number) {
    const existing = this.entities.get(id);
    if (!existing) return;
    if (existing.activeAlarms !== activeAlarms) {
      existing.activeAlarms = activeAlarms;
      this.cachedCollection = null;
    }
  }

  public toFeatureCollection(): FeatureCollection<Point, CameraFeatureProperties> {
    if (this.cachedCollection) return this.cachedCollection;

    const features: Feature<Point, CameraFeatureProperties>[] = [];
    for (const cam of this.entities.values()) {
      const serverOffline = cam.serverId ? (this.serverOfflineStatus.get(cam.serverId) ?? false) : false;
      const feat = cameraToFeature(cam, serverOffline);
      if (feat) {
        features.push(feat);
      }
    }

    this.cachedCollection = {
      type: "FeatureCollection",
      features,
    };
    return this.cachedCollection;
  }
}
