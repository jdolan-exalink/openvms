import type { IconDefinition } from "@fortawesome/fontawesome-svg-core";
import {
  faBell, faBolt, faBriefcase, faBuilding, faCamera, faCar, faClipboardList, faClockRotateLeft, faDownload, faGaugeHigh,
  faKey, faLayerGroup, faMapLocationDot, faPalette, faPaperPlane, faServer, faShieldHalved, faSliders, faUserGear, faUserGroup, faUsers, faVideo,
} from "@fortawesome/free-solid-svg-icons";
import { ShieldCheck } from "lucide-react";
import type { MessageKey } from "@/i18n";
import type { FeatureFlags } from "@/lib/features";

export type NavItem = {
  label: MessageKey;
  icon: IconDefinition;
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
  { items: [{ label: "nav.live", icon: faVideo, to: "/live", permission: "live.view" }] },
  {
    title: "nav.investigation",
    items: [
      { label: "nav.maps", icon: faMapLocationDot, to: "/maps", permission: "maps.view", feature: "maps" },
      { label: "nav.alarms", icon: faBell, to: "/alarms", permission: "alarms.view" },
      { label: "nav.events", icon: faShieldHalved, to: "/events", permission: "events.view" },
      { label: "nav.plates", icon: faCar, to: "/plates", permission: "lpr.view" },
      { label: "nav.recordings", icon: faClockRotateLeft, to: "/playback", permission: "recordings.view" },
      { label: "nav.exports", icon: faDownload, to: "/exports" },
      { label: "nav.cases", icon: faBriefcase, milestone: "M8" },
    ],
  },
  { items: [{ label: "nav.settings", icon: faSliders, to: "/settings" }] },
];

// Settings area sub-navigation: the former "Infraestructura"/"Administración" sidebar groups,
// plus a "Resumen" landing item that shows the former Panel/Dashboard content.
export const settingsNavGroups: NavGroup[] = [
  { items: [{ label: "nav.overview", icon: faGaugeHigh, to: "/settings" }] },
  {
    title: "nav.infrastructure",
    items: [
      { label: "nav.sites", icon: faBuilding, to: "/sites" },
      { label: "nav.servers", icon: faServer, to: "/servers", permission: "servers.view" },
      { label: "nav.cameras", icon: faCamera, to: "/cameras", permission: "cameras.view" },
      { label: "nav.cameraGroups", icon: faLayerGroup, to: "/camera-groups", permission: "cameras.view" },
      { label: "nav.rules", icon: faBolt, to: "/rules", permission: "notifications.manage" },
      { label: "nav.channels", icon: faPaperPlane, to: "/channels", permission: "notifications.manage" },
      { label: "nav.notifications", icon: faBell, to: "/notifications" },
    ],
  },
  {
    title: "nav.administration",
    items: [
      { label: "nav.users", icon: faUsers, to: "/users", permission: "users.view" },
      { label: "nav.groups", icon: faUserGroup, to: "/groups", permission: "groups.view" },
      { label: "nav.permissions", icon: faKey, to: "/permissions", permission: "permissions.manage" },
      { label: "nav.audit", icon: faClipboardList, to: "/audit", permission: "audit.view" },
      { label: "nav.watermark", icon: faPalette, to: "/branding", permission: "tenant.manage" },
      { label: "nav.myAccount", icon: faUserGear, to: "/account" },
    ],
  },
];

export const brandIcon = ShieldCheck;
