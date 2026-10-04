import { render } from "@testing-library/react";
import { createElement } from "react";
import { describe, expect, it } from "vitest";
import { Icon } from "./Icon";
import navSource from "./nav.ts?raw";
import { brandIcon, navGroups, settingsNavGroups } from "./nav";

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
