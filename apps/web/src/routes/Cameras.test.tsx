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
  created_at: "",
  updated_at: "",
  ...extra,
});

describe("Cameras", () => {
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
});
