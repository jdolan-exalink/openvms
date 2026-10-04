import {
  Bell, BellRing, Building, Camera, CarFront, ClipboardList, Download, GitBranch, History, KeyRound, LayoutDashboard, Layers, Map as MapIcon,
  Palette, Send, Server, ShieldCheck, SlidersHorizontal, UserCog, Users, UsersRound, Video, Zap, BriefcaseBusiness,
  type LucideIcon,
} from "lucide-react";
import type { MessageKey } from "@/i18n";
import type { Schemas } from "@/api/client";
import type { FeatureFlags } from "@/lib/features";
import { can } from "@/lib/perm";

export type NavItem = {
  label: MessageKey;
  icon: LucideIcon;
  to?: string;
  /** Milestone that delivers this section, shown while it is not built yet. */
  milestone?: string;
  /** Shown only to users holding this permission somewhere; the API enforces it regardless. */
  permission?: string;
  /** Rollout feature flag gating visibility. */
  feature?: keyof FeatureFlags;
};

export type NavGroup = { title?: MessageKey; items: NavItem[] };

// Main sidebar from PRD §80: only operational pages plus one entry into the settings area.
export const navGroups: NavGroup[] = [
  { items: [{ label: "nav.live", icon: Video, to: "/live", permission: "live.view" }] },
  {
    title: "nav.investigation",
    items: [
      { label: "nav.maps", icon: MapIcon, to: "/maps", permission: "maps.view", feature: "maps" },
      { label: "nav.alarms", icon: Bell, to: "/alarms", permission: "alarms.view" },
      { label: "nav.events", icon: Zap, to: "/events", permission: "events.view" },
      { label: "nav.plates", icon: CarFront, to: "/plates", permission: "lpr.view" },
      { label: "nav.recordings", icon: History, to: "/playback", permission: "recordings.view" },
      { label: "nav.exports", icon: Download, to: "/exports" },
      { label: "nav.cases", icon: BriefcaseBusiness, milestone: "M8" },
    ],
  },
  { items: [{ label: "nav.settings", icon: SlidersHorizontal, to: "/settings" }] },
];

// Settings area sub-navigation: the former "Infraestructura"/"Administración" sidebar groups,
// plus a "Resumen" landing item that shows the former Panel/Dashboard content.
export const settingsNavGroups: NavGroup[] = [
  { items: [{ label: "nav.overview", icon: LayoutDashboard, to: "/settings" }] },
  {
    title: "nav.infrastructure",
    items: [
      { label: "nav.sites", icon: Building, to: "/sites" },
      { label: "nav.servers", icon: Server, to: "/servers", permission: "servers.view" },
      { label: "nav.cameras", icon: Camera, to: "/cameras", permission: "cameras.view" },
      { label: "nav.cameraGroups", icon: Layers, to: "/camera-groups", permission: "cameras.view" },
      { label: "nav.rules", icon: GitBranch, to: "/rules", permission: "notifications.manage" },
      { label: "nav.channels", icon: Send, to: "/channels", permission: "notifications.manage" },
      { label: "nav.notifications", icon: BellRing, to: "/notifications" },
    ],
  },
  {
    title: "nav.administration",
    items: [
      { label: "nav.users", icon: Users, to: "/users", permission: "users.view" },
      { label: "nav.groups", icon: UsersRound, to: "/groups", permission: "groups.view" },
      { label: "nav.permissions", icon: KeyRound, to: "/permissions", permission: "permissions.manage" },
      { label: "nav.audit", icon: ClipboardList, to: "/audit", permission: "audit.view" },
      { label: "nav.watermark", icon: Palette, to: "/branding", permission: "tenant.manage" },
      { label: "nav.myAccount", icon: UserCog, to: "/account" },
    ],
  },
];

export const brandIcon = ShieldCheck;

/** Permission and rollout-flag filter shared by the rail, the bottom bar and the more sheet. */
export function isNavItemVisible(item: NavItem, me: Schemas["Me"] | undefined, features: FeatureFlags): boolean {
  return (!item.permission || can(me, item.permission)) && (!item.feature || !!features[item.feature]);
}

/** Settings sub-routes keep the single "Settings" destination active; /live only matches exactly. */
export function isNavItemActive(item: NavItem, pathname: string): boolean {
  if (item.to == null) return false;
  if (pathname === item.to) return true;
  if (item.to === "/settings") {
    return settingsNavGroups.some((group) => group.items.some((sub) => sub.to && (pathname === sub.to || pathname.startsWith(`${sub.to}/`))));
  }
  return item.to !== "/live" && pathname.startsWith(`${item.to}/`);
}

const PREFERRED_PRIMARY = ["/live", "/maps", "/events", "/plates"];
const MAX_PRIMARY = 4;

/**
 * Mobile destinations: up to four primary entries (preferred ones first, then the first other
 * permitted operational ones) and every remaining permitted destination, settings area included,
 * grouped for the "More" sheet.
 */
export function bottomNavModel(me: Schemas["Me"] | undefined, features: FeatureFlags): { primary: NavItem[]; more: NavGroup[] } {
  const seen = new Set<string>();
  const groups: NavGroup[] = [];
  for (const group of [...navGroups, ...settingsNavGroups]) {
    const items = group.items.filter((item) => {
      if (!isNavItemVisible(item, me, features)) return false;
      if (!item.to) return true;
      if (seen.has(item.to)) return false;
      seen.add(item.to);
      return true;
    });
    if (items.length) groups.push({ title: group.title, items });
  }
  const linkable = groups.flatMap((group) => group.items).filter((item) => item.to);
  const preferred = PREFERRED_PRIMARY.flatMap((to) => linkable.filter((item) => item.to === to));
  // Fill only from the operational groups so Settings and its sub-pages stay behind "More".
  const operational = linkable.filter((item) => navGroups.some((group) => group.items.includes(item)) && item.to !== "/settings");
  const primary = [...preferred, ...operational.filter((item) => !preferred.includes(item))].slice(0, MAX_PRIMARY);
  const more = groups
    .map((group) => ({ title: group.title, items: group.items.filter((item) => !primary.includes(item)) }))
    .filter((group) => group.items.length);
  return { primary, more };
}
