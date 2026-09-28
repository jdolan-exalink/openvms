import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { SettingsLayout } from "./SettingsLayout";

afterEach(() => vi.unstubAllGlobals());

/** meResponse builds a /api/v1/me response allowing exactly the given permissions. */
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

describe("SettingsLayout", () => {
  it("shows only the sub-nav pages the user's grants allow, plus the ungated ones", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi({ "/api/v1/me": () => meResponse("servers.view") })));

    renderPage(SettingsLayout);

    // Gated and granted: wait for the "/api/v1/me" grants to resolve.
    expect(await screen.findByRole("link", { name: /Servidores/ })).toBeInTheDocument();

    // Ungated: always shown regardless of grants.
    expect(screen.getByRole("link", { name: /Resumen/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Sitios/ })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Mi cuenta/ })).toBeInTheDocument();

    // Gated and NOT granted: hidden entirely, not just disabled.
    expect(screen.queryByRole("link", { name: /Cámaras/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Usuarios/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Grupos/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Permisos/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Auditoría/ })).not.toBeInTheDocument();
  });

  it("shows a not-yet-built page as an inert entry with its milestone", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi({ "/api/v1/me": () => meResponse() })));

    renderPage(SettingsLayout);

    expect(await screen.findByText("Notificaciones")).toBeInTheDocument();
    expect(screen.getByText("M8")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Notificaciones/ })).not.toBeInTheDocument();
  });
});
