import {
  Bell, Camera, Download, FileSearch, FolderLock, Gauge, History, KeyRound, LayoutGrid,
  ListVideo, MapPin, ScanLine, Server, Settings, ShieldCheck, UserCog, Users, UsersRound,
  type LucideIcon,
} from "lucide-react";

export type NavItem = {
  label: string;
  icon: LucideIcon;
  to?: string;
  /** Milestone that delivers this section, shown while it is not built yet. */
  milestone?: string;
  /** Shown only to users holding this permission somewhere; the API enforces it regardless. */
  permission?: string;
};

export type NavGroup = { title?: string; items: NavItem[] };

// Main sidebar from PRD §80: only operational pages plus one entry into the settings area.
export const navGroups: NavGroup[] = [
  { items: [{ label: "En vivo", icon: LayoutGrid, to: "/live", permission: "live.view" }] },
  {
    title: "Investigación",
    items: [
      { label: "Eventos", icon: ListVideo, to: "/events", permission: "events.view" },
      { label: "Patentes", icon: ScanLine, to: "/plates", permission: "lpr.view" },
      { label: "Grabaciones", icon: History, to: "/playback", permission: "recordings.view" },
      { label: "Exportaciones", icon: Download, to: "/exports" },
      { label: "Casos", icon: FolderLock, milestone: "M8" },
    ],
  },
  { items: [{ label: "Configuración", icon: Settings, to: "/settings" }] },
];

// Settings area sub-navigation: the former "Infraestructura"/"Administración" sidebar groups,
// plus a "Resumen" landing item that shows the former Panel/Dashboard content.
export const settingsNavGroups: NavGroup[] = [
  { items: [{ label: "Resumen", icon: Gauge, to: "/settings" }] },
  {
    title: "Infraestructura",
    items: [
      { label: "Sitios", icon: MapPin, to: "/sites" },
      { label: "Servidores", icon: Server, to: "/servers", permission: "servers.view" },
      { label: "Cámaras", icon: Camera, to: "/cameras", permission: "cameras.view" },
      { label: "Notificaciones", icon: Bell, milestone: "M8" },
    ],
  },
  {
    title: "Administración",
    items: [
      { label: "Usuarios", icon: Users, to: "/users", permission: "users.view" },
      { label: "Grupos", icon: UsersRound, to: "/groups", permission: "groups.view" },
      { label: "Permisos", icon: KeyRound, to: "/permissions", permission: "permissions.manage" },
      { label: "Auditoría", icon: FileSearch, to: "/audit", permission: "audit.view" },
      { label: "Mi cuenta", icon: UserCog, to: "/account" },
    ],
  },
];

export const brandIcon = ShieldCheck;
