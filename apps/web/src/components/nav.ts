import type { IconDefinition } from "@fortawesome/fontawesome-svg-core";
import {
  faBell, faBolt, faBriefcase, faBuilding, faCamera, faCar, faClipboardList, faClockRotateLeft, faDownload, faGaugeHigh,
  faKey, faLayerGroup, faMapLocationDot, faPalette, faPaperPlane, faServer, faShieldHalved, faSliders, faUserGear, faUserGroup, faUsers, faVideo,
} from "@fortawesome/free-solid-svg-icons";
import { ShieldCheck } from "lucide-react";
import type { FeatureFlags } from "@/lib/features";

export type NavItem = {
  label: string;
  icon: IconDefinition;
  to?: string;
  /** Milestone that delivers this section, shown while it is not built yet. */
  milestone?: string;
  /** Shown only to users holding this permission somewhere; the API enforces it regardless. */
  permission?: string;
  /** Rollout feature flag gating visibility. */
  feature?: keyof FeatureFlags;
};

export type NavGroup = { title?: string; items: NavItem[] };

// Main sidebar from PRD §80: only operational pages plus one entry into the settings area.
export const navGroups: NavGroup[] = [
  { items: [{ label: "En vivo", icon: faVideo, to: "/live", permission: "live.view" }] },
  {
    title: "Investigación",
    items: [
      { label: "Mapas", icon: faMapLocationDot, to: "/maps", permission: "maps.view", feature: "maps" },
      { label: "Alarmas", icon: faBell, to: "/alarms", permission: "alarms.view" },
      { label: "Eventos", icon: faShieldHalved, to: "/events", permission: "events.view" },
      { label: "Patentes", icon: faCar, to: "/plates", permission: "lpr.view" },
      { label: "Grabaciones", icon: faClockRotateLeft, to: "/playback", permission: "recordings.view" },
      { label: "Exportaciones", icon: faDownload, to: "/exports" },
      { label: "Casos", icon: faBriefcase, milestone: "M8" },
    ],
  },
  { items: [{ label: "Configuración", icon: faSliders, to: "/settings" }] },
];

// Settings area sub-navigation: the former "Infraestructura"/"Administración" sidebar groups,
// plus a "Resumen" landing item that shows the former Panel/Dashboard content.
export const settingsNavGroups: NavGroup[] = [
  { items: [{ label: "Resumen", icon: faGaugeHigh, to: "/settings" }] },
  {
    title: "Infraestructura",
    items: [
      { label: "Sitios", icon: faBuilding, to: "/sites" },
      { label: "Servidores", icon: faServer, to: "/servers", permission: "servers.view" },
      { label: "Cámaras", icon: faCamera, to: "/cameras", permission: "cameras.view" },
      { label: "Grupos de cámaras", icon: faLayerGroup, to: "/camera-groups", permission: "cameras.view" },
      { label: "Reglas", icon: faBolt, to: "/rules", permission: "notifications.manage" },
      { label: "Canales", icon: faPaperPlane, to: "/channels", permission: "notifications.manage" },
      { label: "Notificaciones", icon: faBell, to: "/notifications" },
    ],
  },
  {
    title: "Administración",
    items: [
      { label: "Usuarios", icon: faUsers, to: "/users", permission: "users.view" },
      { label: "Grupos", icon: faUserGroup, to: "/groups", permission: "groups.view" },
      { label: "Permisos", icon: faKey, to: "/permissions", permission: "permissions.manage" },
      { label: "Auditoría", icon: faClipboardList, to: "/audit", permission: "audit.view" },
      { label: "Marca de agua", icon: faPalette, to: "/branding", permission: "tenant.manage" },
      { label: "Mi cuenta", icon: faUserGear, to: "/account" },
    ],
  },
];

export const brandIcon = ShieldCheck;
