import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { CameraGroups } from "./CameraGroups";

afterEach(() => vi.unstubAllGlobals());

const cameraGroup = (id: string, name: string, camera_ids: string[] = [], extra: object = {}) => ({
  id,
  tenant_id: "t1",
  name,
  description: `Desc ${name}`,
  camera_ids,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  member_count_total: camera_ids.length,
  member_count_hidden: 0,
  version: 1,
  ...extra,
});

const dynamicGroup = cameraGroup("cg3", "Exteriores", ["c1"], {
  filter: { tags: ["exterior"], name_contains: "norte" },
  member_count_total: 4,
  member_count_hidden: 3,
  version: 5,
});

const camera = (id: string, name: string) => ({
  id,
  tenant_id: "t1",
  display_name: name,
  site_id: "s1",
  server_id: "srv1",
  enabled: true,
});

function setup(perms: string[], withDynamic = false, opts: { routes?: Record<string, () => Response>; putStatus?: number } = {}) {
  const routes = stubApi({
    "/api/v1/me": () =>
      json({
        id: "u-me",
        tenant_id: "t1",
        grants: perms.map((permission) => ({ permission, effect: "allow", scope_type: "platform" })),
      }),
    "/api/v1/camera-groups": () =>
      json({
        items: [
          cameraGroup("cg1", "Perímetro", ["c1"]),
          cameraGroup("cg2", "Acceso", ["c1", "c2"]),
          ...(withDynamic ? [dynamicGroup] : []),
        ],
      }),
    "/api/v1/sites": () => json({ items: [{ id: "s1", tenant_id: "t1", name: "Helvecia" }] }),
    "/api/v1/servers": () => json({ items: [{ id: "srv1", tenant_id: "t1", name: "frigate-h01" }] }),
    ...opts.routes,
    "/api/v1/cameras": () =>
      json({
        items: [camera("c1", "Entrada"), camera("c2", "Estacionamiento")],
      }),
  });

  const writes: { method: string; url: string; body: unknown; ifMatch?: string | null }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      if (["POST", "PUT", "DELETE"].includes(req.method)) {
        writes.push({
          method: req.method,
          url: new URL(req.url).pathname,
          body: req.method === "DELETE" ? null : await req.clone().json(),
          ifMatch: req.headers.get("If-Match"),
        });
        if (req.method === "PUT" && opts.putStatus === 409) {
          return json({ code: "conflict", message: "camera group was modified by someone else; reload and retry with the current version" }, 409);
        }
        return json(cameraGroup("cg1", "Perímetro"));
      }
      return routes(req);
    }),
  );

  renderPage(CameraGroups);
  return { writes };
}

describe("CameraGroups", () => {
  it("summarizes and filters camera groups by name with N de M", async () => {
    setup(["cameras.manage"]);
    expect(await screen.findByText("2 grupos")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Buscar grupo"), { target: { value: "perím" } });
    expect(await screen.findByText("1 de 2 grupos")).toBeInTheDocument();
    expect(screen.queryByText("Acceso")).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Buscar grupo"), { target: { value: "zzz" } });
    expect(await screen.findByText("Ningún grupo coincide con el filtro.")).toBeInTheDocument();
  });

  it("creates a camera group from a drawer with POST payload", async () => {
    const { writes } = setup(["cameras.manage"]);
    fireEvent.click(await screen.findByRole("button", { name: "Nuevo grupo" }));

    const dialog = await screen.findByRole("dialog", { name: "Nuevo grupo de cámaras" });
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Patio Norte" } });
    fireEvent.change(within(dialog).getByLabelText("Descripción"), { target: { value: "Cámaras del patio" } });
    fireEvent.click(await within(dialog).findByLabelText("Entrada"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));

    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]).toMatchObject({
      method: "POST",
      url: "/api/v1/camera-groups",
      ifMatch: null,
      body: {
        tenant_id: "t1",
        name: "Patio Norte",
        description: "Cámaras del patio",
        camera_ids: ["c1"],
      },
    });
  });

  it("edits a camera group with PUT payload", async () => {
    const { writes } = setup(["cameras.manage"]);
    const editButtons = await screen.findAllByRole("button", { name: /Editar/ });
    fireEvent.click(editButtons[0]!);

    const dialog = await screen.findByRole("dialog");
    fireEvent.click(await within(dialog).findByLabelText("Estacionamiento"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));

    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]).toMatchObject({
      method: "PUT",
      url: "/api/v1/camera-groups/cg1",
      ifMatch: "1",
      body: {
        tenant_id: "t1",
        name: "Perímetro",
        description: "Desc Perímetro",
        camera_ids: ["c1", "c2"],
      },
    });
  });

  it("confirms in the UI before deleting, without window.confirm", async () => {
    const confirmSpy = vi.fn(() => true);
    vi.stubGlobal("confirm", confirmSpy);

    const { writes } = setup(["cameras.manage"]);
    const editButtons = await screen.findAllByRole("button", { name: /Editar/ });
    fireEvent.click(editButtons[1]!);

    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Eliminar grupo" }));
    const confirm = await screen.findByRole("dialog", { name: "Eliminar grupo de cámaras" });
    expect(writes).toHaveLength(0);

    fireEvent.click(within(confirm).getByRole("button", { name: "Eliminar" }));
    await waitFor(() =>
      expect(writes).toEqual([{ method: "DELETE", url: "/api/v1/camera-groups/cg2", body: null, ifMatch: null }]),
    );
    expect(confirmSpy).not.toHaveBeenCalled();
  });

  it("hides manage actions without cameras.manage and links to Permisos with permissions.manage", async () => {
    setup(["permissions.manage"]);
    await screen.findByText("2 grupos");
    expect(screen.queryByRole("button", { name: "Nuevo grupo" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Editar/ })).not.toBeInTheDocument();

    const links = await screen.findAllByRole("link", { name: /Permisos/ });
    expect(links[0]).toHaveAttribute("href", expect.stringContaining("subject=camera_group%3Acg1"));
  });

  it("shows the dynamic badge and total/hidden member counts", async () => {
    setup(["cameras.manage"], true);
    const row = (await screen.findByText("Exteriores")).closest("tr")!;
    expect(within(row).getByText("Dynamic")).toBeInTheDocument();
    expect(within(row).getByText("4 cámaras")).toBeInTheDocument();
    expect(within(row).getByText("3 hidden from you")).toBeInTheDocument();
    const manualRow = (await screen.findByText("Acceso")).closest("tr")!;
    expect(within(manualRow).queryByText("Dynamic")).not.toBeInTheDocument();
    expect(within(manualRow).queryByText(/hidden from you/)).not.toBeInTheDocument();
  });

  it("creates a dynamic group with a filter and no camera_ids", async () => {
    const { writes } = setup(["cameras.manage"]);
    fireEvent.click(await screen.findByRole("button", { name: "Nuevo grupo" }));

    const dialog = await screen.findByRole("dialog", { name: "Nuevo grupo de cámaras" });
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Exteriores" } });
    fireEvent.change(within(dialog).getByLabelText("Group type"), { target: { value: "dynamic" } });
    expect(within(dialog).queryByLabelText("Entrada")).not.toBeInTheDocument();
    fireEvent.click(await within(dialog).findByLabelText("Helvecia"));
    fireEvent.change(within(dialog).getByLabelText("Tags (comma separated)"), { target: { value: " exterior, acceso " } });
    fireEvent.change(within(dialog).getByLabelText("Name contains"), { target: { value: "norte" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));

    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]).toMatchObject({
      method: "POST",
      url: "/api/v1/camera-groups",
      body: {
        tenant_id: "t1",
        name: "Exteriores",
        description: "",
        filter: { site_ids: ["s1"], tags: ["exterior", "acceso"], name_contains: "norte" },
      },
    });
    expect(writes[0]!.body).not.toHaveProperty("camera_ids");
  });

  it("edits a dynamic group with its filter and version, without camera_ids", async () => {
    const { writes } = setup(["cameras.manage"], true);
    fireEvent.click(await screen.findByRole("button", { name: "Editar Exteriores" }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("Group type")).toBeDisabled();
    expect(within(dialog).getByLabelText("Tags (comma separated)")).toHaveValue("exterior");
    fireEvent.change(within(dialog).getByLabelText("Name contains"), { target: { value: "sur" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));

    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]).toMatchObject({
      method: "PUT",
      url: "/api/v1/camera-groups/cg3",
      ifMatch: "5",
      body: { tenant_id: "t1", name: "Exteriores", filter: { tags: ["exterior"], name_contains: "sur" } },
    });
    expect(writes[0]!.body).not.toHaveProperty("camera_ids");
  });

  it("blocks saving a dynamic group with an empty filter", async () => {
    const { writes } = setup(["cameras.manage"]);
    fireEvent.click(await screen.findByRole("button", { name: "Nuevo grupo" }));
    const dialog = await screen.findByRole("dialog", { name: "Nuevo grupo de cámaras" });
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Vacio" } });
    fireEvent.change(within(dialog).getByLabelText("Group type"), { target: { value: "dynamic" } });
    fireEvent.change(within(dialog).getByLabelText("Tags (comma separated)"), { target: { value: " , " } });
    fireEvent.change(within(dialog).getByLabelText("Name contains"), { target: { value: "   " } });
    expect(within(dialog).getByText("Select at least one site, server, tag or name filter.")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Guardar" })).toBeDisabled();
    expect(writes).toHaveLength(0);
  });

  it("shows loading and error states for the site and server pickers and blocks saving on error", async () => {
    setup(["cameras.manage"], false, {
      routes: { "/api/v1/sites": () => json({ code: "internal", message: "boom" }, 500) },
    });
    fireEvent.click(await screen.findByRole("button", { name: "Nuevo grupo" }));
    const dialog = await screen.findByRole("dialog", { name: "Nuevo grupo de cámaras" });
    fireEvent.change(within(dialog).getByLabelText("Group type"), { target: { value: "dynamic" } });
    expect(within(dialog).getByText("Loading servers…")).toBeInTheDocument();
    expect(await within(dialog).findByText("Could not load sites.")).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText("Name contains"), { target: { value: "norte" } });
    expect(within(dialog).getByRole("button", { name: "Guardar" })).toBeDisabled();
  });

  it("selects servers in the filter", async () => {
    const { writes } = setup(["cameras.manage"]);
    fireEvent.click(await screen.findByRole("button", { name: "Nuevo grupo" }));
    const dialog = await screen.findByRole("dialog", { name: "Nuevo grupo de cámaras" });
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Por servidor" } });
    fireEvent.change(within(dialog).getByLabelText("Group type"), { target: { value: "dynamic" } });
    fireEvent.click(await within(dialog).findByLabelText("frigate-h01"));
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]!.body).toEqual({ tenant_id: "t1", name: "Por servidor", description: "", filter: { server_ids: ["srv1"] } });
  });

  it("sends only the active kind when switching between manual and dynamic before saving", async () => {
    const { writes } = setup(["cameras.manage"]);
    fireEvent.click(await screen.findByRole("button", { name: "Nuevo grupo" }));
    const dialog = await screen.findByRole("dialog", { name: "Nuevo grupo de cámaras" });
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Cambio" } });
    fireEvent.click(await within(dialog).findByLabelText("Entrada"));
    fireEvent.change(within(dialog).getByLabelText("Group type"), { target: { value: "dynamic" } });
    fireEvent.change(within(dialog).getByLabelText("Name contains"), { target: { value: "norte" } });
    fireEvent.change(within(dialog).getByLabelText("Group type"), { target: { value: "manual" } });
    expect(within(dialog).getByLabelText("Entrada")).toBeChecked();
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]!.body).toEqual({ tenant_id: "t1", name: "Cambio", description: "", camera_ids: ["c1"] });
  });

  it("offers a reload when the group version is stale", async () => {
    const { writes } = setup(["cameras.manage"], true, { putStatus: 409 });
    fireEvent.click(await screen.findByRole("button", { name: "Editar Exteriores" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    expect(await within(dialog).findByText("This group was changed by someone else since you opened it.")).toBeInTheDocument();
    expect(writes[0]).toMatchObject({ method: "PUT", ifMatch: "5" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Reload latest" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });
});
