import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { BulkConfigureDialog } from "./BulkConfigureDialog";

afterEach(() => vi.unstubAllGlobals());

const row = (id: string, name: string, status: string, extra: object = {}) => ({ camera_id: id, name, status, changes: [], impact: [], reason: "", ...extra });
const totals = (o: object = {}) => ({ cameras: 4, will_apply: 2, applied: 0, failed: 0, incompatible: 1, skipped: 1, unreachable: 0, ...o });
const reviewReport = {
  section: "detection",
  restarts: 1,
  totals: totals(),
  rows: [
    row("c1", "Entrada", "will_apply", { changes: [{ path: "detection.enabled", label: "detección de objetos", from: false, to: true }], impact: [{ code: "engine_reload", message: "Frigate recargará la configuración" }] }),
    row("c2", "Patio", "will_apply"),
    row("c3", "Provision", "incompatible", { reason: "La cámara no admite este cambio" }),
    row("c4", "Garage", "skipped", { reason: "Ya tiene esos valores" }),
  ],
};

function mount(routes: Record<string, () => Response> = {}, target: React.ComponentProps<typeof BulkConfigureDialog>["target"] = { groupId: "g1" }) {
  const sent: { path: string; body: any }[] = [];
  const api = stubApi({
    "/api/v1/camera-config/bulk/review": () => json(reviewReport),
    "/api/v1/camera-config/bulk/apply": () => json({ ...reviewReport, totals: totals({ will_apply: 0, applied: 1, failed: 1 }), rows: [row("c1", "Entrada", "applied"), row("c2", "Patio", "failed", { reason: "El motor rechazó el cambio" })] }),
    ...routes,
  });
  vi.stubGlobal("fetch", vi.fn(async (req: Request) => {
    sent.push({ path: new URL(req.url).pathname, body: await req.clone().json() });
    return api(req);
  }));
  const onClose = vi.fn();
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <BulkConfigureDialog target={target} targetLabel="Perímetro" onClose={onClose} />
    </QueryClientProvider>,
  );
  return { sent, onClose };
}

describe("BulkConfigureDialog", () => {
  it("needs a value before reviewing", () => {
    mount();
    expect(screen.getByRole("button", { name: "Revisar" })).toBeDisabled();
  });

  it("offers one section at a time and sends only that section", async () => {
    const { sent } = mount();
    expect(screen.getAllByRole("radio")).toHaveLength(4);
    fireEvent.change(screen.getByLabelText("Detección de objetos"), { target: { value: "on" } });
    fireEvent.click(screen.getByRole("radio", { name: "Grabación" }));
    expect(screen.queryByLabelText("Detección de objetos")).toBeNull();
    expect(screen.getByRole("button", { name: "Revisar" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Grabación", { selector: "select" }), { target: { value: "off" } });
    fireEvent.click(screen.getByRole("button", { name: "Revisar" }));
    await screen.findByTestId("bulk-total");
    expect(sent[0]).toEqual({ path: "/api/v1/camera-config/bulk/review", body: { group_id: "g1", patch: { recording: { enabled: false } }, origin: "group" } });
  });

  it("reviews affected, incompatible, skipped cameras and restarts with a total line", async () => {
    mount();
    fireEvent.change(screen.getByLabelText("Detección de objetos"), { target: { value: "on" } });
    fireEvent.click(screen.getByRole("button", { name: "Revisar" }));
    expect(await screen.findByTestId("bulk-total")).toHaveTextContent("Aplicar detección a 2 cámaras");
    const summary = within(screen.getByRole("list", { name: "Resumen" }));
    expect(summary.getByText("Cámaras afectadas: 2")).toBeInTheDocument();
    expect(summary.getByText("Incompatibles: 1")).toBeInTheDocument();
    expect(summary.getByText("Omitidas: 1")).toBeInTheDocument();
    expect(summary.getByText("Reinicios necesarios: 1")).toBeInTheDocument();
    const table = within(screen.getByRole("table", { name: "Cámaras" }));
    expect(table.getByText("La cámara no admite este cambio")).toBeInTheDocument();
    expect(table.getByText("Ya tiene esos valores")).toBeInTheDocument();
    expect(table.getByText(/detección de objetos: OFF → ON/)).toBeInTheDocument();
  });

  it("disables Aplicar when nothing will apply", async () => {
    mount({ "/api/v1/camera-config/bulk/review": () => json({ ...reviewReport, restarts: 0, totals: totals({ will_apply: 0 }), rows: [row("c3", "Provision", "incompatible", { reason: "x" })] }) });
    fireEvent.change(screen.getByLabelText("Detección de objetos"), { target: { value: "on" } });
    fireEvent.click(screen.getByRole("button", { name: "Revisar" }));
    expect(await screen.findByTestId("bulk-total")).toHaveTextContent("Aplicar detección a 0 cámaras");
    expect(screen.getByRole("button", { name: "Aplicar" })).toBeDisabled();
  });

  it("applies and shows the per-camera outcome", async () => {
    const { sent } = mount();
    fireEvent.change(screen.getByLabelText("Detección de objetos"), { target: { value: "on" } });
    fireEvent.click(screen.getByRole("button", { name: "Revisar" }));
    await screen.findByTestId("bulk-total");
    fireEvent.click(screen.getByRole("button", { name: "Aplicar" }));
    await waitFor(() => expect(screen.getByTestId("bulk-total")).toHaveTextContent("1 cámara actualizada, 1 falló"));
    expect(screen.getByText("Aplicada")).toBeInTheDocument();
    expect(screen.getByText("Falló")).toBeInTheDocument();
    expect(screen.getByText("El motor rechazó el cambio")).toBeInTheDocument();
    expect(sent.at(-1)?.path).toBe("/api/v1/camera-config/bulk/apply");
    expect(sent.at(-1)?.body.patch).toEqual({ detection: { enabled: true } });
  });

  it("targets explicit camera ids with the api origin", async () => {
    const { sent } = mount({}, { cameraIds: ["c1", "c2"] });
    fireEvent.change(screen.getByLabelText("Detección de objetos"), { target: { value: "on" } });
    fireEvent.click(screen.getByRole("button", { name: "Revisar" }));
    await screen.findByTestId("bulk-total");
    expect(sent[0]?.body).toMatchObject({ camera_ids: ["c1", "c2"], origin: "api" });
    expect(sent[0]?.body.group_id).toBeUndefined();
  });

  it("shows server errors from the review", async () => {
    mount({ "/api/v1/camera-config/bulk/review": () => json({ code: "bad_request", message: "máximo 200 cámaras" }, 400) });
    fireEvent.change(screen.getByLabelText("Detección de objetos"), { target: { value: "on" } });
    fireEvent.click(screen.getByRole("button", { name: "Revisar" }));
    expect(await screen.findByText(/máximo 200 cámaras/)).toBeInTheDocument();
  });
});
