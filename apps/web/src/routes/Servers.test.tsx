import { screen } from "@testing-library/react";
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
});
