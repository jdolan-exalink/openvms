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
