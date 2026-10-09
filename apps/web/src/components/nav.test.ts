import { render } from "@testing-library/react";
import { createElement } from "react";
import { describe, expect, it } from "vitest";
import { Icon } from "./Icon";
import navSource from "./nav.ts?raw";
import type { Schemas } from "@/api/client";
import { brandIcon, bottomNavModel, isNavItemActive, navGroups, settingsNavGroups } from "./nav";

const items = [...navGroups, ...settingsNavGroups].flatMap((group) => group.items);

describe("nav icons", () => {
  it("has items to check", () => {
    expect(items.length).toBeGreaterThan(10);
  });

  it.each(items.map((item) => [item.label, item] as const))("%s renders a lucide svg through Icon", (_label, item) => {
    const { container } = render(createElement(Icon, { icon: item.icon }));
    const svg = container.querySelector("svg");
    expect(svg).not.toBeNull();
    expect(svg?.classList.contains("lucide")).toBe(true);
    expect(svg?.getAttribute("data-icon")).toBeNull();
  });

  it("renders the brand icon through Icon", () => {
    const { container } = render(createElement(Icon, { icon: brandIcon }));
    expect(container.querySelector("svg.lucide")).not.toBeNull();
  });

  it("does not reference FontAwesome", () => {
    expect(navSource).not.toMatch(/fortawesome/i);
  });
});

const grant = (permission: string) => ({ permission, effect: "allow" as const, scope_type: "platform" as const });
const meWith = (...permissions: string[]) => ({ id: "u1", username: "u", display_name: "U", tenant_id: "t1", mfa_enabled: false, must_change_password: false, auth_method: "session", grants: permissions.map(grant) }) as unknown as Schemas["Me"];
const allFeatures = { maps: true } as never;

describe("bottomNavModel", () => {
  it("prefers Live, Maps, Events and Plates when permitted, leaving Servers under More", () => {
    const me = meWith("live.view", "maps.view", "events.view", "lpr.view", "servers.view", "alarms.view");
    const { primary, more } = bottomNavModel(me, allFeatures);
    expect(primary.map((item) => item.to)).toEqual(["/live", "/maps", "/events", "/plates"]);
    expect(more.flatMap((group) => group.items).map((item) => item.to)).toContain("/servers");
  });

  it("fills with the first permitted operational destinations, caps at four and keeps settings in more", () => {
    const { primary, more } = bottomNavModel(meWith("events.view", "alarms.view", "lpr.view", "recordings.view"), allFeatures);
    expect(primary.map((item) => item.to)).toEqual(["/events", "/plates", "/alarms", "/playback"]);
    expect(more.flatMap((group) => group.items).map((item) => item.to)).toContain("/settings");
  });

  it("never exposes items the user lacks permission for, and keeps the rest in more", () => {
    const me = meWith("events.view");
    const model = bottomNavModel(me, { maps: false } as never);
    const all = [...model.primary, ...model.more.flatMap((group) => group.items)].map((item) => item.to);
    expect(all).not.toContain("/live");
    expect(all).not.toContain("/maps");
    expect(all).not.toContain("/servers");
    expect(all).toContain("/settings");
    expect(model.more.flatMap((group) => group.items).map((item) => item.to)).not.toContain("/events");
    expect(new Set(all.filter(Boolean)).size).toBe(all.filter(Boolean).length);
  });
});

describe("isNavItemActive", () => {
  const byTo = (to: string) => navGroups.flatMap((group) => group.items).find((item) => item.to === to)!;
  it("matches nested routes and treats settings sub-routes as settings", () => {
    expect(isNavItemActive(byTo("/events"), "/events/123")).toBe(true);
    expect(isNavItemActive(byTo("/settings"), "/sites")).toBe(true);
    expect(isNavItemActive(byTo("/live"), "/live")).toBe(true);
    expect(isNavItemActive(byTo("/live"), "/events")).toBe(false);
  });
});
