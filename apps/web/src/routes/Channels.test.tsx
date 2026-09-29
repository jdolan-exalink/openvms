import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage } from "@/test-utils";
import { Channels } from "./Channels";

afterEach(() => vi.unstubAllGlobals());

const manager = {
  id: "u1", username: "admin", display_name: "Admin", tenant_id: "t1", mfa_enabled: false,
  must_change_password: false, auth_method: "session",
  grants: [{ permission: "notifications.manage", effect: "allow", scope_type: "tenant" }],
};

function makeChannel(overrides: Partial<Schemas["NotificationChannel"]> = {}): Schemas["NotificationChannel"] {
  return {
    id: "ch1", tenant_id: "t1", name: "Webhook guardia", type: "webhook", enabled: true,
    config: { url: "https://hooks.example.com/vms" }, secrets_set: ["signing_secret"],
    created_at: "2026-09-29T10:00:00Z", updated_at: "2026-09-29T10:00:00Z", ...overrides,
  };
}

type Handler = (req: Request, url: URL) => Response | undefined;
function stub(channels: Schemas["NotificationChannel"][], extra?: Handler) {
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
      if (url.pathname === "/api/v1/notification-channels" && req.method === "GET") return json({ items: channels });
      if (url.pathname === "/api/v1/notification-deliveries") return json({ items: [] });
      if (url.pathname.endsWith("/test")) return json({ results: [{ destination: "", ok: true }] });
      if (url.pathname.startsWith("/api/v1/notification-channels")) return json(makeChannel());
      return json({}, 404);
    }),
  );
  return calls;
}

describe("Channels route", () => {
  it("lists channels with type and never shows secrets", async () => {
    stub([makeChannel()]);
    renderPage(() => <Channels />);
    expect(await screen.findByText("Webhook guardia")).toBeInTheDocument();
    expect(screen.getByText("Webhook")).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: /Webhook guardia/ })).toBeChecked();
  });

  it("shows a clear message when the user cannot manage channels", async () => {
    stub([], (_req, url) => {
      if (url.pathname === "/api/v1/notification-channels") return json({ code: "forbidden", message: "sin permiso" }, 403);
      return undefined;
    });
    renderPage(() => <Channels />);
    expect(await screen.findByText(/No tenés permiso para administrar canales/i)).toBeInTheDocument();
  });

  it("creates a webhook channel with write-only secrets", async () => {
    const calls = stub([]);
    renderPage(() => <Channels />);
    fireEvent.click(await screen.findByRole("button", { name: /Nuevo canal/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Nombre"), { target: { value: "Guardia" } });
    fireEvent.change(within(dialog).getByLabelText(/^URL/), { target: { value: "https://hooks.example.com/x" } });
    const secret = within(dialog).getByLabelText(/Secreto de firma/);
    expect(secret).toHaveAttribute("type", "password");
    fireEvent.change(secret, { target: { value: "s3cret" } });
    fireEvent.change(within(dialog).getByLabelText(/Cabeceras/), { target: { value: "Authorization: Bearer abc\nX-Env: prod" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toEqual({
      method: "POST",
      path: "/api/v1/notification-channels",
      body: {
        name: "Guardia", type: "webhook", enabled: true,
        config: { url: "https://hooks.example.com/x" },
        secrets: { signing_secret: "s3cret", headers: { Authorization: "Bearer abc", "X-Env": "prod" } },
      },
    });
  });

  it("shows per-type fields and the WhatsApp ban warning", async () => {
    stub([]);
    renderPage(() => <Channels />);
    fireEvent.click(await screen.findByRole("button", { name: /Nuevo canal/ }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Tipo"), { target: { value: "email" } });
    expect(within(dialog).getByLabelText("Servidor SMTP")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Cifrado")).toBeInTheDocument();
    fireEvent.change(within(dialog).getByLabelText("Tipo"), { target: { value: "telegram" } });
    expect(within(dialog).getByLabelText(/Token del bot/)).toHaveAttribute("type", "password");
    fireEvent.change(within(dialog).getByLabelText("Tipo"), { target: { value: "whatsapp" } });
    expect(within(dialog).getByText(/número dedicado/i)).toBeInTheDocument();
    expect(within(dialog).getByLabelText(/Sesión de WAHA/)).toHaveValue("default");
  });

  it("keeps stored secrets unless the user types a new one or removes it", async () => {
    const calls = stub([makeChannel({ secrets_set: ["signing_secret", "headers"] })]);
    renderPage(() => <Channels />);
    fireEvent.click(await screen.findByRole("button", { name: "Editar Webhook guardia" }));
    const dialog = await screen.findByRole("dialog");
    const secret = within(dialog).getByLabelText(/Secreto de firma/);
    expect(secret).toHaveValue("");
    expect(secret).toHaveAttribute("placeholder", expect.stringMatching(/configurado/i));
    fireEvent.click(within(dialog).getByRole("checkbox", { name: /Quitar cabeceras/i }));
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(calls[0]).toEqual({
      method: "PATCH",
      path: "/api/v1/notification-channels/ch1",
      body: { name: "Webhook guardia", config: { url: "https://hooks.example.com/vms" }, clear_secrets: ["headers"] },
    });
  });

  it("sends a test message and reports the outcome per destination", async () => {
    const calls = stub([makeChannel({ id: "tg1", name: "Telegram guardia", type: "telegram", config: { chat_ids: ["1", "2"] } })], (_req, url) => {
      if (url.pathname === "/api/v1/notification-channels/tg1/test") {
        return json({ results: [{ destination: "1", ok: true }, { destination: "2", ok: false, error: "HTTP 403: bot was blocked" }] });
      }
      return undefined;
    });
    renderPage(() => <Channels />);
    fireEvent.click(await screen.findByRole("button", { name: "Enviar prueba a Telegram guardia" }));
    await waitFor(() => expect(calls).toContainEqual({ method: "POST", path: "/api/v1/notification-channels/tg1/test", body: undefined }));
    const result = await screen.findByRole("status", { name: /Resultado de la prueba/ });
    expect(within(result).getByText(/HTTP 403: bot was blocked/)).toBeInTheDocument();
  });

  it("requires confirmation before deleting", async () => {
    const calls = stub([makeChannel()]);
    renderPage(() => <Channels />);
    fireEvent.click(await screen.findByRole("button", { name: "Eliminar Webhook guardia" }));
    expect(calls).toHaveLength(0);
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Cancelar" }));
    expect(calls).toHaveLength(0);
    fireEvent.click(await screen.findByRole("button", { name: "Eliminar Webhook guardia" }));
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Eliminar" }));
    await waitFor(() => expect(calls).toContainEqual({ method: "DELETE", path: "/api/v1/notification-channels/ch1", body: undefined }));
  });

  it("shows the WhatsApp pairing status and QR while waiting for a scan", async () => {
    stub([makeChannel({ id: "wa1", name: "WhatsApp guardia", type: "whatsapp", config: { session: "default", recipients: ["5491155555555@c.us"] }, secrets_set: [] })], (_req, url) => {
      if (url.pathname === "/api/v1/notification-channels/wa1/whatsapp/session") return json({ name: "default", status: "SCAN_QR_CODE" });
      if (url.pathname === "/api/v1/notification-channels/wa1/whatsapp/qr") return json({ mimetype: "image/png", data: "QVJD" });
      return undefined;
    });
    renderPage(() => <Channels />);
    fireEvent.click(await screen.findByRole("button", { name: "Vincular WhatsApp guardia" }));
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByText(/Esperando escaneo/)).toBeInTheDocument();
    const img = await within(dialog).findByRole("img", { name: /Código QR/ });
    expect(img).toHaveAttribute("src", "data:image/png;base64,QVJD");
  });

  it("offers to start a stopped WhatsApp session", async () => {
    const calls = stub([makeChannel({ id: "wa1", name: "WhatsApp guardia", type: "whatsapp", config: { session: "default", recipients: [] }, secrets_set: [] })], (_req, url) => {
      if (url.pathname === "/api/v1/notification-channels/wa1/whatsapp/session") return json({ name: "default", status: "STOPPED" });
      if (url.pathname === "/api/v1/notification-channels/wa1/whatsapp/session/start") return json({ name: "default", status: "STARTING" });
      return undefined;
    });
    renderPage(() => <Channels />);
    fireEvent.click(await screen.findByRole("button", { name: "Vincular WhatsApp guardia" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(await within(dialog).findByRole("button", { name: "Iniciar sesión" }));
    await waitFor(() => expect(calls).toContainEqual({ method: "POST", path: "/api/v1/notification-channels/wa1/whatsapp/session/start", body: undefined }));
  });
});
