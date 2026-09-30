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

  describe("settings drawer", () => {
    const manager = { id: "u", tenant_id: "t", grants: [{ permission: "cameras.manage", effect: "allow" }] };
    const viewer = { id: "u", tenant_id: "t", grants: [{ permission: "cameras.view", effect: "allow" }] };

    function setup(me: object, patch: (req: Request) => Response = () => json(camera("c1", "nuevo"))) {
      const routes = stubApi({ ...inventory([camera("c1", "acceso")]), "/api/v1/me": () => json(me) });
      const fetchMock = vi.fn(async (req: Request) => (req.method === "PATCH" ? patch(req) : routes(req)));
      vi.stubGlobal("fetch", fetchMock);
      return fetchMock;
    }

    it("shows read-only context without edit controls for users without cameras.manage", async () => {
      setup(viewer);
      renderPage(Cameras);
      fireEvent.click(await screen.findByRole("button", { name: "Ajustes de acceso" }));
      const dialog = await screen.findByRole("dialog", { name: "Ajustes de cámara" });
      expect(within(dialog).getByText("Helvecia")).toBeInTheDocument();
      expect(within(dialog).getByText("frigate-h01")).toBeInTheDocument();
      expect(within(dialog).queryByLabelText("Nombre")).not.toBeInTheDocument();
      expect(within(dialog).queryByRole("button", { name: "Guardar" })).not.toBeInTheDocument();
    });

    it("saves the name and enabled flag with PATCH and refreshes the inventory", async () => {
      const fetchMock = setup(manager);
      renderPage(Cameras);
      fireEvent.click(await screen.findByRole("button", { name: "Ajustes de acceso" }));
      const name = await screen.findByLabelText("Nombre");
      fireEvent.change(name, { target: { value: "nuevo" } });
      fireEvent.click(screen.getByLabelText("Habilitada"));
      fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH");
      expect(new URL(patch!.url).pathname).toBe("/api/v1/cameras/c1");
      expect(await patch!.clone().json()).toEqual({
        display_name: "nuevo",
        enabled: false,
        default_live_quality: "sub",
        description: "",
        location: "",
        tags: [],
      });
      const lists = fetchMock.mock.calls.filter(([r]) => new URL((r as Request).url).pathname === "/api/v1/cameras" && (r as Request).method === "GET");
      expect(lists.length).toBeGreaterThan(1);
    });

    it("edits default live quality, description, location and tags", async () => {
      const fetchMock = setup(manager);
      renderPage(Cameras);
      fireEvent.click(await screen.findByRole("button", { name: "Ajustes de acceso" }));
      expect(await screen.findByLabelText(/Calidad en vivo por defecto/)).toHaveValue("sub");
      fireEvent.change(screen.getByLabelText(/Calidad en vivo por defecto/), { target: { value: "main" } });
      fireEvent.change(screen.getByLabelText("Descripción"), { target: { value: "Entrada principal" } });
      fireEvent.change(screen.getByLabelText("Ubicación"), { target: { value: "Planta baja" } });
      fireEvent.change(screen.getByLabelText(/Etiquetas/), { target: { value: " acceso, exterior ,, " } });
      fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH");
      expect(await patch!.clone().json()).toMatchObject({
        default_live_quality: "main",
        description: "Entrada principal",
        location: "Planta baja",
        tags: ["acceso", "exterior"],
      });
    });

    it("prefills the stored VMS settings for users with cameras.manage", async () => {
      const stored = { default_live_quality: "main", description: "Entrada", location: "Norte", tags: ["a", "b"] };
      const routes = stubApi({ ...inventory([camera("c1", "acceso", stored)]), "/api/v1/me": () => json(manager) });
      vi.stubGlobal("fetch", vi.fn(routes));
      renderPage(Cameras);
      fireEvent.click(await screen.findByRole("button", { name: "Ajustes de acceso" }));
      expect(await screen.findByLabelText(/Calidad en vivo por defecto/)).toHaveValue("main");
      expect(screen.getByLabelText("Descripción")).toHaveValue("Entrada");
      expect(screen.getByLabelText("Ubicación")).toHaveValue("Norte");
      expect(screen.getByLabelText(/Etiquetas/)).toHaveValue("a, b");
    });

    it("does not show the new edit controls without cameras.manage", async () => {
      setup(viewer);
      renderPage(Cameras);
      fireEvent.click(await screen.findByRole("button", { name: "Ajustes de acceso" }));
      await screen.findByRole("dialog");
      expect(screen.queryByLabelText(/Calidad en vivo por defecto/)).not.toBeInTheDocument();
      expect(screen.queryByLabelText(/Etiquetas/)).not.toBeInTheDocument();
    });

    it("rejects an empty name without calling the API", async () => {
      const fetchMock = setup(manager);
      renderPage(Cameras);
      fireEvent.click(await screen.findByRole("button", { name: "Ajustes de acceso" }));
      fireEvent.change(await screen.findByLabelText("Nombre"), { target: { value: "" } });
      fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
      expect(await screen.findByText("El nombre no puede estar vacío.")).toBeInTheDocument();
      expect(fetchMock.mock.calls.some(([r]) => (r as Request).method === "PATCH")).toBe(false);
    });

    it("keeps the drawer open and shows the API error when saving fails", async () => {
      setup(manager, () => json({ code: "forbidden", message: "sin permiso" }, 403));
      renderPage(Cameras);
      fireEvent.click(await screen.findByRole("button", { name: "Ajustes de acceso" }));
      fireEvent.click(await screen.findByRole("button", { name: "Guardar" }));
      expect(await screen.findByRole("alert")).toHaveTextContent("sin permiso");
      expect(screen.getByRole("dialog")).toBeInTheDocument();
    });

    it("closes with Escape and returns focus to the trigger", async () => {
      setup(viewer);
      renderPage(Cameras);
      const trigger = await screen.findByRole("button", { name: "Ajustes de acceso" });
      trigger.focus();
      fireEvent.click(trigger);
      await screen.findByRole("dialog");
      fireEvent.keyDown(document, { key: "Escape" });
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
      expect(trigger).toHaveFocus();
    });

    it("links to the Frigate config editor when user has servers.config", async () => {
      const serverConfigUser = {
        id: "u",
        tenant_id: "t",
        grants: [
          { permission: "cameras.manage", effect: "allow" },
          { permission: "servers.config", effect: "allow" },
        ],
      };
      const frigateCfg = {
        camera_id: "c1",
        camera_name: "acceso",
        server_id: "srv1",
        detect_enabled: true,
        tracked_objects: ["person"],
        lpr_enabled: false,
        zones: ["entrada"],
      };
      vi.stubGlobal(
        "fetch",
        vi.fn(
          stubApi({
            ...inventory([camera("c1", "acceso")]),
            "/api/v1/me": () => json(serverConfigUser),
            "/api/v1/cameras/c1/config": () => json(frigateCfg),
          }),
        ),
      );
      renderPage(Cameras);

      fireEvent.click(await screen.findByRole("button", { name: "Ajustes de acceso" }));
      expect(await screen.findByText("Configuración en Frigate")).toBeInTheDocument();
      const link = await screen.findByRole("link", { name: /Editar configuración de Frigate/ });
      expect(link).toHaveAttribute("href", "/cameras/c1/frigate");
      expect(await screen.findByText("Detección")).toBeInTheDocument();
    });
  });
});
