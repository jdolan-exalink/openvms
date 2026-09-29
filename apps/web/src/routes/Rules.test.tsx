import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage } from "@/test-utils";
import { Rules } from "./Rules";

afterEach(() => vi.unstubAllGlobals());

const manager = {
  id: "u1", username: "admin", display_name: "Admin", tenant_id: "t1", mfa_enabled: false,
  must_change_password: false, auth_method: "session",
  grants: [{ permission: "notifications.manage", effect: "allow", scope_type: "tenant" }],
};

function makeRule(overrides: Partial<Schemas["Rule"]> = {}): Schemas["Rule"] {
  return {
    id: "r1", tenant_id: "t1", name: "Alertas de acceso", trigger_type: "event",
    conditions: { camera_ids: ["c1"], labels: ["person"] },
    actions: { create_alarm: true, notify_in_app: true, severity: "warning" },
    enabled: true, created_at: "2026-09-29T10:00:00Z", updated_at: "2026-09-29T10:00:00Z", ...overrides,
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
      if (url.pathname === "/api/v1/cameras") return json({ items: [{ id: "c1", display_name: "Cámara Acceso", zones: ["entrada"] }, { id: "c2", display_name: "Cámara Patio", zones: ["patio", "entrada"] }] });
      if (url.pathname === "/api/v1/sites") return json({ items: [{ id: "site1", name: "Sucursal Central" }, { id: "site2", name: "Depósito" }] });
      if (url.pathname === "/api/v1/servers") return json({ items: [{ id: "s1", name: "Servidor Norte" }] });
      if (url.pathname.startsWith("/api/v1/rules")) return json(makeRule());
      return json({}, 404);
    }),
  );
  return calls;
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
    expect(within(dialog).getByLabelText(/Etiquetas/)).toBeInTheDocument();
    expect(within(dialog).getByLabelText(/Zonas/)).toBeInTheDocument();
    expect(within(dialog).getByRole("checkbox", { name: "Alerta" })).toBeInTheDocument();
    expect(within(dialog).queryByLabelText(/Minutos/)).not.toBeInTheDocument();

    fireEvent.change(within(dialog).getByLabelText("Disparador"), { target: { value: "camera_offline" } });
    expect(within(dialog).getByLabelText(/Minutos sin conexión/)).toBeInTheDocument();
    expect(within(dialog).getByRole("checkbox", { name: "Cámara Patio" })).toBeInTheDocument();
    expect(within(dialog).queryByLabelText(/Etiquetas/)).not.toBeInTheDocument();

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
    fireEvent.change(within(dialog).getByLabelText(/Etiquetas/), { target: { value: "person, car" } });
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "Alerta" }));
    fireEvent.change(within(dialog).getByLabelText("Severidad de la notificación"), { target: { value: "critical" } });
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

  it("creates a camera offline rule converting minutes to seconds", async () => {
    const calls = stub([]);
    renderPage(() => <Rules />);
    fireEvent.click(await screen.findByRole("button", { name: /Nueva regla/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Cámara caída" } });
    fireEvent.change(within(dialog).getByLabelText("Disparador"), { target: { value: "camera_offline" } });
    fireEvent.change(within(dialog).getByLabelText(/Minutos sin conexión/), { target: { value: "10" } });
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
    expect(options(/Zonas/)).toEqual(["entrada"]);

    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Zonas" } });
    const labels = within(dialog).getByLabelText(/Etiquetas/);
    fireEvent.change(labels, { target: { value: "forklift" } });
    fireEvent.keyDown(labels, { key: "Enter" });
    const zones = within(dialog).getByLabelText(/Zonas/);
    fireEvent.change(zones, { target: { value: "entrada" } });
    fireEvent.keyDown(zones, { key: "Enter" });
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
});
