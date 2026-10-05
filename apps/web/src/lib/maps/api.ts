import { queryOptions } from "@tanstack/react-query";
import type { Polygon } from "geojson";
import { api, unwrap } from "@/api/client";
import type { Schemas } from "@/api/client";
import type {
  Building,
  CameraEntity,
  Floor,
  MapConfig,
  MapEntity,
  MapPosition,
  Site,
  Zone,
} from "./types";

export function mapWirePosition(pos: Schemas["MapPosition"]): MapPosition {
  if (pos.k === "floor" && pos.floor_id) {
    return {
      kind: "floor",
      floorId: pos.floor_id,
      x: pos.x ?? 0,
      y: pos.y ?? 0,
    };
  }
  return {
    kind: "geo",
    lat: pos.lat ?? 0,
    lng: pos.lng ?? 0,
  };
}

export function mapWireEntity(entity: Schemas["MapEntity"]): MapEntity | CameraEntity {
  const base: MapEntity = {
    id: entity.id,
    type: entity.t,
    siteId: entity.site,
    serverId: entity.srv ?? undefined,
    name: entity.name,
    position: mapWirePosition(entity.pos),
    status: entity.st,
    metadata: {},
    revision: entity.rev,
  };

  if (entity.t === "camera" && entity.cam) {
    const camEntity: CameraEntity = {
      ...base,
      type: "camera",
      activeAlarms: entity.alarms ?? 0,
      camera: {
        bearingDeg: entity.cam.bearing != null ? entity.cam.bearing : null,
        fovDeg: entity.cam.fov,
        rangeM: entity.cam.range,
        cameraType: (entity.cam.type as CameraEntity["camera"]["cameraType"]) || "fixed",
        ptz: entity.cam.ptz,
        lpr: entity.cam.lpr,
      },
    };
    return camEntity;
  }

  return base;
}

export const mapsConfigQuery = queryOptions({
  queryKey: ["maps", "config"],
  queryFn: async (): Promise<MapConfig> => {
    const res = unwrap(await api.GET("/api/v1/maps/config"));
    return {
      provider: {
        id: res.provider.id,
        kind: res.provider.kind,
        styleUrl: {
          light: res.provider.style_url_light ?? undefined,
          dark: res.provider.style_url_dark ?? undefined,
        },
        tiles: res.provider.tiles ?? undefined,
        attribution: res.provider.attribution,
        maxZoom: res.provider.max_zoom,
        offline: res.provider.offline,
      },
      defaultCenter: {
        lat: res.default_center.lat,
        lng: res.default_center.lng,
      },
      defaultZoom: res.default_zoom,
    };
  },
  staleTime: 60_000,
});

export const mapsOverviewQuery = queryOptions({
  queryKey: ["maps", "overview"],
  queryFn: async (): Promise<Site[]> => {
    const res = unwrap(await api.GET("/api/v1/maps/overview"));
    return res.items.map((item) => ({
      id: item.id,
      name: item.name,
      regionId: item.region_id ?? undefined,
      regionName: item.region_name ?? undefined,
      center:
        item.lat != null && item.lng != null
          ? { kind: "geo", lat: item.lat, lng: item.lng }
          : undefined,
      defaultZoom: item.default_zoom ?? undefined,
      cameraCount: item.camera_count,
      health: {
        online: item.online_cameras,
        offline: item.offline_cameras,
        degraded: item.degraded_cameras,
        activeAlarms: item.alarm_count,
        severity:
          item.alarm_count > 0 || item.offline_cameras > 0
            ? "CRITICAL"
            : item.degraded_cameras > 0
              ? "WARNING"
              : "OK",
      },
    }));
  },
  refetchInterval: 30_000,
});

export const siteDetailQuery = (siteId: string) =>
  queryOptions({
    queryKey: ["maps", "sites", siteId],
    queryFn: async () => {
      const res = unwrap(await api.GET("/api/v1/maps/sites/{siteId}", { params: { path: { siteId } } }));
      const buildings: Building[] = (res.buildings ?? []).map((b) => ({
        id: b.id,
        siteId: b.site_id,
        name: b.name,
        footprint: b.footprint as Polygon | undefined,
        floors: (b.floors ?? []).map((f): Floor => ({
          id: f.id,
          buildingId: f.building_id,
          name: f.name,
          ordinal: f.ordinal,
          plan: f.plan_key
            ? {
                url: `/api/v1/maps/floors/${f.id}/plan`,
                widthPx: f.plan_width_px ?? 0,
                heightPx: f.plan_height_px ?? 0,
              }
            : undefined,
        })),
      }));

      const zones: Zone[] = (res.zones ?? []).map((z) => ({
        id: z.id,
        siteId: z.site_id,
        floorId: z.floor_id ?? undefined,
        name: z.name,
        kind: z.kind as Zone["kind"],
        geometry: z.geometry as unknown as Polygon,
        style: (z.style as Zone["style"]) || {},
        metadata: (z.metadata as Record<string, unknown>) || {},
        ruleIds: [],
      }));

      return {
        id: res.id,
        name: res.name,
        lat: res.lat,
        lng: res.lng,
        defaultZoom: res.default_zoom,
        regionId: res.region_id,
        buildings,
        zones,
      };
    },
    enabled: !!siteId,
  });

export const siteEntitiesQuery = (siteId: string) =>
  queryOptions({
    queryKey: ["maps", "sites", siteId, "entities"],
    queryFn: async () => {
      const res = unwrap(
        await api.GET("/api/v1/maps/sites/{siteId}/entities", {
          params: { path: { siteId }, query: { is_geo: true } },
        }),
      );
      return {
        revision: res.revision,
        entities: res.entities.map(mapWireEntity),
      };
    },
    enabled: !!siteId,
    refetchInterval: 30_000,
  });

export const unplacedCamerasQuery = (siteId: string) =>
  queryOptions({
    queryKey: ["maps", "unplaced", siteId],
    queryFn: async () => {
      const res = unwrap(
        await api.GET("/api/v1/maps/unplaced", {
          params: { query: { site_id: siteId } },
        }),
      );
      return res.cameras;
    },
    enabled: !!siteId,
  });

export const siteZonesQuery = (siteId: string) =>
  queryOptions({
    queryKey: ["maps", "sites", siteId, "zones"],
    queryFn: async (): Promise<Zone[]> => {
      const res = unwrap(
        await api.GET("/api/v1/maps/sites/{siteId}/zones", {
          params: { path: { siteId } },
        }),
      );
      return res.zones.map((z) => ({
        id: z.id,
        siteId: z.site_id,
        floorId: z.floor_id ?? undefined,
        name: z.name,
        kind: z.kind as Zone["kind"],
        geometry: z.geometry as unknown as Polygon,
        style: (z.style as Zone["style"]) || {},
        metadata: (z.metadata as Record<string, unknown>) || {},
        ruleIds: [],
      }));
    },
    enabled: !!siteId,
  });

export interface MapAnalyticsParams {
  site_id?: string;
  camera_id?: string;
  zone_id?: string;
  metric?: "object" | "person" | "vehicle" | "motion" | "alarm" | "lpr";
  object_type?: string;
  start?: string;
  end?: string;
  coverage?: boolean;
}

export const mapAnalyticsQuery = (params: MapAnalyticsParams) =>
  queryOptions({
    queryKey: ["maps", "analytics", params],
    queryFn: async () => {
      const res = unwrap(
        await api.GET("/api/v1/maps/analytics", {
          params: { query: params },
        }),
      );
      return res;
    },
    enabled: !!params.site_id || !!params.camera_id,
    staleTime: 60_000,
  });

