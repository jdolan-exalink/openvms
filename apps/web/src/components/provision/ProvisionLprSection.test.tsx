import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { ProvisionLprSection } from "@/components/provision/ProvisionLprSection";
import { json, renderPage } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const cameraID = "aaaaaaaa-1111-2222-3333-444444444444";
const serverID = "bbbbbbbb-1111-2222-3333-444444444444";
const camera = {
  id: cameraID, server_id: serverID, display_name: "Portón", remote_name: "porton", tags: ["provision"],
  onvif_endpoint: "http://10.0.15.107/onvif/device_service",
} as unknown as Schemas["Camera"];

type Call = { method: string; path: string; body?: string; csrf: string | null };

function stubServer(configureAnswer: Record<string, unknown>) {
  const calls: Call[] = [];
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(typeof input === "string" ? input : input instanceof Request ? input.url : input.toString(), "http://localhost");
    const method = init?.method ?? (input instanceof Request ? input.method : "GET");
    calls.push({ method, path: url.pathname, body: typeof init?.body === "string" ? init.body : undefined, csrf: new Headers(init?.headers).get("X-OpenVMS-Request") });
    if (url.pathname === `/api/v1/cameras/${cameraID}/provision-lpr/status`) {
      return json({ camera_id: cameraID, camera_ip: "10.0.15.107", online: false, keepalive_count: 0, event_count: 0, target_port: 7419, keepalive_path: "/SendKeepalive", event_path: "/v1/provision-lpr/event", recent_events: [], recent_plates: [] });
    }
    if (url.pathname === `/api/v1/cameras/${cameraID}/provision-lpr/configure` && method === "POST") return json(configureAnswer);
    if (url.pathname === `/api/v1/servers/${serverID}/provision-lpr/registry`) return json({ enforce: false, cameras: [], unregistered_seen: [] });
    return json({ code: "not_found", message: "nope" }, 404);
  }));
  return calls;
}

const waiting = {
  success: false, verified: false, registered: true, camera_id: cameraID, method: "manual_instructions",
  message: "Ingresá estos valores en la interfaz web de la cámara.",
  parameters: { "Server IP / Domain": "10.1.1.5", "Server Port": "7419", "Keepalive Endpoint": "/SendKeepalive", "LPR Event Endpoint": "/v1/provision-lpr/event", "Heartbeat Interval": "20 s" },
};

describe("ProvisionLprSection", () => {
  it("does not ask for the camera's credentials or HTTP port, since nothing is applied to the camera", async () => {
    stubServer(waiting);
    renderPage(() => <ProvisionLprSection camera={camera} />);
    expect(await screen.findByRole("button", { name: "Registrar cámara y preparar configuración" })).toBeInTheDocument();
    for (const label of ["Usuario Cámara", "Contraseña", "Puerto HTTP"]) expect(screen.queryByText(label)).not.toBeInTheDocument();
  });

  it("registers the camera with the CSRF header, sends no credentials, and shows the values the agent prepared", async () => {
    const calls = stubServer(waiting);
    renderPage(() => <ProvisionLprSection camera={camera} />);
    fireEvent.click(await screen.findByRole("button", { name: "Registrar cámara y preparar configuración" }));

    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path.endsWith("/provision-lpr/configure"))).toBe(true));
    const post = calls.find((c) => c.method === "POST" && c.path.endsWith("/provision-lpr/configure"))!;
    expect(post.csrf).toBe("1");
    const sent = JSON.parse(post.body ?? "{}") as Record<string, unknown>;
    expect(sent).not.toHaveProperty("username");
    expect(sent).not.toHaveProperty("password");
    expect(sent).not.toHaveProperty("camera_port");

    expect(await screen.findByText("Ingresá estos valores en la interfaz web de la cámara.")).toBeInTheDocument();
    expect(screen.getByText(/Cámara registrada en el agente/)).toBeInTheDocument();
    expect(screen.getByText("10.1.1.5")).toBeInTheDocument();
  });

  it("shows the integration as verified once the camera reports in", async () => {
    stubServer({ ...waiting, success: true, verified: true, method: "verified", message: "La cámara ya reporta a este agente: la integración está verificada." });
    renderPage(() => <ProvisionLprSection camera={camera} />);
    fireEvent.click(await screen.findByRole("button", { name: "Registrar cámara y preparar configuración" }));
    expect(await screen.findByText(/integración está verificada/)).toBeInTheDocument();
    expect(screen.queryByText(/no aplica estos valores/)).not.toBeInTheDocument();
  });

  it("shows the registry of the camera's server", async () => {
    stubServer(waiting);
    renderPage(() => <ProvisionLprSection camera={camera} />);
    expect(await screen.findByText("Cámaras permitidas en el receptor")).toBeInTheDocument();
    expect(await screen.findByText(/Sin exigir/)).toBeInTheDocument();
  });
});
