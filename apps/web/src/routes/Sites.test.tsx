import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Sites } from "./Sites";

afterEach(() => vi.unstubAllGlobals());

const site = (id: string, name: string, extra: object = {}) => ({
  id,
  tenant_id: "t",
  name,
  timezone: "America/Argentina/Buenos_Aires",
  server_count: 2,
  camera_count: 7,
  ...extra,
});

function stub(sites: object[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(stubApi({ "/api/v1/sites": () => json({ items: sites }), "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [] }) })),
  );
}

describe("Sites", () => {
  it("links each count to the matching filtered inventory", async () => {
    stub([site("s1", "Helvecia")]);
    renderPage(Sites);
    expect(await screen.findByRole("link", { name: "2" })).toHaveAttribute("href", "/servers?site_id=s1");
    expect(screen.getByRole("link", { name: "7" })).toHaveAttribute("href", "/cameras?site_id=s1");
  });

  it("filters by name or address and reports the visible count", async () => {
    stub([site("s1", "Helvecia"), site("s2", "Rosario", { address: "Av. Pellegrini" })]);
    renderPage(Sites);
    expect(await screen.findByText("2 sitios")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Buscar sitio"), { target: { value: "pelle" } });
    expect(screen.getByText("1 de 2 sitios")).toBeInTheDocument();
    expect(screen.queryByText("Helvecia")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Buscar sitio"), { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("Buscar sitio"), { target: { value: "zzz" } });
    expect(screen.getByText("Ningún sitio coincide con la búsqueda.")).toBeInTheDocument();
  });
});
