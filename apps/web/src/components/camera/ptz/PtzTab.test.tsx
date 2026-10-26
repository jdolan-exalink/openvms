import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json } from "@/test-utils";
import { PtzOverlay } from "./PtzOverlay";
import { PtzTab } from "./PtzTab";
import { velocityOf } from "./usePtz";

afterEach(() => vi.unstubAllGlobals());

type Sent = { method: string; path: string; body: Record<string, unknown> | null };

function stub(opts: { presets?: Array<{ token: string; name: string }>; tours?: unknown[] } = {}) {
  const sent: Sent[] = [];
  const tours = [...(opts.tours ?? [])];
  vi.stubGlobal("fetch", async (req: Request) => {
    const path = new URL(req.url).pathname;
    const body = req.method === "GET" || req.method === "DELETE" ? null : ((await req.json()) as Record<string, unknown>);
    sent.push({ method: req.method, path, body });
    if (path.endsWith("/onvif/ptz")) return json({ success: true, presets: body?.action === "get_presets" ? (opts.presets ?? []) : undefined });
    if (path.endsWith("/ptz/tours") && req.method === "GET") return json({ tours });
    if (path.endsWith("/ptz/tours") && req.method === "POST") {
      const t = { id: "t1", camera_id: "c1", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", ...body };
      tours.push(t);
      return json(t, 201);
    }
    if (req.method === "DELETE") {
      tours.length = 0;
      return new Response(null, { status: 204 });
    }
    return json({ code: "not_found", message: "x" }, 404);
  });
  return sent;
}

const wrap = (ui: React.ReactNode) =>
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{ui}</QueryClientProvider>);
const ptzCalls = (sent: Sent[]) => sent.filter((s) => s.path.endsWith("/onvif/ptz") && s.body?.action !== "get_presets");

describe("velocityOf", () => {
  it("maps directions to ONVIF velocities and clamps the speed", () => {
    expect(velocityOf("left", 0.5)).toEqual({ pan: -0.5 });
    expect(velocityOf("up", 2)).toEqual({ tilt: 1 });
    expect(velocityOf("zoom_out", 0)).toEqual({ zoom: -0.1 });
  });
});

describe("PtzTab", () => {
  it("sends move, zoom and stop from the pad, using the chosen speed", async () => {
    const sent = stub();
    wrap(<PtzTab cameraId="c1" canManage />);
    fireEvent.change(screen.getByLabelText(/^Velocidad/), { target: { value: "0.8" } });
    const move = screen.getByRole("group", { name: "Mover" });
    fireEvent.pointerDown(within(move).getByRole("button", { name: "Derecha" }));
    await waitFor(() => expect(ptzCalls(sent)).toHaveLength(1));
    expect(ptzCalls(sent)[0]!.body).toMatchObject({ action: "continuous_move", pan: 0.8 });
    fireEvent.pointerUp(within(move).getByRole("button", { name: "Derecha" }));
    await waitFor(() => expect(ptzCalls(sent)).toHaveLength(2));
    expect(ptzCalls(sent)[1]!.body).toMatchObject({ action: "stop" });
    fireEvent.pointerDown(screen.getByRole("button", { name: "Zoom +" }));
    await waitFor(() => expect(ptzCalls(sent)).toHaveLength(3));
    expect(ptzCalls(sent)[2]!.body).toMatchObject({ action: "continuous_move", zoom: 0.8 });
  });

  it("lists the camera presets and goes to one", async () => {
    const sent = stub({ presets: [{ token: "1", name: "Puerta" }, { token: "2", name: "Playa" }] });
    wrap(<PtzTab cameraId="c1" canManage />);
    fireEvent.click(await screen.findByRole("button", { name: "Ir a Playa" }));
    await waitFor(() => expect(ptzCalls(sent)).toHaveLength(1));
    expect(ptzCalls(sent)[0]!.body).toMatchObject({ action: "goto_preset", preset_token: "2" });
  });

  it("shows what the device path cannot do as disabled controls with their reason", async () => {
    stub({ presets: [] });
    wrap(<PtzTab cameraId="c1" canManage />);
    const presets = await screen.findByRole("region", { name: "Presets" });
    for (const name of ["Crear preset", "Renombrar", "Eliminar", "Ir a home", "Guardar home"]) {
      expect(within(presets).getByRole("button", { name })).toBeDisabled();
    }
    expect(within(presets).getAllByText(/requiere un agente con soporte de edición PTZ/).length).toBeGreaterThan(0);
    expect(screen.getByText(/El foco no se controla/)).toBeInTheDocument();
    expect(screen.getByText(/límites de movimiento no se exponen/)).toBeInTheDocument();
  });

  it("disables the pad and sends no request without the manage permission", () => {
    const sent = stub();
    wrap(<PtzTab cameraId="c1" canManage={false} />);
    const up = screen.getByRole("button", { name: "Arriba" });
    expect(up).toBeDisabled();
    fireEvent.pointerDown(up);
    expect(sent.filter((s) => s.path.endsWith("/onvif/ptz"))).toHaveLength(0);
  });

  it("creates a tour from the presets and stores it", async () => {
    const sent = stub({ presets: [{ token: "1", name: "Puerta" }] });
    wrap(<PtzTab cameraId="c1" canManage />);
    expect(await screen.findByText(/ejecución automática todavía no está disponible/)).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("button", { name: "Nuevo tour" }));
    fireEvent.change(screen.getByLabelText("Nombre del tour"), { target: { value: "Ronda" } });
    fireEvent.click(screen.getByRole("button", { name: "Agregar paso" }));
    const step = screen.getByRole("group", { name: "Paso 1" });
    fireEvent.change(await within(step).findByRole("option", { name: "Puerta" }).then((o) => o.closest("select")!), { target: { value: "1" } });
    fireEvent.change(within(step).getByLabelText("Permanencia (s)"), { target: { value: "20" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar tour" }));
    await waitFor(() => expect(sent.some((s) => s.method === "POST" && s.path.endsWith("/ptz/tours"))).toBe(true));
    const post = sent.find((s) => s.method === "POST" && s.path.endsWith("/ptz/tours"))!;
    expect(post.body).toEqual({ name: "Ronda", repeat_count: 0, steps: [{ preset_token: "1", preset_name: "Puerta", dwell_seconds: 20, speed: 0.5 }] });
    expect(await screen.findByText(/1 pasos/)).toBeInTheDocument();
  });

  it("does not offer tour editing without the manage permission", async () => {
    stub({ tours: [{ id: "t1", camera_id: "c1", name: "Ronda", steps: [{ preset_token: "1", dwell_seconds: 5, speed: 0.5 }], repeat_count: 2 }] });
    wrap(<PtzTab cameraId="c1" canManage={false} />);
    expect(await screen.findByText("Ronda")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Nuevo tour" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Eliminar Ronda" })).not.toBeInTheDocument();
  });
});

describe("PtzOverlay", () => {
  it("drives the same ONVIF path from the preview", async () => {
    const sent = stub();
    wrap(<PtzOverlay cameraId="c1" />);
    fireEvent.pointerDown(screen.getByRole("button", { name: "Arriba" }));
    await waitFor(() => expect(ptzCalls(sent)).toHaveLength(1));
    expect(ptzCalls(sent)[0]!.body).toMatchObject({ action: "continuous_move", tilt: 0.5 });
  });
});
