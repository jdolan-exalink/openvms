import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { FrigateCameraConfig } from "./FrigateCameraConfig";

vi.mock("@/components/zones/ZoneEditorModal", () => ({
  ZoneEditorModal: ({ value, onSave }: { value: { zones: Record<string, unknown> }; onSave: (v: unknown) => void }) => (
    <div role="dialog" aria-label="Editor de zonas">
      <span>{Object.keys(value.zones).join(",")}</span>
      <button type="button" onClick={() => onSave({ zones: { patio: { coordinates: "0.1,0.1,0.9,0.1,0.5,0.9" } }, motionMask: [], objectMask: undefined, objectFilterMasks: {} })}>guardar zonas</button>
    </div>
  ),
}));

afterEach(() => vi.unstubAllGlobals());

const schema = {
  $ref: "#/$defs/CameraConfig",
  $defs: {
    CameraConfig: {
      type: "object",
      properties: { detect: { $ref: "#/$defs/Detect" }, zones: { type: "object", additionalProperties: { type: "object" } }, onvif: { $ref: "#/$defs/Onvif" }, objects: { $ref: "#/$defs/Objects" } },
    },
    Detect: { type: "object", properties: { enabled: { type: "boolean", default: true }, fps: { type: "integer", minimum: 1, maximum: 30, description: "Frames por segundo" } } },
    Objects: { type: "object", properties: { track: { type: "array", items: { type: "string" } } } },
    Onvif: { type: "object", properties: { host: { type: "string" }, password: { type: "string" } } },
  },
};

function mount(doc: object, extra: Record<string, () => Response> = {}) {
  const routes = stubApi({
    "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [{ permission: "servers.restart", effect: "allow" }] }),
    "/api/v1/cameras/c1/frigate-config": () => json({ camera_id: "c1", camera_name: "acceso", server_id: "s1", frigate_version: "0.17.0", editable: true, secrets_visible: false, config: { detect: { enabled: true, fps: 5 }, onvif: { host: "10.0.0.2", password: "********" }, objects: { track: ["person"] } }, ...doc }),
    "/api/v1/servers/s1/frigate-config/schema": () => json(schema),
    ...extra,
  });
  const fetchMock = vi.fn(async (req: Request) => routes(req));
  vi.stubGlobal("fetch", fetchMock);
  const root = createRootRoute();
  const page = createRoute({ getParentRoute: () => root, path: "/cameras/$cameraId/frigate", component: FrigateCameraConfig });
  const cams = createRoute({ getParentRoute: () => root, path: "/cameras", component: () => null });
  const router = createRouter({ routeTree: root.addChildren([page, cams]), history: createMemoryHistory({ initialEntries: ["/cameras/c1/frigate"] }) });
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return fetchMock;
}

describe("FrigateCameraConfig", () => {
  it("edits a section, shows the diff and patches only the changed keys", async () => {
    const fetchMock = mount({}, {
      "/api/v1/cameras/c1/frigate-config": () => json({ camera_id: "c1", camera_name: "acceso", server_id: "s1", frigate_version: "0.16.1", editable: true, secrets_visible: false, config: { detect: { enabled: true, fps: 5 }, onvif: { host: "h", password: "****" } } }),
    });
    const nav = await screen.findByRole("navigation", { name: "Secciones de configuración" });
    expect(within(nav).getByText("Detección")).toBeInTheDocument();
    expect(within(nav).getAllByText("Requiere reinicio").length).toBeGreaterThan(0);

    fireEvent.change(await screen.findByLabelText("Fps"), { target: { value: "12" } });
    fireEvent.click(screen.getByRole("button", { name: "Revisar cambios" }));
    const dialog = await screen.findByRole("dialog", { name: "Revisar cambios" });
    expect(within(dialog).getByText("12")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Aplicar" }));

    await waitFor(() => {
      const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH");
      expect(patch).toBeDefined();
    });
    const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH")!;
    expect(await patch.clone().json()).toEqual({ sections: { detect: { fps: 12 } } });
  });

  it("blocks review on invalid values and locks credentials without the secrets permission", async () => {
    mount({});
    fireEvent.change(await screen.findByLabelText("Fps"), { target: { value: "99" } });
    expect(await screen.findByText("Debe ser como máximo 30.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revisar cambios" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: /ONVIF/ }));
    expect(await screen.findByLabelText("Host")).toHaveValue("10.0.0.2");
    expect(screen.getAllByText("Protegido: requiere permiso de credenciales").length).toBe(1);
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  });

  it("is read-only on Frigate older than 0.16", async () => {
    mount({ frigate_version: "0.14.1", editable: false });
    expect(await screen.findByText(/requiere 0\.16 o superior/)).toBeInTheDocument();
    expect(await screen.findByLabelText("Fps")).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Revisar cambios" })).not.toBeInTheDocument();
  });

  it("picks tracked objects from an emoji grid", async () => {
    const fetchMock = mount({});
    fireEvent.click(await screen.findByRole("button", { name: /^Objetos/ }));
    const person = await screen.findByRole("checkbox", { name: "Persona" });
    expect(person).toBeChecked();
    fireEvent.click(screen.getByRole("checkbox", { name: "Perro" }));
    fireEvent.click(screen.getByRole("button", { name: "Revisar cambios" }));
    fireEvent.click(await screen.findByRole("button", { name: "Aplicar" }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([r]) => (r as Request).method === "PATCH")).toBe(true));
    const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH")!;
    expect(await patch.clone().json()).toEqual({ sections: { objects: { track: ["person", "dog"] } } });
  });

  it("lists revisions and restores one after confirmation", async () => {
    const rev = { id: "r1", server_id: "s1", camera_id: "c1", actor_name: "ana", kind: "camera_patch", sections: ["detect"], patch: { detect: { fps: 8 } }, before_yaml: "a: 1\nb: 2", after_yaml: "a: 1\nb: 3", created_at: "2026-09-30T10:00:00Z" };
    const fetchMock = mount({ secrets_visible: true }, {
      "/api/v1/servers/s1/frigate-config/revisions": () => json({ items: [rev] }),
      "/api/v1/servers/s1/frigate-config/revisions/r1/rollback": () => json({ revision_id: "r2", restart_required: true }),
    });
    fireEvent.click(await screen.findByRole("button", { name: "Historial" }));
    expect(await screen.findByText("Cambio de cámara")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Ver cambio/ }));
    expect(await screen.findByLabelText("Diferencias del YAML")).toHaveTextContent("+ b: 3");
    fireEvent.click(screen.getByRole("button", { name: "Cerrar" }));
    fireEvent.click(screen.getByRole("button", { name: /Restaurar versión anterior/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Restaurar" }));
    expect(await screen.findByText(/Hay cambios que requieren reiniciar el servidor/)).toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([r]) => (r as Request).method === "POST" && (r as Request).url.endsWith("/revisions/r1/rollback"))).toBe(true);
  });

  it("opens the zone editor, drafts its result and patches removed zones as null", async () => {
    const fetchMock = mount({ config: { zones: { puerta: { coordinates: "0,0,1,0,1,1" } }, detect: { enabled: true, fps: 5 } } });
    fireEvent.click(await screen.findByRole("button", { name: /Zonas/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Editar zonas" }));
    expect(await screen.findByText("puerta")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "guardar zonas" }));
    fireEvent.click(await screen.findByRole("button", { name: "Revisar cambios" }));
    const dialog = await screen.findByRole("dialog", { name: "Revisar cambios" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Aplicar" }));
    await waitFor(() => expect(fetchMock.mock.calls.some(([r]) => (r as Request).method === "PATCH")).toBe(true));
    const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH")!;
    expect(await patch.clone().json()).toEqual({ sections: { zones: { puerta: null, patio: { coordinates: "0.1,0.1,0.9,0.1,0.5,0.9" } } } });
  });

  it("saves the OpenVMS fields from the General tab", async () => {
    const fetchMock = mount({}, {
      "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [{ permission: "cameras.manage", effect: "allow" }] }),
      "/api/v1/cameras/c1": () => json({ id: "c1", display_name: "acceso", enabled: true, default_live_quality: "sub", description: "", location: "", tags: [] }),
    });
    fireEvent.click(await screen.findByRole("button", { name: "General" }));
    fireEvent.change(await screen.findByLabelText("Ubicación"), { target: { value: "Boca norte" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => {
      const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH" && new URL(r.url).pathname === "/api/v1/cameras/c1");
      expect(patch).toBeDefined();
    });
    const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH" && new URL(r.url).pathname === "/api/v1/cameras/c1")!;
    expect(await patch.clone().json()).toMatchObject({ location: "Boca norte", display_name: "acceso" });
  });
});
