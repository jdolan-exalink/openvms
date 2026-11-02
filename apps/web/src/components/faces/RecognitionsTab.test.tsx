import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Faces } from "@/routes/Faces";
import { json, renderPage } from "@/test-utils";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function install(items: unknown[]) {
  vi.stubGlobal("fetch", vi.fn(async (raw: Request) => {
    const path = new URL(raw.url).pathname;
    if (path === "/api/v1/me") return json({ id: "u1", username: "u1", display_name: "u1", mfa_enabled: false, must_change_password: false, auth_method: "session", tenant_id: "t1", grants: [{ permission: "faces.view", effect: "allow", scope_type: "platform" }] });
    if (path === "/api/v1/faces/recognitions") return json({ items });
    return json({ items: [] });
  }));
}

const one = { event_id: "e1", person_id: "p1", person_name: "Ana", camera_id: "c1", camera_name: "Acceso", site_id: "s1", site_name: "Sede", server_name: "h01", start_time: "2026-11-02T10:05:07Z", has_thumbnail: false, has_snapshot: false };

describe("Recognitions export", () => {
  it("is disabled with no results and downloads a CSV of what is loaded", async () => {
    install([one]);
    const create = vi.fn(() => "blob:x");
    vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: create, revokeObjectURL: vi.fn() }));
    renderPage(Faces);
    fireEvent.click(await screen.findByRole("tab", { name: "Reconocimientos" }));
    const button = await screen.findByRole("button", { name: "Exportar CSV" });
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
    expect(create).toHaveBeenCalledTimes(1);
    const blob = (create.mock.calls as unknown as [Blob][])[0]![0];
    expect(blob.type).toContain("text/csv");
    expect(await blob.text()).toContain("2026-11-02 10:05:07,Ana,Acceso,Sede,h01");
  });

  it("stays disabled when there is nothing to export", async () => {
    install([]);
    renderPage(Faces);
    fireEvent.click(await screen.findByRole("tab", { name: "Reconocimientos" }));
    expect(await screen.findByRole("button", { name: "Exportar CSV" })).toBeDisabled();
  });
});
