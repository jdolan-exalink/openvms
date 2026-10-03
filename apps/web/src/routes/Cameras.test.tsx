import { screen } from "@testing-library/react";
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

  it("opens one configuration page for the camera", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi(inventory([camera("c1", "acceso")]))));
    renderPage(Cameras);
    const link = await screen.findByRole("link", { name: "Configuración de acceso" });
    expect(link).toHaveAttribute("href", "/cameras/c1/frigate");
    expect(screen.queryByRole("button", { name: "Ajustes de acceso" })).not.toBeInTheDocument();
  });
});
