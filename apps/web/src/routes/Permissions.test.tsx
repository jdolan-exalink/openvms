import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Permissions } from "./Permissions";

afterEach(() => vi.unstubAllGlobals());

const catalog = [
  { permission: "cameras.view", description: "Ver cámaras", narrowest_scope: "camera" as const },
  { permission: "live.view", description: "Ver video en vivo", narrowest_scope: "camera" as const },
  { permission: "events.view", description: "Ver eventos", narrowest_scope: "camera" as const },
  { permission: "users.manage", description: "Gestionar usuarios", narrowest_scope: "tenant" as const },
];

const user = (id: string, username: string) => ({
  id,
  tenant_id: "t1",
  username,
  display_name: `Nombre ${username}`,
  status: "active",
  group_ids: [],
});

const group = (id: string, name: string) => ({
  id,
  tenant_id: "t1",
  name,
  description: "",
  member_ids: [],
});

const site = (id: string, name: string) => ({ id, tenant_id: "t1", name, server_count: 1, camera_count: 2 });

const grant = (id: string, permission: string, effect: "allow" | "deny", scope_type: string, scope_id?: string) => ({
  id,
  tenant_id: "t1",
  subject_type: "user",
  subject_id: "u1",
  permission,
  effect,
  scope_type,
  scope_id,
});

function setup(entry = "/?subject=user:u1", grantsList = [grant("g-1", "live.view", "allow", "site", "s1"), grant("g-2", "cameras.view", "deny", "camera", "c1")]) {
  const writes: { method: string; url: string; body: unknown }[] = [];
  const routes = stubApi({
    "/api/v1/me": () =>
      json({
        id: "u-admin",
        tenant_id: "t1",
        grants: [{ permission: "permissions.manage", effect: "allow", scope_type: "platform" }],
      }),
    "/api/v1/users": () => json({ items: [user("u1", "carlos"), user("u2", "marina")] }),
    "/api/v1/groups": () => json({ items: [group("grp1", "Operadores"), group("grp2", "Seguridad")] }),
    "/api/v1/permissions": () => json({ items: catalog }),
    "/api/v1/grants": () => json({ items: grantsList }),
    "/api/v1/tenants": () => json({ items: [{ id: "t1", name: "Tenant 1" }] }),
    "/api/v1/sites": () => json({ items: [site("s1", "Sucursal Central")] }),
    "/api/v1/servers": () => json({ items: [] }),
    "/api/v1/cameras": () => json({ items: [{ id: "c1", tenant_id: "t1", display_name: "Camara Entrada" }] }),
    "/api/v1/camera-groups": () => json({ items: [] }),
  });

  const fetchMock = vi.fn(async (req: Request) => {
    if (["POST", "DELETE"].includes(req.method)) {
      writes.push({
        method: req.method,
        url: new URL(req.url).pathname,
        body: req.method === "DELETE" ? null : await req.clone().json(),
      });
      return json({ ok: true });
    }
    return routes(req);
  });
  vi.stubGlobal("fetch", fetchMock);

  renderPage(Permissions, entry);
  return { writes };
}

describe("Permissions", () => {
  it("filters searchable subjects and updates selection", async () => {
    setup("/");
    expect(await screen.findByRole("heading", { name: "Permisos" })).toBeInTheDocument();

    const searchInput = screen.getByLabelText("Buscar sujeto");
    fireEvent.change(searchInput, { target: { value: "mari" } });

    const select = screen.getByLabelText("Usuario o grupo");
    // Groups should be filtered out
    expect(within(select).queryByText("Operadores")).not.toBeInTheDocument();
    // marina should remain visible
    expect(within(select).getByText("Nombre marina (marina)")).toBeInTheDocument();
  });

  it("loads from deep link, displays summary count, and renders grouped grants table", async () => {
    setup("/?subject=user:u1");
    expect(await screen.findByText("2 permisos directos")).toBeInTheDocument();

    // Group headers or scope names
    expect(await screen.findByText(/Sitio: Sucursal Central/i)).toBeInTheDocument();
    expect(await screen.findByText(/Cámara: Camara Entrada/i)).toBeInTheDocument();

    // Effects displayed in table
    const table = screen.getByRole("table", { name: "Permisos otorgados" });
    expect(within(table).getByText("Permitir")).toBeInTheDocument();
    expect(within(table).getByText("Denegar")).toBeInTheDocument();
  });

  it("shows empty state when subject has no grants", async () => {
    setup("/?subject=user:u1", []);
    expect(await screen.findByText("Sin permisos directos.")).toBeInTheDocument();
  });

  it("revokes a grant with DELETE request", async () => {
    const { writes } = setup("/?subject=user:u1");
    const revokeButtons = await screen.findAllByRole("button", { name: /Revocar/i });
    fireEvent.click(revokeButtons[0]!);

    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]).toEqual({
      method: "DELETE",
      url: "/api/v1/grants/g-1",
      body: null,
    });
  });

  it("grants an individual permission with POST request", async () => {
    const { writes } = setup("/?subject=user:u1");
    await screen.findByText("2 permisos directos");

    fireEvent.change(screen.getByLabelText("Qué"), { target: { value: "users.manage" } });
    fireEvent.change(screen.getByLabelText("Alcance"), { target: { value: "tenant" } });
    fireEvent.change(screen.getByLabelText("Organización"), { target: { value: "t1" } });

    fireEvent.click(screen.getByRole("button", { name: "Otorgar" }));

    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]).toEqual({
      method: "POST",
      url: "/api/v1/grants",
      body: {
        subject_type: "user",
        subject_id: "u1",
        permission: "users.manage",
        effect: "allow",
        scope_type: "tenant",
        scope_id: "t1",
      },
    });
  });
});
