import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { AppShell, setContextSidebarCollapsed } from "./AppShell";

describe("AppShell", () => {
  beforeEach(() => localStorage.clear());

  it("provides named primary navigation and main workspace regions", () => {
    render(
      <AppShell primaryNav={<nav>Existing navigation</nav>}>
        <h1>Current route</h1>
      </AppShell>,
    );

    expect(screen.getByLabelText("Primary Nav Rail")).toHaveClass("hidden", "md:flex");
    expect(screen.getByRole("complementary", { name: "Primary Nav Rail" })).toHaveTextContent("Existing navigation");
    const main = screen.getByRole("main", { name: "Main Workspace" });
    expect(main).toContainElement(screen.getByRole("heading", { name: "Current route" }));
    expect(main).toHaveAttribute("id", "main-content");
    expect(screen.getByRole("link", { name: "Saltar al contenido" })).toHaveAttribute("href", "#main-content");
    expect(screen.queryByRole("complementary", { name: "Context Sidebar" })).not.toBeInTheDocument();
  });

  it("renders the context sidebar only when the current route supplies context", () => {
    const { container } = render(
      <AppShell primaryNav={<nav>Existing navigation</nav>} contextSidebar={<div>Camera context</div>}>
        <h1>Current route</h1>
      </AppShell>,
    );

    const contextSidebar = screen.getByRole("complementary", { name: "Context Sidebar" });
    expect(contextSidebar).toHaveTextContent("Camera context");
    expect(container.querySelector('[data-shell="openvms"]')).toHaveClass("flex-col", "md:flex-row");
    expect(contextSidebar).toHaveClass("w-full", "md:w-(--sidebar-w)", "md:overflow-x-hidden", "border-b", "md:border-b-0", "md:border-r");
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
    expect(contextSidebar).toHaveClass("md:w-0", "md:overflow-hidden");
    act(() => setContextSidebarCollapsed(false));
    expect(screen.getByRole("complementary", { name: "Context Sidebar" })).not.toHaveClass("md:w-0");
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
