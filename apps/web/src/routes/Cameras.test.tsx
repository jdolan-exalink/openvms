import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Cameras } from "./Cameras";

afterEach(() => vi.unstubAllGlobals());

const camera = (id: string, name: string, extra: object = {}) => ({
  id,
  tenant_id: "t",
  site_id: "s1",
  server_id: "srv1",
  remote_name: name,
  display_name: name,
  enabled: true,
  zones: ["entrada"],
  lpr: false,
  status: "online",
  fps: 5,
  group_ids: [],
  default_live_quality: "sub",
  description: "",
  location: "",
  tags: [],
  created_at: "",
  updated_at: "",
  ...extra,
});

const inventory = (cameras: object[]) => ({
  "/api/v1/cameras": () => json({ items: cameras }),
  "/api/v1/sites": () => json({ items: [{ id: "s1", name: "Helvecia" }] }),
  "/api/v1/servers": () => json({ items: [{ id: "srv1", name: "frigate-h01" }] }),
});

describe("Cameras", () => {
  it("discovers credential-free endpoints only for the chosen managed server and interface", async () => {
    const fetchMock = vi.fn(stubApi({
      ...inventory([]),
      "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [{ id: "g", permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "srv1" }] }),
      "POST /api/v1/servers/srv1/onvif/discover": () => json({ devices: [{ xaddrs: ["http://192.168.1.20:80/onvif/device_service?token=secret"] }] }),
    }));
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Cameras);

    const serverSelect = await screen.findByLabelText("Servidor para descubrir cámaras");
    await within(serverSelect).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(serverSelect, { target: { value: "srv1" } });
    fireEvent.change(screen.getByLabelText("Interfaz de red del agente"), { target: { value: "eth0" } });
    await waitFor(() => expect(screen.getByRole("button", { name: "Buscar dispositivos ONVIF" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Buscar dispositivos ONVIF" }));

    expect(await screen.findByText("http://192.168.1.20/onvif/device_service")).toBeInTheDocument();
    expect(screen.getByText(/todavía no se agregó/i)).toBeInTheDocument();
    const request = fetchMock.mock.calls.map(([request]) => request as Request).find((request) => request.url.includes("/onvif/discover"));
    expect(request).toBeDefined();
    expect(request?.method).toBe("POST");
    expect(await request?.json()).toEqual({ interface_name: "eth0" });
    expect(fetchMock.mock.calls.some(([request]) => (request as Request).url.includes("192.168.1.20"))).toBe(false);
  });

  it("requires a server, interface, and scoped manage permission before discovery", async () => {
    const fetchMock = vi.fn(stubApi({ ...inventory([]), "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [] }) }));
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Cameras);

    expect(await screen.findByLabelText("Servidor para descubrir cámaras")).toBeInTheDocument();
    const search = screen.getByRole("button", { name: "Buscar dispositivos ONVIF" });
    expect(search).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Servidor para descubrir cámaras"), { target: { value: "srv1" } });
    fireEvent.change(screen.getByLabelText("Interfaz de red del agente"), { target: { value: "eth0" } });
    expect(search).toBeDisabled();
    expect(fetchMock.mock.calls.some(([request]) => (request as Request).url.includes("/onvif/discover"))).toBe(false);
  });

  it("shows pending state and ignores a stale discovery response after changing server", async () => {
    let resolveDiscovery!: (response: Response) => void;
    let discoverySignal: AbortSignal | undefined;
    const fetchMock = vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/servers") return json({ items: [{ id: "srv1", name: "frigate-h01" }, { id: "srv2", name: "frigate-r01" }] });
      if (path === "/api/v1/sites") return json({ items: [] });
      if (path === "/api/v1/cameras") return json({ items: [] });
      if (path === "/api/v1/me") return json({ id: "u", tenant_id: "t", grants: [{ id: "g", permission: "servers.manage", effect: "allow", scope_type: "tenant", scope_id: "t" }] });
      if (path.endsWith("/onvif/discover")) {
        discoverySignal = request.signal;
        return new Promise<Response>((resolve) => { resolveDiscovery = resolve; });
      }
      return json({ code: "not_found", message: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Cameras);
    const serverSelect = await screen.findByLabelText("Servidor para descubrir cámaras");
    await within(serverSelect).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(serverSelect, { target: { value: "srv1" } });
    fireEvent.change(screen.getByLabelText("Interfaz de red del agente"), { target: { value: "eth0" } });
    await waitFor(() => expect(screen.getByRole("button", { name: "Buscar dispositivos ONVIF" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "Buscar dispositivos ONVIF" }));
    expect(await screen.findByText(/buscando dispositivos en la red/i)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Servidor para descubrir cámaras"), { target: { value: "srv2" } });
    expect(discoverySignal?.aborted).toBe(true);
    resolveDiscovery(json({ devices: [] }));
    await waitFor(() => expect(screen.queryByText(/no se encontraron dispositivos/i)).not.toBeInTheDocument());
  });

  it("aborts and ignores a deferred result when the interface changes", async () => {
    let resolveDiscovery!: (response: Response) => void;
    let discoverySignal: AbortSignal | undefined;
    const fetchMock = vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/servers") return json({ items: [{ id: "srv1", name: "frigate-h01", site_id: "s1" }] });
      if (path === "/api/v1/sites") return json({ items: [] });
      if (path === "/api/v1/cameras") return json({ items: [] });
      if (path === "/api/v1/me") return json({ id: "u", tenant_id: "t", grants: [{ id: "g", permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "srv1" }] });
      if (path.endsWith("/onvif/discover")) {
        discoverySignal = request.signal;
        return new Promise<Response>((resolve) => { resolveDiscovery = resolve; });
      }
      return json({ code: "not_found", message: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Cameras);
    const serverSelect = await screen.findByLabelText("Servidor para descubrir cámaras");
    await within(serverSelect).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(serverSelect, { target: { value: "srv1" } });
    const interfaceInput = screen.getByLabelText("Interfaz de red del agente");
    fireEvent.change(interfaceInput, { target: { value: "eth0" } });
    const search = screen.getByRole("button", { name: "Buscar dispositivos ONVIF" });
    await waitFor(() => expect(search).toBeEnabled());
    fireEvent.click(search);
    expect(await screen.findByText(/buscando dispositivos en la red/i)).toBeInTheDocument();
    fireEvent.change(interfaceInput, { target: { value: "eth1" } });
    expect(discoverySignal?.aborted).toBe(true);
    resolveDiscovery(json({ devices: [{ xaddrs: ["http://192.168.1.55/onvif"] }] }));
    await waitFor(() => expect(screen.queryByText("http://192.168.1.55/onvif")).not.toBeInTheDocument());
  });

  it("aborts a pending discovery request when the Cameras route unmounts", async () => {
    let discoverySignal: AbortSignal | undefined;
    const fetchMock = vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/servers") return json({ items: [{ id: "srv1", name: "frigate-h01", site_id: "s1" }] });
      if (path === "/api/v1/sites") return json({ items: [] });
      if (path === "/api/v1/cameras") return json({ items: [] });
      if (path === "/api/v1/me") return json({ id: "u", tenant_id: "t", grants: [{ id: "g", permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "srv1" }] });
      if (path.endsWith("/onvif/discover")) {
        discoverySignal = request.signal;
        return new Promise<Response>(() => {});
      }
      return json({ code: "not_found", message: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);
    const page = renderPage(Cameras);
    const serverSelect = await screen.findByLabelText("Servidor para descubrir cámaras");
    await within(serverSelect).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(serverSelect, { target: { value: "srv1" } });
    fireEvent.change(screen.getByLabelText("Interfaz de red del agente"), { target: { value: "eth0" } });
    const search = screen.getByRole("button", { name: "Buscar dispositivos ONVIF" });
    await waitFor(() => expect(search).toBeEnabled());
    fireEvent.click(search);
    expect(await screen.findByText(/buscando dispositivos en la red/i)).toBeInTheDocument();
    page.unmount();
    expect(discoverySignal?.aborted).toBe(true);
  });

  it("hides discovery controls when a matching deny grant overrides allow", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi({
      ...inventory([]),
      "/api/v1/servers": () => json({ items: [{ id: "srv1", name: "frigate-h01", site_id: "s1" }] }),
      "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [
        { id: "allow", permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "srv1" },
        { id: "deny", permission: "servers.manage", effect: "deny", scope_type: "server", scope_id: "srv1" },
      ] }),
    })));
    renderPage(Cameras);
    const serverSelect = await screen.findByLabelText("Servidor para descubrir cámaras");
    await within(serverSelect).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(serverSelect, { target: { value: "srv1" } });
    expect(await screen.findByText(/no tiene permiso servers\.manage/i)).toBeInTheDocument();
    expect(screen.queryByLabelText("Interfaz de red del agente")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Buscar dispositivos ONVIF" })).not.toBeInTheDocument();
  });

  it("reports empty and failed discovery responses", async () => {
    const discoveryResponses = [json({ devices: [] }), json({ code: "agent_unavailable", message: "Agent unavailable" }, 502)];
    const fetchMock = vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/servers") return json({ items: [{ id: "srv1", name: "frigate-h01", site_id: "s1" }] });
      if (path === "/api/v1/sites") return json({ items: [] });
      if (path === "/api/v1/cameras") return json({ items: [] });
      if (path === "/api/v1/me") return json({ id: "u", tenant_id: "t", grants: [{ id: "g", permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "srv1" }] });
      if (path.endsWith("/onvif/discover")) return discoveryResponses.shift() ?? json({ devices: [] });
      return json({ code: "not_found", message: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Cameras);
    const serverSelect = await screen.findByLabelText("Servidor para descubrir cámaras");
    await within(serverSelect).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(serverSelect, { target: { value: "srv1" } });
    fireEvent.change(screen.getByLabelText("Interfaz de red del agente"), { target: { value: "eth0" } });
    const search = screen.getByRole("button", { name: "Buscar dispositivos ONVIF" });
    await waitFor(() => expect(search).toBeEnabled());
    fireEvent.click(search);
    expect(await screen.findByText(/no se encontraron dispositivos/i)).toBeInTheDocument();
    fireEvent.click(search);
    expect(await screen.findByRole("alert")).toHaveTextContent("Agent unavailable");
  });

  it("summarizes the inventory and links each server to its site's servers", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi(inventory([camera("c1", "acceso"), camera("c2", "muelle", { enabled: false, status: "offline" })]))));
    renderPage(Cameras);
    expect(await screen.findByText("2 cámaras · 1 en línea · 1 deshabilitada")).toBeInTheDocument();
    const link = await screen.findAllByRole("link", { name: "frigate-h01" });
    expect(link[0]).toHaveAttribute("href", "/servers?site_id=s1");
  });

  it("starts filtered by the site or server carried in the URL", async () => {
    const fetchMock = vi.fn(stubApi(inventory([camera("c1", "acceso")])));
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Cameras, "/?server_id=srv1");
    await screen.findByText("acceso");
    const urls = fetchMock.mock.calls.map(([r]) => (r as Request).url);
    expect(urls.some((u) => u.includes("/api/v1/cameras") && u.includes("server_id=srv1"))).toBe(true);
    expect(await screen.findByLabelText("Filtrar por servidor")).toHaveValue("srv1");
  });

  it("lists what the API returns with site, server and state", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/cameras": () =>
            json({ items: [camera("c1", "acceso_norte", { lpr: true }), camera("c2", "muelle", { status: "offline", missing_since: "2026-09-26T00:00:00Z" })] }),
          "/api/v1/sites": () => json({ items: [{ id: "s1", name: "Helvecia" }] }),
          "/api/v1/servers": () => json({ items: [{ id: "srv1", name: "frigate-h01" }] }),
        }),
      ),
    );

    renderPage(Cameras);

    expect(await screen.findByText("acceso_norte")).toBeInTheDocument();
    expect(screen.getByText("LPR")).toBeInTheDocument();
    expect(screen.getByText("Fuera de línea")).toBeInTheDocument();
    expect(screen.getByText("Ya no aparece en Frigate")).toBeInTheDocument();
    expect(await screen.findAllByText("Helvecia")).not.toHaveLength(0);
  });

  it("explains an empty result", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi({ "/api/v1/cameras": () => json({ items: [] }) })));
    renderPage(Cameras);
    expect(await screen.findByText("No hay cámaras que coincidan.")).toBeInTheDocument();
  });

  it("opens one configuration page for the camera", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi(inventory([camera("c1", "acceso")]))));
    renderPage(Cameras);
    const link = await screen.findByRole("link", { name: "Configuración de acceso" });
    expect(link).toHaveAttribute("href", "/cameras/c1/frigate");
    expect(screen.queryByRole("button", { name: "Ajustes de acceso" })).not.toBeInTheDocument();
  });
});

  it("probes a selected server with transient credentials and shows read-only device details", async () => {
    const fetchMock = vi.fn(stubApi({
      ...inventory([]),
      "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [
        { permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "srv1" },
        { permission: "servers.config.secrets", effect: "allow", scope_type: "server", scope_id: "srv1" },
      ] }),
      "POST /api/v1/servers/srv1/onvif/probe": () => json({
        device_information: { manufacturer: "Acme", model: "M1", firmware_version: "1.2", serial_number: "S1", hardware_id: "H1" },
        services: [{ namespace: "device", xaddrs: ["http://192.168.1.20/onvif/device_service"], version: { major: 2, minor: 0 } }],
        system_time: { date_time_type: "NTP", utc: { time: { hour: 1, minute: 2, second: 3 }, date: { year: 2026, month: 10, day: 5 } }, local: { time: { hour: 2, minute: 2, second: 3 }, date: { year: 2026, month: 10, day: 5 } } },
      }),
    }));
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Cameras);
    const server = await screen.findByLabelText("Servidor para probar ONVIF");
    await within(server).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(server, { target: { value: "srv1" } });
    await screen.findByLabelText("Usuario ONVIF");
    fireEvent.change(screen.getByLabelText("Endpoint ONVIF"), { target: { value: "https://192.168.1.20:8443/onvif/device_service" } });
    fireEvent.change(screen.getByLabelText("Usuario ONVIF"), { target: { value: "operator" } });
    fireEvent.change(screen.getByLabelText("Usuario ONVIF"), { target: { value: "operator" } });
    fireEvent.change(screen.getByLabelText("Contraseña ONVIF"), { target: { value: "secret-pass" } });
    fireEvent.click(screen.getByRole("button", { name: "Probar y detectar" }));
    expect(await screen.findByText("Acme · M1")).toBeInTheDocument();
    expect(screen.getByText(/todavía no se guardó/i)).toBeInTheDocument();
    const request = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.url.includes("/onvif/probe"));
    expect(request?.url).toContain("/api/v1/servers/srv1/onvif/probe");
    expect(await request?.json()).toEqual({ endpoint: "https://192.168.1.20:8443/onvif/device_service", username: "operator", password: "secret-pass" });
    expect(screen.getByLabelText("Contraseña ONVIF")).toHaveValue("");
    expect(screen.getByLabelText("Usuario ONVIF")).toHaveValue("");
    expect(screen.queryByText("secret-pass")).not.toBeInTheDocument();
  });

  it("does not probe without both scoped permissions or after an explicit deny", async () => {
    const fetchMock = vi.fn(stubApi({
      ...inventory([]),
      "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [
        { permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "srv1" },
        { permission: "servers.config.secrets", effect: "allow", scope_type: "server", scope_id: "srv1" },
        { permission: "servers.config.secrets", effect: "deny", scope_type: "server", scope_id: "srv1" },
      ] }),
    }));
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Cameras);
    const server = await screen.findByLabelText("Servidor para probar ONVIF");
    await within(server).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(server, { target: { value: "srv1" } });
    expect(await screen.findByText(/no tiene permiso servers\.config\.secrets/i)).toBeInTheDocument();
    expect(screen.queryByLabelText("Contraseña ONVIF")).not.toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([r]) => (r as Request).url.includes("/onvif/probe"))).toBe(false);
  });

  it("clears the password and hides hostile API error details", async () => {
    const fetchMock = vi.fn(stubApi({
      ...inventory([]),
      "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [
        { permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "srv1" },
        { permission: "servers.config.secrets", effect: "allow", scope_type: "server", scope_id: "srv1" },
      ] }),
      "POST /api/v1/servers/srv1/onvif/probe": () => json({ code: "agent_tls_not_configured", message: "secret-pass internal host" }, 424),
    }));
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Cameras);
    const probeServer = await screen.findByLabelText("Servidor para probar ONVIF");
    await within(probeServer).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(probeServer, { target: { value: "srv1" } });
    await screen.findByLabelText("Contraseña ONVIF");
    fireEvent.change(screen.getByLabelText("Endpoint ONVIF"), { target: { value: "http://192.168.1.20/onvif/device_service" } });
    fireEvent.change(screen.getByLabelText("Usuario ONVIF"), { target: { value: "operator" } });
    fireEvent.change(screen.getByLabelText("Contraseña ONVIF"), { target: { value: "secret-pass" } });
    fireEvent.click(screen.getByRole("button", { name: "Probar y detectar" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/confianza tls/i);
    expect(screen.getByRole("alert")).not.toHaveTextContent(/secret-pass|internal host/);
    expect(screen.getByLabelText("Contraseña ONVIF")).toHaveValue("");
  });

  it("does not probe automatically and aborts a pending probe when the endpoint changes", async () => {
    let probeSignal: AbortSignal | undefined;
    let probeCalls = 0;
    const fetchMock = vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/servers") return json({ items: [{ id: "srv1", name: "frigate-h01", site_id: "s1" }] });
      if (path === "/api/v1/sites") return json({ items: [] });
      if (path === "/api/v1/cameras") return json({ items: [] });
      if (path === "/api/v1/me") return json({ id: "u", tenant_id: "t", grants: [
        { permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "srv1" },
        { permission: "servers.config.secrets", effect: "allow", scope_type: "server", scope_id: "srv1" },
      ] });
      if (path.endsWith("/onvif/probe")) {
        probeCalls++;
        probeSignal = request.signal;
        return new Promise<Response>(() => {});
      }
      return json({ code: "not_found", message: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Cameras);
    const server = await screen.findByLabelText("Servidor para probar ONVIF");
    await within(server).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(server, { target: { value: "srv1" } });
    await screen.findByLabelText("Contraseña ONVIF");
    expect(probeCalls).toBe(0);
    fireEvent.change(screen.getByLabelText("Endpoint ONVIF"), { target: { value: "http://192.0.2.1/onvif" } });
    fireEvent.change(screen.getByLabelText("Usuario ONVIF"), { target: { value: "operator" } });
    fireEvent.change(screen.getByLabelText("Contraseña ONVIF"), { target: { value: "temporary" } });
    fireEvent.click(screen.getByRole("button", { name: "Probar y detectar" }));
    expect(await screen.findByText(/consultando el dispositivo/i)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Endpoint ONVIF"), { target: { value: "http://192.0.2.2/onvif" } });
    expect(probeSignal?.aborted).toBe(true);
    expect(screen.getByLabelText("Contraseña ONVIF")).toHaveValue("");
  });

  it("does not offer credential probing without scoped manage and secrets permissions", async () => {
    const fetchMock = vi.fn(stubApi({ ...inventory([]), "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [] }) }));
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Cameras);
    const server = await screen.findByLabelText("Servidor para probar ONVIF");
    await within(server).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(server, { target: { value: "srv1" } });
    expect(await screen.findByText(/no tiene permiso servers\.manage/i)).toBeInTheDocument();
    expect(screen.queryByLabelText("Contraseña ONVIF")).not.toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([r]) => (r as Request).url.includes("/onvif/probe"))).toBe(false);
  });

  it("does not let a stale probe completion clear credentials entered for a new endpoint", async () => {
    let resolveProbe!: (response: Response) => void;
    const fetchMock = vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/servers") return json({ items: [{ id: "srv1", name: "frigate-h01", site_id: "s1" }] });
      if (path === "/api/v1/sites") return json({ items: [] });
      if (path === "/api/v1/cameras") return json({ items: [] });
      if (path === "/api/v1/me") return json({ id: "u", tenant_id: "t", grants: [
        { permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "srv1" },
        { permission: "servers.config.secrets", effect: "allow", scope_type: "server", scope_id: "srv1" },
      ] });
      if (path.endsWith("/onvif/probe")) return new Promise<Response>((resolve) => { resolveProbe = resolve; });
      return json({ code: "not_found", message: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Cameras);
    const server = await screen.findByLabelText("Servidor para probar ONVIF");
    await within(server).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(server, { target: { value: "srv1" } });
    await screen.findByLabelText("Contraseña ONVIF");
    const endpoint = screen.getByLabelText("Endpoint ONVIF");
    fireEvent.change(endpoint, { target: { value: "http://192.0.2.1/onvif" } });
    fireEvent.change(screen.getByLabelText("Usuario ONVIF"), { target: { value: "old-user" } });
    fireEvent.change(screen.getByLabelText("Contraseña ONVIF"), { target: { value: "old-pass" } });
    fireEvent.click(screen.getByRole("button", { name: "Probar y detectar" }));
    expect(await screen.findByText(/consultando el dispositivo/i)).toBeInTheDocument();

    fireEvent.change(endpoint, { target: { value: "http://192.0.2.2/onvif" } });
    fireEvent.change(screen.getByLabelText("Usuario ONVIF"), { target: { value: "new-user" } });
    fireEvent.change(screen.getByLabelText("Contraseña ONVIF"), { target: { value: "new-pass" } });
    resolveProbe(json({ device_information: { manufacturer: "Old", model: "Old", firmware_version: "", serial_number: "", hardware_id: "" }, services: [], system_time: { date_time_type: "", utc: { time: { hour: 0, minute: 0, second: 0 }, date: { year: 0, month: 0, day: 0 } }, local: { time: { hour: 0, minute: 0, second: 0 }, date: { year: 0, month: 0, day: 0 } } } }));
    await waitFor(() => expect(screen.getByLabelText("Contraseña ONVIF")).toHaveValue("new-pass"));
    expect(screen.getByLabelText("Usuario ONVIF")).toHaveValue("new-user");
    expect(screen.queryByText("Old · Old")).not.toBeInTheDocument();
  });

  it("aborts a pending probe when the selected server changes or the route unmounts", async () => {
    const probeSignals: AbortSignal[] = [];
    const fetchMock = vi.fn(async (request: Request) => {
      const path = new URL(request.url).pathname;
      if (path === "/api/v1/servers") return json({ items: [{ id: "srv1", name: "frigate-h01", site_id: "s1" }, { id: "srv2", name: "frigate-r01", site_id: "s1" }] });
      if (path === "/api/v1/sites") return json({ items: [] });
      if (path === "/api/v1/cameras") return json({ items: [] });
      if (path === "/api/v1/me") return json({ id: "u", tenant_id: "t", grants: [
        { permission: "servers.manage", effect: "allow", scope_type: "tenant", scope_id: "t" },
        { permission: "servers.config.secrets", effect: "allow", scope_type: "tenant", scope_id: "t" },
      ] });
      if (path.endsWith("/onvif/probe")) {
        probeSignals.push(request.signal);
        return new Promise<Response>(() => {});
      }
      return json({ code: "not_found", message: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);
    const page = renderPage(Cameras);
    const server = await screen.findByLabelText("Servidor para probar ONVIF");
    await within(server).findByRole("option", { name: "frigate-h01" });
    fireEvent.change(server, { target: { value: "srv1" } });
    await screen.findByLabelText("Contraseña ONVIF");
    fireEvent.change(screen.getByLabelText("Endpoint ONVIF"), { target: { value: "http://192.0.2.10/onvif" } });
    fireEvent.change(screen.getByLabelText("Usuario ONVIF"), { target: { value: "operator" } });
    fireEvent.change(screen.getByLabelText("Contraseña ONVIF"), { target: { value: "secret" } });
    fireEvent.click(screen.getByRole("button", { name: "Probar y detectar" }));
    expect(await screen.findByText(/consultando el dispositivo/i)).toBeInTheDocument();

    fireEvent.change(server, { target: { value: "srv2" } });
    expect(probeSignals[0]?.aborted).toBe(true);
    await waitFor(() => expect(screen.getByLabelText("Contraseña ONVIF")).toHaveValue(""));
    fireEvent.change(screen.getByLabelText("Usuario ONVIF"), { target: { value: "operator" } });
    fireEvent.change(screen.getByLabelText("Contraseña ONVIF"), { target: { value: "secret" } });
    fireEvent.click(screen.getByRole("button", { name: "Probar y detectar" }));
    expect(await screen.findByText(/consultando el dispositivo/i)).toBeInTheDocument();
    page.unmount();
    expect(probeSignals[1]?.aborted).toBe(true);
  });
