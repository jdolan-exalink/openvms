import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AnalyticsTab } from "./AnalyticsTab";
import { type AnalyticsModule, anyModuleAvailable, draftEnabled, draftEnhancement, draftMinArea, enabledPatch, enhancementPatch, minAreaPatch } from "./draft";

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

  it("shows the face detection card mirroring recognition, with the engine's reason and no switch of its own", () => {
    const reason = "Frigate detecta rostros solo mientras el reconocimiento facial está activo; no tiene un interruptor de detección aparte";
    renderTab(all({
      face_detection: { available: true, engine: "frigate", enabled: true, reason, advanced: [{ label: "Umbral de detección (servidor)", value: "0.7" }] },
      face_recognition: { available: true, writable: true, engine: "frigate", reason: undefined, enabled: true, settings: { min_area: 750 }, advanced: [{ label: "Base de rostros", value: "Las listas de vigilancia son del tenant" }] },
    }));
    const det = screen.getByTestId("module-face_detection");
    expect(within(det).getByRole("switch")).toBeDisabled();
    expect(within(det).getByRole("switch")).toHaveAttribute("aria-checked", "true");
    expect(within(det).getByText(reason)).toBeInTheDocument();
    expect(within(det).getByText("Umbral de detección (servidor)")).toBeInTheDocument();
    expect(screen.getByText("Base de rostros")).toBeInTheDocument();
  });

  it("OpenVMS modules are switchable and show their detail only while on", () => {
    const openvms = (id: AnalyticsModule["id"], label: string, enabled: boolean) =>
      mod({ id, label, available: true, writable: true, enabled, engine: "openvms", reason: undefined, advanced: [{ label: "Alcance", value: "Eventos nuevos de esta cámara" }] });
    const modules = all().map((m) =>
      m.id === "vehicle_color" ? openvms("vehicle_color", "Color de vehículo", true) : m.id === "clothing_color" ? openvms("clothing_color", "Color de ropa", false) : m,
    );
    const setPatch = renderTab(modules);
    const on = screen.getByTestId("module-vehicle_color");
    expect(within(on).getByText("Motor: OpenVMS")).toBeInTheDocument();
    expect(within(on).getByText("Eventos nuevos de esta cámara")).toBeInTheDocument();
    expect(within(screen.getByTestId("module-clothing_color")).queryByText("Eventos nuevos de esta cámara")).not.toBeInTheDocument();
    fireEvent.click(within(screen.getByTestId("module-clothing_color")).getByRole("switch"));
    expect(setPatch).toHaveBeenLastCalledWith({ analytics: { modules: { clothing_color: { enabled: true } } } });
  });

  it("shows a server-wide lock as the reason and keeps the switch disabled", () => {
    const k = mod({ id: "vehicle_classification", label: "Clasificación de vehículo", available: true, writable: false, enabled: false, engine: "openvms", reason: "Desactivado para todo el servidor" });
    renderTab(all().map((m) => (m.id === "vehicle_classification" ? k : m)));
    const card = screen.getByTestId("module-vehicle_classification");
    expect(within(card).getByRole("switch")).toBeDisabled();
    expect(within(card).getByText("Desactivado para todo el servidor")).toBeInTheDocument();
  });

  it("stages the LPR enhancement level, bounded to 0..10", () => {
    const setPatch = renderTab(all({ lpr: { enabled: true, settings: { min_area: 1000, enhancement: 2 } } }));
    const input = screen.getByLabelText(/Mejora de imagen/);
    expect(input).toHaveValue(2);
    fireEvent.change(input, { target: { value: "99" } });
    expect(setPatch).toHaveBeenLastCalledWith({ analytics: { modules: { lpr: { settings: { enhancement: 10 } } } } });
  });

  it("shows the specific reason of an unavailable module", () => {
    const why = "Frigate ofrece la búsqueda semántica para todo el servidor, no por cámara, y OpenVMS no calcula similitud de vehículos";
    renderTab(all().map((m) => (m.id === "similar_search" ? { ...m, reason: why } : m)));
    expect(within(screen.getByTestId("module-similar_search")).getByText(why)).toBeInTheDocument();
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
    expect(enhancementPatch("lpr", 4)).toEqual({ analytics: { modules: { lpr: { settings: { enhancement: 4 } } } } });
    expect(draftEnhancement({ analytics: { modules: { lpr: { settings: { enhancement: 6 } } } } }, m)).toBe(6);
    expect(draftEnhancement({}, lpr({ settings: { enhancement: 1 } }))).toBe(1);
  });

  it("anyModuleAvailable needs positive evidence", () => {
    expect(anyModuleAvailable(all())).toBe(true);
    expect(anyModuleAvailable(all().map((m) => ({ ...m, available: false })))).toBe(false);
    expect(anyModuleAvailable(undefined)).toBe(false);
  });
});
