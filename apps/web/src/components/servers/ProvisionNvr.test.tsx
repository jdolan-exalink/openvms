import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json } from "@/test-utils";
import { ProvisionCapabilities } from "./ProvisionCapabilities";
import { NewProvisionNvr } from "./NewProvisionNvr";
import { ProvisionNvrModal } from "./ProvisionNvrModal";

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const wrap = (ui: React.ReactNode) =>
  render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{ui}</QueryClientProvider>);

const channel = (id: string, name: string, extra: object = {}) => ({ id, name, address: `192.0.2.${id}`, online: true, ...extra });
const detection = {
  device: { name: "Test", model: "NVR8-16400AN(1U)", hardware_model: "TD-3116H2-B1", firmware: "1.4.7" },
  channel_capacity: 16,
  channels: [channel("1", "Cámara 1"), channel("2", "Cámara 2"), channel("3", "Cámara 3", { online: false })],
  disks: [],
  cycle_record: true,
  capabilities: [
    { name: "channels", state: "confirmed", source: "queryNodeList", detail: "3 channels" },
    { name: "rtsp", state: "unknown", source: "onvif", detail: "reported, not exercised" },
    { name: "ptz", state: "unsupported", source: "onvif", detail: "no PTZ profile" },
  ],
};

type Sent = { method: string; path: string; body?: unknown };
function stub(routes: Record<string, () => Response>) {
  const sent: Sent[] = [];
  vi.stubGlobal("fetch", async (req: Request) => {
    const path = new URL(req.url).pathname;
    const text = req.method === "GET" ? "" : await req.clone().text();
    sent.push({ method: req.method, path, body: text ? JSON.parse(text) : undefined });
    const handler = routes[`${req.method} ${path}`];
    return handler ? handler() : json({ code: "not_found", message: "x" }, 404);
  });
  return sent;
}

describe("ProvisionCapabilities", () => {
  it("labels what was confirmed, what is only claimed and what is absent, and never shows a claim as available", () => {
    wrap(<ProvisionCapabilities capabilities={detection.capabilities as never} />);
    const list = screen.getByRole("list", { name: "Capacidades" });
    expect(within(list).getByText("Canales").parentElement).toHaveTextContent("Confirmada");
    expect(within(list).getByText("RTSP").parentElement).toHaveTextContent("Sin verificar");
    expect(within(list).getByText("RTSP").parentElement).not.toHaveTextContent("Confirmada");
    expect(within(list).getByText("PTZ").parentElement).toHaveTextContent("No disponible");
  });
});

describe("NewProvisionNvr", () => {
  function fill() {
    fireEvent.change(screen.getByLabelText("Sitio"), { target: { value: "s1" } });
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "NVR Feedlot" } });
    fireEvent.change(screen.getByLabelText("Dirección del NVR"), { target: { value: "192.168.1.50" } });
    fireEvent.change(screen.getByLabelText("Usuario del NVR"), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText(/^Contraseña/), { target: { value: "s3cret" } });
  }
  const sites = [{ id: "s1", name: "Helvecia" }] as never;

  it("detects first, lets the user pick channels, and registers only the chosen ones", async () => {
    const sent = stub({
      "POST /api/v1/servers/provision-nvr/detect": () => json(detection),
      "POST /api/v1/servers/provision-nvr": () => json({ id: "srv", name: "NVR Feedlot" }, 201),
    });
    const onDone = vi.fn();
    wrap(<NewProvisionNvr sites={sites} onBack={() => {}} onDone={onDone} onCancel={() => {}} onDirtyChange={() => {}} />);
    fill();
    expect(screen.queryByRole("button", { name: "Registrar NVR" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Detectar" }));

    expect(await screen.findByText(/NVR8-16400AN\(1U\) · firmware 1.4.7 · 3 canales/)).toBeInTheDocument();
    expect(sent.map((s) => s.path)).toEqual(["/api/v1/servers/provision-nvr/detect"]);
    expect(sent[0]!.body).toMatchObject({ site_id: "s1", host: "192.168.1.50", username: "admin", password: "s3cret", https: false });

    // Every channel starts selected; drop the second one.
    fireEvent.click(screen.getByRole("checkbox", { name: /Cámara 2/ }));
    fireEvent.click(screen.getByRole("button", { name: "Registrar NVR" }));
    await waitFor(() => expect(onDone).toHaveBeenCalled());
    const create = sent.find((s) => s.path === "/api/v1/servers/provision-nvr")!;
    expect(create.body).toMatchObject({ name: "NVR Feedlot", channel_ids: ["1", "3"] });
  });

  it("cannot register with no channel selected", async () => {
    stub({ "POST /api/v1/servers/provision-nvr/detect": () => json(detection) });
    wrap(<NewProvisionNvr sites={sites} onBack={() => {}} onDone={() => {}} onCancel={() => {}} onDirtyChange={() => {}} />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Detectar" }));
    fireEvent.click(await screen.findByRole("checkbox", { name: /Todos los canales/ }));
    expect(screen.getByRole("alert")).toHaveTextContent("Elegí al menos un canal.");
    expect(screen.getByRole("button", { name: "Registrar NVR" })).toBeDisabled();
  });

  it("changing the connection after a detection asks for a new detection", async () => {
    stub({ "POST /api/v1/servers/provision-nvr/detect": () => json(detection) });
    wrap(<NewProvisionNvr sites={sites} onBack={() => {}} onDone={() => {}} onCancel={() => {}} onDirtyChange={() => {}} />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Detectar" }));
    await screen.findByText(/firmware 1.4.7/);
    fireEvent.change(screen.getByLabelText("Dirección del NVR"), { target: { value: "192.168.1.51" } });
    expect(screen.queryByText(/firmware 1.4.7/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Detectar" })).toBeInTheDocument();
  });

  it("shows the recorder's message when the password is rejected and registers nothing", async () => {
    const sent = stub({ "POST /api/v1/servers/provision-nvr/detect": () => json({ code: "nvr_auth_failed", message: "the recorder rejected the username or password" }, 502) });
    wrap(<NewProvisionNvr sites={sites} onBack={() => {}} onDone={() => {}} onCancel={() => {}} onDirtyChange={() => {}} />);
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Detectar" }));
    expect(await screen.findByText(/rejected the username or password/)).toBeInTheDocument();
    expect(sent.map((s) => s.path)).toEqual(["/api/v1/servers/provision-nvr/detect"]);
  });
});

describe("ProvisionNvrModal", () => {
  const server = {
    id: "srv", kind: "provision_nvr", name: "NVR Feedlot", site_id: "s1", base_url: "http://192.168.1.50:80", username: "admin", status: "online",
    camera_count: 2, last_error: "", problems: [{ code: "recorder_disk_bad" }],
    provision: { model: "NVR8-16400AN(1U)", hardware_model: "TD", firmware: "1.4.7", channel_capacity: 16, capabilities: detection.capabilities },
  } as never;
  const sites = [{ id: "s1", name: "Helvecia" }] as never;

  it("shows the recorder summary, its problems and capabilities, with no Frigate tabs", () => {
    wrap(<ProvisionNvrModal server={server} sites={sites} onDone={() => {}} onDelete={() => {}} />);
    expect(screen.getByText("NVR8-16400AN(1U)")).toBeInTheDocument();
    expect(screen.getByText("El NVR informa un disco dañado")).toBeInTheDocument();
    expect(screen.getByRole("list", { name: "Capacidades" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: /Frigate/ })).not.toBeInTheDocument();
  });

  it("reads the recorder's channels and adds the new ones, leaving imported ones alone", async () => {
    const sent = stub({
      "GET /api/v1/servers/srv/provision-nvr/channels": () => json({ items: [channel("1", "Cámara 1", { imported: true }), channel("2", "Cámara 2", { imported: true }), channel("9", "Cámara nueva", { imported: false })] }),
      "POST /api/v1/servers/srv/provision-nvr/channels": () => json({ added: 1, skipped: 0 }),
    });
    wrap(<ProvisionNvrModal server={server} sites={sites} onDone={() => {}} onDelete={() => {}} />);
    fireEvent.click(screen.getByRole("tab", { name: "Canales a importar" }));
    expect(sent).toEqual([]); // nothing is read until asked
    fireEvent.click(screen.getByRole("button", { name: "Leer canales del NVR" }));

    const imported = await screen.findByRole("checkbox", { name: /Cámara 1/ });
    expect(imported).toBeChecked();
    expect(imported).toBeDisabled();
    const fresh = screen.getByRole("checkbox", { name: /Cámara nueva/ });
    expect(fresh).not.toBeChecked();
    fireEvent.click(fresh);
    fireEvent.click(screen.getByRole("button", { name: "Agregar los elegidos" }));
    await screen.findByText(/Se agregaron 1 canales/);
    expect(sent.find((s) => s.method === "POST")!.body).toEqual({ channel_ids: ["9"] });
  });

  it("renames the recorder but shows its address and account read-only", async () => {
    const sent = stub({ "PATCH /api/v1/servers/srv": () => json({ id: "srv" }) });
    wrap(<ProvisionNvrModal server={server} sites={sites} onDone={() => {}} onDelete={() => {}} />);
    fireEvent.click(screen.getByRole("tab", { name: "Conexión" }));
    expect(screen.getByLabelText("Dirección")).toHaveAttribute("readonly");
    expect(screen.getByLabelText("Usuario")).toHaveAttribute("readonly");
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "NVR renombrado" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(sent.some((s) => s.method === "PATCH")).toBe(true));
    expect(sent.find((s) => s.method === "PATCH")!.body).toEqual({ name: "NVR renombrado" });
  });
});
