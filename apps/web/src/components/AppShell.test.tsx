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

    expect(screen.getByRole("complementary", { name: "Primary Nav Rail" })).toHaveTextContent("Existing navigation");
    expect(screen.getByRole("main", { name: "Main Workspace" })).toContainElement(screen.getByRole("heading", { name: "Current route" }));
    expect(screen.queryByRole("complementary", { name: "Context Sidebar" })).not.toBeInTheDocument();
  });

  it("renders the context sidebar only when the current route supplies context", () => {
    render(
      <AppShell primaryNav={<nav>Existing navigation</nav>} contextSidebar={<div>Camera context</div>}>
        <h1>Current route</h1>
      </AppShell>,
    );

    expect(screen.getByRole("complementary", { name: "Context Sidebar" })).toHaveTextContent("Camera context");
  });
});
