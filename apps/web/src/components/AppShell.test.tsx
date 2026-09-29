import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppShell } from "./AppShell";

describe("AppShell", () => {
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
    expect(contextSidebar).toHaveClass("w-full", "md:w-64", "border-b", "md:border-b-0", "md:border-r");
  });
});
