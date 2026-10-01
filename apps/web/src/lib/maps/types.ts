import type { Polygon } from "geojson";
import type { Schemas } from "@/api/client";

export type EntityType =
  | "camera"
  | "server"
  | "device"
  | "sensor"
  | "door"
  | "alarm_point"
  | "lpr"
  | "label"
  | "building"
  | "custom";

export type GeoPosition = { kind: "geo"; lat: number; lng: number };
export type FloorPosition = { kind: "floor"; floorId: string; x: number; y: number }; // x,y in [0,1]
export type MapPosition = GeoPosition | FloorPosition;

export type CameraDisplayState =
  | "ONLINE"
  | "DEGRADED"
  | "OFFLINE"
  | "NO_SIGNAL"
  | "RECORDING_ERROR"
  | "UNREACHABLE"
  | "ALARM";

export interface MapProviderConfig {
  id: string;
  kind: "vector-style" | "pmtiles" | "raster";
  styleUrl?: { light?: string; dark?: string };
  tiles?: string[];
  attribution: string;
  maxZoom: number;
  offline: boolean;
}

export interface MapDefaultCenter {
  lat: number;
  lng: number;
}

export interface MapConfig {
  provider: MapProviderConfig;
  defaultCenter: MapDefaultCenter;
  defaultZoom: number;
}

export interface MapEntity {
  id: string;
  type: EntityType;
  siteId: string;
  serverId?: string;
  name: string;
  position: MapPosition;
  status: "unknown" | "online" | "degraded" | "offline";
  metadata: Record<string, unknown>;
  /** Placement revision read from the site entities endpoint (the editor's If-Match token). */
  revision?: number;
}

export interface CameraMapProps {
  bearingDeg: number | null;
  fovDeg: number;
  rangeM: number;
  cameraType: "fixed" | "dome" | "ptz" | "fisheye" | "lpr";
  ptz: boolean;
  lpr: boolean;
}

export interface CameraEntity extends MapEntity {
  type: "camera";
  camera: CameraMapProps;
  activeAlarms: number;
}

export interface SiteHealth {
  online: number;
  offline: number;
  degraded: number;
  activeAlarms: number;
  severity: "OK" | "WARNING" | "CRITICAL";
}

export interface Site {
  id: string;
  name: string;
  regionId?: string;
  regionName?: string;
  center?: GeoPosition;
  defaultZoom?: number;
  health?: SiteHealth;
  cameraCount?: number;
}

export interface FloorGeoreference {
  origin: GeoPosition;
  rotationDeg: number;
  metersPerPx: number;
}

export interface FloorPlan {
  url: string;
  widthPx: number;
  heightPx: number;
  metersPerPx?: number;
  georef?: FloorGeoreference;
}

export interface Floor {
  id: string;
  buildingId: string;
  name: string;
  ordinal: number;
  plan?: FloorPlan;
}

export interface Building {
  id: string;
  siteId: string;
  name: string;
  footprint?: Polygon;
  floors: Floor[];
}

/** ZoneKind mirrors the contract's map-zone enum (openapi.yaml: MapZone.kind). */
export type ZoneKind = "security" | "perimeter" | "warning" | "custom";

export interface Zone {
  id: string;
  siteId: string;
  floorId?: string;
  name: string;
  kind: ZoneKind;
  geometry: Polygon;
  style: {
    color?: string;
    opacity?: number;
    pattern?: "solid" | "hatched";
  };
  metadata: Record<string, unknown>;
  ruleIds: string[];
}

/**
 * Layer toggles, expressed with the contract's own keys so preferences travel to
 * `/me/map-prefs` without a conversion layer to keep in sync. Every key has a default, so
 * `LayerPreference` is the complete set the UI renders from.
 */
export type LayerPreference = Required<Schemas["MapLayerPreference"]>;

export const DEFAULT_LAYER_PREFERENCE: LayerPreference = {
  sites: true,
  cameras: true,
  coverage: true,
  ptz_direction: false,
  ai_person: true,
  ai_vehicle: true,
  lpr: true,
  faces: false,
  events_alarm: true,
  events_motion: true,
  events_audio: false,
  infra_servers: true,
  infra_network: false,
  infra_access: false,
  infra_sensors: false,
  heatmap: false,
  traffic: false,
};

/** Combinable map filters. Absent dimensions do not restrict anything. */
export type MapFilters = Schemas["MapFilters"];

/** Client-owned default: an operator who never filtered anything draws every camera. */
export const EMPTY_FILTERS: MapFilters = {};

export interface SavedMapView {
  id: string;
  name: string;
  shared: boolean;
  ownerId: string;
  state: {
    center: [number, number];
    zoom: number;
    bearing: number;
    siteId?: string;
    floorId?: string;
    layers: LayerPreference;
    filters: MapFilters;
    zoneIds?: string[];
  };
}

export type AutoFocusPolicy = "none" | "highlight" | "center" | "center_zoom" | "center_preview" | "incident_mode";

export interface RealtimeFrame<T = unknown> {
  v: 2;
  id: string;
  type: string;
  ts: string;
  tenantId: string;
  siteId?: string;
  cameraId?: string;
  serverId?: string;
  data: T;
}

export type MapMode = "live" | "investigate" | "analytics" | "edit";
