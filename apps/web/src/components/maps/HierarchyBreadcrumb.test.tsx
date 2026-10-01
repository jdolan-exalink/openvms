import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { HierarchyBreadcrumb } from "./HierarchyBreadcrumb";

describe("Global map navigation", () => {
  it("offers all sites before a current site is selected, including unlocated sites", () => {
    const select = vi.fn();
    render(<HierarchyBreadcrumb sites={[{ id: "a", name: "Unlocated" }, { id: "b", name: "Other" }]} onSelectSite={select} />);
    fireEvent.change(screen.getByRole("combobox", { name: "Seleccionar sitio" }), { target: { value: "a" } });
    expect(select).toHaveBeenCalledWith("a");
    expect(screen.getByRole("option", { name: /Unlocated/ })).toBeInTheDocument();
  });
});
