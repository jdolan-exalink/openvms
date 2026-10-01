import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FiltersPanel } from "./FiltersPanel";
import type { MapFilters } from "@/lib/maps/types";

describe("FiltersPanel", () => {
  it("offers the filters the entity payload can actually answer", () => {
    render(<FiltersPanel filters={{}} onChange={() => {}} />);
    expect(screen.getByRole("checkbox", { name: "Offline" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "Con alarma" })).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "PTZ" })).toBeInTheDocument();
    expect(screen.queryByRole("checkbox", { name: "Personas" })).not.toBeInTheDocument();
  });

  it("adds and removes a status from the combinable filter", () => {
    const onChange = vi.fn<(next: MapFilters) => void>();
    const view = render(<FiltersPanel filters={{}} onChange={onChange} />);
    fireEvent.click(screen.getByRole("checkbox", { name: "Offline" }));
    expect(onChange.mock.calls[0]![0]).toEqual({ status: ["OFFLINE"] });

    view.rerender(<FiltersPanel filters={onChange.mock.calls[0]![0]} onChange={onChange} />);
    fireEvent.click(screen.getByRole("checkbox", { name: "Sin señal" }));
    expect(onChange.mock.calls[1]![0]).toEqual({ status: ["OFFLINE", "NO_SIGNAL"] });
  });

  it("removes a status that is already active", () => {
    const onChange = vi.fn<(next: MapFilters) => void>();
    render(<FiltersPanel filters={{ status: ["OFFLINE", "ONLINE"] }} onChange={onChange} />);
    expect(screen.getByRole("checkbox", { name: "Offline" })).toBeChecked();
    fireEvent.click(screen.getByRole("checkbox", { name: "Offline" }));
    expect(onChange.mock.calls[0]![0]).toEqual({ status: ["ONLINE"] });
  });

  it("offers alarm-only cameras as a display state", () => {
    const onChange = vi.fn<(next: MapFilters) => void>();
    render(<FiltersPanel filters={{}} onChange={onChange} />);
    fireEvent.click(screen.getByRole("checkbox", { name: "Con alarma" }));
    expect(onChange.mock.calls[0]![0]).toEqual({ status: ["ALARM"] });
  });

  it("keeps the other dimensions while one changes", () => {
    const onChange = vi.fn<(next: MapFilters) => void>();
    render(<FiltersPanel filters={{ camera_types: ["ptz"], status: ["OFFLINE"] }} onChange={onChange} />);
    expect(screen.getByRole("checkbox", { name: "PTZ" })).toBeChecked();
    fireEvent.click(screen.getByRole("checkbox", { name: "Fisheye" }));
    expect(onChange.mock.calls[0]![0]).toEqual({ camera_types: ["ptz", "fisheye"], status: ["OFFLINE"] });
  });

  it("clears every dimension at once", () => {
    const onChange = vi.fn<(next: MapFilters) => void>();
    render(<FiltersPanel filters={{ status: ["OFFLINE"], priority: ["alert"], camera_types: ["ptz"] }} onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Limpiar" }));
    expect(onChange.mock.calls[0]![0]).toEqual({});
  });
});
