import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MobileNav } from "./MobileNav";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children, ...props }: { to: string; children: React.ReactNode }) => <a href={to} {...props}>{children}</a>,
}));

const features = { alarms: true } as never;
const me = { id: "u1", grants: [{ permission: "live.view", effect: "allow", scope_type: "platform" }] } as never;

describe("MobileNav", () => {
  it("respects the side insets of the device", () => {
    render(<MobileNav me={me} features={features} pathname="/events" />);
    const region = screen.getByRole("navigation", { name: "Navegación móvil" }).closest('[data-shell-region="bottom-nav"]');
    expect(region).toHaveClass("pl-[env(safe-area-inset-left)]", "pr-[env(safe-area-inset-right)]", "pb-[env(safe-area-inset-bottom)]");
  });

  it("uses a taller bar by default and a shorter one when compact, keeping 44px targets", () => {
    const { rerender } = render(<MobileNav me={me} features={features} pathname="/events" />);
    expect(screen.getByRole("navigation", { name: "Navegación móvil" })).toHaveClass("h-20");
    rerender(<MobileNav me={me} features={features} pathname="/live" compact />);
    const bar = screen.getByRole("navigation", { name: "Navegación móvil" });
    expect(bar).toHaveClass("h-14");
    expect(bar).not.toHaveClass("h-20");
    expect(screen.getByRole("button", { name: "Más" })).toHaveClass("min-h-11");
  });

  it("hides the bar only on the desktop shell, so phones in landscape keep it", () => {
    render(<MobileNav me={me} features={features} pathname="/events" />);
    const region = screen.getByRole("navigation", { name: "Navegación móvil" }).closest('[data-shell-region="bottom-nav"]');
    expect(region).toHaveClass("shell-md:hidden");
    expect(region).not.toHaveClass("md:hidden");
  });
});
