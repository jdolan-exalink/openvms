import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json } from "@/test-utils";
import { renderPage } from "@/test-utils";
import { Branding } from "./Branding";

afterEach(() => vi.unstubAllGlobals());

function meResponse(...permissions: string[]) {
  return json({
    id: "u1",
    username: "u1",
    display_name: "u1",
    mfa_enabled: false,
    must_change_password: false,
    auth_method: "session",
    tenant_id: "t1",
    grants: permissions.map((permission) => ({ permission, effect: "allow" as const, scope_type: "platform" as const })),
  });
}

function brandingResponse(overrides: Partial<{ owner_name: string; timezone: string; has_logo: boolean }> = {}) {
  return json({
    tenant_id: "t1",
    owner_name: "",
    timezone: "America/Argentina/Buenos_Aires",
    has_logo: false,
    updated_at: "2024-01-01T00:00:00Z",
    ...overrides,
  });
}

describe("Branding", () => {
  it("shows the current owner name without editing controls without tenant.manage", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/tenants/t1/branding") return brandingResponse({ owner_name: "Municipalidad" });
        if (url.pathname === "/api/v1/me") return meResponse();
        return json({ code: "not_found", message: "not found" }, 404);
      }),
    );

    renderPage(Branding);

    const input = await screen.findByLabelText(/Nombre del propietario/);
    expect(input).toHaveValue("Municipalidad");
    expect(input).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Guardar" })).not.toBeInTheDocument();
    expect(screen.getByText("Necesitás el permiso tenant.manage para modificar la marca de agua.")).toBeInTheDocument();
  });

  it("saves the owner name with tenant.manage", async () => {
    let putBody: unknown;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/tenants/t1/branding" && input.method === "PUT") {
          putBody = await input.json();
          return brandingResponse({ owner_name: "Nueva Municipalidad" });
        }
        if (url.pathname === "/api/v1/tenants/t1/branding") return brandingResponse();
        if (url.pathname === "/api/v1/me") return meResponse("tenant.manage");
        return json({ code: "not_found", message: "not found" }, 404);
      }),
    );

    renderPage(Branding);

    const input = await screen.findByLabelText(/Nombre del propietario/);
    expect(input).toBeEnabled();
    fireEvent.change(input, { target: { value: "Nueva Municipalidad" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));

    await waitFor(() => {
      expect(putBody).toEqual({ owner_name: "Nueva Municipalidad", remove_logo: false });
    });
  });

  // PDW-7: the timezone select shows the tenant's currently configured branding.timezone, and
  // changing it sends the new IANA name in the PUT body (owner_name is left unsent, since it
  // was not touched — mirrors the "only send what changed" behavior owner_name/logo already
  // have).
  it("shows and saves the configured watermark time zone", async () => {
    let putBody: unknown;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/tenants/t1/branding" && input.method === "PUT") {
          putBody = await input.json();
          return brandingResponse({ timezone: "Europe/Madrid" });
        }
        if (url.pathname === "/api/v1/tenants/t1/branding") return brandingResponse({ timezone: "Europe/Madrid" });
        if (url.pathname === "/api/v1/me") return meResponse("tenant.manage");
        return json({ code: "not_found", message: "not found" }, 404);
      }),
    );

    renderPage(Branding);

    const select = await screen.findByLabelText(/Zona horaria/);
    await waitFor(() => expect(select).toHaveValue("Europe/Madrid"));

    fireEvent.change(select, { target: { value: "America/Argentina/Buenos_Aires" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));

    await waitFor(() => {
      expect(putBody).toEqual({ timezone: "America/Argentina/Buenos_Aires", remove_logo: false });
    });
  });

  it("offers to remove the logo only when one is configured", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/tenants/t1/branding") return brandingResponse({ has_logo: true });
        if (url.pathname === "/api/v1/me") return meResponse("tenant.manage");
        return json({ code: "not_found", message: "not found" }, 404);
      }),
    );

    renderPage(Branding);

    await screen.findByLabelText(/Nombre del propietario/);
    expect(screen.getByRole("button", { name: "Quitar logo" })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Logo actual" })).toHaveAttribute("src", "/api/v1/tenants/t1/branding/logo");
  });
});
