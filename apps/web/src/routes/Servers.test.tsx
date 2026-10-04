import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Servers } from "./Servers";

afterEach(() => vi.unstubAllGlobals());

const server = (id: string, name: string, site_id: string, extra: object = {}) => ({
  id,
  tenant_id: "t",
  site_id,
  name,
  base_url: `https://${name}:8971`,
  status: "online",
  frigate_version: "0.16",
  camera_count: 4,
  ...extra,
});

function stub() {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      stubApi({
        "/api/v1/servers": () => json({ items: [server("a", "frigate-h01", "s1"), server("b", "frigate-r01", "s2", { status: "offline" })] }),
        "/api/v1/sites": () => json({ items: [{ id: "s1", name: "Helvecia" }, { id: "s2", name: "Rosario" }] }),
        "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [] }),
      }),
    ),
  );
}

describe("Servers", () => {
  it("summarizes health and links camera counts to the server's cameras", async () => {
    stub();
    renderPage(Servers);
    expect(await screen.findByText("2 servidores · 1 en línea · 1 fuera de línea")).toBeInTheDocument();
    expect((await screen.findAllByRole("link", { name: "4" }))[0]).toHaveAttribute("href", "/cameras?server_id=a");
  });

  it("narrows to the site carried in the URL and lets the user clear it", async () => {
    stub();
    renderPage(Servers, "/?site_id=s1");
    expect(await screen.findByText("frigate-h01")).toBeInTheDocument();
    expect(screen.queryByText("frigate-r01")).not.toBeInTheDocument();
    expect(screen.getByText(/^1 de 2 servidores/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Quitar filtro de sitio" })).toHaveAttribute("href", "/servers");
  });

  it("shows restart button only for users with servers.restart and handles confirmation", async () => {
    const fetchMock = vi.fn(
      stubApi({
        "/api/v1/servers": () => json({ items: [server("a", "frigate-h01", "s1")] }),
        "/api/v1/sites": () => json({ items: [{ id: "s1", name: "Helvecia" }] }),
        "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [{ permission: "servers.restart", effect: "allow" }] }),
        "/api/v1/servers/a/restart": () => json({ success: true }),
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    renderPage(Servers);

    const restartBtn = await screen.findByRole("button", { name: /Reiniciar/i });
    expect(restartBtn).toBeInTheDocument();

    // Click restart -> shows confirmation
    restartBtn.click();
    expect(await screen.findByText("¿Reiniciar?")).toBeInTheDocument();
    const confirmBtn = screen.getByRole("button", { name: "Sí, reiniciar" });

    // Confirm restart -> calls POST /api/v1/servers/a/restart
    confirmBtn.click();
    expect(await screen.findByText("Reinicio solicitado")).toBeInTheDocument();

    const postCall = fetchMock.mock.calls.find(([r]) => (r as Request).method === "POST" && (r as Request).url.includes("/api/v1/servers/a/restart"));
    expect(postCall).toBeDefined();
  });

  it("does not show restart button without servers.restart permission", async () => {
    stub();
    renderPage(Servers);
    await screen.findByText("frigate-h01");
    expect(screen.queryByRole("button", { name: /Reiniciar/i })).not.toBeInTheDocument();
  });

  describe("edit and delete", () => {
    const manager = { id: "u", tenant_id: "t", grants: [{ permission: "servers.manage", effect: "allow" }] };
    function setup(me: object) {
      const fetchMock = vi.fn(
        stubApi({
          "/api/v1/servers": () => json({ items: [server("a", "frigate-h01", "s1", { auth_mode: "credentials", username: "admin", tls_skip_verify: false })] }),
          "/api/v1/sites": () => json({ items: [{ id: "s1", name: "Helvecia" }, { id: "s2", name: "Rosario" }] }),
          "/api/v1/me": () => json(me),
          "/api/v1/servers/a": () => json(server("a", "frigate-h01", "s1")),
        }),
      );
      vi.stubGlobal("fetch", fetchMock);
      return fetchMock;
    }
    const writes = (m: ReturnType<typeof setup>, method: string) =>
      m.mock.calls.map(([r]) => r as Request).filter((r) => r.method === method && new URL(r.url).pathname === "/api/v1/servers/a");

    it("offers a new Frigate and an existing one that stays untouched", async () => {
      const fetchMock = setup(manager);
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: "Registrar servidor" }));
      expect(screen.getByRole("button", { name: /Instalar Frigate en un host nuevo/ })).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: /Registrar un Frigate existente/ }));
      expect(screen.getByRole("form", { name: "Importar servidor Frigate existente" }).querySelector('input[type="url"]')).toBeInTheDocument();
      expect(screen.queryByLabelText("Host SSH")).not.toBeInTheDocument();
      const calls = fetchMock.mock.calls.map(([r]) => new URL((r as Request).url).pathname);
      expect(calls.filter((path) => path.includes("probe"))).toHaveLength(0);
    });

    it("offers a fresh-host SSH installation with its full system side effects disclosed", async () => {
      setup(manager);
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: "Registrar servidor" }));
      fireEvent.click(screen.getByRole("button", { name: /Instalar Frigate en un host nuevo/ }));
      const form = screen.getByRole("form", { name: "Instalación de Frigate nuevo" });
      expect(within(form).getByLabelText(/Permitir temporalmente grabaciones en el disco del sistema/i)).not.toBeChecked();
      expect(within(form).getByLabelText("Host SSH")).toBeInTheDocument();
      expect(form).toHaveTextContent(/actualiza paquetes.*Docker.*Frigate/s);
      expect(form).toHaveTextContent(/Chrony\/NTP/);
      expect(within(form).queryByLabelText("URL de Frigate")).not.toBeInTheDocument();
    });

    it("imports an existing Frigate through probe/create and never starts the new-host installer", async () => {
      const fetchMock = vi.fn(stubApi({
        "/api/v1/servers": () => json({ items: [] }),
        "/api/v1/sites": () => json({ items: [{ id: "s2", name: "Exalink" }] }),
        "/api/v1/me": () => json(manager),
        "POST /api/v1/servers/probe": () => json({ frigate_version: "0.16", adapter: "v1", cameras: [], capabilities: { review: false, preview: false, exports: false, lpr: false, face_recognition: false, semantic_search: false, audio: false, ptz: false } }),
        "POST /api/v1/servers": () => json({ id: "server-1", site_id: "s2", name: "Frigate existente", base_url: "http://10.1.1.144:5000", status: "online", camera_count: 0 }),
      }));
      vi.stubGlobal("fetch", fetchMock);
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: "Registrar servidor" }));
      fireEvent.click(screen.getByRole("button", { name: /Registrar un Frigate existente/ }));
      const form = screen.getByRole("form", { name: "Importar servidor Frigate existente" });
      fireEvent.change(within(form).getByLabelText("Sitio"), { target: { value: "s2" } });
      fireEvent.change(within(form).getByLabelText("Nombre"), { target: { value: "Frigate existente" } });
      fireEvent.change(within(form).getByLabelText("Acceso a Frigate"), { target: { value: "none" } });
      fireEvent.change(within(form).getByPlaceholderText("http://10.20.0.11:5000"), { target: { value: "http://10.1.1.144:5000" } });
      fireEvent.click(within(form).getByRole("button", { name: "Probar conexión" }));
      await waitFor(() => expect(fetchMock.mock.calls.some(([r]) => new URL((r as Request).url).pathname === "/api/v1/servers/probe")).toBe(true));
      const paths = fetchMock.mock.calls.map(([r]) => new URL((r as Request).url).pathname);
      expect(paths).toContain("/api/v1/servers/probe");
      expect(paths).not.toContain("/api/v1/servers/provision");
    });

    it("requires a site and server display name, then explicitly opts into first-contact SSH trust", async () => {
      const fetchMock = setup(manager);
      fetchMock.mockImplementation(vi.fn(stubApi({
        "/api/v1/servers": () => json({ items: [] }),
        "/api/v1/sites": () => json({ items: [{ id: "s1", name: "Helvecia" }, { id: "s2", name: "Exalink" }] }),
        "/api/v1/me": () => json(manager),
        "/api/v1/servers/provision": () => json({ id: "job-1", status: "running", warning: "cpu_system_disk", steps: [{ id: "connecting", state: "running" }] }, 202),
        "/api/v1/servers/provision/job-1": () => json({ id: "job-1", status: "running", warning: "cpu_system_disk", steps: [{ id: "connecting", state: "running" }] }),
      })));
      vi.stubGlobal("fetch", fetchMock);
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: "Registrar servidor" }));
      fireEvent.click(screen.getByRole("button", { name: /Instalar Frigate en un host nuevo/ }));
      const form = screen.getByRole("form", { name: "Instalación de Frigate nuevo" });
      fireEvent.click(within(form).getByLabelText(/Permitir temporalmente grabaciones en el disco del sistema/i));
      expect(screen.getByText(/Si \/mnt\/cctv no está montado/i)).toBeInTheDocument();
      fireEvent.change(within(form).getByLabelText("Sitio"), { target: { value: "s2" } });
      fireEvent.change(within(form).getByLabelText("Nombre visible del servidor"), { target: { value: "Exalink Frigate" } });
      fireEvent.change(within(form).getByLabelText("Host SSH"), { target: { value: "10.1.1.144" } });
      fireEvent.change(within(form).getByLabelText("Usuario SSH"), { target: { value: "root" } });
      fireEvent.change(within(form).getByLabelText(/^Contraseña SSH/), { target: { value: "one-time-secret" } });
      expect(screen.getByText(/La identidad SSH del host no se verifica automáticamente/i)).toBeInTheDocument();
      fireEvent.click(within(form).getByRole("button", { name: "Instalar agente" }));
      await waitFor(() => expect(fetchMock.mock.calls.some(([r]) => (r as Request).url.endsWith("/api/v1/servers/provision"))).toBe(true));
      const request = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.url.endsWith("/api/v1/servers/provision"));
      expect(await request!.clone().json()).toEqual({ site_id: "s2", server_name: "Exalink Frigate", ip: "10.1.1.144", ssh_user: "root", ssh_password: "one-time-secret", trust_on_first_use: true, allow_system_disk: true });
      expect(await screen.findByText(/Conectando|connecting/i)).toBeInTheDocument();
      expect(screen.getByText("Instalación en curso para Exalink")).toBeInTheDocument();
      expect(screen.getByText(/Frigate usará CPU/)).toBeInTheDocument();
      expect(screen.getByText(/Frigate guardará las grabaciones en el disco del sistema/i)).toBeInTheDocument();
      expect(screen.queryByText("one-time-secret")).not.toBeInTheDocument();
    });

    it("loads agent metrics only while mounted and exposes an authorized update action", async () => {
      const fetchMock = vi.fn(stubApi({
        "/api/v1/servers": () => json({ items: [server("a", "frigate-h01", "s1")] }),
        "/api/v1/sites": () => json({ items: [{ id: "s1", name: "Helvecia" }] }),
        "/api/v1/me": () => json(manager),
        "/api/v1/servers/a/agent": () => json({ installed: true, version: "1", current_version: "2", outdated: true, cpu_percent: 72, memory_total_bytes: 100, memory_available_bytes: 20 }),
        "/api/v1/servers/a/agent/update": () => json({ installed: true, version: "2", current_version: "2", outdated: false }),
      }));
      vi.stubGlobal("fetch", fetchMock);
      const view = renderPage(Servers);
      expect(await screen.findByText(/72%/)).toBeInTheDocument();
      const metrics = fetchMock.mock.calls.filter(([r]) => new URL((r as Request).url).pathname === "/api/v1/servers/a/agent" && (r as Request).method === "GET");
      expect(metrics.length).toBeGreaterThan(0);
      fireEvent.click(screen.getByRole("button", { name: /Actualizar agente/i }));
      await waitFor(() => expect(fetchMock.mock.calls.some(([r]) => new URL((r as Request).url).pathname === "/api/v1/servers/a/agent/update")).toBe(true));
      const countBeforeUnmount = fetchMock.mock.calls.filter(([r]) => new URL((r as Request).url).pathname === "/api/v1/servers/a/agent" && (r as Request).method === "GET").length;
      view.unmount();
      await new Promise((resolve) => setTimeout(resolve, 5200));
      const countAfterUnmount = fetchMock.mock.calls.filter(([r]) => new URL((r as Request).url).pathname === "/api/v1/servers/a/agent" && (r as Request).method === "GET").length;
      expect(countAfterUnmount).toBe(countBeforeUnmount);
    }, 10_000);

    it("hides edit and delete without servers.manage", async () => {
      setup({ id: "u", tenant_id: "t", grants: [] });
      renderPage(Servers);
      await screen.findByText("frigate-h01");
      expect(screen.queryByRole("button", { name: "Editar" })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Eliminar" })).not.toBeInTheDocument();
    });

    it("edits with PATCH sending only the changed fields, blank password stays unchanged", async () => {
      const fetchMock = setup(manager);
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: "Editar" }));
      const dialog = await screen.findByRole("dialog", { name: /Editar servidor/ });
      fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "frigate-nuevo" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      const [patch] = writes(fetchMock, "PATCH");
      expect(await patch!.clone().json()).toEqual({ name: "frigate-nuevo" });
    });

    it("sends the new password and connection fields when they change", async () => {
      const fetchMock = setup(manager);
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: "Editar" }));
      const dialog = await screen.findByRole("dialog", { name: /Editar servidor/ });
      fireEvent.change(within(dialog).getByLabelText(/^Contraseña/), { target: { value: "s3cret" } });
      fireEvent.change(within(dialog).getByLabelText("Sitio"), { target: { value: "s2" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      const [patch] = writes(fetchMock, "PATCH");
      expect(await patch!.clone().json()).toEqual({ site_id: "s2", password: "s3cret" });
    });

    it("asks for confirmation and deletes only after confirming", async () => {
      const fetchMock = setup(manager);
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: "Eliminar" }));
      const dialog = await screen.findByRole("dialog", { name: "Eliminar servidor" });
      expect(dialog).toHaveTextContent("¿Está seguro de eliminar el servidor «frigate-h01»?");
      expect(dialog).toHaveTextContent("Esta acción no se puede deshacer.");
      expect(writes(fetchMock, "DELETE")).toHaveLength(0);
      fireEvent.click(within(dialog).getByRole("button", { name: "Eliminar" }));
      await waitFor(() => expect(writes(fetchMock, "DELETE")).toHaveLength(1));
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      const lists = fetchMock.mock.calls.filter(([r]) => new URL((r as Request).url).pathname === "/api/v1/servers" && (r as Request).method === "GET");
      expect(lists.length).toBeGreaterThan(1);
    });

    it("cancelling the confirmation does not delete", async () => {
      const fetchMock = setup(manager);
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: "Eliminar" }));
      const dialog = await screen.findByRole("dialog", { name: "Eliminar servidor" });
      fireEvent.click(within(dialog).getByRole("button", { name: "Cancelar" }));
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      expect(writes(fetchMock, "DELETE")).toHaveLength(0);
    });
  });
});
