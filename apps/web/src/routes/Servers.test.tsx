import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Servers } from "./Servers";

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

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

  describe("agent-only SSH installation", () => {
    const installGrants = [
      { permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "a" },
      { permission: "servers.config.secrets", effect: "allow", scope_type: "server", scope_id: "a" },
    ];
    const setupAgentInstall = (
      grants = installGrants,
      installHandler?: (request: Request) => Promise<Response>,
      pollHandler?: (request: Request) => Promise<Response>,
      httpsPortHandler?: (request: Request) => Promise<Response>,
      agentStatus: () => object = () => ({ installed: false, binary_status: "not_installed", binary_upgrade_available: false }),
    ) => {
      const calls: Request[] = [];
      const fetchMock = vi.fn(async (request: Request) => {
        calls.push(request);
        const url = new URL(request.url);
        if (url.pathname === "/api/v1/servers") return json({ items: [server("a", "frigate-h01", "s1"), server("b", "frigate-r01", "s2")] });
        if (url.pathname === "/api/v1/sites") return json({ items: [{ id: "s1", name: "Helvecia" }, { id: "s2", name: "Rosario" }] });
        if (url.pathname === "/api/v1/me") return json({ id: "u", tenant_id: "t", grants });
        if (url.pathname === "/api/v1/servers/a/agent") return json(agentStatus());
        if (url.pathname === "/api/v1/servers/b/agent") return json({ installed: false, binary_status: "not_installed", binary_upgrade_available: false });
        if (url.pathname === "/api/v1/classify/policy") return json({ servers: [] });
        if (url.pathname === "/api/v1/sync/status") return json({ items: [] });
        if (url.pathname === "/api/v1/servers/a/agent/install" && request.method === "POST" && installHandler) return installHandler(request);
        if (url.pathname === "/api/v1/servers/a/agent/install/job-1") return pollHandler?.(request) ?? json({ id: "job-1", status: "succeeded", stage: "complete" });
        if (url.pathname === "/.well-known/openvms-https-port") return httpsPortHandler?.(request) ?? new Response("8443", { status: 200 });
        return json({ code: "not_found", message: "not found" }, 404);
      });
      vi.stubGlobal("fetch", fetchMock);
      return { calls, fetchMock };
    };

    it("shows rounded CPU, only fresh per-interface rates, and explicit verified-outdated emphasis", async () => {
      setupAgentInstall(installGrants, undefined, undefined, undefined, () => ({
        installed: true,
        version: "0.1.0",
        cpu_percent: 72.6,
        network_interfaces: [{ name: "eth0", rx_bytes_per_second: 1024, tx_bytes_per_second: 2048 }],
        network_sampled_at: new Date(Date.now() - 1000).toISOString(),
        binary_status: "update_available",
        binary_outdated: true,
      }));
      renderPage(Servers);
      expect(await screen.findByText("CPU 73%")).toBeInTheDocument();
      expect(screen.getByText("frigate-h01").closest("li")?.textContent).toContain("eth0");
      expect(screen.getByText(/eth0.*1\.0 KiB\/s.*2\.0 KiB\/s/)).toBeInTheDocument();
      expect(screen.getByRole("status", { name: /binario del agente está desactualizado/i })).toHaveClass("font-semibold", "text-warn");
    });

    it("does not display legacy missing resource samples as zero or claim outdated state", async () => {
      setupAgentInstall(installGrants, undefined, undefined, undefined, () => ({ installed: true, version: "0.1.0" }));
      renderPage(Servers);
      expect(await screen.findByText(/CPU no disponible/i)).toBeInTheDocument();
      expect(screen.queryByText(/0\.0 KiB\/s/)).not.toBeInTheDocument();
      expect(screen.queryByRole("status", { name: /versión del agente desactualizada/i })).not.toBeInTheDocument();
    });

    it.each([-16_000, 5_000])("treats stale or future network samples as unavailable (%i ms)", async (offset) => {
      setupAgentInstall(installGrants, undefined, undefined, undefined, () => ({
        installed: true,
        cpu_percent: 150,
        network_interfaces: [{ name: "eth0", rx_bytes_per_second: 1024, tx_bytes_per_second: 1024 }],
        network_sampled_at: new Date(Date.now() + offset).toISOString(),
        binary_status: "current",
        binary_outdated: false,
      }));
      renderPage(Servers);
      expect(await screen.findByText(/Tasas de red no disponibles/i)).toBeInTheDocument();
      expect(screen.getByText("CPU 100%")).toBeInTheDocument();
      expect(screen.queryByRole("status", { name: /binario del agente está desactualizado/i })).not.toBeInTheDocument();
    });

    it("does not request a manual SSH fingerprint", async () => {
      setupAgentInstall();
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      expect(within(dialog).queryByLabelText(/Huella SHA-256 SSH verificada/i)).not.toBeInTheDocument();
      expect(within(dialog).getByText(/primer contacto/i)).toBeInTheDocument();
    });

    it("shows no elapsed time while submission is pending and starts after a job is accepted", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      let accept!: (response: Response) => void;
      let failPoll = false;
      setupAgentInstall(
        installGrants,
        () => new Promise((resolve) => { accept = resolve; }),
        async () => failPoll ? json({ code: "not_found", message: "unknown" }, 404) : json({ id: "job-1", status: "queued", stage: "validating" }),
      );
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      fireEvent.change(within(dialog).getByLabelText("Host SSH IPv4"), { target: { value: "10.20.30.44" } });
      fireEvent.click(within(dialog).getByRole("checkbox", { name: /Confirmo que quiero instalar únicamente el agente OpenVMS/i }));
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "one-time" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Instalar agente" }));
      expect(await within(dialog).findByText(/Iniciando la operación del agente/i)).toBeInTheDocument();
      expect(within(dialog).queryByText(/Tiempo transcurrido:/i)).not.toBeInTheDocument();
      expect(within(dialog).getByRole("status").querySelector("[aria-hidden]")).toHaveClass("motion-safe:animate-spin", "motion-reduce:animate-none");
      accept(json({ id: "job-1", status: "queued", stage: "validating" }, 202));
      expect(await within(dialog).findByText(/Tiempo transcurrido: 0:00/i)).toBeInTheDocument();
      expect(await within(dialog).findByText(/Tiempo transcurrido: 0:01/i, {}, { timeout: 3000 })).toBeInTheDocument();
      failPoll = true;
      expect(await within(dialog).findByText(/No se puede confirmar el resultado/i, {}, { timeout: 3000 })).toBeInTheDocument();
      const frozen = within(dialog).getByRole("timer").textContent;
      expect(within(dialog).getByRole("timer").querySelector("[aria-hidden]")).not.toHaveClass("motion-safe:animate-spin");
      await new Promise((resolve) => window.setTimeout(resolve, 1100));
      expect(within(dialog).getByRole("timer")).toHaveTextContent(frozen ?? "");
    }, 10_000);

    it("requires both matching scoped grants and respects an explicit deny", async () => {
      setupAgentInstall([installGrants[0]!]);
      renderPage(Servers);
      await screen.findByText("frigate-h01");
      expect(screen.queryByRole("button", { name: /Instalar agente vía SSH/i })).not.toBeInTheDocument();
      expect(screen.getByRole("note")).toHaveTextContent(/servers\.manage y servers\.config\.secrets/i);
    });

    it("does not offer installation when a matching server deny overrides both allows", async () => {
      setupAgentInstall([...installGrants, { permission: "servers.manage", effect: "deny", scope_type: "server", scope_id: "a" }]);
      renderPage(Servers);
      await screen.findByText("frigate-h01");
      expect(screen.queryByRole("button", { name: /Instalar agente vía SSH/i })).not.toBeInTheDocument();
    });

    it("does not submit root credentials from an HTTP browser", async () => {
      setupAgentInstall();
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      expect(within(dialog).getByLabelText("Host SSH IPv4")).toHaveValue("");
      fireEvent.change(within(dialog).getByLabelText("Host SSH IPv4"), { target: { value: "10.0.0.44" } });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("checkbox", { name: /Confirmo que quiero instalar únicamente el agente OpenVMS/i }));
      fireEvent.click(within(dialog).getByRole("button", { name: "Instalar agente" }));
      expect(await screen.findByRole("alert")).toHaveTextContent(/HTTPS antes de enviar credenciales/i);
      const posts = (await screen.findAllByRole("dialog")).flatMap((element) => element.querySelectorAll("form")).length;
      expect(posts).toBeGreaterThan(0);
      expect(Array.from({ length: localStorage.length }, (_, index) => localStorage.getItem(localStorage.key(index) ?? "") ?? "").join(" ")).not.toContain("secret-root-password");
      expect(Array.from({ length: sessionStorage.length }, (_, index) => sessionStorage.getItem(sessionStorage.key(index) ?? "") ?? "").join(" ")).not.toContain("secret-root-password");
      expect((globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls.some(([request]) => (request as Request).method === "POST" && new URL((request as Request).url).pathname.includes("/agent/install"))).toBe(false);
      fireEvent.click(within(dialog).getByRole("button", { name: "Cancelar" }));
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      expect(document.body).not.toHaveTextContent("secret-root-password");
    });

    it("redirects only on explicit click using configured port and same path without URL credentials", async () => {
      const assign = vi.fn();
      vi.stubGlobal("location", { protocol: "http:", origin: "http://openvms.example:8000", href: "http://operator:secret@openvms.example:8000/servers?token=query-secret#private", assign });
      const { calls } = setupAgentInstall(installGrants, undefined, undefined, async (request) => {
        expect(request.method).toBe("GET");
        expect(new URL(request.url).origin).toBe("http://openvms.example:8000");
        expect(request.credentials).toBe("omit");
        expect(request.redirect).toBe("error");
        return new Response("9443", { status: 200, headers: { "Content-Type": "text/plain" } });
      });
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      const redirect = within(dialog).getByRole("button", { name: /Abrir OpenVMS por HTTPS/i });
      expect(assign).not.toHaveBeenCalled();
      fireEvent.click(redirect);
      await waitFor(() => expect(assign).toHaveBeenCalledTimes(1));
      const target = new URL(assign.mock.calls[0]![0] as string);
      expect(target.origin).toBe("https://openvms.example:9443");
      expect(target.pathname).toBe("/servers");
      expect(target.search).toBe("");
      expect(target.hash).toBe("");
      expect(target.username).toBe("");
      expect(target.password).toBe("");
      expect(within(dialog).getByLabelText("Contraseña SSH root")).toHaveValue("");
      expect(document.body).not.toHaveTextContent("secret-root-password");
      expect(calls.some((request) => request.method === "POST" && new URL(request.url).pathname.includes("/agent/install"))).toBe(false);
    });

    it("does not navigate on an invalid runtime port", async () => {
      const assign = vi.fn();
      vi.stubGlobal("location", { protocol: "http:", origin: "http://openvms.example:8000", href: "http://openvms.example:8000/servers", assign });
      setupAgentInstall(installGrants, undefined, undefined, async () => new Response("65536", { status: 200, headers: { "Content-Type": "text/plain" } }));
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      fireEvent.click(within(dialog).getByRole("button", { name: /Abrir OpenVMS por HTTPS/i }));
      expect(await within(dialog).findByText(/No se pudo leer el puerto HTTPS/i)).toHaveAttribute("role", "alert");
      expect(assign).not.toHaveBeenCalled();
    });

    it("does not guess a port when the runtime hint is unavailable", async () => {
      const assign = vi.fn();
      vi.stubGlobal("location", { protocol: "http:", origin: "http://openvms.example:8000", href: "http://openvms.example:8000/servers", assign });
      setupAgentInstall(installGrants, undefined, undefined, async () => new Response("unavailable", { status: 503, headers: { "Content-Type": "text/plain" } }));
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      fireEvent.click(within(dialog).getByRole("button", { name: /Abrir OpenVMS por HTTPS/i }));
      expect(await within(dialog).findByText(/No se pudo leer el puerto HTTPS/i)).toHaveAttribute("role", "alert");
      expect(assign).not.toHaveBeenCalled();
    });

    it("rejects an oversized runtime port response without navigation", async () => {
      const assign = vi.fn();
      vi.stubGlobal("location", { protocol: "http:", origin: "http://openvms.example:8000", href: "http://openvms.example:8000/servers", assign });
      setupAgentInstall(installGrants, undefined, undefined, async () => new Response("9".repeat(64 * 1024), { status: 200, headers: { "Content-Type": "text/plain" } }));
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      fireEvent.click(within(dialog).getByRole("button", { name: /Abrir OpenVMS por HTTPS/i }));
      expect(await within(dialog).findByText(/No se pudo leer el puerto HTTPS/i)).toHaveAttribute("role", "alert");
      expect(assign).not.toHaveBeenCalled();
    });

    it("aborts the port lookup at its request deadline", async () => {
      const assign = vi.fn();
      vi.stubGlobal("location", { protocol: "http:", origin: "http://openvms.example:8000", href: "http://openvms.example:8000/servers", assign });
      const scheduled: Array<() => void> = [];
      const setTimeout = window.setTimeout.bind(window);
      vi.spyOn(window, "setTimeout").mockImplementation(((handler: TimerHandler, timeout?: number, ...args: unknown[]) => {
        if (timeout === 5000 && typeof handler === "function") scheduled.push(handler as () => void);
        return setTimeout(handler, timeout, ...args);
      }) as typeof window.setTimeout);
      let configRequest: Request | undefined;
      setupAgentInstall(installGrants, undefined, undefined, async (request) => {
        configRequest = request;
        return new Promise((_resolve, reject) => request.signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), { once: true }));
      });
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      fireEvent.click(within(dialog).getByRole("button", { name: /Abrir OpenVMS por HTTPS/i }));
      await waitFor(() => expect(configRequest).toBeDefined());
      expect(scheduled).toHaveLength(1);
      await act(async () => scheduled[0]!());
      expect(configRequest!.signal.aborted).toBe(true);
      expect(await within(dialog).findByText(/No se pudo leer el puerto HTTPS/i)).toHaveAttribute("role", "alert");
      expect(assign).not.toHaveBeenCalled();
    });

    it("aborts the HTTPS-port lookup when the dialog closes and never redirects late", async () => {
      const assign = vi.fn();
      vi.stubGlobal("location", { protocol: "http:", origin: "http://openvms.example:8000", href: "http://openvms.example:8000/servers", assign });
      let configRequest: Request | undefined;
      setupAgentInstall(installGrants, undefined, undefined, async (request) => {
        configRequest = request;
        return new Promise((_resolve, reject) => request.signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), { once: true }));
      });
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      fireEvent.click(within(dialog).getByRole("button", { name: /Abrir OpenVMS por HTTPS/i }));
      await waitFor(() => expect(configRequest).toBeDefined());
      fireEvent.click(within(dialog).getByRole("button", { name: "Cancelar" }));
      await waitFor(() => expect(configRequest!.signal.aborted).toBe(true));
      expect(assign).not.toHaveBeenCalled();
    });

    it("sends the explicit pinned target once, clears the password, polls metadata, and does not claim health", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      const { calls } = setupAgentInstall(undefined, async (request) => {
        const body = await request.clone().json();
        expect(body).toEqual({ ssh_host: "10.20.30.44", ssh_port: 2222, ssh_password: "secret-root-password" });
        return json({ id: "job-1", status: "queued", stage: "validating" }, 202);
      });
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      expect(within(dialog).queryByRole("button", { name: /Abrir OpenVMS por HTTPS/i })).not.toBeInTheDocument();
      fireEvent.change(within(dialog).getByLabelText("Host SSH IPv4"), { target: { value: "10.20.30.44" } });
      fireEvent.change(within(dialog).getByLabelText("Puerto SSH"), { target: { value: "2222" } });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("checkbox", { name: /Confirmo que quiero instalar únicamente el agente OpenVMS/i }));
      fireEvent.click(within(dialog).getByRole("button", { name: "Instalar agente" }));
      expect(await screen.findByText(/HTTPS aún no está verificada/i)).toBeInTheDocument();
      expect(screen.queryByDisplayValue("secret-root-password")).not.toBeInTheDocument();
      const post = calls.find((request) => request.method === "POST" && new URL(request.url).pathname.endsWith("/agent/install"));
      expect(post).toBeDefined();
      expect(new URL(post!.url).search).toBe("");
      await waitFor(() => expect(calls.some((request) => request.method === "GET" && new URL(request.url).pathname.endsWith("/install/job-1"))).toBe(true));
      const polls = calls.filter((request) => request.method === "GET" && new URL(request.url).pathname.endsWith("/install/job-1"));
      expect(polls.every((request) => request.body === null)).toBe(true);
      expect(document.body).not.toHaveTextContent("secret-root-password");
    });

    it("returns to verified agent status after installation completes", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      const agent = { installed: false, binary_status: "not_installed", binary_upgrade_available: false };
      const { calls } = setupAgentInstall(installGrants, async () => json({ id: "job-1", status: "queued", stage: "validating" }, 202), async () => {
        Object.assign(agent, { installed: true, binary_status: "current", binary_outdated: false, binary_upgrade_available: false, binary_observed: { sha256: "a".repeat(64), architecture: "amd64" }, binary_available: { sha256: "a".repeat(64), architecture: "amd64" } });
        return json({ id: "job-1", status: "succeeded", stage: "complete" });
      }, undefined, () => agent);
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      fireEvent.change(within(dialog).getByLabelText("Host SSH IPv4"), { target: { value: "10.20.30.44" } });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("checkbox", { name: /Confirmo que quiero instalar únicamente el agente OpenVMS/i }));
      fireEvent.click(within(dialog).getByRole("button", { name: "Instalar agente" }));
      expect(await screen.findByRole("button", { name: /Agente actualizado/i })).toBeDisabled();
      expect(calls.filter((request) => request.method === "POST" && new URL(request.url).pathname.endsWith("/agent/install"))).toHaveLength(1);
    });

    it("clears the password on a start error and hides hostile API details", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      let respond: ((response: Response) => void) | undefined;
      setupAgentInstall(undefined, async () => new Promise((resolve) => { respond = resolve; }));
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      fireEvent.change(within(dialog).getByLabelText("Host SSH IPv4"), { target: { value: "10.20.30.44" } });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("checkbox", { name: /Confirmo que quiero instalar únicamente el agente OpenVMS/i }));
      fireEvent.click(within(dialog).getByRole("button", { name: "Instalar agente" }));
      expect(screen.queryByDisplayValue("secret-root-password")).not.toBeInTheDocument();
      await waitFor(() => expect(respond).toBeDefined());
      respond!(json({ code: "conflict", message: "secret-root-password from target 10.20.30.44" }, 409));
      expect(await screen.findByRole("alert")).toHaveTextContent(/no se reemplaza automáticamente/i);
      expect(screen.queryByDisplayValue("secret-root-password")).not.toBeInTheDocument();
      expect(document.body).not.toHaveTextContent("secret-root-password");
    });

    it("keeps polling memory-only job state after its server is filtered out", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      let pollRequest: Request | undefined;
      const { calls } = setupAgentInstall(undefined, async () => json({ id: "job-1", status: "queued", stage: "validating" }, 202), async (request) => {
        pollRequest = request;
        return new Promise((_resolve, reject) => request.signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), { once: true }));
      });
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      fireEvent.change(within(dialog).getByLabelText("Host SSH IPv4"), { target: { value: "10.20.30.44" } });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("checkbox", { name: /Confirmo que quiero instalar únicamente el agente OpenVMS/i }));
      fireEvent.click(within(dialog).getByRole("button", { name: "Instalar agente" }));
      expect(await screen.findByText(/Instalación en cola/i)).toBeInTheDocument();
      await waitFor(() => expect(pollRequest).toBeDefined());
      expect((await pollRequest!.clone().json().catch(() => null))).toBeNull();
      fireEvent.click(within(dialog).getAllByRole("button", { name: "Cerrar" }).at(-1)!);
      expect(await screen.findByText(/La instalación continúa en segundo plano/i)).toBeInTheDocument();
      fireEvent.click(await screen.findByRole("button", { name: /Ver progreso de instalación/i }));
      const reopened = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      expect(within(reopened).getByText(/Cerrar esta ventana no cancela/i)).toBeInTheDocument();
      fireEvent.change(screen.getByLabelText("Buscar servidor"), { target: { value: "frigate-r01" } });
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      expect(pollRequest!.signal.aborted).toBe(false);
      expect(calls.some((request) => request.method === "GET" && new URL(request.url).searchParams.has("ssh_password"))).toBe(false);
    });

    it("reports an unknown outcome safely when an in-memory job is lost", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      setupAgentInstall(undefined, async () => json({ id: "job-1", status: "queued", stage: "validating" }, 202), async () => json({ code: "not_found", message: "secret-root-password" }, 404));
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Instalar agente vía SSH/i }));
      const dialog = await screen.findByRole("dialog", { name: /Instalar agente OpenVMS/ });
      fireEvent.change(within(dialog).getByLabelText("Host SSH IPv4"), { target: { value: "10.20.30.44" } });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("checkbox", { name: /Confirmo que quiero instalar únicamente el agente OpenVMS/i }));
      fireEvent.click(within(dialog).getByRole("button", { name: "Instalar agente" }));
      expect((await screen.findAllByText(/No se puede confirmar el resultado/i)).length).toBeGreaterThan(0);
      expect(document.body).not.toHaveTextContent("secret-root-password");
    });
  });

  describe("existing-agent SSH update", () => {
    const grants = [
      { permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "a" },
      { permission: "servers.config.secrets", effect: "allow", scope_type: "server", scope_id: "a" },
    ];
    function setup(updateHandler?: (request: Request) => Promise<Response>, pollHandler?: (request: Request) => Promise<Response>, meGrants = grants, httpsPortHandler?: (request: Request) => Promise<Response>, status: object = { installed: true, version: "0.1.0", current_version: "0.1.0", outdated: false, binary_status: "update_available", binary_observed: { sha256: "1".repeat(64), architecture: "amd64", version: "0.1.0" }, binary_available: { sha256: "2".repeat(64), architecture: "amd64", version: "0.1.1" }, binary_outdated: true, binary_upgrade_available: true }, failStatus = false) {
      const calls: Request[] = [];
      const fetchMock = vi.fn(async (request: Request) => {
        calls.push(request);
        const url = new URL(request.url);
        if (url.pathname === "/api/v1/servers") return json({ items: [server("a", "frigate-h01", "s1")] });
        if (url.pathname === "/api/v1/sites") return json({ items: [{ id: "s1", name: "Helvecia" }] });
        if (url.pathname === "/api/v1/me") return json({ id: "u", tenant_id: "t", grants: meGrants });
        if (url.pathname === "/api/v1/servers/a/agent") return failStatus ? json({ code: "unavailable", message: "private detail" }, 503) : json(status);
        if (url.pathname === "/api/v1/classify/policy") return json({ servers: [] });
        if (url.pathname === "/api/v1/sync/status") return json({ items: [] });
        if (url.pathname === "/api/v1/servers/a/agent/update-ssh" && request.method === "POST" && updateHandler) return updateHandler(request);
        if (/^\/api\/v1\/servers\/a\/agent\/update-ssh\/job-\d+$/.test(url.pathname)) return pollHandler?.(request) ?? json({ id: url.pathname.split("/").at(-1), status: "succeeded", stage: "complete" });
        if (url.pathname === "/.well-known/openvms-https-port") return httpsPortHandler?.(request) ?? new Response("8443", { status: 200 });
        return json({ code: "not_found", message: "not found" }, 404);
      });
      vi.stubGlobal("fetch", fetchMock);
      return { calls, fetchMock };
    }

    it("does not request a manual SSH fingerprint", async () => {
      setup();
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      const dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i });
      expect(within(dialog).queryByLabelText(/Huella SHA-256 SSH verificada/i)).not.toBeInTheDocument();
      expect(within(dialog).getByText(/primer contacto/i)).toBeInTheDocument();
    });

    it("requires both matching server-scoped permissions", async () => {
      setup(undefined, undefined, [grants[0]!]);
      renderPage(Servers);
      await screen.findByText("frigate-h01");
      expect(screen.queryByRole("button", { name: /Actualizar agente y configurar HTTPS/i })).not.toBeInTheDocument();
    });

    it("shows one install action for a server with no registered agent", async () => {
      const { calls } = setup(undefined, undefined, grants, undefined, { installed: false, binary_status: "not_installed", binary_upgrade_available: false });
      renderPage(Servers);
      expect(await screen.findByRole("button", { name: /Instalar agente vía SSH/i })).toBeEnabled();
      expect(screen.queryByRole("button", { name: /Actualizar agente y configurar HTTPS/i })).not.toBeInTheDocument();
      expect(calls.filter((request) => request.method === "POST" && /agent\/(install|update-ssh)$/.test(new URL(request.url).pathname))).toHaveLength(0);
    });

    it("disables the unified action only for a digest-verified current agent", async () => {
      setup(undefined, undefined, grants, undefined, { installed: true, version: "0.1.0", binary_status: "current", binary_outdated: false, binary_upgrade_available: false, binary_observed: { sha256: "a".repeat(64), architecture: "amd64", version: "0.1.1", commit: "0123456789abcdef" }, binary_available: { sha256: "a".repeat(64), architecture: "amd64", version: "0.1.1", commit: "0123456789abcdef" } });
      renderPage(Servers);
      expect(await screen.findByRole("button", { name: /Agente actualizado/i })).toBeDisabled();
      expect(screen.getByText("Versión del protocolo 0.1.0")).toBeInTheDocument();
      expect(screen.getByText("Binario instalado: 0.1.1 (0123456789ab)")).toBeInTheDocument();
      expect(screen.getByText("Binario disponible: 0.1.1 (0123456789ab)")).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /Actualizar agente y configurar HTTPS/i })).not.toBeInTheDocument();
    });

    it("does not trust a current label when the observed and available digests disagree", async () => {
      setup(undefined, undefined, grants, undefined, { installed: true, binary_status: "current", binary_outdated: false, binary_upgrade_available: false, binary_observed: { sha256: "a".repeat(64), architecture: "amd64" }, binary_available: { sha256: "b".repeat(64), architecture: "amd64" } });
      renderPage(Servers);
      expect((await screen.findAllByText(/No se pudo verificar el estado del agente/i)).length).toBeGreaterThan(0);
      expect(screen.queryByRole("button", { name: /Agente actualizado/i })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /Actualizar agente y configurar HTTPS/i })).not.toBeInTheDocument();
    });

    it.each([
      ["architecture mismatch", "arm64", "amd64"],
      ["unsupported matching architecture", "bogus", "bogus"],
      ["malformed matching architecture", " amd64 ", " amd64 "],
    ])("does not treat a current digest as verified with %s", async (_scenario, observedArchitecture, availableArchitecture) => {
      setup(undefined, undefined, grants, undefined, {
        installed: true,
        binary_status: "current",
        binary_outdated: false,
        binary_upgrade_available: false,
        binary_observed: { sha256: "a".repeat(64), architecture: observedArchitecture },
        binary_available: { sha256: "a".repeat(64), architecture: availableArchitecture },
      });
      renderPage(Servers);
      expect((await screen.findAllByText(/No se pudo verificar el estado del agente/i)).length).toBeGreaterThan(0);
      expect(screen.queryByRole("button", { name: /Agente actualizado/i })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /Actualizar agente y configurar HTTPS/i })).not.toBeInTheDocument();
    });

    it("keeps unknown or offline registration state explicit and offers status refresh, not install or update", async () => {
      const { calls } = setup(undefined, undefined, grants, undefined, { installed: true, binary_status: "unknown", binary_upgrade_available: false });
      renderPage(Servers);
      expect((await screen.findAllByText(/No se pudo verificar el estado del agente/i)).length).toBeGreaterThan(0);
      expect(screen.getByRole("button", { name: /Consultar estado del agente/i })).toBeEnabled();
      expect(screen.queryByRole("button", { name: /Instalar agente vía SSH/i })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /Actualizar agente y configurar HTTPS/i })).not.toBeInTheDocument();
      expect(calls.filter((request) => request.method === "POST" && /agent\/(install|update-ssh)$/.test(new URL(request.url).pathname))).toHaveLength(0);
    });

    it("labels an unverified registered agent while allowing only the explicit available upgrade", async () => {
      setup(undefined, undefined, grants, undefined, { installed: true, binary_status: "unreachable", binary_upgrade_available: true });
      renderPage(Servers);
      expect(await screen.findByText(/No se pudo verificar el binario instalado/i)).toBeInTheDocument();
      expect(screen.getByRole("button", { name: /Actualizar agente y configurar HTTPS/i })).toBeEnabled();
      expect(screen.queryByRole("button", { name: /Instalar agente vía SSH/i })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /Agente actualizado/i })).not.toBeInTheDocument();
    });

    it("does not treat an offline agent status as missing, outdated, or current", async () => {
      const { calls } = setup(undefined, undefined, grants, undefined, {}, true);
      renderPage(Servers);
      expect((await screen.findAllByText(/No se pudo verificar el estado del agente/i)).length).toBeGreaterThan(0);
      expect(screen.queryByRole("button", { name: /Instalar agente vía SSH/i })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /Actualizar agente y configurar HTTPS/i })).not.toBeInTheDocument();
      const before = calls.filter((request) => request.method === "GET" && new URL(request.url).pathname === "/api/v1/servers/a/agent").length;
      fireEvent.click(screen.getByRole("button", { name: /Consultar estado del agente/i }));
      await waitFor(() => expect(calls.filter((request) => request.method === "GET" && new URL(request.url).pathname === "/api/v1/servers/a/agent").length).toBeGreaterThan(before));
    });

    it("does not transmit credentials from HTTP and offers no editable host", async () => {
      const { calls } = setup();
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      const dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i });
      expect(within(dialog).queryByLabelText(/Host SSH/i)).not.toBeInTheDocument();
      fireEvent.change(within(dialog).getByLabelText("Puerto SSH"), { target: { value: "2222" } });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Actualizar agente" }));
      expect(await screen.findByRole("alert")).toHaveTextContent(/HTTPS antes de enviar credenciales/i);
      expect(calls.some((request) => request.method === "POST" && new URL(request.url).pathname.endsWith("/update-ssh"))).toBe(false);
      expect(document.body).not.toHaveTextContent("secret-root-password");
    });

    it("redirects the existing-agent dialog only after an explicit click and drops query and fragment", async () => {
      const assign = vi.fn();
      vi.stubGlobal("location", { protocol: "http:", origin: "http://openvms.example:8000", href: "http://openvms.example:8000/servers?token=query-secret#private", assign });
      const { calls } = setup(undefined, undefined, grants, async (request) => {
        expect(request.credentials).toBe("omit");
        expect(request.redirect).toBe("error");
        return new Response("9555", { status: 200, headers: { "Content-Type": "text/plain" } });
      });
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      const dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("button", { name: /Abrir OpenVMS por HTTPS/i }));
      await waitFor(() => expect(assign).toHaveBeenCalledTimes(1));
      const target = new URL(assign.mock.calls[0]![0] as string);
      expect(target.origin).toBe("https://openvms.example:9555");
      expect(target.pathname).toBe("/servers");
      expect(target.search).toBe("");
      expect(target.hash).toBe("");
      expect(within(dialog).getByLabelText("Contraseña SSH root")).toHaveValue("");
      expect(calls.some((request) => request.method === "POST" && new URL(request.url).pathname.endsWith("/update-ssh"))).toBe(false);
    });

    it("posts only the SSH port, transient password and verified pin, then reports TLS-health success", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      const agent = { installed: true, version: "0.1.0", current_version: "0.1.0", outdated: false, binary_status: "update_available", binary_observed: { sha256: "1".repeat(64), architecture: "amd64", version: "0.1.0" }, binary_available: { sha256: "2".repeat(64), architecture: "amd64", version: "0.1.1" }, binary_outdated: true, binary_upgrade_available: true };
      const { calls } = setup(async (request) => {
        expect(new URL(request.url).pathname).toBe("/api/v1/servers/a/agent/update-ssh");
        expect(await request.clone().json()).toEqual({ ssh_port: 2222, ssh_password: "secret-root-password" });
        return json({ id: "job-1", status: "queued", stage: "validating" }, 202);
      }, async () => {
        Object.assign(agent, { binary_status: "current", binary_outdated: false, binary_upgrade_available: false, binary_observed: { sha256: "a".repeat(64), architecture: "amd64" }, binary_available: { sha256: "a".repeat(64), architecture: "amd64" } });
        return json({ id: "job-1", status: "succeeded", stage: "complete" });
      }, grants, undefined, agent);
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      const dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i });
      expect(within(dialog).queryByRole("button", { name: /Abrir OpenVMS por HTTPS/i })).not.toBeInTheDocument();
      fireEvent.change(within(dialog).getByLabelText("Puerto SSH"), { target: { value: "2222" } });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Actualizar agente" }));
      expect(await screen.findByRole("button", { name: /Agente actualizado/i })).toBeDisabled();
      expect(calls.filter((request) => request.method === "POST" && new URL(request.url).pathname.endsWith("/update-ssh"))).toHaveLength(1);
      expect(screen.queryByDisplayValue("secret-root-password")).not.toBeInTheDocument();
      const post = calls.find((request) => request.method === "POST" && new URL(request.url).pathname.endsWith("/update-ssh"));
      expect(post).toBeDefined();
      expect(new URL(post!.url).search).toBe("");
      await waitFor(() => expect(calls.some((request) => request.method === "GET" && new URL(request.url).pathname.endsWith("/update-ssh/job-1"))).toBe(true));
      expect(document.body).not.toHaveTextContent("secret-root-password");
    });

    it("clears the password when the update dialog is closed before submission", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      setup();
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      let dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Cancelar" }));
      expect(document.body).not.toHaveTextContent("secret-root-password");
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i });
      expect(within(dialog).getByLabelText("Contraseña SSH root")).toHaveValue("");
    });

    it("aborts a poll after its 15-second request deadline", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      const scheduled: Array<() => void> = [];
      const setTimeout = window.setTimeout.bind(window);
      vi.spyOn(window, "setTimeout").mockImplementation(((handler: TimerHandler, timeout?: number, ...args: unknown[]) => {
        if (timeout === 15_000 && typeof handler === "function") scheduled.push(handler as () => void);
        return setTimeout(handler, timeout, ...args);
      }) as typeof window.setTimeout);
      let pollRequest: Request | undefined;
      setup(async () => json({ id: "job-1", status: "queued", stage: "validating" }, 202), async (request) => {
        pollRequest = request;
        return new Promise((_resolve, reject) => request.signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), { once: true }));
      });
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      const dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Actualizar agente" }));
      await waitFor(() => expect(pollRequest).toBeDefined());
      expect(scheduled).toHaveLength(1);
      await act(async () => scheduled[0]!());
      expect(pollRequest!.signal.aborted).toBe(true);
      expect((await screen.findAllByText(/No se puede confirmar el resultado/i)).length).toBeGreaterThan(0);
    });

    it("aborts deadline-exceeding polls and ignores responses that arrive afterward", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      const scheduled: Array<() => void> = [];
      const setTimeout = window.setTimeout.bind(window);
      vi.spyOn(window, "setTimeout").mockImplementation(((handler: TimerHandler, timeout?: number, ...args: unknown[]) => {
        if (typeof timeout === "number" && timeout >= 10 * 60_000 && typeof handler === "function") scheduled.push(handler as () => void);
        return setTimeout(handler, timeout, ...args);
      }) as typeof window.setTimeout);
      let pollRequest: Request | undefined;
      let resolvePoll: ((response: Response) => void) | undefined;
      setup(async () => json({ id: "job-1", status: "queued", stage: "validating" }, 202), async (request) => {
        pollRequest = request;
        return new Promise((resolve) => { resolvePoll = resolve; });
      });
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      const dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Actualizar agente" }));
      await waitFor(() => expect(pollRequest).toBeDefined());
      expect(scheduled.length).toBeGreaterThan(0);
      await act(async () => { for (const callback of scheduled) callback(); });
      expect(pollRequest!.signal.aborted).toBe(true);
      expect((await screen.findAllByText(/No se puede confirmar el resultado/i)).length).toBeGreaterThan(0);
      await act(async () => resolvePoll!(json({ id: "job-1", status: "succeeded", stage: "complete" })));
      expect(screen.queryByText(/se autenticó correctamente por HTTPS verificado/i)).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: /Ver progreso de actualización/i })).toBeEnabled();
      expect((globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls.filter(([request]) => (request as Request).method === "POST" && new URL((request as Request).url).pathname.endsWith("/update-ssh"))).toHaveLength(1);
    });

    it("renders a generic conflict without assuming the registered agent is TLS-enabled", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      setup(async () => json({ code: "conflict", message: "secret-root-password; agent already busy" }, 409));
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      const dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Actualizar agente" }));
      expect(await screen.findByRole("alert")).toHaveTextContent(/otra activa o esta configuración no admite/i);
      expect(document.body).not.toHaveTextContent("secret-root-password");
      expect(document.body).not.toHaveTextContent(/agente .*TLS|TLS.*habilitado/i);
    });

    it("shows an unknown outcome instead of leaking a failed poll response", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      setup(async () => json({ id: "job-1", status: "queued", stage: "validating" }, 202), async () => json({ code: "not_found", message: "secret-root-password" }, 404));
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      const dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Actualizar agente" }));
      expect((await screen.findAllByText(/No se puede confirmar el resultado/i)).length).toBeGreaterThan(0);
      expect(document.body).not.toHaveTextContent("secret-root-password");
    });

    it("maps an allowlisted diagnostic message to localized safe failure copy", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      setup(
        async () => json({ id: "job-1", status: "queued", stage: "validating" }, 202),
        async () => json({ id: "job-1", status: "failed", stage: "failed", message: "HTTPS health verification failed; the previous agent files were restored." }),
      );
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      const dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i });
      fireEvent.change(within(dialog).getByLabelText("Puerto SSH"), { target: { value: "2222" } });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Actualizar agente" }));
      expect(await screen.findByRole("alert")).toHaveTextContent(/falló la verificación de salud HTTPS.*se restauraron los archivos anteriores/i);
      expect(document.body).not.toHaveTextContent("secret-root-password");
    });

    it("offers a fresh-password retry only after the known pre-mutation SSH connection failure", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      const bodies: unknown[] = [];
      const agent = { installed: true, version: "0.1.0", current_version: "0.1.0", outdated: false, binary_status: "update_available", binary_observed: { sha256: "1".repeat(64), architecture: "amd64", version: "0.1.0" }, binary_available: { sha256: "2".repeat(64), architecture: "amd64", version: "0.1.1" }, binary_outdated: true, binary_upgrade_available: true };
      const { calls } = setup(
        async (request) => {
          bodies.push(await request.clone().json());
          if (bodies.length === 1) return json({ id: "job-1", status: "queued", stage: "validating" }, 202);
          Object.assign(agent, { binary_status: "current", binary_outdated: false, binary_upgrade_available: false, binary_observed: { sha256: "a".repeat(64), architecture: "amd64" }, binary_available: { sha256: "a".repeat(64), architecture: "amd64" } });
          return json({ id: "job-2", status: "succeeded", stage: "complete" });
        },
        async () => json({ id: "job-1", status: "failed", stage: "failed", message: "Could not establish SSH. Verify reachability and root SSH access, then inspect the target before retrying." }),
        grants,
        undefined,
        agent,
      );
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      let dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/ });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "first-root-secret" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Actualizar agente" }));
      expect(await screen.findByRole("alert")).toHaveTextContent(/No se pudo establecer la conexión SSH/i);
      fireEvent.click(await within(dialog).findByRole("button", { name: /Volver a intentar con una contraseña nueva/i }));
      dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/ });
      expect(within(dialog).getByLabelText("Contraseña SSH root")).toHaveValue("");
      expect(document.body).not.toHaveTextContent("first-root-secret");
      expect(calls.filter((request) => request.method === "POST" && new URL(request.url).pathname.endsWith("/update-ssh"))).toHaveLength(1);
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "fresh-root-secret" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Actualizar agente" }));
      expect(await screen.findByRole("button", { name: /Agente actualizado/i })).toBeDisabled();
      expect(bodies).toEqual([
        { ssh_port: 22, ssh_password: "first-root-secret" },
        { ssh_port: 22, ssh_password: "fresh-root-secret" },
      ]);
      expect(document.body).not.toHaveTextContent("first-root-secret");
      expect(document.body).not.toHaveTextContent("fresh-root-secret");
    });

    it("does not offer a credential retry after a changed SSH host key", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      setup(
        async () => json({ id: "job-1", status: "queued", stage: "validating" }, 202),
        async () => json({ id: "job-1", status: "failed", stage: "failed", message: "SSH host key changed since first use; verify the server identity before retrying." }),
      );
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      const dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/ });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "first-root-secret" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Actualizar agente" }));
      expect(await screen.findByRole("alert")).toHaveTextContent(/clave SSH guardada cambió/i);
      expect(within(dialog).queryByRole("button", { name: /Ingresar una contraseña nueva/i })).not.toBeInTheDocument();
    });

    it("uses generic safe copy for an unrecognized server failure message", async () => {
      vi.stubGlobal("location", { protocol: "https:", origin: "https://localhost" });
      setup(
        async () => json({ id: "job-1", status: "queued", stage: "validating" }, 202),
        async () => json({ id: "job-1", status: "failed", stage: "failed", message: "password-secret raw-ssh-output" }),
      );
      renderPage(Servers);
      fireEvent.click(await screen.findByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      const dialog = await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i });
      fireEvent.change(within(dialog).getByLabelText("Puerto SSH"), { target: { value: "2222" } });
      fireEvent.change(within(dialog).getByLabelText("Contraseña SSH root"), { target: { value: "secret-root-password" } });
      fireEvent.click(within(dialog).getByRole("button", { name: "Actualizar agente" }));
      expect(await screen.findByRole("alert")).toHaveTextContent(/La actualización no se completó o no se pudo confirmar el resultado/i);
      expect(document.body).not.toHaveTextContent("password-secret");
      expect(document.body).not.toHaveTextContent("raw-ssh-output");
      expect(document.body).not.toHaveTextContent("secret-root-password");
    });
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
      expect(within(form).getByRole("switch", { name: /Permitir temporalmente grabaciones en el disco del sistema/i })).not.toBeChecked();
      expect(within(form).getByLabelText("Host SSH")).toBeInTheDocument();
      expect(form).toHaveTextContent(/actualiza paquetes.*Docker.*Frigate/s);
      expect(form).toHaveTextContent(/Chrony\/NTP/);
      expect(within(form).queryByLabelText("URL de Frigate")).not.toBeInTheDocument();
    });

    describe("register wizard keeps typed data", () => {
      async function openHostForm() {
        setup(manager);
        renderPage(Servers);
        fireEvent.click(await screen.findByRole("button", { name: "Registrar servidor" }));
        fireEvent.click(screen.getByRole("button", { name: /Instalar Frigate en un host nuevo/ }));
        return screen.getByRole("form", { name: "Instalación de Frigate nuevo" });
      }

      it("closes on Escape without asking when nothing was typed", async () => {
        await openHostForm();
        fireEvent.keyDown(document, { key: "Escape" });
        expect(screen.queryByRole("dialog", { name: "Registrar servidor" })).not.toBeInTheDocument();
        expect(screen.queryByRole("dialog", { name: "¿Descartar los datos cargados?" })).not.toBeInTheDocument();
      });

      it("asks before discarding typed data on Escape and keeps the form when told to keep editing", async () => {
        const form = await openHostForm();
        fireEvent.change(within(form).getByLabelText("Host SSH"), { target: { value: "10.1.1.144" } });
        fireEvent.keyDown(document, { key: "Escape" });
        const confirm = screen.getByRole("dialog", { name: "¿Descartar los datos cargados?" });
        expect(screen.getByRole("dialog", { name: "Registrar servidor" })).toBeInTheDocument();
        fireEvent.click(within(confirm).getByRole("button", { name: "Seguir editando" }));
        expect(screen.queryByRole("dialog", { name: "¿Descartar los datos cargados?" })).not.toBeInTheDocument();
        expect(within(screen.getByRole("form", { name: "Instalación de Frigate nuevo" })).getByLabelText("Host SSH")).toHaveValue("10.1.1.144");
      });

      it("asks on backdrop click and the close button, and closes only after confirming", async () => {
        const form = await openHostForm();
        fireEvent.change(within(form).getByLabelText("Nombre visible del servidor"), { target: { value: "Exalink" } });
        const dialog = screen.getByRole("dialog", { name: "Registrar servidor" });
        fireEvent.mouseDown(dialog.parentElement!);
        expect(screen.getByRole("dialog", { name: "¿Descartar los datos cargados?" })).toBeInTheDocument();
        fireEvent.click(screen.getByRole("button", { name: "Seguir editando" }));
        fireEvent.click(within(dialog).getAllByRole("button", { name: "Cerrar" })[0]!);
        fireEvent.click(within(screen.getByRole("dialog", { name: "¿Descartar los datos cargados?" })).getByRole("button", { name: "Descartar" }));
        expect(screen.queryByRole("dialog", { name: "Registrar servidor" })).not.toBeInTheDocument();
      });

      it("never keeps the SSH password after discarding", async () => {
        const form = await openHostForm();
        fireEvent.change(within(form).getByLabelText(/^Contraseña SSH/), { target: { value: "one-time-secret" } });
        fireEvent.keyDown(document, { key: "Escape" });
        fireEvent.click(screen.getByRole("button", { name: "Descartar" }));
        expect(screen.queryByText("one-time-secret")).not.toBeInTheDocument();
        expect(screen.queryByDisplayValue("one-time-secret")).not.toBeInTheDocument();
        fireEvent.click(await screen.findByRole("button", { name: "Registrar servidor" }));
        fireEvent.click(screen.getByRole("button", { name: /Instalar Frigate en un host nuevo/ }));
        expect(screen.getByLabelText(/^Contraseña SSH/)).toHaveValue("");
      });

      it("asks when the existing-Frigate import form has typed data", async () => {
        setup(manager);
        renderPage(Servers);
        fireEvent.click(await screen.findByRole("button", { name: "Registrar servidor" }));
        fireEvent.click(screen.getByRole("button", { name: /Registrar un Frigate existente/ }));
        fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Frigate-H01" } });
        fireEvent.keyDown(document, { key: "Escape" });
        expect(screen.getByRole("dialog", { name: "¿Descartar los datos cargados?" })).toBeInTheDocument();
      });
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
      fireEvent.click(within(form).getByRole("switch", { name: /Permitir temporalmente grabaciones en el disco del sistema/i }));
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

    it("loads agent metrics only while mounted and exposes the scoped SSH update action", async () => {
      const fetchMock = vi.fn(stubApi({
        "/api/v1/servers": () => json({ items: [server("a", "frigate-h01", "s1")] }),
        "/api/v1/sites": () => json({ items: [{ id: "s1", name: "Helvecia" }] }),
        "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [
          { permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "a" },
          { permission: "servers.config.secrets", effect: "allow", scope_type: "server", scope_id: "a" },
        ] }),
        "/api/v1/servers/a/agent": () => json({ installed: true, version: "0.1.0", current_version: "0.1.0", outdated: false, binary_status: "update_available", binary_outdated: true, binary_upgrade_available: true, binary_observed: { sha256: "1".repeat(64), architecture: "amd64" }, binary_available: { sha256: "2".repeat(64), architecture: "amd64", version: "0.1.1" }, cpu_percent: 72, memory_total_bytes: 100, memory_available_bytes: 20 }),
      }));
      vi.stubGlobal("fetch", fetchMock);
      const view = renderPage(Servers);
      expect(await screen.findByText(/72%/)).toBeInTheDocument();
      const metrics = fetchMock.mock.calls.filter(([r]) => new URL((r as Request).url).pathname === "/api/v1/servers/a/agent" && (r as Request).method === "GET");
      expect(metrics.length).toBeGreaterThan(0);
      fireEvent.click(screen.getByRole("button", { name: /Actualizar agente y configurar HTTPS/i }));
      expect(await screen.findByRole("dialog", { name: /Actualizar el agente OpenVMS/i })).toBeInTheDocument();
      expect(fetchMock.mock.calls.some(([r]) => new URL((r as Request).url).pathname === "/api/v1/servers/a/agent/update")).toBe(false);
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
