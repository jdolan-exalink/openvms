import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LayersPanel } from "./LayersPanel";
import { DEFAULT_LAYER_PREFERENCE, type LayerPreference } from "@/lib/maps/types";

describe("LayersPanel", () => {
  it("renders every layer group the map actually draws", () => {
    render(<LayersPanel layers={DEFAULT_LAYER_PREFERENCE} onChange={() => {}} />);
    for (const name of ["Sitios", "Cámaras", "Conos FOV", "Alarmas", "Detecciones"]) {
      expect(screen.getByRole("checkbox", { name })).toBeInTheDocument();
    }
  });

  it("does not offer toggles for groups that have no layer yet", () => {
    render(<LayersPanel layers={DEFAULT_LAYER_PREFERENCE} onChange={() => {}} />);
    expect(screen.queryByRole("checkbox", { name: "Heatmap" })).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Caras" })).not.toBeInTheDocument();
    expect(screen.getByText(/Fase 2/)).toBeInTheDocument();
  });

  it("shows the stored state, not the defaults", () => {
    render(<LayersPanel layers={{ ...DEFAULT_LAYER_PREFERENCE, sites: false, events_alarm: false }} onChange={() => {}} />);
    expect(screen.getByRole("checkbox", { name: "Sitios" })).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Alarmas" })).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: "Cámaras" })).toBeChecked();
  });

  it("reports a single, complete preference set on change", () => {
    const onChange = vi.fn<(next: LayerPreference) => void>();
    render(<LayersPanel layers={DEFAULT_LAYER_PREFERENCE} onChange={onChange} />);
    fireEvent.click(screen.getByRole("checkbox", { name: "Conos FOV" }));
    expect(onChange).toHaveBeenCalledTimes(1);
    const next = onChange.mock.calls[0]![0];
    expect(next.coverage).toBe(false);
    expect(next).toEqual({ ...DEFAULT_LAYER_PREFERENCE, coverage: false });
  });

  it("closes on demand", () => {
    const onClose = vi.fn();
    render(<LayersPanel layers={DEFAULT_LAYER_PREFERENCE} onChange={() => {}} onClose={onClose} />);
    fireEvent.click(screen.getByRole("button", { name: "Close layers panel" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
