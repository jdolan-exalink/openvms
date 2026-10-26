import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { CameraProfiles } from "./CameraProfiles";

afterEach(() => vi.unstubAllGlobals());

const profile = (id: string, name: string, extra: object = {}) => ({
  id, name, description: `Desc ${name}`, patch: { detection: { enabled: true }, recording: { enabled: true } }, version: 2, camera_count: 3,
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", ...extra,
});

function setup(perms: string[], routes: Record<string, () => Response> = {}) {
  const sent: { method: string; path: string; body: unknown }[] = [];
  const api = stubApi({
    "/api/v1/me": () => json({ id: "u", tenant_id: "t1", grants: perms.map((permission) => ({ permission, effect: "allow", scope_type: "platform" })) }),
    "/api/v1/camera-config/profiles": () => json([profile("p1", "Exterior"), profile("p2", "LPR", { camera_count: 0 })]),
    "/api/v1/camera-config/profiles/p1": () => json({ ...profile("p1", "Exterior"), cameras: [{ camera_id: "c1", outdated: true }, { camera_id: "c2", outdated: false }] }),
    "/api/v1/camera-config/profiles/p2": () => json({ ...profile("p2", "LPR", { camera_count: 0 }), cameras: [] }),
    "/api/v1/cameras": () => json({ items: [{ id: "c1", tenant_id: "t1", display_name: "Entrada", site_id: "s", server_id: "v", enabled: true }, { id: "c2", tenant_id: "t1", display_name: "Patio", site_id: "s", server_id: "v", enabled: true }] }),
    ...routes,
  });
  vi.stubGlobal("fetch", vi.fn(async (req: Request) => {
    if (req.method !== "GET") sent.push({ method: req.method, path: new URL(req.url).pathname, body: req.method === "DELETE" ? null : await req.clone().json() });
    return api(req);
  }));
  renderPage(() => <CameraProfiles />);
  return sent;
}

describe("CameraProfiles", () => {
  it("lists profiles with followers and outdated counts", async () => {
    setup(["cameras.manage"]);
    const row = (await screen.findByText("Exterior")).closest("tr")!;
    expect(within(row).getByText("3 cámaras")).toBeInTheDocument();
    expect(await within(row).findByText("1 desactualizadas")).toBeInTheDocument();
    expect(within(row).getByText(/Detección activada · Grabación activada/)).toBeInTheDocument();
  });

  it("is read-only without cameras.manage", async () => {
    setup(["cameras.view"]);
    await screen.findByText("Exterior");
    expect(screen.queryByRole("button", { name: /Nuevo perfil/ })).toBeNull();
    expect(screen.queryByRole("button", { name: "Editar Exterior" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Eliminar Exterior" })).toBeNull();
    expect(screen.getByRole("button", { name: "Ver cámaras de Exterior" })).toBeInTheDocument();
  });

  it("creates a profile with a structured patch", async () => {
    const sent = setup(["cameras.manage"], { "POST /api/v1/camera-config/profiles": () => json(profile("p3", "Nuevo"), 201) });
    fireEvent.click(await screen.findByRole("button", { name: /Nuevo perfil/ }));
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Interior" } });
    fireEvent.change(screen.getByLabelText("Detección de objetos"), { target: { value: "off" } });
    fireEvent.change(screen.getByLabelText("Stream principal: FPS"), { target: { value: "10" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(sent.find((s) => s.method === "POST")).toBeDefined());
    expect(sent.find((s) => s.method === "POST")?.body).toEqual({ name: "Interior", description: "", patch: { detection: { enabled: false }, streams: { primary: { fps: 10 } } } });
  });

  it("blocks saving an invalid value and shows server validation errors", async () => {
    setup(["cameras.manage"], { "POST /api/v1/camera-config/profiles": () => json({ code: "validation_error", message: "analytics.lpr: el motor no lo soporta" }, 400) });
    fireEvent.click(await screen.findByRole("button", { name: /Nuevo perfil/ }));
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "X" } });
    fireEvent.change(screen.getByLabelText("Stream principal: FPS"), { target: { value: "abc" } });
    expect(screen.getByRole("button", { name: "Guardar" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText(/Stream principal: FPS/), { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("Lectura de patentes (LPR)"), { target: { value: "on" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(await screen.findByText(/el motor no lo soporta/)).toBeInTheDocument();
  });

  it("edits sending the expected version", async () => {
    const sent = setup(["cameras.manage"], { "PUT /api/v1/camera-config/profiles/p1": () => json(profile("p1", "Exterior")) });
    fireEvent.click(await screen.findByRole("button", { name: "Editar Exterior" }));
    fireEvent.change(screen.getByLabelText("Lectura de patentes (LPR)"), { target: { value: "on" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(sent.find((s) => s.method === "PUT")).toBeDefined());
    expect(sent.find((s) => s.method === "PUT")?.body).toEqual({ name: "Exterior", description: "Desc Exterior", patch: { detection: { enabled: true }, recording: { enabled: true }, analytics: { lpr: true } }, expected_version: 2 });
  });

  it("explains how many cameras use a profile that cannot be deleted", async () => {
    setup(["cameras.manage"], { "DELETE /api/v1/camera-config/profiles/p1": () => json({ code: "profile_in_use", message: "in use" }, 409) });
    fireEvent.click(await screen.findByRole("button", { name: "Eliminar Exterior" }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Eliminar" }));
    expect(await screen.findByText(/3 cámaras usan este perfil/)).toBeInTheDocument();
  });

  it("deletes an unused profile", async () => {
    const sent = setup(["cameras.manage"], { "DELETE /api/v1/camera-config/profiles/p2": () => new Response(null, { status: 204 }) });
    fireEvent.click(await screen.findByRole("button", { name: "Eliminar LPR" }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Eliminar" }));
    await waitFor(() => expect(sent.some((s) => s.method === "DELETE" && s.path.endsWith("/p2"))).toBe(true));
  });

  it("lists the outdated cameras to reapply the profile on", async () => {
    setup(["cameras.manage"]);
    fireEvent.click(await screen.findByRole("button", { name: "Ver cámaras de Exterior" }));
    const outdated = await screen.findByRole("region", { name: "Cámaras desactualizadas" });
    expect(await within(outdated).findByText("Entrada")).toBeInTheDocument();
    expect(within(outdated).queryByText("Patio")).toBeNull();
    expect(within(screen.getByRole("region", { name: "Cámaras al día" })).getByText("Patio")).toBeInTheDocument();
  });

  describe("re-apply to outdated cameras", () => {
    const review = (parameters: number) => ({ review: { changes: [], impact: [{ code: "engine_reload", message: "Frigate recargará configuración" }], unsupported: [] }, parameters, shadowed: [], converted: [], dropped: [] });
    const twoOutdated = { "/api/v1/camera-config/profiles/p1": () => json({ ...profile("p1", "Exterior"), cameras: [{ camera_id: "c1", outdated: true }, { camera_id: "c2", outdated: true }] }) };

    it("reviews every outdated camera, applies nothing until confirmed, then reports per-camera outcomes", async () => {
      const sent = setup(["cameras.manage"], {
        ...twoOutdated,
        "/api/v1/cameras/c1/profile/review": () => json(review(2)),
        "/api/v1/cameras/c2/profile/review": () => json(review(1)),
        "/api/v1/cameras/c1/profile/apply": () => json({ review: review(2) }),
        "/api/v1/cameras/c2/profile/apply": () => json({ message: "boom" }, 500),
      });
      fireEvent.click(await screen.findByRole("button", { name: "Ver cámaras de Exterior" }));
      fireEvent.click(await screen.findByRole("button", { name: "Reaplicar a las desactualizadas" }));
      const confirm = await screen.findByRole("button", { name: "Aplicar a 2 cámaras" });
      const table = within(screen.getByRole("table", { name: "Resumen de reaplicación" }));
      expect(table.getByText("Entrada")).toBeInTheDocument();
      expect(table.getAllByText("Frigate recargará configuración")).toHaveLength(2);
      expect(sent.filter((s) => s.path.endsWith("/profile/apply"))).toHaveLength(0);
      fireEvent.click(confirm);
      await waitFor(() => expect(sent.filter((s) => s.path.endsWith("/profile/apply"))).toHaveLength(2));
      expect(await table.findByText("Aplicada")).toBeInTheDocument();
      expect(await table.findByText(/Falló/)).toBeInTheDocument();
      expect(sent.filter((s) => s.path.endsWith("/profile/apply"))[0]?.body).toEqual({ profile_id: "p1" });
    });

    it("keeps a camera whose review fails out of the confirmation", async () => {
      setup(["cameras.manage"], {
        ...twoOutdated,
        "/api/v1/cameras/c1/profile/review": () => json(review(2)),
        "/api/v1/cameras/c2/profile/review": () => json({ message: "sin permiso" }, 403),
      });
      fireEvent.click(await screen.findByRole("button", { name: "Ver cámaras de Exterior" }));
      fireEvent.click(await screen.findByRole("button", { name: "Reaplicar a las desactualizadas" }));
      expect(await screen.findByRole("button", { name: "Aplicar a 1 cámaras" })).toBeEnabled();
      expect(screen.getAllByRole("alert").length).toBeGreaterThan(0);
    });
  });
});
