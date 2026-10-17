import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AppShell, setContextSidebarCollapsed } from "./AppShell";

function stubDevice(phone: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({ matches: phone && query === "(pointer: coarse)", media: query, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal("screen", { width: phone ? 844 : 1920, height: phone ? 390 : 1080 }); // phone in landscape
}

describe("AppShell", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.unstubAllGlobals());

  it("marks phone devices so the shell keeps the mobile layout even in landscape", () => {
    stubDevice(true);
    const { container } = render(
      <AppShell primaryNav={<nav>Nav</nav>} bottomNav={<nav>Bottom</nav>}>
        <h1>Route</h1>
      </AppShell>,
    );
    expect(container.querySelector('[data-shell="openvms"]')).toHaveAttribute("data-device", "phone");
    expect(screen.getByLabelText("Primary Nav Rail").className).toMatch(/shell-md:flex/);
    expect(screen.getByLabelText("Primary Nav Rail").className).not.toMatch(/(^|\s)md:/);
  });

  it("does not mark desktops and tablets", () => {
    stubDevice(false);
    const { container } = render(
      <AppShell primaryNav={<nav>Nav</nav>}>
        <h1>Route</h1>
      </AppShell>,
    );
    expect(container.querySelector('[data-shell="openvms"]')).not.toHaveAttribute("data-device");
  });

  it("provides named primary navigation and main workspace regions", () => {
    render(
      <AppShell primaryNav={<nav>Existing navigation</nav>}>
        <h1>Current route</h1>
      </AppShell>,
    );

    expect(screen.getByLabelText("Primary Nav Rail")).toHaveClass("hidden", "shell-md:flex");
    expect(screen.getByRole("complementary", { name: "Primary Nav Rail" })).toHaveTextContent("Existing navigation");
    const main = screen.getByRole("main", { name: "Main Workspace" });
    expect(main).toContainElement(screen.getByRole("heading", { name: "Current route" }));
    expect(main).toHaveAttribute("id", "main-content");
    expect(screen.getByRole("link", { name: "Saltar al contenido" })).toHaveAttribute("href", "#main-content");
    expect(screen.queryByRole("complementary", { name: "Context Sidebar" })).not.toBeInTheDocument();
  });

  it("renders the bottom navigation slot and reserves room for it on mobile only", () => {
    const { container } = render(
      <AppShell primaryNav={<nav>Existing navigation</nav>} bottomNav={<nav data-shell-region="bottom-nav">Bottom</nav>}>
        <h1>Current route</h1>
      </AppShell>,
    );
    expect(container.querySelector('[data-shell="openvms"]')).not.toHaveClass("pl-16");
    expect(screen.getByRole("main", { name: "Main Workspace" }).className).toMatch(/pb-\[calc\(5rem\+env\(safe-area-inset-bottom\)\)\]/);
    expect(screen.getByRole("main", { name: "Main Workspace" })).toHaveClass("shell-md:rounded-m3-2xl", "shell-md:bg-surface");
    expect(screen.getByText("Bottom")).toBeInTheDocument();
  });

  it("keeps content out of the notch and side insets on the whole shell", () => {
    const { container } = render(
      <AppShell primaryNav={<nav>Nav</nav>}>
        <h1>Route</h1>
      </AppShell>,
    );
    expect(container.querySelector('[data-shell="openvms"]')).toHaveClass(
      "pt-[env(safe-area-inset-top)]",
      "pl-[env(safe-area-inset-left)]",
      "pr-[env(safe-area-inset-right)]",
    );
  });

  it("reserves a shorter bottom strip when the bottom navigation is compact", () => {
    render(
      <AppShell primaryNav={<nav>Nav</nav>} bottomNav={<nav>Bottom</nav>} compactBottomNav>
        <h1>Route</h1>
      </AppShell>,
    );
    const main = screen.getByRole("main", { name: "Main Workspace" });
    expect(main.className).toMatch(/pb-\[calc\(3\.5rem\+env\(safe-area-inset-bottom\)\)\]/);
    expect(main.className).not.toMatch(/calc\(5rem/);
  });

  it("keeps the raised sheet look free of margins in fit-viewport mode", () => {
    render(
      <AppShell primaryNav={<nav>Nav</nav>} fitViewport>
        <h1>Live</h1>
      </AppShell>,
    );
    const main = screen.getByRole("main", { name: "Main Workspace" });
    expect(main).toHaveClass("shell-md:rounded-m3-2xl", "shell-md:bg-surface");
    expect(main.className).not.toMatch(/shell-md:m[trbl]?-/);
  });

  it("renders the context sidebar only when the current route supplies context", () => {
    const { container } = render(
      <AppShell primaryNav={<nav>Existing navigation</nav>} contextSidebar={<div>Camera context</div>}>
        <h1>Current route</h1>
      </AppShell>,
    );

    const contextSidebar = screen.getByRole("complementary", { name: "Context Sidebar" });
    expect(contextSidebar).toHaveTextContent("Camera context");
    expect(container.querySelector('[data-shell="openvms"]')).toHaveClass("flex-col", "shell-md:flex-row");
    expect(contextSidebar).toHaveClass("w-full", "shell-md:w-(--sidebar-w)", "shell-md:overflow-x-hidden", "border-b", "shell-md:border-b-0", "shell-md:border-r");
  });

  it("collapses the context sidebar to zero width and hides it from assistive tech", () => {
    render(
      <AppShell primaryNav={<nav>Existing navigation</nav>} contextSidebar={<div>Camera context</div>}>
        <h1>Current route</h1>
      </AppShell>,
    );

    act(() => setContextSidebarCollapsed(true));
    const contextSidebar = document.querySelector('[data-shell-region="context-sidebar"]');
    expect(contextSidebar).toHaveAttribute("aria-hidden", "true");
    expect(contextSidebar).toHaveClass("shell-md:w-0", "shell-md:overflow-hidden");
    act(() => setContextSidebarCollapsed(false));
    expect(screen.getByRole("complementary", { name: "Context Sidebar" })).not.toHaveClass("shell-md:w-0");
  });

  it("exposes an accessible resize separator that resizes by keyboard, persists and resets on double click", () => {
    render(
      <AppShell primaryNav={<nav>Existing navigation</nav>} contextSidebar={<div>Camera context</div>}>
        <h1>Current route</h1>
      </AppShell>,
    );
    const handle = screen.getByRole("separator", { name: "Redimensionar barra lateral" });
    expect(handle).toHaveAttribute("aria-orientation", "vertical");
    expect(handle).toHaveAttribute("aria-valuemin", "256");
    expect(handle).toHaveAttribute("tabindex", "0");
    const start = Number(handle.getAttribute("aria-valuenow"));
    expect(start).toBeGreaterThanOrEqual(256);

    fireEvent.keyDown(handle, { key: "ArrowRight" });
    expect(handle).toHaveAttribute("aria-valuenow", String(start + 16));
    expect(localStorage.getItem("openvms.live.sidebar.width")).toBe(String(start + 16));
    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    expect(handle).toHaveAttribute("aria-valuenow", "256");

    fireEvent.keyDown(handle, { key: "ArrowRight" });
    fireEvent.doubleClick(handle);
    expect(handle).toHaveAttribute("aria-valuenow", "256");
  });

  it("keeps the last width while collapsed and removes the handle", () => {
    render(
      <AppShell primaryNav={<nav>Existing navigation</nav>} contextSidebar={<div>Camera context</div>}>
        <h1>Current route</h1>
      </AppShell>,
    );
    fireEvent.keyDown(screen.getByRole("separator"), { key: "ArrowRight" });
    act(() => setContextSidebarCollapsed(true));
    expect(screen.queryByRole("separator")).not.toBeInTheDocument();
    act(() => setContextSidebarCollapsed(false));
    expect(screen.getByRole("separator")).toHaveAttribute("aria-valuenow", "272");
  });
});
