import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ZonesPanel } from "./ZonesPanel";
import { emptyZoneDraft, type ZoneDraft } from "@/lib/maps/zoneDraft";
import type { Zone } from "@/lib/maps/types";

const zone = (over: Partial<Zone> = {}): Zone => ({
  id: "z1",
  siteId: "s",
  name: "acceso",
  kind: "security",
  geometry: {
    type: "Polygon",
    coordinates: [[[-64.19, -31.42], [-64.18, -31.42], [-64.185, -31.415], [-64.19, -31.42]]],
  },
  style: {},
  metadata: {},
  ruleIds: [],
  ...over,
});

const validDraft = (): ZoneDraft => ({
  zoneId: "z1",
  name: "acceso",
  kind: "security",
  points: [
    { lng: -64.19, lat: -31.42 },
    { lng: -64.18, lat: -31.42 },
    { lng: -64.185, lat: -31.415 },
  ],
  closed: true,
});

const base = {
  zones: [zone(), zone({ id: "z2", name: "deposito", kind: "perimeter" })],
  canEdit: true,
  onStartCreate: vi.fn(),
  onSelectZone: vi.fn(),
  onDraftChange: vi.fn(),
  onClosePolygon: vi.fn(),
  onReopenPolygon: vi.fn(),
  onUndoPoint: vi.fn(),
  onSave: vi.fn(),
  onDelete: vi.fn(),
  onCancel: vi.fn(),
};

describe("ZonesPanel", () => {
  it("lists the site zones with their kind", () => {
    render(<ZonesPanel {...base} />);
    expect(screen.getByRole("region", { name: "Zonas" })).toHaveTextContent("Zonas (2)");
    expect(screen.getByText("acceso")).toBeInTheDocument();
    expect(screen.getByText("perímetro")).toBeInTheDocument();
  });

  it("offers no write action without the zone permission", () => {
    render(<ZonesPanel {...base} canEdit={false} />);
    expect(screen.queryByRole("button", { name: "Nueva zona" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Editar" })).not.toBeInTheDocument();
    expect(screen.getByText("acceso")).toBeInTheDocument();
  });

  it("blocks the save until the draft is something the backend accepts", () => {
    const onSave = vi.fn();
    render(<ZonesPanel {...base} draft={{ ...emptyZoneDraft(), points: [{ lng: 1, lat: 2 }, { lng: 3, lat: 4 }] }} onSave={onSave} />);
    expect(screen.getByText("Se necesitan al menos 3 puntos.")).toBeInTheDocument();
    expect(screen.getByText("El nombre es obligatorio.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Guardar" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(onSave).not.toHaveBeenCalled();
  });

  it("drives the drawing flow the operator performs on the map", () => {
    const { onStartCreate, onClosePolygon, onReopenPolygon, onUndoPoint, onSave, onDraftChange, onCancel } = base;
    const view = render(<ZonesPanel {...base} draft={validDraft()} />);
    fireEvent.click(screen.getByRole("button", { name: "Nueva zona" }));
    expect(onStartCreate).toHaveBeenCalledTimes(1);

    expect(screen.getByText("Puntos: 3")).toBeInTheDocument();
    expect(screen.getByLabelText("Nombre de la zona")).toHaveValue("acceso");
    expect(screen.getByLabelText("Tipo de zona")).toHaveValue("security");

    fireEvent.click(screen.getByRole("button", { name: "Deshacer punto" }));
    expect(onUndoPoint).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Reabrir polígono" }));
    expect(onReopenPolygon).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(onSave).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Cancelar" }));
    expect(onCancel).toHaveBeenCalledTimes(1);
    view.unmount();

    render(<ZonesPanel {...base} draft={{ ...validDraft(), closed: false }} />);
    fireEvent.click(screen.getByRole("button", { name: "Cerrar polígono" }));
    expect(onClosePolygon).toHaveBeenCalledTimes(1);

    fireEvent.change(screen.getByLabelText("Nombre de la zona"), { target: { value: "deposito" } });
    expect(onDraftChange).toHaveBeenLastCalledWith({ name: "deposito" });
  });

  it("edits and deletes an existing zone", () => {
    const { onSelectZone, onDelete } = base;
    render(<ZonesPanel {...base} />);
    fireEvent.click(screen.getAllByRole("button", { name: "Editar" })[0]!);
    expect(onSelectZone).toHaveBeenCalledWith("z1");
    fireEvent.click(screen.getAllByRole("button", { name: "Eliminar" })[1]!);
    expect(onDelete).toHaveBeenCalledWith("z2");
  });
});
