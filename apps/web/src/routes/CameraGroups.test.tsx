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
  ...extra,
});

const camera = (id: string, name: string) => ({
  id,
  tenant_id: "t1",
  display_name: name,
  site_id: "s1",
  server_id: "srv1",
  enabled: true,
});

function setup(perms: string[]) {
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
        ],
      }),
    "/api/v1/cameras": () =>
      json({
        items: [camera("c1", "Entrada"), camera("c2", "Estacionamiento")],
      }),
  });

  const writes: { method: string; url: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      if (["POST", "PUT", "DELETE"].includes(req.method)) {
        writes.push({
          method: req.method,
          url: new URL(req.url).pathname,
          body: req.method === "DELETE" ? null : await req.clone().json(),
        });
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
    expect(writes[0]).toEqual({
      method: "POST",
      url: "/api/v1/camera-groups",
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
    expect(writes[0]).toEqual({
      method: "PUT",
      url: "/api/v1/camera-groups/cg1",
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
      expect(writes).toEqual([{ method: "DELETE", url: "/api/v1/camera-groups/cg2", body: null }]),
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
});
