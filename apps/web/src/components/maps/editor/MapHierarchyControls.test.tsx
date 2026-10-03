import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { afterEach, it, expect, vi } from "vitest";
import { MapHierarchyControls } from "./MapHierarchyControls";
import { json, stubApi } from "@/test-utils";

const floor = { id: "f", building_id: "b", name: "Second floor", ordinal: 2, revision: 7 };
const site = { id: "s", name: "Site", zones: [], buildings: [{ id: "b", site_id: "s", name: "Plant", revision: 3, floors: [floor] }] };
afterEach(() => vi.unstubAllGlobals());

it("renames the selected map without resetting its ordinal and uses its exact revision", async () => {
  const handler = stubApi({ "/api/v1/maps/sites/s/floors/f": () => json(floor) });
  const fetch = vi.fn(handler);
  vi.stubGlobal("fetch", fetch);
  const saved = vi.fn();
  render(<MapHierarchyControls site={site} activeFloor={floor} disabled={false} onSaved={saved} onCreated={vi.fn()} onRemoved={vi.fn()} />);
  fireEvent.click(screen.getByRole("button", { name: "Mapas" }));
  fireEvent.click(screen.getByRole("button", { name: "Editar Second floor" }));
  fireEvent.change(screen.getByLabelText("Nombre del mapa"), { target: { value: "Renamed plant" } });
  fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
  await waitFor(() => expect(saved).toHaveBeenCalled());
  const request = fetch.mock.calls[0]![0] as Request;
  expect(request.method).toBe("PATCH");
  expect(request.headers.get("If-Match")).toBe("7");
  expect(await request.json()).toEqual({ name: "Renamed plant", ordinal: 2 });
});

it("creates an image map and shows a deletion refusal without mutation retries", async () => {
  const handler = stubApi({
    "POST /api/v1/maps/sites/s/buildings/b/floors": () => json({ ...floor, id: "new", name: "Third plant", ordinal: 3 }),
    "DELETE /api/v1/maps/sites/s/floors/new": () => json({ message: "Map contains active placements" }, 409),
  });
  const fetch = vi.fn(handler);
  vi.stubGlobal("fetch", fetch);
  const created = vi.fn();
  const removed = vi.fn();
  render(<MapHierarchyControls site={site} activeFloor={floor} disabled={false} onSaved={vi.fn()} onCreated={created} onRemoved={removed} />);
  fireEvent.click(screen.getByRole("button", { name: "Mapas" }));
  fireEvent.click(screen.getByRole("button", { name: "Nuevo mapa" }));
  fireEvent.click(screen.getByRole("radio", { name: /Fondo de imagen/ }));
  fireEvent.change(screen.getByLabelText("Nombre del mapa"), { target: { value: "Third plant" } });
  fireEvent.click(screen.getByRole("button", { name: "Crear mapa" }));
  await waitFor(() => expect(created).toHaveBeenCalledWith("new"));
  const body = await (fetch.mock.calls[0]![0] as Request).json();
  expect(body).toEqual({ name: "Third plant", ordinal: 3 });
  fireEvent.click(screen.getByRole("button", { name: "Borrar" }));
  fireEvent.click(screen.getByRole("button", { name: "Eliminar mapa" }));
  await screen.findByText("Map contains active placements");
  expect(fetch).toHaveBeenCalledTimes(2);
  expect(removed).not.toHaveBeenCalled();
});

it("opens the geographic map when the new map is a real map", () => {
  const openGeo = vi.fn();
  render(<MapHierarchyControls site={site} disabled={false} onSaved={vi.fn()} onCreated={vi.fn()} onRemoved={vi.fn()} onOpenGeographic={openGeo} />);
  fireEvent.click(screen.getByRole("button", { name: "Mapas" }));
  fireEvent.click(screen.getByRole("button", { name: "Nuevo mapa" }));
  fireEvent.click(screen.getByRole("radio", { name: /Mapa real/ }));
  fireEvent.click(screen.getByRole("button", { name: "Usar mapa real" }));
  expect(openGeo).toHaveBeenCalledOnce();
  expect(screen.queryByRole("dialog", { name: "Mapas" })).not.toBeInTheDocument();
});
