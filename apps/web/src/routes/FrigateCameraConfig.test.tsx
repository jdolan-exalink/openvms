import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { FrigateCameraConfig } from "./FrigateCameraConfig";

vi.mock("@/components/zones/ZoneEditorModal", () => ({
  ZoneEditorModal: ({ value, onSave, onRetrySync, saveError }: { value: { zones: Record<string, unknown>; managedZones?: Record<string, unknown> }; onSave: (v: unknown) => void; onRetrySync?: (id: string) => void; saveError?: string | null }) => (
    <div role="dialog" aria-label="Editor de zonas">
      <span>{Object.keys(value.zones).join(",")}</span>
      {value.managedZones && <span data-testid="managed">{Object.keys(value.managedZones).join(",")}</span>}
      {saveError && <span role="alert">{saveError}</span>}
      <button type="button" onClick={() => onSave({ zones: { patio: { coordinates: "0.1,0.1,0.9,0.1,0.5,0.9" } }, motionMask: [], objectMask: undefined, objectFilterMasks: {} })}>guardar zonas</button>
      <button
        type="button"
        onClick={() =>
          onSave({
            zones: { patio: { coordinates: "0,0,1,0,1,1" }, entrada: { inertia: 3, coordinates: "0.2,0.2,0.9,0.1,0.5,0.9" }, nueva: { coordinates: "0.2,0.2,0.8,0.2,0.5,0.7" } },
            managedZones: { entrada: { id: "z1" }, nueva: {} },
            motionMask: [],
            objectMask: undefined,
            objectFilterMasks: {},
          })
        }
      >
        guardar gestionadas
      </button>
      {onRetrySync && <button type="button" onClick={() => onRetrySync("z1")}>reintentar z1</button>}
    </div>
  ),
}));

afterEach(() => vi.unstubAllGlobals());

const schema = {
  $ref: "#/$defs/CameraConfig",
  $defs: {
    CameraConfig: {
      type: "object",
      properties: {
        detect: { $ref: "#/$defs/Detect" },
        zones: { type: "object", additionalProperties: { type: "object" } },
        onvif: { $ref: "#/$defs/Onvif" },
        objects: { $ref: "#/$defs/Objects" },
        autotracking: { $ref: "#/$defs/Autotracking" },
        face_recognition: { $ref: "#/$defs/FaceRecognition" },
        genai: { $ref: "#/$defs/GenAI" },
      },
    },
    Detect: { type: "object", properties: { enabled: { type: "boolean", default: true }, fps: { type: "integer", minimum: 1, maximum: 30, description: "Frames por segundo" } } },
    Objects: { type: "object", properties: { track: { type: "array", items: { type: "string" } } } },
    Onvif: { type: "object", properties: { host: { type: "string" }, password: { type: "string" } } },
    Autotracking: { type: "object", properties: { enabled: { type: "boolean", title: "Enabled" } } },
    FaceRecognition: { type: "object", properties: { min_score: { type: "number", title: "Minimum score", minimum: 0, maximum: 1 } } },
    GenAI: { type: "object", properties: { prompt: { type: "string", title: "Prompt" } } },
  },
};

function mount(doc: object, extra: Record<string, () => Response> = {}) {
  const routes = stubApi({
    "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [{ permission: "servers.restart", effect: "allow" }] }),
    "/api/v1/cameras/c1/frigate-config": () => json({ camera_id: "c1", camera_name: "acceso", server_id: "s1", frigate_version: "0.17.0", editable: true, secrets_visible: false, config: { detect: { enabled: true, fps: 5 }, onvif: { host: "10.0.0.2", password: "********" }, objects: { track: ["person"] } }, sections: [], ...doc }),
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

const goTo = async (name: string) => fireEvent.click(await screen.findByRole("tab", { name: new RegExp(`^${name}`) }));
const openMoreActions = async (item: string) => {
  fireEvent.click(await screen.findByRole("button", { name: "Más acciones" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: item }));
};
const manage = () => json({ id: "u", tenant_id: "t", grants: [{ permission: "cameras.manage", effect: "allow" }] });
const cam = (over: Record<string, unknown> = {}) => json({ id: "c1", display_name: "acceso", enabled: true, default_live_quality: "sub", description: "", location: "", tags: [], ...over });
const toggle = { type: "object", properties: { enabled: { type: "boolean", default: false } } };
const fullSchema = {
  ...schema,
  $defs: {
    ...schema.$defs,
    CameraConfig: {
      type: "object",
      properties: {
        ...schema.$defs.CameraConfig.properties,
        enabled: { type: "boolean", default: true },
        ffmpeg: { $ref: "#/$defs/Ffmpeg" },
        audio: { $ref: "#/$defs/Toggle" },
        lpr: { $ref: "#/$defs/Toggle" },
        audio_transcription: { $ref: "#/$defs/Toggle" },
        timestamp_style: { $ref: "#/$defs/Timestamp" },
        best_image_timeout: { type: "integer", default: 60, minimum: 0 },
      },
    },
    Toggle: toggle,
    FaceRecognition: { type: "object", properties: { enabled: { type: "boolean", default: false }, min_score: { type: "number", title: "Minimum score", minimum: 0, maximum: 1 } } },
    Timestamp: { type: "object", properties: { position: { type: "string", enum: ["tl", "tr", "bl", "br"] }, format: { type: "string" } } },
    Ffmpeg: { type: "object", properties: { inputs: { type: "array", items: { type: "object", properties: { path: { type: "string" }, roles: { type: "array", items: { type: "string" } } } } } } },
  },
};
const mountFull = (doc: object = {}, extra: Record<string, () => Response> = {}) =>
  mount(doc, { "/api/v1/me": manage, "/api/v1/cameras/c1": () => cam(), "/api/v1/servers/s1/frigate-config/schema": () => json(fullSchema), ...extra });
const requests = (fetchMock: ReturnType<typeof mount>) => fetchMock.mock.calls.map(([r]) => r as Request);
const cameraPatch = (fetchMock: ReturnType<typeof mount>) => requests(fetchMock).find((r) => r.method === "PATCH" && new URL(r.url).pathname === "/api/v1/cameras/c1");
const frigatePatch = (fetchMock: ReturnType<typeof mount>) => requests(fetchMock).find((r) => r.method === "PATCH" && r.url.endsWith("/frigate-config"));
async function applyReviewDialog() {
  const dialog = await screen.findByRole("dialog", { name: "Revisar cambios" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Aplicar" }));
}
async function applyReview() {
  fireEvent.click(await screen.findByRole("button", { name: "Revisar cambios" }));
  const dialog = await screen.findByRole("dialog", { name: "Revisar cambios" });
  fireEvent.click(within(dialog).getByRole("button", { name: "Aplicar" }));
}

describe("FrigateCameraConfig", () => {
  it("edits a section, shows the diff and patches only the changed keys", async () => {
    const fetchMock = mount({}, {
      "/api/v1/cameras/c1/frigate-config": () => json({ camera_id: "c1", camera_name: "acceso", server_id: "s1", frigate_version: "0.17.1", editable: true, secrets_visible: false, config: { detect: { enabled: true, fps: 5 }, onvif: { host: "h", password: "****" } }, sections: [] }),
    });
    const nav = await screen.findByRole("navigation", { name: "Secciones de configuración" });
    expect(within(nav).getByRole("tab", { name: "Detección" })).toBeInTheDocument();

    await goTo("Avanzado");
    expect(within(await screen.findByRole("navigation", { name: "Subsecciones de Avanzado" })).getAllByText("Requiere reinicio").length).toBeGreaterThan(0);
    await goTo("Detección");
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

  it("applies the reviewed camera sections to the selected cameras on the same server", async () => {
    const fetchMock = mount({}, {
      "/api/v1/cameras": () => json({ items: [
        { id: "c1", display_name: "acceso", server_id: "s1" },
        { id: "c2", display_name: "patio", server_id: "s1" },
      ] }),
      "/api/v1/cameras/c2/frigate-config": () => json({ camera_id: "c2", camera_name: "patio", server_id: "s1", frigate_version: "0.17.0", editable: true, config: { detect: { enabled: true, fps: 3 } }, sections: [] }),
    });
    await goTo("Detección");
    fireEvent.change(await screen.findByLabelText("Fps"), { target: { value: "12" } });
    await openMoreActions("Aplicar a varias cámaras");
    const dialog = await screen.findByRole("dialog", { name: "Aplicar a varias cámaras" });
    await waitFor(() => expect(fetchMock.mock.calls.some(([r]) => new URL((r as Request).url).searchParams.get("server_id") === "s1")).toBe(true));
    expect(within(dialog).getByRole("checkbox", { name: "acceso (actual)" })).toBeChecked();
    expect(within(dialog).getByRole("checkbox", { name: "acceso (actual)" })).toBeDisabled();
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "patio" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Aplicar cambios" }));

    await waitFor(() => {
      const patches = fetchMock.mock.calls.map(([r]) => r as Request).filter((r) => r.method === "PATCH");
      expect(patches.map((r) => new URL(r.url).pathname)).toEqual(expect.arrayContaining([
        "/api/v1/cameras/c1/frigate-config",
        "/api/v1/cameras/c2/frigate-config",
      ]));
    });
    const patches = fetchMock.mock.calls.map(([r]) => r as Request).filter((r) => r.method === "PATCH");
    expect(await Promise.all(patches.map((r) => r.clone().json()))).toEqual([
      { sections: { detect: { fps: 12 } } },
      { sections: { detect: { fps: 12 } } },
    ]);
  });

  it("reports partial bulk failures without hiding successful camera updates", async () => {
    const fetchMock = mount({}, {
      "/api/v1/cameras": () => json({ items: [
        { id: "c1", display_name: "acceso", server_id: "s1" },
        { id: "c2", display_name: "patio", server_id: "s1" },
      ] }),
      "PATCH /api/v1/cameras/c2/frigate-config": () => json({ code: "forbidden", message: "permission denied" }, 403),
    });
    await goTo("Detección");
    fireEvent.change(await screen.findByLabelText("Fps"), { target: { value: "12" } });
    await openMoreActions("Aplicar a varias cámaras");
    const dialog = await screen.findByRole("dialog", { name: "Aplicar a varias cámaras" });
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "patio" }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Aplicar cambios" }));

    expect(await within(dialog).findByText("acceso: aplicada")).toBeInTheDocument();
    expect(await within(dialog).findByText(/patio: no aplicada/)).toBeInTheDocument();
    expect(screen.getByLabelText("Fps")).toHaveValue(12);
    expect(fetchMock.mock.calls.map(([r]) => r as Request).filter((r) => r.method === "PATCH")).toHaveLength(2);
  });

  it("blocks review on invalid values and locks credentials without the secrets permission", async () => {
    mount({});
    await goTo("Detección");
    fireEvent.change(await screen.findByLabelText("Fps"), { target: { value: "99" } });
    expect(await screen.findByText("Debe ser como máximo 30.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Revisar cambios" })).toBeDisabled();
    await goTo("Avanzado");
    fireEvent.click(await screen.findByRole("button", { name: /ONVIF \/ PTZ/ }));
    expect(await screen.findByLabelText("Host")).toHaveValue("10.0.0.2");
    expect(screen.getAllByText("Protegido: requiere permiso de credenciales").length).toBe(1);
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  });

  it("is read-only without the config permission", async () => {
    mount({ editable: false });
    expect(await screen.findByText(/Necesitas el permiso servers\.config/)).toBeInTheDocument();
    await goTo("Detección");
    expect(await screen.findByLabelText("Fps")).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Revisar cambios" })).not.toBeInTheDocument();
  });

  it("picks tracked objects from an emoji grid", async () => {
    const fetchMock = mount({});
    await goTo("Detección");
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

  it.each([
    {
      section: "autotracking",
      navLabel: "Autotracking",
      panelTitle: "Seguimiento automático",
      initial: { enabled: false },
      edit: async (panel: HTMLElement) => {
        const field = within(panel).getByRole("switch", { name: "Enabled" });
        fireEvent.click(field);
        return { enabled: true };
      },
      expected: { autotracking: { enabled: true } },
    },
    {
      section: "genai",
      navLabel: "IA generativa",
      panelTitle: "IA generativa",
      initial: { prompt: "Describe the scene" },
      edit: async (panel: HTMLElement) => {
        fireEvent.change(within(panel).getByLabelText("Prompt"), { target: { value: "Summarize the scene" } });
        return { prompt: "Summarize the scene" };
      },
      expected: { genai: { prompt: "Summarize the scene" } },
    },
  ])("shows schema-backed $section settings and saves through the camera patch", async ({ section, navLabel, panelTitle, initial, edit, expected }) => {
    const fetchMock = mount({ config: { [section]: initial } });
    await goTo("Avanzado");
    fireEvent.click(await screen.findByRole("button", { name: new RegExp(navLabel, "i") }));
    const heading = await screen.findByRole("heading", { name: panelTitle });
    expect(heading).toBeInTheDocument();
    expect(await screen.findByText("Opciones avanzadas")).toBeInTheDocument();
    await edit(heading.parentElement!);
    fireEvent.click(screen.getByRole("button", { name: "Revisar cambios" }));
    const dialog = await screen.findByRole("dialog", { name: "Revisar cambios" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Aplicar" }));

    await waitFor(() => {
      const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH");
      expect(patch).toBeDefined();
    });
    const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH")!;
    expect(await patch.clone().json()).toEqual({ sections: expected });
  });

  it("lists revisions and restores one after confirmation", async () => {
    const rev = { id: "r1", server_id: "s1", camera_id: "c1", actor_name: "ana", kind: "camera_patch", sections: ["detect"], patch: { detect: { fps: 8 } }, before_yaml: "a: 1\nb: 2", after_yaml: "a: 1\nb: 3", created_at: "2026-09-30T10:00:00Z" };
    const fetchMock = mount({ secrets_visible: true }, {
      "/api/v1/servers/s1/frigate-config/revisions": () => json({ items: [rev] }),
      "/api/v1/servers/s1/frigate-config/revisions/r1/rollback": () => json({ revision_id: "r2", restart_required: true }),
    });
    await goTo("Avanzado");
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
    await goTo("Zonas");
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
    fireEvent.change(await screen.findByLabelText("Ubicación"), { target: { value: "Boca norte" } });
    fireEvent.click(within(screen.getByRole("region", { name: "General" })).getByRole("button", { name: "Guardar" }));
    await waitFor(() => {
      const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH" && new URL(r.url).pathname === "/api/v1/cameras/c1");
      expect(patch).toBeDefined();
    });
    const patch = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "PATCH" && new URL(r.url).pathname === "/api/v1/cameras/c1")!;
    expect(await patch.clone().json()).toMatchObject({ location: "Boca norte", display_name: "acceso" });
  });

  it("displays ONVIF capabilities and applies detected host to Frigate onvif config", async () => {
    mount({}, {
      "/api/v1/cameras/c1": () =>
        json({
          id: "c1",
          display_name: "acceso",
          remote_name: "acceso",
          server_id: "s1",
          site_id: "site1",
          onvif_endpoint: "http://10.1.1.220/onvif/device_service",
          tags: [],
          enabled: true,
          onvif_profile: "ONVIF-T",
          onvif_version: "2.0",
          onvif_capabilities: {
            manufacturer: "Hikvision",
            model: "DS-2CD2043G2-I",
            firmware_version: "V5.5.800",
            serial_number: "DS-2CD2043G2-I20210101AAWR",
            profiles: ["Profile T", "Profile M"],
            has_analytics: true,
            has_events: true,
            has_ptz: false,
            has_media2: true,
            has_media1: true,
          },
        }),
    });

    fireEvent.click(await screen.findByRole("button", { name: "Capacidades ONVIF" }));
    const dialog = await screen.findByRole("dialog", { name: "Capacidades ONVIF" });
    expect(within(dialog).getByText("Soporte y Capacidades ONVIF")).toBeInTheDocument();
    expect(within(dialog).getByText("Hikvision DS-2CD2043G2-I")).toBeInTheDocument();
    expect(within(dialog).getByText("Soportado (Profile M)")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Comprobar y guardar ONVIF" })).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Sincronizar fecha y hora" })).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole("button", { name: "Cargar en Frigate" }));
    expect(await screen.findByLabelText("Host")).toHaveValue("10.1.1.220");
    expect(screen.queryByRole("dialog", { name: "Capacidades ONVIF" })).not.toBeInTheDocument();
    expect(screen.getByText(/1 cambio sin guardar/)).toBeInTheDocument();
  });

  it("opens on General and keeps ONVIF facts out of the sidebar", async () => {
    mountFull({}, { "/api/v1/cameras/c1": () => cam({ onvif_profile: "ONVIF-T", onvif_capabilities: { manufacturer: "Hikvision", has_analytics: true } }) });
    const general = await screen.findByRole("tab", { name: "General" });
    expect(general).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByLabelText("Fps")).not.toBeInTheDocument();
    const nav = screen.getByRole("navigation", { name: "Secciones de configuración" });
    expect(within(nav).queryByRole("tab", { name: /^Capacidades/ })).not.toBeInTheDocument();
    expect(screen.queryByText("Soporte y Capacidades ONVIF")).not.toBeInTheDocument();
    expect(screen.queryByText("Analítica ONVIF")).not.toBeInTheDocument();
  });

  describe("integration of the LPR test and the camera web view", () => {
    it("lists the camera web view in the sidebar and shows the viewer when selected", async () => {
      mountFull({}, { "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [{ permission: "cameras.manage", effect: "allow" }, { permission: "servers.manage", effect: "allow" }, { permission: "servers.config.secrets", effect: "allow" }] }) });
      await goTo("Integraciones");
      fireEvent.click(await screen.findByRole("button", { name: /^Web de la cámara/ }));
      expect(await screen.findByRole("region", { name: "Web de la cámara" })).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Abrir" })).toBeInTheDocument();
    });

    it("hides the camera web view entry when the user cannot manage the camera", async () => {
      mountFull({}, { "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [{ permission: "cameras.view", effect: "allow" }] }) });
      await goTo("Integraciones");
      expect(await screen.findByRole("region", { name: "Capacidades ONVIF" })).toBeInTheDocument();
      expect(screen.queryByRole("region", { name: "Acceso web a la cámara" })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /^Web de la cámara/ })).not.toBeInTheDocument();
    });

    it("shows the LPR connection test in General for a camera read by Frigate's own engine", async () => {
      mountFull({ config: { lpr: { enabled: true } } }, { "/api/v1/cameras/c1": () => cam({ tags: ["lpr"] }) });
      expect(await screen.findByRole("button", { name: /Probar conexión/ })).toBeInTheDocument();
    });

    it("does not show the LPR connection test in General for a generic camera", async () => {
      mountFull({}, { "/api/v1/cameras/c1": () => cam({ tags: [] }) });
      await screen.findByLabelText("Tipo de cámara");
      expect(screen.queryByRole("button", { name: /Probar conexión/ })).not.toBeInTheDocument();
    });
  });

  describe("General fields", () => {
    it("the enabled switch saves the camera in OpenVMS and stages the Frigate section", async () => {
      const fetchMock = mountFull({ config: { enabled: true, detect: { enabled: true, fps: 5 } } });
      const sw = await screen.findByRole("switch", { name: "Cámara habilitada" });
      expect(sw).toHaveAttribute("aria-checked", "true");
      expect(screen.getByText(/también detiene el procesamiento en Frigate/i)).toBeInTheDocument();
      fireEvent.click(sw);
      await waitFor(() => expect(cameraPatch(fetchMock)).toBeDefined());
      expect(await cameraPatch(fetchMock)!.clone().json()).toEqual({ enabled: false });
      expect(await screen.findByText(/1 cambio sin guardar/)).toBeInTheDocument();
      await applyReview();
      await waitFor(() => expect(frigatePatch(fetchMock)).toBeDefined());
      expect(await frigatePatch(fetchMock)!.clone().json()).toEqual({ sections: { enabled: false } });
    });

    it.each([
      { tags: [], frigate: undefined, expected: "generic" },
      { tags: ["lpr"], frigate: false, expected: "lpr_frigate" },
      { tags: [], frigate: true, expected: "lpr_frigate" },
      { tags: ["lpr", "provision"], frigate: false, expected: "lpr_provision" },
      { tags: ["lpr", "provision_isr"], frigate: false, expected: "lpr_provision" },
    ])("derives the camera type $expected from tags $tags", async ({ tags, frigate, expected }) => {
      mountFull({ config: { lpr: frigate === undefined ? undefined : { enabled: frigate } } }, { "/api/v1/cameras/c1": () => cam({ tags }) });
      expect(await screen.findByLabelText("Tipo de cámara")).toHaveValue(expected);
      expect(screen.getByText(/Frigate lee las matrículas/)).toBeInTheDocument();
    });

    it("lpr_frigate edits the tags and stages lpr.enabled=true", async () => {
      const fetchMock = mountFull({ config: { lpr: { enabled: false } } }, { "/api/v1/cameras/c1": () => cam({ tags: ["ptz", "provision"] }) });
      fireEvent.change(await screen.findByLabelText("Tipo de cámara"), { target: { value: "lpr_frigate" } });
      await waitFor(() => expect(cameraPatch(fetchMock)).toBeDefined());
      expect(await cameraPatch(fetchMock)!.clone().json()).toEqual({ tags: ["ptz", "lpr"] });
      await applyReview();
      await waitFor(() => expect(frigatePatch(fetchMock)).toBeDefined());
      expect(await frigatePatch(fetchMock)!.clone().json()).toEqual({ sections: { lpr: { enabled: true } } });
    });

    it("switching from lpr_frigate to generic removes lpr and stages lpr.enabled=false", async () => {
      const fetchMock = mountFull({ config: { lpr: { enabled: true } } }, { "/api/v1/cameras/c1": () => cam({ tags: ["lpr", "norte"] }) });
      fireEvent.change(await screen.findByLabelText("Tipo de cámara"), { target: { value: "generic" } });
      await waitFor(() => expect(cameraPatch(fetchMock)).toBeDefined());
      expect(await cameraPatch(fetchMock)!.clone().json()).toEqual({ tags: ["norte"] });
      await applyReview();
      await waitFor(() => expect(frigatePatch(fetchMock)).toBeDefined());
      expect(await frigatePatch(fetchMock)!.clone().json()).toEqual({ sections: { lpr: { enabled: false } } });
    });

    it("lpr_provision adds the provision tags without touching Frigate", async () => {
      const fetchMock = mountFull({ config: { lpr: { enabled: false } } });
      fireEvent.change(await screen.findByLabelText("Tipo de cámara"), { target: { value: "lpr_provision" } });
      await waitFor(() => expect(cameraPatch(fetchMock)).toBeDefined());
      expect(await cameraPatch(fetchMock)!.clone().json()).toEqual({ tags: ["provision", "lpr"] });
      expect(screen.queryByRole("button", { name: "Revisar cambios" })).not.toBeInTheDocument();
    });

    describe("analytics modules", () => {
      const modules = (lprAvailable: boolean) => [
        { id: "lpr", label: "Matrículas (LPR)", available: lprAvailable, enabled: false, writable: lprAvailable, engine: lprAvailable ? "frigate" : "none", reason: lprAvailable ? undefined : "No disponible con este motor", settings: {}, advanced: [] },
        { id: "face_detection", label: "Detección facial", available: false, enabled: false, writable: false, engine: "none", reason: "No disponible con este motor", settings: {}, advanced: [] },
        { id: "face_recognition", label: "Reconocimiento facial", available: false, enabled: false, writable: false, engine: "none", reason: "No disponible con este motor", settings: {}, advanced: [] },
      ];
      const model = (lprAvailable: boolean) => () => json({ analytics: { modules: modules(lprAvailable) }, state: { desired_revision: 1, applied_revision: 1, sync_state: "synced", last_error: "", drift: [] } });

      it("hides Analíticas when no module is available", async () => {
        mountFull({}, { "/api/v1/cameras/c1/normalized-config": model(false) });
        await screen.findByLabelText("Tipo de cámara");
        await waitFor(() => expect(screen.queryByRole("tab", { name: /^Analíticas/ })).not.toBeInTheDocument());
      });

      it("shows Analíticas with the module cards when a module is available, and stages the LPR switch", async () => {
        mountFull({ config: { lpr: { enabled: false } } }, { "/api/v1/cameras/c1/normalized-config": model(true) });
        await goTo("Analíticas");
        const card = await screen.findByTestId("module-lpr");
        fireEvent.click(within(card).getByRole("switch"));
        expect(await screen.findByRole("button", { name: "Revisar cambios" })).toBeInTheDocument();
        expect(within(screen.getByTestId("module-face_recognition")).getByRole("switch")).toBeDisabled();
        // The raw engine panel stays reachable behind the modules.
        expect(screen.getByRole("button", { name: /Avanzado · LPR/ })).toBeInTheDocument();
      });
    });

    it("shows the Provision LPR section for a provision-typed camera", async () => {
      mountFull({}, { "/api/v1/cameras/c1": () => cam({ tags: ["lpr", "provision"] }) });
      await goTo("Analíticas");
      expect(await screen.findByRole("button", { name: /Provision LPR/ })).toBeInTheDocument();
    });

    it("keeps the audio transcription switch disabled while audio is off", async () => {
      mountFull({ config: { audio: { enabled: false }, audio_transcription: { enabled: false } } });
      const transcription = await screen.findByRole("switch", { name: "Transcripción de audio" });
      expect(transcription).toBeDisabled();
      expect(screen.getByText(/Requiere activar el audio/)).toBeInTheDocument();
      fireEvent.click(screen.getByRole("switch", { name: "Audio" }));
      expect(screen.getByRole("switch", { name: "Transcripción de audio" })).toBeEnabled();
    });

    it("enables audio transcription when audio is on and patches both sections", async () => {
      const fetchMock = mountFull({ config: { audio: { enabled: true }, audio_transcription: { enabled: false } } });
      fireEvent.click(await screen.findByRole("switch", { name: "Transcripción de audio" }));
      await applyReview();
      await waitFor(() => expect(frigatePatch(fetchMock)).toBeDefined());
      expect(await frigatePatch(fetchMock)!.clone().json()).toEqual({ sections: { audio_transcription: { enabled: true } } });
    });

    it("stages LPR, face recognition and timestamp settings as Frigate sections", async () => {
      const fetchMock = mountFull({ config: { lpr: { enabled: false }, face_recognition: { enabled: false, min_score: 0.6 }, timestamp_style: { position: "tl", format: "%H" } } });
      fireEvent.click(await screen.findByRole("switch", { name: "LPR" }));
      fireEvent.click(screen.getByRole("switch", { name: "Reconocimiento facial" }));
      fireEvent.change(screen.getByLabelText("Posición"), { target: { value: "br" } });
      await applyReview();
      await waitFor(() => expect(frigatePatch(fetchMock)).toBeDefined());
      expect(await frigatePatch(fetchMock)!.clone().json()).toEqual({
        sections: { lpr: { enabled: true }, face_recognition: { enabled: true }, timestamp_style: { position: "br" } },
      });
    });

    it("keeps the rest of the face recognition settings reachable from General", async () => {
      mountFull({ config: { face_recognition: { enabled: true, min_score: 0.6 } } });
      fireEvent.click(await screen.findByText("Más ajustes de Reconocimiento facial"));
      expect((await screen.findAllByLabelText("Minimum score")).length).toBeGreaterThan(0);
      expect(screen.queryByRole("tab", { name: /Reconocimiento facial/ })).not.toBeInTheDocument();
    });

    it("edits the best image timeout when the schema has it", async () => {
      const fetchMock = mountFull({ config: { best_image_timeout: 60 } });
      const input = await screen.findByLabelText(/Tiempo de mejor imagen/);
      expect(input).toHaveValue(60);
      fireEvent.change(input, { target: { value: "90" } });
      await applyReview();
      await waitFor(() => expect(frigatePatch(fetchMock)).toBeDefined());
      expect(await frigatePatch(fetchMock)!.clone().json()).toEqual({ sections: { best_image_timeout: 90 } });
    });

    it("shows the input streams editor in General with masked URLs and no Streams FFmpeg entry", async () => {
      mountFull({ secrets_visible: false, config: { ffmpeg: { inputs: [{ path: "rtsp://u:p@10.0.0.9/1", roles: ["detect"] }] } } });
      expect(await screen.findByText("Streams de entrada")).toBeInTheDocument();
      expect(screen.queryByLabelText("Ruta del stream 1")).not.toBeInTheDocument();
      expect(screen.getAllByText(/requieren permiso de credenciales/).length).toBeGreaterThan(0);
      expect(screen.queryByText(/rtsp:\/\/u:p/)).not.toBeInTheDocument();
      expect(screen.queryByRole("tab", { name: "Streams FFmpeg" })).not.toBeInTheDocument();
    });

    it("edits the input streams from General when credentials are visible", async () => {
      const fetchMock = mountFull({ secrets_visible: true, config: { ffmpeg: { inputs: [{ path: "rtsp://10.0.0.9/1", roles: ["detect"] }] } } });
      fireEvent.change(await screen.findByLabelText("Ruta del stream 1"), { target: { value: "rtsp://10.0.0.9/2" } });
      await applyReview();
      await waitFor(() => expect(frigatePatch(fetchMock)).toBeDefined());
      expect(await frigatePatch(fetchMock)!.clone().json()).toEqual({ sections: { ffmpeg: { inputs: [{ path: "rtsp://10.0.0.9/2", roles: ["detect"] }] } } });
    });

    it("renders the Video tab as streams and keeps the Frigate video sections under Avanzado de video", async () => {
      mountFull({ config: { ffmpeg: { inputs: [{ path: "rtsp://10.0.0.9/1", roles: ["detect"] }] } } });
      await goTo("Video");
      expect(await screen.findByRole("region", { name: "Stream principal" })).toBeInTheDocument();
      expect(screen.getByRole("region", { name: "Stream de detección" })).toBeInTheDocument();
      const details = screen.getByText("Avanzado de video (Frigate)").closest("details")!;
      expect(details.open).toBe(false);
      expect(within(details).getByText("FFmpeg y streams")).toBeInTheDocument();
    });

    it("locks the Frigate fields without the config permission", async () => {
      mountFull({ editable: false, config: { audio: { enabled: true } } });
      expect(await screen.findByRole("switch", { name: "Audio" })).toBeDisabled();
    });

    it("hides the controls of sections an older Frigate schema does not have", async () => {
      mountFull({}, { "/api/v1/servers/s1/frigate-config/schema": () => json(schema) });
      expect(await screen.findByRole("switch", { name: "Cámara habilitada" })).toBeInTheDocument();
      for (const name of ["Audio", "LPR", "Transcripción de audio"]) expect(screen.queryByRole("switch", { name })).not.toBeInTheDocument();
      expect(screen.queryByLabelText("Posición")).not.toBeInTheDocument();
      expect(screen.queryByText("Streams de entrada")).not.toBeInTheDocument();
      expect(screen.queryByText(/detiene el procesamiento en Frigate/)).not.toBeInTheDocument();
      expect(screen.getByLabelText(/Tiempo de mejor imagen/)).toBeDisabled();
      expect(screen.getByText(/no la expone/)).toBeInTheDocument();
    });
  });

  describe("persisted OpenVMS zones", () => {
    const zone = (id: string, name: string, status: string) => ({ id, camera_id: "c1", name, points: [{ x: 0.1, y: 0.1 }, { x: 0.9, y: 0.1 }, { x: 0.5, y: 0.9 }], settings: { inertia: 3 }, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z", sync_status: status });
    const frigateConfig = { zones: { patio: { coordinates: "0,0,1,0,1,1" }, ovms_entrada: { inertia: 3, coordinates: "0.1,0.1,0.9,0.1,0.5,0.9" }, ovms_borrar: { inertia: 3, coordinates: "0.1,0.1,0.9,0.1,0.5,0.9" } }, detect: { enabled: true, fps: 5 } };
    const manage = () => json({ id: "u", tenant_id: "t", grants: [{ permission: "cameras.manage", effect: "allow" }] });
    const zonesList = () => json({ zones: [zone("z1", "entrada", "failed"), zone("z2", "borrar", "synced")] });
    const methods = (fetchMock: ReturnType<typeof mount>) => fetchMock.mock.calls.map(([r]) => `${(r as Request).method} ${new URL((r as Request).url).pathname}`);

    it("edits persisted zones through the camera-zone API and leaves their Frigate keys untouched", async () => {
      const fetchMock = mount({ config: frigateConfig }, {
        "/api/v1/me": manage,
        "/api/v1/cameras/c1/zones": zonesList,
        "POST /api/v1/cameras/c1/zones": () => json(zone("z3", "nueva", "pending"), 201),
        "PUT /api/v1/cameras/c1/zones/z1": () => json(zone("z1", "entrada", "pending")),
        "DELETE /api/v1/cameras/c1/zones/z2": () => new Response(null, { status: 204 }),
      });
      await goTo("Zonas");
      fireEvent.click(await screen.findByRole("button", { name: "Editar zonas" }));
      expect(await screen.findByText("patio,entrada,borrar")).toBeInTheDocument();
      expect(screen.getByTestId("managed")).toHaveTextContent("entrada,borrar");
      fireEvent.click(screen.getByRole("button", { name: "guardar gestionadas" }));
      await waitFor(() => expect(screen.queryByRole("dialog", { name: "Editor de zonas" })).not.toBeInTheDocument());
      const calls = methods(fetchMock);
      expect(calls).toEqual(expect.arrayContaining(["DELETE /api/v1/cameras/c1/zones/z2", "PUT /api/v1/cameras/c1/zones/z1", "POST /api/v1/cameras/c1/zones"]));
      const post = fetchMock.mock.calls.map(([r]) => r as Request).find((r) => r.method === "POST" && r.url.endsWith("/zones"))!;
      expect(await post.clone().json()).toEqual({ name: "nueva", points: [{ x: 0.2, y: 0.2 }, { x: 0.8, y: 0.2 }, { x: 0.5, y: 0.7 }], settings: {} });
      // Foreign zones are unchanged and ovms_ keys are preserved, so no Frigate patch is pending.
      expect(screen.queryByRole("button", { name: "Revisar cambios" })).not.toBeInTheDocument();
      expect(calls).not.toContain("PATCH /api/v1/cameras/c1/frigate-config");
    });

    it("keeps the editor open and shows the error when persisting fails", async () => {
      mount({ config: frigateConfig }, {
        "/api/v1/me": manage,
        "/api/v1/cameras/c1/zones": zonesList,
        "DELETE /api/v1/cameras/c1/zones/z2": () => json({ code: "forbidden", message: "sin permiso" }, 403),
      });
      await goTo("Zonas");
      fireEvent.click(await screen.findByRole("button", { name: "Editar zonas" }));
      fireEvent.click(await screen.findByRole("button", { name: "guardar gestionadas" }));
      expect(await screen.findByRole("alert")).toHaveTextContent(/No se pudieron guardar las zonas/);
      expect(screen.getByRole("dialog", { name: "Editor de zonas" })).toBeInTheDocument();
    });

    it("retries a failed zone synchronization only for users who can manage cameras", async () => {
      const fetchMock = mount({ config: frigateConfig }, {
        "/api/v1/me": manage,
        "/api/v1/cameras/c1/zones": zonesList,
        "POST /api/v1/cameras/c1/zones/z1/sync": () => new Response(null, { status: 202 }),
      });
      await goTo("Zonas");
      fireEvent.click(await screen.findByRole("button", { name: "Editar zonas" }));
      fireEvent.click(await screen.findByRole("button", { name: "reintentar z1" }));
      await waitFor(() => expect(methods(fetchMock)).toContain("POST /api/v1/cameras/c1/zones/z1/sync"));
    });

    it("falls back to the plain Frigate zones without the manage permission", async () => {
      mount({ config: frigateConfig }, { "/api/v1/cameras/c1/zones": zonesList });
      await goTo("Zonas");
      fireEvent.click(await screen.findByRole("button", { name: "Editar zonas" }));
      expect(await screen.findByText("patio,ovms_entrada,ovms_borrar")).toBeInTheDocument();
      expect(screen.queryByTestId("managed")).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "reintentar z1" })).not.toBeInTheDocument();
    });
  });

  describe("shell layout", () => {
    it("shows the header, the state chips, the preview and the summary cards", async () => {
      mountFull({ config: { record: { enabled: true }, detect: { enabled: true, width: 640, height: 360, fps: 5 } } }, {
        "/api/v1/servers": () => json({ items: [{ id: "s1", name: "Cayastá" }] }),
        "/api/v1/cameras/c1": () => cam({ status: "online", lpr: true, onvif_capabilities: { manufacturer: "Hikvision", model: "DS-1", has_ptz: true } }),
      });
      expect(await screen.findByRole("heading", { name: "acceso" })).toBeInTheDocument();
      expect(await screen.findByText("Servidor: Cayastá · Motor: Frigate")).toBeInTheDocument();
      const chips = within(screen.getByRole("list", { name: "Estado de la cámara" }));
      await waitFor(() => expect(chips.getByText("LPR activo")).toBeInTheDocument());
      for (const label of ["En línea", "Grabando", "Detección activa", "ONVIF"]) expect(chips.getByText(label)).toBeInTheDocument();
      const toolbar = screen.getByRole("toolbar", { name: "Controles de la vista previa" });
      for (const name of ["Reproducir", "Activar sonido", "Capturar imagen", "Maximizar"]) expect(within(toolbar).getByLabelText(name)).toBeInTheDocument();
      expect(screen.getByRole("region", { name: "Origen" })).toBeInTheDocument();
      expect(await within(screen.getByRole("region", { name: "Dispositivo" })).findByText("Hikvision")).toBeInTheDocument();
      expect(within(screen.getByRole("region", { name: "Video" })).getByText("640×360 · 5 fps")).toBeInTheDocument();
      expect(within(screen.getByRole("region", { name: "Capacidades" })).getByText(/ONVIF.*PTZ.*LPR/)).toBeInTheDocument();
      expect(screen.queryByText("Opciones de Cámara")).not.toBeInTheDocument();
    });

    it("shows a dash for data the camera does not have", async () => {
      mountFull();
      const device = within(await screen.findByRole("region", { name: "Dispositivo" }));
      expect(device.getAllByText("—").length).toBe(3);
      expect(device.getByText("10.0.0.2")).toBeInTheDocument();
    });

    it("lists only the tabs the camera needs", async () => {
      mountFull({}, { "/api/v1/cameras/c1": () => cam({ tags: [], lpr: false }) });
      await screen.findByRole("tab", { name: "General" });
      expect(screen.getAllByRole("tab").map((t) => t.textContent)).toEqual(expect.arrayContaining(["General", "Video", "Grabación", "Detección", "Zonas", "Integraciones", "Avanzado"]));
      expect(screen.queryByRole("tab", { name: "PTZ" })).not.toBeInTheDocument();
      expect(screen.queryByRole("tab", { name: "Fisheye" })).not.toBeInTheDocument();
    });

    it("adds PTZ and Fisheye tabs when the camera has them", async () => {
      mountFull({}, { "/api/v1/cameras/c1": () => cam({ tags: ["fisheye"], onvif_capabilities: { has_ptz: true } }) });
      expect(await screen.findByRole("tab", { name: "PTZ" })).toBeInTheDocument();
      expect(await screen.findByRole("tab", { name: "Fisheye" })).toBeInTheDocument();
    });

    it("keeps Fisheye reachable from Avanzado for other cameras, with the history", async () => {
      mountFull();
      await goTo("Avanzado");
      const subnav = within(await screen.findByRole("navigation", { name: "Subsecciones de Avanzado" }));
      expect(subnav.getByRole("button", { name: /Fisheye/ })).toBeInTheDocument();
      expect(subnav.getByRole("button", { name: /Historial/ })).toBeInTheDocument();
      expect(subnav.getByRole("button", { name: /ONVIF \/ PTZ/ })).toBeInTheDocument();
    });

    it("keeps every former sidebar section reachable from a tab", async () => {
      mountFull();
      const reach = async (tab: string, entry: RegExp) => {
        await goTo(tab);
        expect(await within(await screen.findByRole("navigation", { name: `Subsecciones de ${tab}` })).findByRole("button", { name: entry })).toBeInTheDocument();
      };
      await reach("Grabación", /^Capturas/);
      await reach("Detección", /^Objetos/);
      await reach("Detección", /^Movimiento/);
      await reach("Zonas", /^Revisión y Alertas/);
    });

    it("shows pending changes in a bar that discards them", async () => {
      mount({});
      expect(screen.queryByText(/sin guardar/)).not.toBeInTheDocument();
      await goTo("Detección");
      fireEvent.change(await screen.findByLabelText("Fps"), { target: { value: "12" } });
      expect(await screen.findByText("1 cambio sin guardar")).toBeInTheDocument();
      expect(within(screen.getByRole("tab", { name: /^Detección/ })).getByLabelText("con cambios")).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Descartar" }));
      await waitFor(() => expect(screen.queryByText(/sin guardar/)).not.toBeInTheDocument());
      expect(screen.getByLabelText("Fps")).toHaveValue(5);
    });

    it("saves from the header through the review dialog", async () => {
      const fetchMock = mount({});
      await goTo("Detección");
      fireEvent.change(await screen.findByLabelText("Fps"), { target: { value: "12" } });
      fireEvent.click(screen.getAllByRole("button", { name: "Guardar" })[0]!);
      await applyReviewDialog();
      await waitFor(() => expect(frigatePatch(fetchMock)).toBeDefined());
      expect(await frigatePatch(fetchMock)!.clone().json()).toEqual({ sections: { detect: { fps: 12 } } });
    });

    it("opens the camera web view from the header for users with the permissions", async () => {
      mountFull({}, { "/api/v1/me": () => json({ id: "u", tenant_id: "t", grants: [{ permission: "cameras.manage", effect: "allow" }, { permission: "servers.manage", effect: "allow" }, { permission: "servers.config.secrets", effect: "allow" }] }) });
      fireEvent.click(await screen.findByRole("button", { name: "Abrir interfaz web" }));
      expect(await screen.findByRole("region", { name: "Web de la cámara" })).toBeInTheDocument();
    });

    it("hides Abrir interfaz web without the permissions", async () => {
      mountFull();
      await screen.findByRole("tab", { name: "General" });
      expect(screen.queryByRole("button", { name: "Abrir interfaz web" })).not.toBeInTheDocument();
    });

    it("shows the sync badge and the drift panel once the normalized config loads", async () => {
      mountFull({}, { "/api/v1/cameras/c1/normalized-config": () => json({ state: { desired_revision: 1, applied_revision: 1, sync_state: "drift", last_error: "", drift: [{ path: "detection.enabled", desired: true, effective: false }] } }) });
      expect((await screen.findAllByTestId("sync-badge"))[0]).toHaveTextContent("Diferencia detectada");
      expect(screen.getByText("La cámara fue modificada externamente.")).toBeInTheDocument();
    });

    it("hides the sync badge when the normalized config is unavailable", async () => {
      mountFull();
      await screen.findByRole("tab", { name: "General" });
      expect(screen.queryByTestId("sync-badge")).not.toBeInTheDocument();
    });
  });
  describe("PTZ and Fisheye tabs", () => {
    it("hides both tabs for a fixed camera", async () => {
      mountFull({}, { "/api/v1/cameras/c1": () => cam({ onvif_capabilities: { has_ptz: false } }) });
      await screen.findByRole("tab", { name: "General" });
      expect(screen.queryByRole("tab", { name: /^PTZ/ })).not.toBeInTheDocument();
      expect(screen.queryByRole("tab", { name: /^Fisheye/ })).not.toBeInTheDocument();
    });

    it("shows PTZ for a PTZ camera, with the pad and contextual preview controls only inside it", async () => {
      mountFull({}, { "/api/v1/cameras/c1": () => cam({ onvif_capabilities: { has_ptz: true } }), "/api/v1/cameras/c1/onvif/ptz": () => json({ success: true, presets: [] }), "/api/v1/cameras/c1/ptz/tours": () => json({ tours: [] }) });
      await screen.findByRole("tab", { name: "General" });
      expect(screen.queryByRole("group", { name: "Controles PTZ de la vista previa" })).not.toBeInTheDocument();
      await goTo("PTZ");
      expect(await screen.findByRole("region", { name: "Movimiento" })).toBeInTheDocument();
      expect(screen.getByRole("group", { name: "Controles PTZ de la vista previa" })).toBeInTheDocument();
      await goTo("General");
      expect(screen.queryByRole("group", { name: "Controles PTZ de la vista previa" })).not.toBeInTheDocument();
    });

    it("shows Fisheye only for a fisheye camera", async () => {
      mountFull({}, { "/api/v1/cameras/c1": () => cam({ tags: ["fisheye"] }) });
      await goTo("Fisheye");
      expect(await screen.findByRole("region", { name: "Original y vista" })).toBeInTheDocument();
    });
  });
});
