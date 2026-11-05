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
  it("shows a recorder channel with a delete action instead of Frigate settings, and deletes it on confirm", async () => {
    const calls: string[] = [];
    vi.stubGlobal("fetch", vi.fn(async (input: Request) => {
      calls.push(`${input.method} ${new URL(input.url).pathname}`);
      return stubApi({
        "/api/v1/cameras": () => json({ items: [camera("c1", "Cámara IP 01")] }),
        "/api/v1/sites": () => json({ items: [{ id: "s1", name: "Helvecia" }] }),
        "/api/v1/servers": () => json({ items: [{ id: "srv1", name: "nvr-feedlot", kind: "provision_nvr" }] }),
        "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [{ id: "g", permission: "servers.manage", effect: "allow", scope_type: "server", scope_id: "srv1" }] }),
        "DELETE /api/v1/cameras/c1": () => new Response(null, { status: 204 }),
      })(input);
    }));
    renderPage(Cameras);

    await screen.findByText("Cámara IP 01");
    const row = screen.getByRole("group", { name: "nvr-feedlot" });
    expect(within(row).queryByText("Configurar Frigate")).not.toBeInTheDocument();
    fireEvent.click(await within(row).findByRole("button", { name: "Eliminar Cámara IP 01" }));
    expect(screen.getByText(/El NVR no se toca/)).toBeInTheDocument();
    expect(calls).not.toContain("DELETE /api/v1/cameras/c1");
    fireEvent.click(screen.getByRole("button", { name: "Eliminar" }));
    await waitFor(() => expect(calls).toContain("DELETE /api/v1/cameras/c1"));
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
    const pick = within(await screen.findByRole("list", { name: "Filtrar por servidor" })).getByRole("button", { name: /frigate-h01/ });
    expect(pick).toHaveAttribute("aria-pressed", "true");
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

  it("shows the analytics reception LED only on cameras that report Provision analytics", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/cameras": () =>
            json({
              items: [
                camera("c1", "acceso_norte", { analytics_reception: { state: "receiving", last_event_at: "2026-11-05T10:00:00Z" } }),
                camera("c2", "muelle"),
              ],
            }),
          "/api/v1/sites": () => json({ items: [{ id: "s1", name: "Helvecia" }] }),
          "/api/v1/servers": () => json({ items: [{ id: "srv1", name: "frigate-h01" }] }),
        }),
      ),
    );
    renderPage(Cameras);
    expect(await screen.findByText("muelle")).toBeInTheDocument();
    expect(screen.getAllByRole("status").filter((el) => el.hasAttribute("data-state"))).toHaveLength(1);
    expect(screen.getByText("Analíticas: recibiendo")).toBeInTheDocument();
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

  it("renders ONVIF profile badges for cameras in the table", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          ...inventory([
            {
              id: "cam-onvif",
              display_name: "Camara Patio",
              remote_name: "patio",
              server_id: "srv1",
              site_id: "s1",
              enabled: true,
              status: "online",
              zones: ["jardin"],
              onvif_profile: "ONVIF-T",
              onvif_version: "2.0",
              onvif_capabilities: {
                has_analytics: true,
                has_ptz: false,
              },
            },
          ]),
          "/api/v1/servers": () => json({ items: [{ id: "srv1", name: "frigate-h01", site_id: "s1" }] }),
        })
      )
    );
    renderPage(Cameras);
    expect(await screen.findByText("Camara Patio")).toBeInTheDocument();
    const badge = screen.getByText("ONVIF-T");
    expect(badge).toBeInTheDocument();
    expect(screen.getByText("v2.0")).toBeInTheDocument();
    expect(screen.getByText(/1 con ONVIF/i)).toBeInTheDocument();
  });

  it("renders Sin ONVIF badge for unlinked cameras in the table", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          ...inventory([
            {
              id: "cam-plain",
              display_name: "Camara Sin Link",
              remote_name: "plain",
              server_id: "srv1",
              site_id: "s1",
              enabled: true,
              status: "online",
              zones: [],
            },
          ]),
          "/api/v1/servers": () => json({ items: [{ id: "srv1", name: "frigate-h01", site_id: "s1" }] }),
        })
      )
    );
    renderPage(Cameras);
    expect(await screen.findByText("Camara Sin Link")).toBeInTheDocument();
    expect(screen.getByText("Sin ONVIF")).toBeInTheDocument();
  });

  describe("tree", () => {
    const seen = "2026-11-02T11:59:00Z";
    const treeApi = (cameras: object[], servers?: object[]) => ({
      "/api/v1/cameras": () => json({ items: cameras }),
      "/api/v1/sites": () => json({ items: [{ id: "s1", name: "Helvecia" }] }),
      "/api/v1/servers": () =>
        json({
          items: servers ?? [
            { id: "srv1", name: "frigate-h01", site_id: "s1", kind: "frigate" },
            { id: "srv2", name: "frigate-h02", site_id: "s1", kind: "frigate", agent_version: "1.4.0", agent_status: "online", agent_last_seen_at: seen },
          ],
        }),
    });
    const two = [camera("c1", "acceso"), camera("c2", "muelle", { server_id: "srv2" })];

    it("groups cameras under their site and server, and collapses a server", async () => {
      vi.stubGlobal("fetch", vi.fn(stubApi(treeApi(two))));
      renderPage(Cameras);
      expect(await screen.findByText("acceso")).toBeInTheDocument();
      expect(screen.getByRole("region", { name: "Sitio Helvecia" })).toBeInTheDocument();
      const group = screen.getByRole("group", { name: "frigate-h01" });
      expect(within(group).getByText("acceso")).toBeInTheDocument();
      expect(within(screen.getByRole("group", { name: "frigate-h02" })).getByText("muelle")).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Contraer frigate-h01" }));
      expect(screen.queryByText("acceso")).not.toBeInTheDocument();
      expect(screen.getByText("muelle")).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Expandir frigate-h01" }));
      expect(screen.getByText("acceso")).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Contraer todo" }));
      expect(screen.queryByText("muelle")).not.toBeInTheDocument();
    });

    it("tells on each server row whether it has an agent and which version", async () => {
      vi.stubGlobal("fetch", vi.fn(stubApi(treeApi(two))));
      renderPage(Cameras);
      await screen.findByText("acceso");
      expect(within(screen.getByRole("group", { name: "frigate-h01" })).getByText("Sin agente")).toBeInTheDocument();
      expect(within(screen.getByRole("group", { name: "frigate-h02" })).getByText(/^Agente v1\.4\.0/)).toBeInTheDocument();
      expect(within(screen.getByRole("group", { name: "frigate-h02" })).getByText("1 cámara · 1 en línea")).toBeInTheDocument();
    });

    it("flags an agent older than the newest one and an agent that stopped reporting", async () => {
      const servers = [
        { id: "srv1", name: "frigate-h01", site_id: "s1", kind: "frigate", agent_version: "1.2.0", agent_status: "online", agent_last_seen_at: "2026-01-01T00:00:00Z" },
        { id: "srv2", name: "frigate-h02", site_id: "s1", kind: "frigate", agent_version: "1.10.0", agent_status: "online", agent_last_seen_at: new Date().toISOString() },
      ];
      vi.stubGlobal("fetch", vi.fn(stubApi(treeApi(two, servers))));
      renderPage(Cameras);
      await screen.findByText("acceso");
      const first = within(screen.getByRole("group", { name: "frigate-h01" }));
      expect(first.getByText("Agente v1.2.0 · actualizar")).toBeInTheDocument();
      expect(first.getByText(/sin conexión/)).toBeInTheDocument();
      expect(within(screen.getByRole("group", { name: "frigate-h02" })).getByText("Agente v1.10.0")).toBeInTheDocument();
    });

    it("no longer offers ONVIF discovery or probing on the list", async () => {
      vi.stubGlobal("fetch", vi.fn(stubApi(treeApi(two))));
      renderPage(Cameras);
      await screen.findByText("acceso");
      expect(screen.queryByText("Descubrir cámaras ONVIF")).not.toBeInTheDocument();
      expect(screen.queryByText("Probar y detectar")).not.toBeInTheDocument();
      expect(screen.queryByLabelText("Servidor para descubrir cámaras")).not.toBeInTheDocument();
      expect(screen.queryByLabelText("Endpoint ONVIF")).not.toBeInTheDocument();
    });

    it("replaces the Frigate button with an icon gear that links to the configuration", async () => {
      vi.stubGlobal("fetch", vi.fn(stubApi(treeApi(two))));
      renderPage(Cameras);
      const gear = await screen.findByRole("link", { name: "Configuración de acceso" });
      expect(gear).toHaveAttribute("href", "/cameras/c1/frigate");
      expect(gear).toHaveAttribute("title", "Configuración");
      expect(gear).toHaveTextContent("");
      expect(screen.queryByText("Configurar Frigate")).not.toBeInTheDocument();
      expect(screen.getAllByRole("link", { name: /^Configuración de / })).toHaveLength(2);
    });

    it("titles the column Analíticas OpenVMS and keeps one labelled switch per camera", async () => {
      vi.stubGlobal("fetch", vi.fn(stubApi(treeApi(two))));
      renderPage(Cameras);
      await screen.findByText("acceso");
      expect(screen.getByText("Analíticas OpenVMS")).toBeInTheDocument();
      expect(screen.queryByText("Clasificación")).not.toBeInTheDocument();
      const switches = screen.getAllByRole("switch", { name: "Clasificación fina" });
      expect(switches).toHaveLength(2);
      // The visible text is hidden by the wrapper (button > last span), leaving only the accessible label.
      expect(switches[0]!.parentElement!.className).toContain("span:last-child]:sr-only");
      expect(switches[0]!.lastElementChild).toHaveTextContent("Clasificación fina");
    });

    it("hides virtual cameras until the switch shows them, without a gear", async () => {
      vi.stubGlobal("fetch", vi.fn(stubApi(treeApi([camera("c1", "acceso"), camera("c9", "birdseye")]))));
      renderPage(Cameras);
      await screen.findByText("acceso");
      expect(screen.queryByText("birdseye")).not.toBeInTheDocument();
      const toggle = screen.getByRole("switch", { name: "Mostrar cámaras virtuales (1)" });
      fireEvent.click(toggle);
      expect(await screen.findByText("birdseye")).toBeInTheDocument();
      expect(screen.getByText("Virtual")).toBeInTheDocument();
      expect(screen.queryByRole("link", { name: "Configuración de birdseye" })).not.toBeInTheDocument();
      expect(screen.getByRole("link", { name: "Configuración de acceso" })).toBeInTheDocument();
    });

    it("finds a camera that sits inside a collapsed server", async () => {
      vi.stubGlobal("fetch", vi.fn(stubApi(treeApi(two))));
      renderPage(Cameras);
      await screen.findByText("acceso");
      fireEvent.click(screen.getByRole("button", { name: "Contraer frigate-h01" }));
      expect(screen.queryByText("acceso")).not.toBeInTheDocument();
      fireEvent.change(screen.getByLabelText("Buscar cámara"), { target: { value: "acce" } });
      expect(await screen.findByText("acceso")).toBeInTheDocument();
      expect(screen.queryByText("muelle")).not.toBeInTheDocument();
      expect(screen.queryByRole("group", { name: "frigate-h02" })).not.toBeInTheDocument();
    });

    it("shows the flat list of one server when it is picked in the rail", async () => {
      const fetchMock = vi.fn(stubApi(treeApi(two)));
      vi.stubGlobal("fetch", fetchMock);
      renderPage(Cameras);
      await screen.findByText("acceso");
      const list = screen.getByRole("list", { name: "Filtrar por servidor" });
      fireEvent.click(within(list).getByRole("button", { name: /frigate-h01/ }));
      await waitFor(() => expect(within(list).getByRole("button", { name: /frigate-h01/ })).toHaveAttribute("aria-pressed", "true"));
      await waitFor(() => expect(fetchMock.mock.calls.some(([r]) => (r as Request).url.includes("server_id=srv1"))).toBe(true));
      expect(screen.queryByRole("button", { name: /^(Contraer|Expandir) frigate-h01$/ })).not.toBeInTheDocument();
      expect(screen.queryByRole("region", { name: "Sitio Helvecia" })?.textContent).not.toContain("Helvecia");
    });

    it("offers the filters in a rail: search, site, server, status, agent and virtual cameras", async () => {
      vi.stubGlobal("fetch", vi.fn(stubApi(treeApi([camera("c1", "acceso"), camera("c2", "muelle", { status: "offline" }), camera("c3", "bodega", { server_id: "srv2", enabled: false })]))));
      renderPage(Cameras);
      await screen.findByText("acceso");
      const rail = within(screen.getByRole("complementary", { name: "Filtros" }));
      expect(rail.getByLabelText("Buscar cámara")).toBeInTheDocument();
      expect(rail.getByLabelText("Filtrar por sitio")).toBeInTheDocument();
      for (const chip of ["En línea", "Sin señal", "Deshabilitada", "Con agente", "Desactualizado", "Sin agente"]) expect(rail.getByRole("button", { name: chip })).toBeInTheDocument();
      fireEvent.click(rail.getByRole("button", { name: "Sin señal" }));
      expect(screen.queryByText("acceso")).not.toBeInTheDocument();
      expect(screen.getByText("muelle")).toBeInTheDocument();
      fireEvent.click(rail.getByRole("button", { name: "Sin señal" }));
      fireEvent.click(rail.getByRole("button", { name: "Con agente" }));
      expect(screen.queryByText("acceso")).not.toBeInTheDocument();
      expect(screen.getByText("bodega")).toBeInTheDocument();
    });
  });
});


describe("bulk selection", () => {
  const withPerm = (cameras: object[], grants: object[]) => ({ ...inventory(cameras), "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants }) });
  const editGrant = { permission: "cameras.config.edit", effect: "allow", scope_type: "platform" };

  it("opens the bulk dialog with the selected camera ids", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi(withPerm([camera("c1", "acceso"), camera("c2", "muelle")], [editGrant]))));
    renderPage(Cameras);
    fireEvent.click(await screen.findByLabelText("Seleccionar acceso"));
    fireEvent.click(screen.getByLabelText("Seleccionar muelle"));
    fireEvent.click(screen.getByRole("button", { name: "Configurar cámaras (2)" }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("2 cámaras");
  });

  it("hides the selection without the camera config permission", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi(withPerm([camera("c1", "acceso")], []))));
    renderPage(Cameras);
    await screen.findByRole("link", { name: "Configuración de acceso" });
    expect(screen.queryByLabelText("Seleccionar acceso")).toBeNull();
  });

  it("caps the selection at 200 cameras with a message", async () => {
    const many = Array.from({ length: 201 }, (_, i) => camera(`c${i}`, `cam${i}`));
    vi.stubGlobal("fetch", vi.fn(stubApi(withPerm(many, [editGrant]))));
    renderPage(Cameras);
    await screen.findByLabelText("Seleccionar cam0");
    for (let i = 0; i < 201; i++) fireEvent.click(screen.getByLabelText(`Seleccionar cam${i}`));
    expect(screen.getByRole("button", { name: "Configurar cámaras (200)" })).toBeInTheDocument();
    expect(screen.getByText(/Máximo 200 cámaras/)).toBeInTheDocument();
  }, 60_000);
});
