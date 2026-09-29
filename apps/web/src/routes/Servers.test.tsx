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
