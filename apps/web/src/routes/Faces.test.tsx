import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage } from "@/test-utils";
import { Faces } from "./Faces";

afterEach(() => vi.unstubAllGlobals());

const sync = (over: Partial<Schemas["FaceSyncSummary"]> = {}): Schemas["FaceSyncSummary"] => ({ total: 2, synced: 2, pending: 0, failed: 0, conflict: 0, disabled: 0, ...over });
const person = (id: string, name: string, over: Partial<Schemas["FacePerson"]> = {}): Schemas["FacePerson"] => ({
  id, tenant_id: "t1", display_name: name, frigate_name: name.toLowerCase(), notes: "", image_count: 2, sync: sync(),
  created_at: "2026-10-01T10:00:00Z", updated_at: "2026-10-01T10:00:00Z", ...over,
});

type Call = { method: string; path: string; search: URLSearchParams; body?: unknown; contentType?: string };

type Backend = {
  permissions: string[];
  persons: Schemas["FacePerson"][];
  images: Schemas["FaceImage"][];
  syncStatus: Schemas["FaceSyncStatus"][];
  train: Schemas["FaceTrainList"];
  recognitions: Schemas["FaceRecognition"][][];
  cameras: { id: string; display_name: string }[];
  /** Forces an error answer: status codes for the image upload and the person PATCH. */
  fail: { upload?: number; patch?: number };
  /** The caller's tenant; null makes the caller a platform administrator. */
  tenantId: string | null;
  sites: { id: string; name: string }[];
  servers: { id: string; name: string; site_id: string }[];
  groups: { id: string; name: string }[];
};

function install(over: Partial<Backend> = {}) {
  const b: Backend = {
    permissions: ["faces.view", "faces.manage"], persons: [], images: [], syncStatus: [], train: { items: [], servers: [] }, recognitions: [[]], cameras: [], fail: {}, tenantId: "t1",
    sites: [{ id: "s1", name: "Sede" }, { id: "s2", name: "Anexo" }], servers: [{ id: "srv1", name: "h01", site_id: "s1" }, { id: "srv2", name: "c01", site_id: "s2" }], groups: [{ id: "g1", name: "Accesos" }],
    ...over,
  };
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (raw: Request | string, init?: RequestInit) => {
      // The typed client passes a Request; multipart uploads go through plain fetch(url, init).
      const input = typeof raw === "string" ? { url: new URL(raw, "http://localhost").href, method: init?.method ?? "GET", headers: new Headers(init?.headers) } : raw;
      const url = new URL(input.url);
      const call: Call = { method: input.method, path: url.pathname, search: url.searchParams, contentType: input.headers.get("content-type") ?? undefined };
      if (typeof raw === "string") {
        call.body = init?.body;
      } else if (input.method !== "GET") {
        const text = await raw.clone().text();
        call.body = call.contentType?.includes("json") && text ? JSON.parse(text) : text;
      }
      calls.push(call);
      const p = url.pathname;
      if (p === "/api/v1/me") {
        return json({ id: "u1", username: "u1", display_name: "u1", mfa_enabled: false, must_change_password: false, auth_method: "session", tenant_id: b.tenantId,
          grants: b.permissions.map((permission) => ({ permission, effect: "allow", scope_type: "platform" })) });
      }
      if (p === "/api/v1/sites") return json({ items: b.sites });
      if (p === "/api/v1/servers") return json({ items: b.servers });
      if (p === "/api/v1/camera-groups") return json({ items: b.groups });
      if (p === "/api/v1/faces/persons" && input.method === "GET") return json({ items: b.persons });
      if (p === "/api/v1/faces/persons" && input.method === "POST") return json(person("new", (call.body as { display_name: string }).display_name), 201);
      if (/\/images$/.test(p) && input.method === "GET") return json({ items: b.images });
      if (/\/images$/.test(p) && input.method === "POST") {
        if (b.fail.upload) return json({ code: "invalid", message: "unsupported picture" }, b.fail.upload);
        return json(b.images[0], 201);
      }
      if (/\/images\/[^/]+$/.test(p) && input.method === "DELETE") return new Response(null, { status: 204 });
      if (/\/sync$/.test(p)) return json({ items: b.syncStatus });
      if (/\/sync\/retry$/.test(p)) return json({ requeued: 1 });
      if (/^\/api\/v1\/faces\/persons\/[^/]+$/.test(p) && input.method === "DELETE") return new Response(null, { status: 204 });
      if (/^\/api\/v1\/faces\/persons\/[^/]+$/.test(p) && input.method === "PATCH" && b.fail.patch) return json({ code: "conflict", message: "name already in use" }, b.fail.patch);
      if (/^\/api\/v1\/faces\/persons\/[^/]+$/.test(p) && input.method === "PATCH") return json({ ...b.persons[0], ...(call.body as object) });
      if (p === "/api/v1/faces/train") return json(b.train);
      if (p === "/api/v1/faces/train/assign") return json({ person: person("p1", "Ana"), image: { id: "i9" }, attempt_removed: true }, 201);
      if (p === "/api/v1/faces/recognitions") {
        const idx = url.searchParams.get("cursor") ? 1 : 0;
        const items = b.recognitions[idx] ?? [];
        return json({ items, next_cursor: idx === 0 && b.recognitions.length > 1 ? "c1" : undefined });
      }
      if (p === "/api/v1/cameras") return json({ items: b.cameras });
      return json({ code: "not_found", message: "not found" }, 404);
    }),
  );
  return { calls, b };
}

const rec = (id: string, who: string, cam: string, personId = "p1"): Schemas["FaceRecognition"] => ({
  event_id: id, person_id: personId, person_name: who, camera_id: `cam-${cam}`, camera_name: cam, site_id: "s1", site_name: "Sitio", server_name: "h01",
  start_time: "2026-10-17T12:00:00Z", has_thumbnail: true, has_snapshot: false,
});

describe("Faces page as a platform administrator", () => {
  const facesCalls = (calls: Call[]) => calls.filter((c) => c.path.startsWith("/api/v1/faces"));

  it("needs no organization: no selector, no tenants request and no tenant_id on any faces call", async () => {
    const { calls } = install({ tenantId: null, persons: [person("p1", "Ana")] });
    renderPage(Faces);
    expect(await screen.findByRole("button", { name: /Ana/ })).toBeInTheDocument();
    expect(screen.queryByLabelText("Organización")).not.toBeInTheDocument();
    expect(calls.some((c) => c.path === "/api/v1/tenants")).toBe(false);
    expect(facesCalls(calls).length).toBeGreaterThan(0);
    expect(facesCalls(calls).some((c) => c.search.has("tenant_id"))).toBe(false);
  });

  it("sends no tenant_id with mutations, images and the other tabs", async () => {
    const { calls } = install({ tenantId: null, persons: [person("p1", "Ana")], images: [{ id: "i1", person_id: "p1", content_type: "image/jpeg", size_bytes: 1, width: 1, height: 1, created_at: "2026-10-01T10:00:00Z" }] });
    renderPage(Faces);
    fireEvent.click(await screen.findByRole("button", { name: "Nueva persona" }));
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Carla" } });
    fireEvent.click(screen.getByRole("button", { name: "Crear persona" }));
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/v1/faces/persons")).toBe(true));
    fireEvent.click(await screen.findByRole("tab", { name: "Entrenar" }));
    await waitFor(() => expect(calls.some((c) => c.path === "/api/v1/faces/train")).toBe(true));
    fireEvent.click(screen.getByRole("tab", { name: "Reconocimientos" }));
    await waitFor(() => expect(calls.some((c) => c.path === "/api/v1/faces/recognitions")).toBe(true));
    expect(facesCalls(calls).some((c) => c.search.has("tenant_id"))).toBe(false);
  });

  it("uses plain train thumbnail URLs and assigns without a tenant", async () => {
    const train = { items: [{ server_id: "s1", server_name: "h01", file: "a.webp" }], servers: [{ server_id: "s1", server_name: "h01" }] };
    const { calls } = install({ tenantId: null, persons: [person("p1", "Ana")], train });
    renderPage(Faces);
    fireEvent.click(await screen.findByRole("tab", { name: "Entrenar" }));
    const img = await screen.findByAltText("Intento de reconocimiento en h01");
    expect(img).toHaveAttribute("src", "/api/v1/faces/train/s1/image?file=a.webp");
    await screen.findAllByRole("option", { name: "Ana" });
    fireEvent.change(screen.getByLabelText("Asignar a"), { target: { value: "p1" } });
    fireEvent.click(screen.getByRole("button", { name: "Asignar" }));
    await waitFor(() => expect(calls.some((c) => c.path === "/api/v1/faces/train/assign")).toBe(true));
    expect(calls.find((c) => c.path === "/api/v1/faces/train/assign")?.search.has("tenant_id")).toBe(false);
  });

  it("keeps the person image URL free of tenant_id", async () => {
    install({ tenantId: null, persons: [person("p1", "Ana")], images: [{ id: "i1", person_id: "p1", content_type: "image/jpeg", size_bytes: 1, width: 1, height: 1, created_at: "2026-10-01T10:00:00Z" }] });
    renderPage(Faces);
    fireEvent.click(await screen.findByRole("button", { name: /Ana/ }));
    expect(await screen.findByAltText("Foto 1 de Ana")).toHaveAttribute("src", "/api/v1/faces/persons/p1/images/i1");
  });
});

describe("Faces page", () => {
  it("shows no data and no calls when the user lacks faces.view", async () => {
    const { calls } = install({ permissions: ["events.view"] });
    renderPage(Faces);
    expect(await screen.findByText("No tienes permiso para ver el reconocimiento facial.")).toBeInTheDocument();
    expect(calls.some((c) => c.path.startsWith("/api/v1/faces"))).toBe(false);
  });

  it("lists people, filters by search, and offers no write controls to a viewer", async () => {
    install({ permissions: ["faces.view"], persons: [person("p1", "Ana"), person("p2", "Beto", { sync: sync({ total: 2, synced: 1, failed: 1 }) })] });
    renderPage(Faces);
    const list = await screen.findByRole("list", { name: "Personas registradas" });
    expect(within(list).getByText("Ana")).toBeInTheDocument();
    expect(within(list).getByText("2 de 2 servidores sincronizados")).toBeInTheDocument();
    expect(within(list).getByText("1 de 2 servidores sincronizados")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Nueva persona" })).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Buscar persona"), { target: { value: "bet" } });
    expect(within(screen.getByRole("list", { name: "Personas registradas" })).queryByText("Ana")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Beto/ }));
    expect(await screen.findByRole("heading", { name: "Beto" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Eliminar persona" })).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Subir foto")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reintentar" })).not.toBeInTheDocument();
  });

  it("explains the empty state and warns when no server has face recognition", async () => {
    install({ persons: [] });
    renderPage(Faces);
    expect(await screen.findByText(/Todavía no hay personas registradas/)).toBeInTheDocument();
    install({ persons: [person("p1", "Ana", { sync: sync({ total: 2, synced: 0, disabled: 2 }) })] });
    renderPage(Faces);
    expect(await screen.findByText(/Ningún servidor tiene activado el reconocimiento facial: las personas se guardan aquí/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Ir a Servidores" })).toHaveAttribute("href", "/servers");
  });

  it("creates a person", async () => {
    const { calls } = install({ persons: [] });
    renderPage(Faces);
    fireEvent.click(await screen.findByRole("button", { name: "Nueva persona" }));
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "  Carla  " } });
    fireEvent.change(screen.getByLabelText("Notas"), { target: { value: "vip" } });
    fireEvent.click(screen.getByRole("button", { name: "Crear persona" }));
    await waitFor(() => expect(calls.find((c) => c.method === "POST" && c.path === "/api/v1/faces/persons")?.body).toEqual({ display_name: "Carla", notes: "vip" }));
  });

  it("shows a person's photos, sync status, and lets a manager upload, delete and retry", async () => {
    const { calls } = install({
      persons: [person("p1", "Ana")],
      images: [{ id: "i1", person_id: "p1", content_type: "image/png", size_bytes: 10, width: 4, height: 4, created_at: "2026-10-01T10:00:00Z" }],
      syncStatus: [
        { server_id: "s1", server_name: "h01", status: "synced", owned: true, desired_revision: 2, synced_revision: 2, updated_at: "2026-10-01T10:00:00Z" },
        { server_id: "s2", server_name: "c01", status: "conflict", owned: false, desired_revision: 2, synced_revision: 0, updated_at: "2026-10-01T10:00:00Z", last_error: "same name" },
      ],
    });
    renderPage(Faces);
    fireEvent.click(await screen.findByRole("button", { name: /Ana/ }));
    const img = await screen.findByAltText("Foto 1 de Ana");
    expect(img).toHaveAttribute("src", "/api/v1/faces/persons/p1/images/i1");
    expect(await screen.findByText("Sincronizado")).toBeInTheDocument();
    expect(screen.getByText("Conflicto")).toBeInTheDocument();

    const file = new File([new Uint8Array([1, 2, 3])], "a.png", { type: "image/png" });
    fireEvent.change(screen.getByLabelText("Subir foto"), { target: { files: [file] } });
    await waitFor(() => {
      const body = calls.find((c) => c.method === "POST" && c.path === "/api/v1/faces/persons/p1/images")?.body;
      expect(body).toBeInstanceOf(FormData);
      expect(((body as FormData).get("file") as File).name).toBe("a.png");
    });

    fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    await waitFor(() => expect(calls.some((c) => c.path === "/api/v1/faces/persons/p1/sync/retry" && c.method === "POST")).toBe(true));

    fireEvent.click(screen.getByRole("button", { name: "Eliminar foto 1" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Eliminar foto" }));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/v1/faces/persons/p1/images/i1")).toBe(true));
  });

  it("saves edits to a person with a trimmed PATCH and keeps the form disabled until something changed", async () => {
    const { calls } = install({ persons: [person("p1", "Ana")] });
    renderPage(Faces);
    fireEvent.click(await screen.findByRole("button", { name: /Ana/ }));
    const save = await screen.findByRole("button", { name: "Guardar" });
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "  Ana María  " } });
    fireEvent.change(screen.getByLabelText("Notas"), { target: { value: " visita " } });
    expect(save).toBeEnabled();
    fireEvent.click(save);
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH" && c.path === "/api/v1/faces/persons/p1")?.body).toEqual({ display_name: "Ana María", notes: "visita" }));
  });

  it("shows the API message when saving a person fails", async () => {
    install({ persons: [person("p1", "Ana")], fail: { patch: 409 } });
    renderPage(Faces);
    fireEvent.click(await screen.findByRole("button", { name: /Ana/ }));
    fireEvent.change(await screen.findByLabelText("Nombre"), { target: { value: "Beto" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(await screen.findByText("name already in use")).toBeInTheDocument();
  });

  it("shows the API error of a failed upload (postForm error path)", async () => {
    install({ persons: [person("p1", "Ana")], fail: { upload: 400 } });
    renderPage(Faces);
    fireEvent.click(await screen.findByRole("button", { name: /Ana/ }));
    const file = new File([new Uint8Array([1])], "a.png", { type: "image/png" });
    fireEvent.change(await screen.findByLabelText("Subir foto"), { target: { files: [file] } });
    expect(await screen.findByText("unsupported picture")).toBeInTheDocument();
  });

  it("asks for confirmation before deleting a person and explains the propagation", async () => {
    const { calls } = install({ persons: [person("p1", "Ana")] });
    renderPage(Faces);
    fireEvent.click(await screen.findByRole("button", { name: /Ana/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Eliminar persona" }));
    const dialog = await screen.findByRole("dialog", { name: "Eliminar a Ana" });
    expect(within(dialog).getByText(/se quitará de todos los servidores Frigate y se borrará su nombre de los eventos guardados/)).toBeInTheDocument();
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
    fireEvent.click(within(dialog).getByRole("button", { name: "Eliminar persona" }));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/v1/faces/persons/p1")).toBe(true));
  });

  describe("Entrenar", () => {
    const attempts = {
      items: [
        { server_id: "s1", server_name: "h01", file: "a.webp", score: 0.41, seen_at: "2026-10-17T12:00:00Z" },
        { server_id: "s1", server_name: "h01", file: "b.webp" },
      ],
      servers: [{ server_id: "s1", server_name: "h01" }, { server_id: "s2", server_name: "c01", error: "the server could not be reached" }],
    };

    it("points to Servers when no server has face recognition", async () => {
      install({ train: { items: [], servers: [] } });
      renderPage(Faces);
      fireEvent.click(await screen.findByRole("tab", { name: "Entrenar" }));
      expect(await screen.findByText(/Ningún servidor tiene activado el reconocimiento facial\. La configuración administrada/)).toBeInTheDocument();
      expect(screen.getByRole("link", { name: "Ir a Servidores" })).toHaveAttribute("href", "/servers");
    });

    it("shows attempts through the proxied thumbnail endpoint and reports unreachable servers", async () => {
      install({ persons: [person("p1", "Ana")], train: attempts });
      renderPage(Faces);
      fireEvent.click(await screen.findByRole("tab", { name: "Entrenar" }));
      const imgs = await screen.findAllByAltText("Intento de reconocimiento en h01");
      expect(imgs[0]).toHaveAttribute("src", "/api/v1/faces/train/s1/image?file=a.webp");
      expect(screen.getByText("c01: the server could not be reached")).toBeInTheDocument();
      expect(screen.getByText("Puntaje 0.41")).toBeInTheDocument();
    });

    it("assigns an attempt to an existing person and to a new one", async () => {
      const { calls } = install({ persons: [person("p1", "Ana")], train: attempts });
      renderPage(Faces);
      fireEvent.click(await screen.findByRole("tab", { name: "Entrenar" }));
      const selects = await screen.findAllByLabelText("Asignar a");
      await screen.findAllByRole("option", { name: "Ana" });
      fireEvent.click(screen.getAllByRole("button", { name: "Asignar" })[0]!);
      expect(await screen.findByText("Elige una persona o escribe un nombre.")).toBeInTheDocument();
      fireEvent.change(selects[0]!, { target: { value: "p1" } });
      fireEvent.click(screen.getAllByRole("button", { name: "Asignar" })[0]!);
      await waitFor(() => expect(calls.filter((c) => c.path === "/api/v1/faces/train/assign").map((c) => c.body)).toEqual([{ server_id: "s1", file: "a.webp", person_id: "p1" }]));
      expect(await screen.findByText("Foto agregada a Ana.")).toBeInTheDocument();

      fireEvent.change(screen.getAllByLabelText("Asignar a")[1]!, { target: { value: "__new__" } });
      fireEvent.change(await screen.findByLabelText("Nombre de la nueva persona"), { target: { value: "Dora" } });
      fireEvent.click(screen.getAllByRole("button", { name: "Asignar" })[1]!);
      await waitFor(() => expect(calls.filter((c) => c.path === "/api/v1/faces/train/assign").map((c) => c.body)).toContainEqual({ server_id: "s1", file: "b.webp", new_person_display_name: "Dora" }));
    });

    it("hides the assign controls from a viewer", async () => {
      install({ permissions: ["faces.view"], persons: [person("p1", "Ana")], train: attempts });
      renderPage(Faces);
      fireEvent.click(await screen.findByRole("tab", { name: "Entrenar" }));
      await screen.findAllByAltText("Intento de reconocimiento en h01");
      expect(screen.queryByRole("button", { name: "Asignar" })).not.toBeInTheDocument();
    });
  });

  describe("Reconocimientos", () => {
    const recCalls = (calls: Call[]) => calls.filter((c) => c.path === "/api/v1/faces/recognitions");

    it("searches today by default and pages", async () => {
      const { calls } = install({ persons: [person("p1", "Ana")], recognitions: [[rec("e1", "Ana", "plaza")], [rec("e2", "Beto", "muelle", "p2")]] });
      renderPage(Faces);
      fireEvent.click(await screen.findByRole("tab", { name: "Reconocimientos" }));
      const list = await screen.findByRole("list", { name: "Reconocimientos recientes" });
      expect(within(list).getByText("Ana")).toBeInTheDocument();
      const first = recCalls(calls)[0]!;
      expect(first.search.get("from")).toBeTruthy();
      expect(first.search.get("to")).toBeTruthy();
      expect(first.search.has("tenant_id")).toBe(false);
      fireEvent.click(screen.getByRole("button", { name: "Cargar más" }));
      expect(await screen.findByText("muelle")).toBeInTheDocument();
    });

    it("lists recognitions with playback links and the event thumbnail", async () => {
      install({ recognitions: [[rec("e1", "Ana", "plaza")]] });
      renderPage(Faces);
      fireEvent.click(await screen.findByRole("tab", { name: "Reconocimientos" }));
      const list = await screen.findByRole("list", { name: "Reconocimientos recientes" });
      const link = within(list).getByRole("link", { name: "Ver grabación" });
      expect(link.getAttribute("href")).toMatch(/^\/playback\?/);
      expect(link.getAttribute("href")).toContain("camera=cam-plaza");
      expect(within(list).getByAltText("Captura del evento en plaza")).toHaveAttribute("src", "/api/v1/events/e1/thumbnail");
    });

    it("maps site, camera group, camera and person to query params only after Buscar, with removable chips", async () => {
      const { calls } = install({
        persons: [person("p1", "Ana")], cameras: [{ id: "cam-plaza", display_name: "plaza" }], recognitions: [[rec("e1", "Ana", "plaza")]],
      });
      renderPage(Faces);
      fireEvent.click(await screen.findByRole("tab", { name: "Reconocimientos" }));
      await screen.findByRole("option", { name: "Anexo" });
      await screen.findByRole("option", { name: "Ana" });
      await screen.findByRole("option", { name: "plaza" });
      const before = recCalls(calls).length;
      fireEvent.change(screen.getByLabelText("Sitio"), { target: { value: "s2" } });
      fireEvent.change(screen.getByLabelText("Grupo de cámaras"), { target: { value: "g1" } });
      fireEvent.change(screen.getByLabelText("Cámara"), { target: { value: "cam-plaza" } });
      fireEvent.change(screen.getByLabelText("Persona"), { target: { value: "p1" } });
      expect(recCalls(calls).length).toBe(before);
      fireEvent.click(screen.getByRole("button", { name: "Buscar" }));
      await waitFor(() => {
        const last = recCalls(calls).at(-1)!;
        expect(last.search.getAll("site_id")).toEqual(["s2"]);
        expect(last.search.getAll("camera_group_id")).toEqual(["g1"]);
        expect(last.search.getAll("camera_id")).toEqual(["cam-plaza"]);
        expect(last.search.getAll("person_id")).toEqual(["p1"]);
      });
      const chips = await screen.findByRole("list", { name: "Filtros aplicados" });
      expect(within(chips).getByText("Sitio: Anexo")).toBeInTheDocument();
      fireEvent.click(within(chips).getByRole("button", { name: /Sitio: Anexo/ }));
      await waitFor(() => expect(recCalls(calls).at(-1)!.search.has("site_id")).toBe(false));
    });

    it("shows an empty state", async () => {
      install({ recognitions: [[]] });
      renderPage(Faces);
      fireEvent.click(await screen.findByRole("tab", { name: "Reconocimientos" }));
      expect(await screen.findByText("No hay reconocimientos para estos filtros.")).toBeInTheDocument();
    });
  });

  describe("Entrenar filters", () => {
    it("narrows the attempts by site and server", async () => {
      const { calls } = install({ train: { items: [], servers: [{ server_id: "srv1", server_name: "h01" }] } });
      renderPage(Faces);
      fireEvent.click(await screen.findByRole("tab", { name: "Entrenar" }));
      await screen.findByRole("option", { name: "Anexo" });
      await screen.findByRole("option", { name: "c01" });
      fireEvent.change(screen.getByLabelText("Sitio"), { target: { value: "s2" } });
      await waitFor(() => expect(calls.some((c) => c.path === "/api/v1/faces/train" && c.search.getAll("site_id").join() === "s2")).toBe(true));
      fireEvent.change(screen.getByLabelText("Servidor"), { target: { value: "srv2" } });
      await waitFor(() => expect(calls.some((c) => c.path === "/api/v1/faces/train" && c.search.getAll("server_id").join() === "srv2")).toBe(true));
    });
  });
});
