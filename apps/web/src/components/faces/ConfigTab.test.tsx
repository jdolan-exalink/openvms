import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { Faces } from "@/routes/Faces";
import { json, renderPage } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());
beforeEach(() => sessionStorage.clear());

const server = (over: Partial<Schemas["FaceServerConfig"]> = {}): Schemas["FaceServerConfig"] => ({
  server_id: "s1", server_name: "h01", site_name: "Sede", frigate_version: "0.17.0", can_edit: true,
  enabled: false, model_size: "small", detection_threshold: 0.7, recognition_threshold: 0.9, unknown_score: 0.8,
  min_faces: 1, min_area: 750, save_attempts: 200, blur_confidence_filter: true,
  cameras: [
    { camera_id: "c1", name: "acceso", enabled: false, min_area: 750 },
    { camera_id: "c2", name: "plaza", enabled: true, enabled_override: true },
  ],
  ...over,
});

type Call = { method: string; path: string; search?: URLSearchParams; body?: unknown };

function install(permissions: string[], servers: Schemas["FaceServerConfig"][]) {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (raw: Request) => {
      const url = new URL(raw.url);
      const call: Call = { method: raw.method, path: url.pathname, search: url.searchParams };
      if (raw.method !== "GET") {
        const text = await raw.clone().text();
        call.body = text ? JSON.parse(text) : undefined;
      }
      calls.push(call);
      const p = url.pathname;
      if (p === "/api/v1/me") {
        return json({ id: "u1", username: "u1", display_name: "u1", mfa_enabled: false, must_change_password: false, auth_method: "session", tenant_id: "t1",
          grants: permissions.map((permission) => ({ permission, effect: "allow", scope_type: "platform" })) });
      }
      if (p === "/api/v1/sites") return json({ items: [{ id: "site1", name: "Sede" }, { id: "site2", name: "Anexo" }] });
      if (p === "/api/v1/faces/config" && raw.method === "GET") return json({ items: servers });
      if (p === "/api/v1/faces/config/s1" && raw.method === "PATCH") return json({ revision_ids: ["r1"], restart_required: true });
      if (p === "/api/v1/servers/s1/restart") return new Response(null, { status: 204 });
      if (p.startsWith("/api/v1/faces/")) return json({ items: [] });
      return json({ code: "not_found", message: "not found" }, 404);
    }),
  );
  return calls;
}

const all = ["faces.view", "faces.manage", "servers.view", "servers.config", "servers.restart"];

async function openConfig() {
  renderPage(Faces);
  fireEvent.click(await screen.findByRole("tab", { name: "Configuración" }));
}

describe("Faces configuration tab", () => {
  it("narrows the list by site and never sends a tenant_id", async () => {
    const calls = install(all, [server()]);
    await openConfig();
    await screen.findByRole("option", { name: "Anexo" });
    fireEvent.change(screen.getByLabelText("Sitio"), { target: { value: "site2" } });
    await waitFor(() => expect(calls.some((c) => c.path === "/api/v1/faces/config" && c.search?.getAll("site_id").join() === "site2")).toBe(true));
    expect(calls.filter((c) => c.path === "/api/v1/faces/config").some((c) => c.search?.has("tenant_id"))).toBe(false);
  });

  it("is hidden without servers.view and never calls the config API", async () => {
    const calls = install(["faces.view"], [server()]);
    renderPage(Faces);
    expect(await screen.findByRole("tab", { name: "Personas" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Configuración" })).not.toBeInTheDocument();
    expect(calls.some((c) => c.path === "/api/v1/faces/config")).toBe(false);
  });

  it("lists servers with their face status and offers no editing to a viewer", async () => {
    install(["faces.view", "servers.view"], [server({ can_edit: false }), server({ server_id: "s2", server_name: "c01", enabled: true, model_size: "large", recognition_threshold: 0.85, can_edit: false })]);
    await openConfig();
    const list = await screen.findByRole("list", { name: "Servidores" });
    expect(within(list).getByText("h01")).toBeInTheDocument();
    expect(within(list).getByText("Desactivado")).toBeInTheDocument();
    expect(within(list).getByText("Activado")).toBeInTheDocument();
    expect(within(list).getByText("Modelo large · reconocimiento 0.85 · 1 de 2 cámaras")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Configurar/ })).not.toBeInTheDocument();
    expect(screen.getAllByText(/Solo lectura/)).toHaveLength(2);
  });

  it("sends only the changed fields and then offers the Frigate restart", async () => {
    const calls = install(all, [server()]);
    await openConfig();
    fireEvent.click(await screen.findByRole("button", { name: "Configurar h01" }));
    fireEvent.click(screen.getByRole("switch", { name: "Reconocimiento facial activado" }));
    fireEvent.change(screen.getByLabelText("Tamaño del modelo"), { target: { value: "large" } });
    fireEvent.change(screen.getByLabelText("Umbral de reconocimiento"), { target: { value: "0.85" } });
    fireEvent.change(screen.getByLabelText("Área mínima de acceso"), { target: { value: "900" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar configuración" }));
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({
      enabled: true, model_size: "large", recognition_threshold: 0.85, cameras: [{ camera_id: "c1", min_area: 900 }],
    }));
    expect(await screen.findByText("Los cambios se guardaron, pero Frigate solo los aplica al reiniciar.")).toBeInTheDocument();
    expect(sessionStorage.getItem("openvms.frigate-restart.s1")).toBe("1");

    expect(calls.some((c) => c.path.endsWith("/restart"))).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Reiniciar Frigate" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Reiniciar Frigate" }));
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/v1/servers/s1/restart")).toBe(true));
    expect(await screen.findByText(/Reinicio solicitado/)).toBeInTheDocument();
    expect(sessionStorage.getItem("openvms.frigate-restart.s1")).toBeNull();
  });

  it("toggles a camera and tells a user without servers.restart who can restart", async () => {
    const calls = install(["faces.view", "faces.manage", "servers.view", "servers.config"], [server()]);
    await openConfig();
    fireEvent.click(await screen.findByRole("button", { name: "Configurar h01" }));
    fireEvent.click(screen.getByRole("switch", { name: "Reconocimiento en acceso" }));
    fireEvent.click(screen.getByRole("button", { name: "Guardar configuración" }));
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ cameras: [{ camera_id: "c1", enabled: true }] }));
    expect(await screen.findByText(/servers\.restart/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reiniciar Frigate" })).not.toBeInTheDocument();
  });

  it("refuses out-of-range values before calling the API", async () => {
    const calls = install(all, [server()]);
    await openConfig();
    fireEvent.click(await screen.findByRole("button", { name: "Configurar h01" }));
    fireEvent.change(screen.getByLabelText("Umbral de detección"), { target: { value: "1.5" } });
    fireEvent.change(screen.getByLabelText("Mínimo de rostros"), { target: { value: "9" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar configuración" }));
    expect(await screen.findByText("Umbral de detección debe estar entre 0 y 1.")).toBeInTheDocument();
    expect(screen.getByText("Mínimo de rostros debe estar entre 1 y 6.")).toBeInTheDocument();
    expect(calls.some((c) => c.method === "PATCH")).toBe(false);
  });

  it("shows why a server could not be read and does not offer to edit it", async () => {
    install(all, [server({ enabled: undefined, model_size: undefined, error: "the server could not be reached" })]);
    await openConfig();
    expect(await screen.findByText("No se pudo leer la configuración: the server could not be reached")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Configurar h01" })).not.toBeInTheDocument();
  });
});
