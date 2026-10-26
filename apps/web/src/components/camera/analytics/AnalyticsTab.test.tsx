import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AnalyticsTab } from "./AnalyticsTab";
import { type AnalyticsModule, anyModuleAvailable, draftEnabled, draftMinArea, enabledPatch, minAreaPatch } from "./draft";

afterEach(() => vi.unstubAllGlobals());

const NA = "No disponible con este motor";
const mod = (over: Partial<AnalyticsModule> & Pick<AnalyticsModule, "id" | "label">): AnalyticsModule => ({
  available: false, enabled: false, writable: false, engine: "none", reason: NA, settings: {}, advanced: [], ...over,
});
const lpr = (over: Partial<AnalyticsModule> = {}) =>
  mod({ id: "lpr", label: "Matrículas (LPR)", available: true, writable: true, engine: "frigate", reason: undefined, settings: { min_area: 1000 }, advanced: [{ label: "Motor", value: "Frigate" }], ...over });
const all = (over: Partial<Record<AnalyticsModule["id"], Partial<AnalyticsModule>>> = {}): AnalyticsModule[] => [
  lpr(over.lpr),
  mod({ id: "face_detection", label: "Detección facial", ...over.face_detection }),
  mod({ id: "face_recognition", label: "Reconocimiento facial", ...over.face_recognition }),
  mod({ id: "vehicle_classification", label: "Clasificación de vehículo" }),
  mod({ id: "vehicle_color", label: "Color de vehículo" }),
  mod({ id: "clothing_color", label: "Color de ropa" }),
  mod({ id: "similar_search", label: "Buscar similares cuando corresponda" }),
];

function renderTab(modules: AnalyticsModule[] | undefined, patch: Record<string, unknown> = {}) {
  const setPatch = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AnalyticsTab cameraId="c1" modules={modules} patch={patch} setPatch={setPatch} />
    </QueryClientProvider>,
  );
  return setPatch;
}

describe("AnalyticsTab", () => {
  it("lists every module and disables the ones without evidence, with the reason", () => {
    renderTab(all());
    for (const id of ["vehicle_classification", "vehicle_color", "clothing_color", "similar_search"]) {
      const card = screen.getByTestId(`module-${id}`);
      expect(within(card).getByRole("switch")).toBeDisabled();
      expect(within(card).getByText(NA)).toBeInTheDocument();
    }
    expect(screen.getByText("Buscar similares cuando corresponda")).toBeInTheDocument();
  });

  it("shows the detail only when the module is on", () => {
    renderTab(all());
    expect(screen.queryByLabelText(/Área mínima de la matrícula/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Probar conexión/ })).not.toBeInTheDocument();
  });

  it("shows the LPR detail with the connection test once enabled", () => {
    renderTab(all({ lpr: { enabled: true } }));
    expect(screen.getByLabelText(/Área mínima de la matrícula/)).toHaveValue(1000);
    expect(screen.getByRole("button", { name: /Probar conexión/ })).toBeInTheDocument();
  });

  it("stages the switch and the setting as a sparse patch (LPR uses analytics.lpr)", () => {
    const setPatch = renderTab(all({ lpr: { enabled: true } }));
    fireEvent.click(within(screen.getByTestId("module-lpr")).getByRole("switch"));
    expect(setPatch).toHaveBeenLastCalledWith({ analytics: { lpr: false } });
    fireEvent.change(screen.getByLabelText(/Área mínima de la matrícula/), { target: { value: "2500" } });
    expect(setPatch).toHaveBeenLastCalledWith({ analytics: { modules: { lpr: { settings: { min_area: 2500 } } } } });
  });

  it("reads the draft over the effective value", () => {
    renderTab(all(), { analytics: { lpr: true } });
    expect(screen.getByLabelText(/Área mínima de la matrícula/)).toBeInTheDocument();
  });

  it("Provision LPR is read only: active, switch disabled, reason shown, test offered", () => {
    renderTab(all({ lpr: { engine: "provision", writable: false, enabled: true, reason: "Se configura en la cámara", settings: {} } }));
    const card = screen.getByTestId("module-lpr");
    expect(within(card).getByRole("switch")).toBeDisabled();
    expect(within(card).getByRole("switch")).toHaveAttribute("aria-checked", "true");
    expect(within(card).getByText("Se configura en la cámara")).toBeInTheDocument();
    expect(screen.queryByLabelText(/Área mínima de la matrícula/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Probar conexión/ })).toBeInTheDocument();
  });

  it("splits face detection from face recognition", () => {
    const setPatch = renderTab(all({ face_recognition: { available: true, writable: true, engine: "frigate", reason: undefined, enabled: true, settings: { min_area: 750 } } }));
    const group = screen.getByRole("region", { name: "Reconocimiento / detección facial" });
    expect(within(group).getByText("Detección facial")).toBeInTheDocument();
    expect(within(group).getByText("Reconocimiento facial")).toBeInTheDocument();
    expect(within(screen.getByTestId("module-face_detection")).getByRole("switch")).toBeDisabled();
    expect(screen.getByLabelText(/Área mínima del rostro/)).toHaveValue(750);
    fireEvent.click(within(screen.getByTestId("module-face_recognition")).getByRole("switch"));
    expect(setPatch).toHaveBeenLastCalledWith({ analytics: { modules: { face_recognition: { enabled: false } } } });
  });

  it("waits for the model", () => {
    renderTab(undefined);
    expect(screen.getByText("Cargando analíticas…")).toBeInTheDocument();
  });
});

describe("draft helpers", () => {
  it("builds sparse patches and reads the draft", () => {
    expect(enabledPatch("face_recognition", true)).toEqual({ analytics: { modules: { face_recognition: { enabled: true } } } });
    expect(minAreaPatch("lpr", 5)).toEqual({ analytics: { modules: { lpr: { settings: { min_area: 5 } } } } });
    const m = lpr();
    expect(draftEnabled({}, m)).toBe(false);
    expect(draftEnabled({ analytics: { lpr: true } }, m)).toBe(true);
    expect(draftMinArea({ analytics: { modules: { lpr: { settings: { min_area: 7 } } } } }, m)).toBe(7);
    expect(draftMinArea({}, m)).toBe(1000);
  });

  it("anyModuleAvailable needs positive evidence", () => {
    expect(anyModuleAvailable(all())).toBe(true);
    expect(anyModuleAvailable(all().map((m) => ({ ...m, available: false })))).toBe(false);
    expect(anyModuleAvailable(undefined)).toBe(false);
  });
});
