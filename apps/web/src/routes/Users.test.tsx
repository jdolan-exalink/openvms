import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Users } from "./Users";

afterEach(() => vi.unstubAllGlobals());

const user = (id: string, username: string, extra: object = {}) => ({
  id,
  tenant_id: "t",
  username,
  display_name: `Nombre ${username}`,
  email: `${username}@example.com`,
  status: "active",
  mfa_enabled: false,
  has_password: true,
  group_ids: [],
  last_login_at: null,
  ...extra,
});

const me = (...permissions: string[]) => ({
  id: "u-me",
  tenant_id: "t",
  grants: permissions.map((permission) => ({ permission, effect: "allow", scope_type: "platform" })),
});

function setup(perms: string[], users = [user("u-me", "yo"), user("u2", "ana"), user("u3", "beto", { status: "disabled" })]) {
  const routes = stubApi({
    "/api/v1/me": () => json(me(...perms)),
    "/api/v1/users": () => json({ items: users }),
    "/api/v1/groups": () => json({ items: [{ id: "g1", tenant_id: "t", name: "Operadores", description: "", member_ids: [] }] }),
  });
  const writes: { method: string; url: string; body: unknown }[] = [];
  const fetchMock = vi.fn(async (req: Request) => {
    if (["POST", "PATCH", "DELETE"].includes(req.method)) {
      writes.push({ method: req.method, url: new URL(req.url).pathname, body: req.method === "DELETE" ? null : await req.clone().json() });
      return json(user("u2", "ana"));
    }
    return routes(req);
  });
  vi.stubGlobal("fetch", fetchMock);
  renderPage(Users);
  return { writes };
}

describe("Users", () => {
  it("summarizes, filters by text and by status, and reports N de M", async () => {
    setup(["users.manage"]);
    expect(await screen.findByText("3 usuarios")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Buscar usuario"), { target: { value: "ANA@" } });
    expect(await screen.findByText("1 de 3 usuarios")).toBeInTheDocument();
    expect(screen.queryByText("Nombre beto")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Buscar usuario"), { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("Filtrar por estado"), { target: { value: "disabled" } });
    expect(await screen.findByText("1 de 3 usuarios")).toBeInTheDocument();
    expect(screen.getByText("Nombre beto")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Buscar usuario"), { target: { value: "zzz" } });
    expect(await screen.findByText("Ningún usuario coincide con el filtro.")).toBeInTheDocument();
  });

  it("creates a user from a drawer with the same POST payload", async () => {
    const { writes } = setup(["users.manage"]);
    fireEvent.click(await screen.findByRole("button", { name: "Nuevo usuario" }));
    const dialog = await screen.findByRole("dialog", { name: "Nuevo usuario" });
    fireEvent.change(within(dialog).getByLabelText("Usuario"), { target: { value: "carla" } });
    fireEvent.change(within(dialog).getByLabelText("Nombre para mostrar"), { target: { value: "Carla" } });
    fireEvent.click(within(dialog).getByLabelText("Operadores"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]).toEqual({
      method: "POST",
      url: "/api/v1/users",
      body: { username: "carla", display_name: "Carla", must_change_password: true, group_ids: ["g1"] },
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("edits a user with the same PATCH payload", async () => {
    const { writes } = setup(["users.manage"]);
    fireEvent.click((await screen.findAllByRole("button", { name: /Editar/ }))[1]!);
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Estado"), { target: { value: "locked" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]).toEqual({
      method: "PATCH",
      url: "/api/v1/users/u2",
      body: { display_name: "Nombre ana", email: "ana@example.com", status: "locked", group_ids: [] },
    });
  });

  it("confirms in the UI before deleting, without window.confirm", async () => {
    const confirmSpy = vi.fn(() => true);
    vi.stubGlobal("confirm", confirmSpy);
    const { writes } = setup(["users.manage"]);
    fireEvent.click((await screen.findAllByRole("button", { name: /Editar/ }))[1]!);
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Eliminar usuario" }));
    const confirm = await screen.findByRole("dialog", { name: "Eliminar usuario" });
    expect(writes).toHaveLength(0);
    fireEvent.click(within(confirm).getByRole("button", { name: "Eliminar" }));
    await waitFor(() => expect(writes).toEqual([{ method: "DELETE", url: "/api/v1/users/u2", body: null }]));
    expect(confirmSpy).not.toHaveBeenCalled();
  });

  it("does not offer deleting the current user", async () => {
    setup(["users.manage"]);
    fireEvent.click((await screen.findAllByRole("button", { name: /Editar/ }))[0]!);
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByRole("button", { name: "Eliminar usuario" })).not.toBeInTheDocument();
  });

  it("hides manage actions without users.manage and links to Permisos with permissions.manage", async () => {
    setup(["permissions.manage"]);
    await screen.findByText("3 usuarios");
    expect(screen.queryByRole("button", { name: "Nuevo usuario" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Editar/ })).not.toBeInTheDocument();
    const links = await screen.findAllByRole("link", { name: /Permisos/ });
    expect(links[1]).toHaveAttribute("href", expect.stringContaining("subject=user%3Au2"));
  });
});
