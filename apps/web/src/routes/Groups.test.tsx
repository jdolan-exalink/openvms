import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Groups } from "./Groups";

afterEach(() => vi.unstubAllGlobals());

const group = (id: string, name: string, extra: object = {}) => ({ id, tenant_id: "t", name, description: "", member_ids: [], ...extra });
const user = (id: string, username: string) => ({ id, tenant_id: "t", username, display_name: `Nombre ${username}`, status: "active", group_ids: [] });

function setup(perms: string[]) {
  const routes = stubApi({
    "/api/v1/me": () => json({ id: "u-me", tenant_id: "t", grants: perms.map((permission) => ({ permission, effect: "allow", scope_type: "platform" })) }),
    "/api/v1/groups": () => json({ items: [group("g1", "Operadores", { member_ids: ["u1"] }), group("g2", "Auditores")] }),
    "/api/v1/users": () => json({ items: [user("u1", "ana"), user("u2", "beto")] }),
  });
  const writes: { method: string; url: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      if (["POST", "PUT", "DELETE"].includes(req.method)) {
        writes.push({ method: req.method, url: new URL(req.url).pathname, body: req.method === "DELETE" ? null : await req.clone().json() });
        return json(group("g1", "x"));
      }
      return routes(req);
    }),
  );
  renderPage(Groups);
  return { writes };
}

describe("Groups", () => {
  it("summarizes and filters by name with N de M", async () => {
    setup(["groups.manage"]);
    expect(await screen.findByText("2 grupos")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Buscar grupo"), { target: { value: "audit" } });
    expect(await screen.findByText("1 de 2 grupos")).toBeInTheDocument();
    expect(screen.queryByText("Operadores")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Buscar grupo"), { target: { value: "zzz" } });
    expect(await screen.findByText("Ningún grupo coincide con el filtro.")).toBeInTheDocument();
  });

  it("creates a group from a drawer with the same POST payload", async () => {
    const { writes } = setup(["groups.manage"]);
    fireEvent.click(await screen.findByRole("button", { name: "Nuevo grupo" }));
    const dialog = await screen.findByRole("dialog", { name: "Nuevo grupo" });
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: " Guardias " } });
    fireEvent.click(await within(dialog).findByLabelText("Nombre ana"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]).toEqual({ method: "POST", url: "/api/v1/groups", body: { name: "Guardias", description: "", member_ids: ["u1"] } });
  });

  it("edits a group with the same PUT payload", async () => {
    const { writes } = setup(["groups.manage"]);
    fireEvent.click((await screen.findAllByRole("button", { name: /Editar/ }))[0]!);
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(await within(dialog).findByLabelText("Nombre beto"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]).toEqual({ method: "PUT", url: "/api/v1/groups/g1", body: { tenant_id: "t", name: "Operadores", description: "", member_ids: ["u1", "u2"] } });
  });

  it("confirms in the UI before deleting, without window.confirm", async () => {
    const confirmSpy = vi.fn(() => true);
    vi.stubGlobal("confirm", confirmSpy);
    const { writes } = setup(["groups.manage"]);
    fireEvent.click((await screen.findAllByRole("button", { name: /Editar/ }))[1]!);
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Eliminar grupo" }));
    const confirm = await screen.findByRole("dialog", { name: "Eliminar grupo" });
    expect(writes).toHaveLength(0);
    fireEvent.click(within(confirm).getByRole("button", { name: "Eliminar" }));
    await waitFor(() => expect(writes).toEqual([{ method: "DELETE", url: "/api/v1/groups/g2", body: null }]));
    expect(confirmSpy).not.toHaveBeenCalled();
  });

  it("hides manage actions without groups.manage and links to Permisos with permissions.manage", async () => {
    setup(["permissions.manage"]);
    await screen.findByText("2 grupos");
    expect(screen.queryByRole("button", { name: "Nuevo grupo" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Editar/ })).not.toBeInTheDocument();
    const links = await screen.findAllByRole("link", { name: /Permisos/ });
    expect(links[0]).toHaveAttribute("href", expect.stringContaining("subject=group%3Ag1"));
  });
});
