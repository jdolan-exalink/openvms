import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { CameraChangeProvider, useCameraChange } from "@/components/camera/CameraChangeProvider";
import { json, stubApi } from "@/test-utils";
import { DetectionTab } from "./DetectionTab";
import { sharedFilter, withFilter } from "./DetectionSettings";

afterEach(() => vi.unstubAllGlobals());

const catalog = {
  engine: "frigate",
  categories: [
    { id: "people", label: "Personas", objects: [{ id: "person", label: "Persona", requires_model: false, supported: true }] },
    {
      id: "vehicle",
      label: "Vehículos",
      objects: [
        { id: "vehicle.car", label: "Auto", requires_model: false, supported: true },
        { id: "vehicle.motorcycle", label: "Moto", requires_model: false, supported: null },
        { id: "vehicle.pickup", label: "Camioneta", requires_model: false, supported: false },
      ],
    },
  ],
};

function PatchProbe() {
  return <pre data-testid="patch">{JSON.stringify(useCameraChange().patch)}</pre>;
}

function mount(model: Record<string, unknown> = {}, props: Partial<React.ComponentProps<typeof DetectionTab>> = {}) {
  vi.stubGlobal("fetch", vi.fn(stubApi({
    "/api/v1/cameras/c1/normalized-config": () => json({ detection: { enabled: true, objects: [], selection: ["person"], ...model }, state: { desired_revision: 1, applied_revision: 1, sync_state: "synced", last_error: "", drift: [] } }),
    "/api/v1/camera-config/object-catalog": () => json(catalog),
  })));
  const onMotion = vi.fn(), onObjects = vi.fn(), onOpenZones = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <CameraChangeProvider cameraId="c1">
        <DetectionTab serverId="s1" settings={{ motion: { threshold: 30 }, objects: { track: ["person"], filters: { person: { min_score: 0.6 } } }, zones: ["patio"] }} onMotion={onMotion} onObjects={onObjects} onOpenZones={onOpenZones} legacy={<p>editores de Frigate</p>} {...props} />
        <PatchProbe />
      </CameraChangeProvider>
    </QueryClientProvider>,
  );
  return { onMotion, onObjects, onOpenZones };
}

const patch = () => JSON.parse(screen.getByTestId("patch").textContent ?? "{}");

describe("DetectionTab", () => {
  it("toggles a whole category with the model-unsupported objects left out", async () => {
    mount();
    fireEvent.click(await screen.findByLabelText("Activar todos los vehículos"));
    expect(patch()).toEqual({ detection: { selection: ["person", "vehicle.car", "vehicle.motorcycle"] } });
    fireEvent.click(screen.getByLabelText("Activar todos los vehículos"));
    expect(patch()).toEqual({ detection: { selection: ["person"] } });
  });

  it("toggles one subtype and sends catalog ids", async () => {
    mount();
    const vehicles = within(await screen.findByRole("group", { name: "Vehículos" }));
    fireEvent.click(vehicles.getByLabelText("Auto"));
    fireEvent.click(vehicles.getByLabelText("Moto"));
    fireEvent.click(vehicles.getByLabelText("Auto"));
    expect(patch()).toEqual({ detection: { selection: ["person", "vehicle.motorcycle"] } });
  });

  it("disables unsupported objects with a hint and leaves unknown ones enabled", async () => {
    mount();
    const vehicles = within(await screen.findByRole("group", { name: "Vehículos" }));
    expect(vehicles.getByLabelText(/Camioneta/)).toBeDisabled();
    expect(vehicles.getByText("Tu modelo no lo soporta")).toBeInTheDocument();
    expect(vehicles.getByLabelText("Moto")).toBeEnabled();
    expect(within(screen.getByRole("group", { name: "Personas" })).queryByText("Tu modelo no lo soporta")).toBeNull();
  });

  it("sends detection.enabled through the shared change", async () => {
    mount();
    const sw = await screen.findByRole("switch", { name: "Detección de objetos" });
    await waitFor(() => expect(sw).toBeEnabled());
    fireEvent.click(sw);
    expect(patch()).toEqual({ detection: { enabled: false } });
  });

  it("shows engine labels the catalog does not know as read-only text", async () => {
    mount({ raw: ["zorro", "hacha"] });
    const box = (await screen.findByText("Otros objetos del motor")).parentElement!;
    expect(within(box).getByText("zorro")).toBeInTheDocument();
    expect(within(box).queryByRole("checkbox")).toBeNull();
  });

  it("keeps the Frigate editors reachable under a collapsed advanced disclosure", async () => {
    mount();
    const summary = await screen.findByText("Avanzado de detección (Frigate)");
    const details = summary.closest("details")!;
    expect(details.open).toBe(false);
    expect(within(details).getByText("editores de Frigate")).toBeInTheDocument();
  });

  it("maps the settings onto the Frigate section edit state", async () => {
    const { onMotion, onObjects, onOpenZones } = mount();
    await screen.findByText("Qué detectar");
    expect(screen.getByLabelText(/Sensibilidad de movimiento/)).toHaveValue(30);
    expect(screen.getByLabelText(/Confianza mínima/)).toHaveValue(0.6);
    fireEvent.change(screen.getByLabelText(/Sensibilidad de movimiento/), { target: { value: "20" } });
    expect(onMotion).toHaveBeenCalledWith({ threshold: 20 });
    fireEvent.change(screen.getByLabelText(/Tamaño mínimo/), { target: { value: "5000" } });
    expect(onObjects).toHaveBeenCalledWith({ track: ["person"], filters: { person: { min_score: 0.6, min_area: 5000 } } });
    fireEvent.click(screen.getByRole("button", { name: "Editar zonas" }));
    expect(onOpenZones).toHaveBeenCalled();
    expect(screen.getByText("patio")).toBeInTheDocument();
    await waitFor(() => expect(patch()).toEqual({}));
  });
});

describe("filters helpers", () => {
  it("reads a shared value only when every tracked label agrees", () => {
    const o = { track: ["a", "b"], filters: { a: { min_score: 0.5 }, b: { min_score: 0.5 } } };
    expect(sharedFilter(o, "min_score")).toBe(0.5);
    expect(sharedFilter({ ...o, filters: { a: { min_score: 0.5 }, b: { min_score: 0.7 } } }, "min_score")).toBeUndefined();
  });
  it("clearing a field keeps the other filter keys", () => {
    expect(withFilter({ track: ["a"], filters: { a: { min_score: 0.5, threshold: 0.7 } } }, "min_score", undefined)).toEqual({ track: ["a"], filters: { a: { threshold: 0.7 } } });
  });
});
