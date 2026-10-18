import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage } from "@/test-utils";
import { Rules } from "./Rules";

afterEach(() => vi.unstubAllGlobals());

const manager = {
  id: "u1", username: "admin", display_name: "Admin", tenant_id: "t1", mfa_enabled: false,
  must_change_password: false, auth_method: "session",
  grants: [
    { permission: "rules.read", effect: "allow", scope_type: "tenant" },
    { permission: "rules.manage", effect: "allow", scope_type: "tenant" },
    { permission: "notifications.manage", effect: "allow", scope_type: "tenant" },
  ],
};

function makeRule(overrides: Partial<Schemas["Rule"]> = {}): Schemas["Rule"] {
  return {
    id: "r1", tenant_id: "t1", name: "Alertas de acceso", trigger_type: "event",
    conditions: { camera_ids: ["c1"], labels: ["person"] },
    actions: { create_alarm: true, notify_in_app: true, severity: "warning" },
    enabled: true, version: 3, created_at: "2026-09-29T10:00:00Z", updated_at: "2026-09-29T10:00:00Z", ...overrides,
  };
}

type Handler = (req: Request, url: URL) => Response | undefined;
function stub(rules: Schemas["Rule"][], extra?: Handler) {
  const calls: { method: string; path: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      let body: unknown;
      if (req.method !== "GET") body = await req.clone().json().catch(() => undefined);
      if (req.method !== "GET") calls.push({ method: req.method, path: url.pathname, body });
      const custom = extra?.(req, url);
      if (custom) return custom;
      if (url.pathname === "/api/v1/me") return json(manager);
      if (url.pathname === "/api/v1/rules" && req.method === "GET") return json({ items: rules });
      if (url.pathname.startsWith("/api/v1/rules/templates")) return json({ items: [{ id: "person-detection", name: "Detección de personas", description: "", trigger_type: "event", conditions: { labels: ["person"] }, actions: { notify_in_app: true } }] });
      if (url.pathname.startsWith("/api/v1/camera-groups")) return json({ items: [{ id: "g1", name: "Perímetro", member_count_total: 1, member_count_hidden: 0, version: 1 }] });
      if (url.pathname.startsWith("/api/v1/rules/simulate") && req.method === "POST") return json({ evaluated: 2, condition_matched: 1, matched: 1, cooldown_suppressed: 0, cooldown_seconds: 45, has_more: false, window_start: "2026-10-16T10:00:00Z", window_end: "2026-10-16T11:00:00Z", matches: [{ event_id: "e1", camera_id: "c1", camera_name: "Cámara Acceso", site_name: "Sucursal Central", severity: "alert", labels: ["person"], start_time: "2026-10-16T10:30:00Z" }] });
      if (url.pathname.endsWith("/versions")) return json({ items: [{ version: 3, snapshot: rules[0] ?? makeRule(), created_at: "2026-09-29T10:00:00Z" }] });
      if (url.pathname === "/api/v1/cameras") return json({ items: [{ id: "c1", display_name: "Cámara Acceso", zones: ["entrada"] }, { id: "c2", display_name: "Cámara Patio", zones: ["patio", "entrada"] }] });
      if (url.pathname === "/api/v1/sites") return json({ items: [{ id: "site1", name: "Sucursal Central" }, { id: "site2", name: "Depósito" }] });
      if (url.pathname === "/api/v1/notification-channels") return json({ items: [
        { id: "ch1", tenant_id: "t1", name: "Telegram guardia", type: "telegram", enabled: true, config: {}, secrets_set: ["bot_token"], created_at: "x", updated_at: "x" },
        { id: "ch2", tenant_id: "t1", name: "Correo operaciones", type: "email", enabled: true, config: {}, secrets_set: [], created_at: "x", updated_at: "x" },
      ] });
      if (url.pathname === "/api/v1/servers") return json({ items: [{ id: "s1", name: "Servidor Norte" }] });
      if (url.pathname.startsWith("/api/v1/rules")) return json(makeRule());
      return json({}, 404);
    }),
  );
  return calls;
}

function advanceWizard(dialog: HTMLElement, count: number) {
  const name = within(dialog).getByLabelText("Nombre");
  if (!(name as HTMLInputElement).value.trim()) fireEvent.change(name, { target: { value: "Regla de prueba" } });
  for (let step = 0; step < count; step += 1) fireEvent.click(within(dialog).getByRole("button", { name: "Siguiente" }));
}

describe("Rules route", () => {
  it("lists rules with trigger and enable state", async () => {
    stub([makeRule()]);
    renderPage(() => <Rules />);
    expect(await screen.findByText("Alertas de acceso")).toBeInTheDocument();
    expect(screen.getByText("Evento nuevo")).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: /Alertas de acceso/ })).toBeChecked();
  });

  it("toggles a rule through PATCH", async () => {
    const calls = stub([makeRule()]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("switch", { name: /Alertas de acceso/ }));
    await waitFor(() => expect(calls).toContainEqual({ method: "PATCH", path: "/api/v1/rules/r1", body: { enabled: false } }));
  });

  it("sends the rule version as If-Match on update", async () => {
    const ifMatch: string[] = [];
    stub([makeRule()], (req) => {
      if (req.method === "PATCH") ifMatch.push(req.headers.get("If-Match") ?? "");
      return undefined;
    });
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("switch", { name: /Alertas de acceso/ }));
    await waitFor(() => expect(ifMatch).toEqual(["3"]));
  });

  it("preserves API-only condition fields when editing an event rule", async () => {
    const apiOnlyRule = makeRule({ conditions: {
      camera_ids: ["c1"], labels: ["person"], camera_group_ids: ["g1"], map_zone_ids: ["z1"],
      cooldown_seconds: 45, schedule: { days_of_week: [1, 2], start_time: "21:00", end_time: "06:00", timezone: "America/Argentina/Buenos_Aires" },
      vehicle_types: ["car"],
    } });
    const calls = stub([apiOnlyRule]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: "Editar Alertas de acceso" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Regla editada" } });
    advanceWizard(dialog, 3);
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]?.body).toMatchObject({ conditions: {
      camera_group_ids: ["g1"], map_zone_ids: ["z1"], cooldown_seconds: 45,
      schedule: { days_of_week: [1, 2], start_time: "21:00", end_time: "06:00", timezone: "America/Argentina/Buenos_Aires" },
      vehicle_types: ["car"],
    } });
  });

  it("offers schedule, timezone, cooldown, camera groups, templates, and a real-event preview", async () => {
    const calls = stub([]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: /Nueva regla/ }));
    const dialog = await screen.findByRole("dialog");
    await within(dialog).findByRole("option", { name: "Detección de personas" });
    fireEvent.change(within(dialog).getByLabelText("Plantilla"), { target: { value: "person-detection" } });
    expect(await within(dialog).findByRole("checkbox", { name: "Perímetro" })).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "Perímetro" }));
    advanceWizard(dialog, 1);
    fireEvent.change(within(dialog).getByLabelText(/Cooldown \(segundos\)/), { target: { value: "45" } });
    fireEvent.change(within(dialog).getByLabelText(/Zona horaria IANA/), { target: { value: "America/Argentina/Buenos_Aires" } });
    fireEvent.change(within(dialog).getByLabelText("Desde (HH:MM)"), { target: { value: "21:00" } });
    fireEvent.change(within(dialog).getByLabelText("Hasta (HH:MM)"), { target: { value: "06:00" } });
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "Lunes" }));
    advanceWizard(dialog, 2);
    fireEvent.click(within(dialog).getByRole("button", { name: "Vista previa con eventos recientes" }));
    await waitFor(() => expect(calls).toContainEqual(expect.objectContaining({ method: "POST", path: "/api/v1/rules/simulate" })));
    expect(await within(dialog).findByText(/1 coinciden/)).toBeInTheDocument();
    const simulation = calls.find((call) => call.path === "/api/v1/rules/simulate");
    expect(simulation?.body).toMatchObject({ conditions: { camera_group_ids: ["g1"], cooldown_seconds: 45, schedule: { days_of_week: [1], start_time: "21:00", end_time: "06:00", timezone: "America/Argentina/Buenos_Aires" } } });
  });

  it("fetches version history for the selected rule", async () => {
    stub([makeRule()]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: "Historial" }));
    expect(await screen.findByText(/Versión 3/)).toBeInTheDocument();
  });

  it("does not silently overwrite on 409 and offers an explicit reload", async () => {
    const latest = makeRule({ name: "Regla actualizada", version: 4 });
    stub([makeRule()], (req, url) => {
      if (req.method === "PATCH") return json({ code: "conflict", message: "stale" }, 409);
      if (req.method === "GET" && url.pathname === "/api/v1/rules/r1") return json(latest);
      return undefined;
    });
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: "Editar Alertas de acceso" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Mi edición" } });
    advanceWizard(dialog, 3);
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    expect(await within(dialog).findByText(/Tus cambios no se guardaron/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cargar versión actual" }));
    expect(await screen.findByRole("dialog", { name: "Editar Regla actualizada" })).toBeInTheDocument();
    expect(screen.getByLabelText("Nombre")).toHaveValue("Regla actualizada");
  });

  it("shows a clear message when the user cannot manage rules", async () => {
    stub([], (_req, url) => {
      if (url.pathname === "/api/v1/rules") return json({ code: "forbidden", message: "sin permiso" }, 403);
      if (url.pathname === "/api/v1/me") return json({ ...manager, grants: [] });
      return undefined;
    });
    renderPage(() => <Rules />);
    expect(await screen.findByText(/No tenés permiso para administrar reglas/i)).toBeInTheDocument();
  });

  it("shows conditional fields per trigger type", async () => {
    stub([]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: /Nueva regla/ }));
    const dialog = await screen.findByRole("dialog");
    advanceWizard(dialog, 1);
    expect(within(dialog).getByLabelText(/Etiquetas/)).toBeInTheDocument();
    expect(within(dialog).getByLabelText(/Zonas/)).toBeInTheDocument();
    expect(within(dialog).getByRole("checkbox", { name: "Alerta" })).toBeInTheDocument();
    expect(within(dialog).queryByLabelText(/Minutos/)).not.toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole("button", { name: "Anterior" }));
    fireEvent.change(within(dialog).getByLabelText("Disparador"), { target: { value: "camera_offline" } });
    expect(within(dialog).getByLabelText(/Minutos sin conexión/)).toBeInTheDocument();
    expect(within(dialog).getByRole("checkbox", { name: "Cámara Patio" })).toBeInTheDocument();

    fireEvent.change(within(dialog).getByLabelText("Disparador"), { target: { value: "server_offline" } });
    expect(within(dialog).getByRole("checkbox", { name: "Servidor Norte" })).toBeInTheDocument();
    expect(within(dialog).queryByRole("checkbox", { name: "Cámara Patio" })).not.toBeInTheDocument();
  });

  it("creates an event rule with the right body", async () => {
    const calls = stub([]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: /Nueva regla/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Personas de noche" } });
    fireEvent.click(await within(dialog).findByRole("checkbox", { name: "Cámara Acceso" }));
    advanceWizard(dialog, 1);
    fireEvent.change(within(dialog).getByLabelText(/Etiquetas/), { target: { value: "person, car" } });
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "Alerta" }));
    advanceWizard(dialog, 1);
    fireEvent.change(within(dialog).getByLabelText("Severidad de la notificación"), { target: { value: "critical" } });
    advanceWizard(dialog, 1);
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toEqual({
      method: "POST",
      path: "/api/v1/rules",
      body: {
        name: "Personas de noche",
        trigger_type: "event",
        enabled: true,
        conditions: { camera_ids: ["c1"], labels: ["person", "car"], severities: ["alert"] },
        actions: { create_alarm: true, notify_in_app: true, severity: "critical" },
      },
    });
  });

  it("guides creation through scope, conditions, actions, and review with activation enabled by default", async () => {
    const calls = stub([]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: /Nueva regla/ }));
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByText("Paso 1 de 4: Alcance")).toBeInTheDocument();
    expect(within(dialog).getByRole("checkbox", { name: "Cámara Acceso" })).toBeInTheDocument();
    expect(within(dialog).queryByRole("textbox", { name: "Etiquetas" })).not.toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Siguiente" }));
    expect(within(dialog).getByRole("alert")).toHaveTextContent("Ingresá un nombre");
    expect(within(dialog).getByText("Paso 1 de 4: Alcance")).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Personas" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Siguiente" }));
    expect(await within(dialog).findByText("Paso 2 de 4: Condiciones")).toBeInTheDocument();
    expect(within(dialog).queryByRole("checkbox", { name: "Cámara Acceso" })).not.toBeInTheDocument();
    expect(within(dialog).getByLabelText("Etiquetas")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Siguiente" }));
    expect(await within(dialog).findByText("Paso 3 de 4: Acciones")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Siguiente" }));
    expect(await within(dialog).findByText("Paso 4 de 4: Revisión y activación")).toBeInTheDocument();
    expect(within(dialog).getByText("Resumen de la regla")).toBeInTheDocument();
    expect(within(dialog).getByText(/Personas · Evento nuevo/)).toBeInTheDocument();
    expect(within(dialog).getByRole("checkbox", { name: "Activar regla al guardar" })).toBeChecked();
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]?.body).toMatchObject({ enabled: true });
  });

  it("validates paired schedule times before leaving the conditions step", async () => {
    stub([]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: /Nueva regla/ }));
    const dialog = await screen.findByRole("dialog");
    advanceWizard(dialog, 1);
    fireEvent.change(within(dialog).getByLabelText("Desde (HH:MM)"), { target: { value: "21:00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Siguiente" }));
    expect(within(dialog).getByRole("alert")).toHaveTextContent("Completá ambos horarios");
    expect(within(dialog).getByText("Paso 2 de 4: Condiciones")).toBeInTheDocument();
  });

  it.each(["-1", "86401"])("keeps invalid cooldown %s in the conditions step with a correction message", async (cooldown) => {
    stub([]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: /Nueva regla/ }));
    const dialog = await screen.findByRole("dialog");
    advanceWizard(dialog, 1);
    fireEvent.change(within(dialog).getByLabelText(/Cooldown \(segundos\)/), { target: { value: cooldown } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Siguiente" }));
    expect(within(dialog).getByRole("alert")).toHaveTextContent("El cooldown debe estar entre 0 y 86400 segundos.");
    expect(within(dialog).getByText("Paso 2 de 4: Condiciones")).toBeInTheDocument();
  });

  it("preserves a disabled rule state when editing without activation changes", async () => {
    const calls = stub([makeRule({ enabled: false })]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: "Editar Alertas de acceso" }));
    const dialog = await screen.findByRole("dialog");
    advanceWizard(dialog, 3);
    expect(within(dialog).getByRole("checkbox", { name: "Activar regla al guardar" })).not.toBeChecked();
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]?.body).toMatchObject({ enabled: false });
  });

  it("creates a camera offline rule converting minutes to seconds", async () => {
    const calls = stub([]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: /Nueva regla/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Cámara caída" } });
    fireEvent.change(within(dialog).getByLabelText("Disparador"), { target: { value: "camera_offline" } });
    advanceWizard(dialog, 1);
    fireEvent.change(within(dialog).getByLabelText(/Minutos sin conexión/), { target: { value: "10" } });
    advanceWizard(dialog, 2);
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]?.body).toMatchObject({ trigger_type: "camera_offline", conditions: { duration_seconds: 600 } });
  });

  it("filters a rule by site for every trigger", async () => {
    const calls = stub([]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: /Nueva regla/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Solo central" } });
    fireEvent.click(await within(dialog).findByRole("checkbox", { name: "Sucursal Central" }));
    fireEvent.change(within(dialog).getByLabelText("Disparador"), { target: { value: "server_offline" } });
    expect(within(dialog).getByRole("checkbox", { name: "Sucursal Central" })).toBeChecked();
    advanceWizard(dialog, 3);
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]?.body).toMatchObject({ trigger_type: "server_offline", conditions: { site_ids: ["site1"] } });
  });

  it("suggests labels and the zones of the selected cameras while still accepting free entry", async () => {
    const calls = stub([]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: /Nueva regla/ }));
    const dialog = await screen.findByRole("dialog");
    const options = (label: RegExp) => {
      const list = (within(dialog).getByLabelText(label) as HTMLInputElement).list;
      return Array.from(list?.querySelectorAll("option") ?? []).map((o) => o.value);
    };
    expect(options(/Etiquetas/)).toEqual(expect.arrayContaining(["person", "car", "dog"]));
    await waitFor(() => expect(options(/Zonas/)).toEqual(["entrada", "patio"]));
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "Cámara Acceso" }));
    advanceWizard(dialog, 1);
    expect(options(/Zonas/)).toEqual(["entrada"]);

    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Zonas" } });
    const labels = within(dialog).getByLabelText(/Etiquetas/);
    fireEvent.change(labels, { target: { value: "forklift" } });
    fireEvent.keyDown(labels, { key: "Enter" });
    const zones = within(dialog).getByLabelText(/Zonas/);
    fireEvent.change(zones, { target: { value: "entrada" } });
    fireEvent.keyDown(zones, { key: "Enter" });
    advanceWizard(dialog, 2);
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]?.body).toMatchObject({ conditions: { camera_ids: ["c1"], labels: ["forklift"], zones: ["entrada"] } });
  });

  it("requires confirmation before deleting", async () => {
    const calls = stub([makeRule()]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: "Eliminar Alertas de acceso" }));
    expect(calls).toHaveLength(0);
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancelar" }));
    expect(calls).toHaveLength(0);
    fireEvent.click(await screen.findByRole("button", { name: "Eliminar Alertas de acceso" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Eliminar" }));
    await waitFor(() => expect(calls).toContainEqual({ method: "DELETE", path: "/api/v1/rules/r1", body: undefined }));
  });

  it("selects external channels for a rule and sends their ids", async () => {
    const calls = stub([]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: /Nueva regla/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Con canales" } });
    advanceWizard(dialog, 2);
    fireEvent.click(await within(dialog).findByRole("checkbox", { name: /Telegram guardia/ }));
    advanceWizard(dialog, 1);
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]?.body).toMatchObject({ actions: { create_alarm: true, notify_in_app: true, severity: "warning", channel_ids: ["ch1"] } });
  });

  it("preselects the channels of an existing rule and lists them in the actions column", async () => {
    stub([makeRule({ actions: { notify_in_app: true, channel_ids: ["ch2"] } })]);
    renderPage(() => <Rules />);
    expect(await screen.findByText(/1 canal externo/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Editar Alertas de acceso" }));
    const dialog = await screen.findByRole("dialog");
    advanceWizard(dialog, 2);
    expect(await within(dialog).findByRole("checkbox", { name: /Correo operaciones/ })).toBeChecked();
    expect(within(dialog).getByRole("checkbox", { name: /Telegram guardia/ })).not.toBeChecked();
  });
  describe("person conditions", () => {
    const withFaces = { ...manager, grants: [...manager.grants, { permission: "faces.view", effect: "allow", scope_type: "tenant" }] };
    const facesHandler = (me: typeof manager | typeof withFaces, seen: string[]) => (req: Request, url: URL) => {
      if (url.pathname === "/api/v1/me") return json(me);
      if (url.pathname.startsWith("/api/v1/faces/")) seen.push(url.pathname);
      if (url.pathname === "/api/v1/faces/persons") return json({ items: [{ id: "p1", display_name: "Ana", frigate_name: "ana" }, { id: "p2", display_name: "Beto", frigate_name: "beto" }] });
      if (url.pathname === "/api/v1/faces/watchlists") return json({ items: [{ id: "w1", name: "VIP", description: "", person_ids: ["p1"] }] });
      void req;
      return undefined;
    };

    it("offers person and watchlist pickers with faces.view and sends the selection", async () => {
      const seen: string[] = [];
      const calls = stub([], facesHandler(withFaces, seen));
      renderPage(() => <Rules />);
      fireEvent.click(await screen.findByRole("button", { name: /Nueva regla/ }));
      const dialog = await screen.findByRole("dialog");
      fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Vigilancia" } });
      advanceWizard(dialog, 1);
      fireEvent.click(await within(dialog).findByRole("checkbox", { name: "Ana" }));
      fireEvent.click(await within(dialog).findByRole("checkbox", { name: "VIP" }));
      advanceWizard(dialog, 2);
      fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
      await waitFor(() => expect(calls).toHaveLength(1));
      expect(calls[0]?.body).toMatchObject({ conditions: { face_person_ids: ["p1"], face_watchlist_ids: ["w1"] } });
    });

    it("hides the pickers without faces.view and keeps the stored person conditions", async () => {
      const seen: string[] = [];
      const stored = makeRule({ conditions: { labels: ["person"], face_person_ids: ["p1"], face_watchlist_ids: ["w1"] } });
      const calls = stub([stored], facesHandler(manager, seen));
      renderPage(() => <Rules />);
      fireEvent.click(await screen.findByRole("button", { name: "Editar Alertas de acceso" }));
      const dialog = await screen.findByRole("dialog");
      advanceWizard(dialog, 1);
      expect(within(dialog).queryByText(/Personas reconocidas/)).not.toBeInTheDocument();
      expect(within(dialog).queryByRole("checkbox", { name: "Ana" })).not.toBeInTheDocument();
      advanceWizard(dialog, 2);
      fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
      await waitFor(() => expect(calls).toHaveLength(1));
      expect(calls[0]?.body).toMatchObject({ conditions: { face_person_ids: ["p1"], face_watchlist_ids: ["w1"] } });
      expect(seen).toEqual([]);
    });
  });
});
