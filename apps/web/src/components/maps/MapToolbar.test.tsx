import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { MapToolbar } from "./MapToolbar";

describe("MapToolbar", () => {
  it("renders modes and excludes editor when canEdit is false", () => {
    const onModeChange = vi.fn();
    render(
      <MapToolbar
        mode="live"
        onModeChange={onModeChange}
        canEdit={false}
      />,
    );

    const live = screen.getByRole("tab", { name: /en vivo/i });
    expect(live).toBeInTheDocument();
    expect(live.querySelector("[data-live-led='on']")).toBeTruthy();
    expect(screen.getByRole("tab", { name: /investigar/i })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /analítica/i })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: /editor/i })).not.toBeInTheDocument();
  });

  it("renders editor mode when canEdit is true and triggers onModeChange", () => {
    const onModeChange = vi.fn();
    render(
      <MapToolbar
        mode="live"
        onModeChange={onModeChange}
        canEdit={true}
      />,
    );

    const editTab = screen.getByRole("tab", { name: /editor/i });
    expect(editTab).toBeInTheDocument();

    fireEvent.click(editTab);
    expect(onModeChange).toHaveBeenCalledWith("edit");
  });

  it("triggers layer and filter toggles", () => {
    const onToggleLayers = vi.fn();
    const onToggleFilters = vi.fn();
    render(
      <MapToolbar
        mode="live"
        onModeChange={vi.fn()}
        onToggleLayers={onToggleLayers}
        onToggleFilters={onToggleFilters}
      />,
    );

    fireEvent.click(screen.getByTitle("Capas del mapa"));
    expect(onToggleLayers).toHaveBeenCalledOnce();

    fireEvent.click(screen.getByTitle("Filtros"));
    expect(onToggleFilters).toHaveBeenCalledOnce();
  });
});
