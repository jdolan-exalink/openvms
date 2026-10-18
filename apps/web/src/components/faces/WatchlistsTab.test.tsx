import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { Faces } from "@/routes/Faces";
import { json, renderPage } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());
beforeEach(() => sessionStorage.clear());

const person = (id: string, name: string): Schemas["FacePerson"] => ({
  id, tenant_id: "t1", display_name: name, frigate_name: name.toLowerCase(), notes: "", image_count: 1,
  sync: { total: 0, synced: 0, pending: 0, failed: 0, conflict: 0, disabled: 0 }, created_at: "2026-10-18T10:00:00Z", updated_at: "2026-10-18T10:00:00Z",
});

type Call = { method: string; path: string; body?: unknown };

function install(permissions: string[], lists: Schemas["FaceWatchlist"][], failDelete = false, failPersons = false) {
  const calls: Call[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (raw: Request) => {
      const url = new URL(raw.url);
      const call: Call = { method: raw.method, path: url.pathname };
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
      if (p === "/api/v1/faces/persons") {
        return failPersons ? json({ code: "internal", message: "boom" }, 500) : json({ items: [person("p1", "Ana"), person("p2", "Beto")] });
      }
      if (p === "/api/v1/faces/watchlists" && raw.method === "GET") return json({ items: lists });
      if (p === "/api/v1/faces/watchlists" && raw.method === "POST") return json({ ...lists[0], id: "w9", name: "Nueva" }, 201);
      if (p === "/api/v1/faces/watchlists/w1" && raw.method === "PATCH") return json(lists[0]);
      if (p === "/api/v1/faces/watchlists/w1" && raw.method === "DELETE") {
        return failDelete ? json({ code: "conflict", message: "the watchlist is used by 1 alarm rule(s)" }, 409) : new Response(null, { status: 204 });
      }
      if (p.startsWith("/api/v1/faces/")) return json({ items: [] });
      return json({ code: "not_found", message: "not found" }, 404);
    }),
  );
  return calls;
}

const list = (over: Partial<Schemas["FaceWatchlist"]> = {}): Schemas["FaceWatchlist"] => ({
  id: "w1", tenant_id: "t1", name: "VIP", description: "recepción", person_ids: ["p1"],
  created_at: "2026-10-18T10:00:00Z", updated_at: "2026-10-18T10:00:00Z", ...over,
});

async function openLists() {
  renderPage(Faces);
  fireEvent.click(await screen.findByRole("tab", { name: "Listas" }));
}

describe("Faces watchlists tab", () => {
  it("shows the lists with their members and no editing to a viewer", async () => {
    install(["faces.view"], [list()]);
    await openLists();
    const items = await screen.findByRole("list", { name: "Listas de personas" });
    expect(within(items).getByText("VIP")).toBeInTheDocument();
    expect(within(items).getByText(/1 personas/)).toBeInTheDocument();
    expect(within(items).getByText(/Ana/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Nueva lista" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Editar VIP/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Eliminar VIP/ })).not.toBeInTheDocument();
  });

  it("creates a list with the chosen people", async () => {
    const calls = install(["faces.view", "faces.manage"], [list()]);
    await openLists();
    fireEvent.click(await screen.findByRole("button", { name: "Nueva lista" }));
    fireEvent.change(screen.getByLabelText("Nombre de la lista"), { target: { value: "Visitas" } });
    fireEvent.click(await screen.findByRole("checkbox", { name: "Beto" }));
    fireEvent.click(screen.getByRole("button", { name: "Crear lista" }));
    await waitFor(() => expect(calls.find((c) => c.method === "POST" && c.path === "/api/v1/faces/watchlists")?.body).toEqual({
      name: "Visitas", description: "", person_ids: ["p2"],
    }));
  });

  it("sends only the changed fields when editing", async () => {
    const calls = install(["faces.view", "faces.manage"], [list()]);
    await openLists();
    fireEvent.click(await screen.findByRole("button", { name: "Editar VIP" }));
    expect(await screen.findByRole("checkbox", { name: "Ana" })).toBeChecked();
    fireEvent.click(screen.getByRole("checkbox", { name: "Beto" }));
    fireEvent.click(screen.getByRole("button", { name: "Guardar lista" }));
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ person_ids: ["p1", "p2"] }));
  });

  it("keeps the members when the people cannot be loaded", async () => {
    const calls = install(["faces.view", "faces.manage"], [list()], false, true);
    await openLists();
    fireEvent.click(await screen.findByRole("button", { name: "Editar VIP" }));
    fireEvent.change(screen.getByLabelText("Nombre de la lista"), { target: { value: "VIP 2" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar lista" }));
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ name: "VIP 2" }));
  });

  it("confirms before deleting and shows why a list in use cannot be deleted", async () => {
    const calls = install(["faces.view", "faces.manage"], [list()], true);
    await openLists();
    fireEvent.click(await screen.findByRole("button", { name: "Eliminar VIP" }));
    const dialog = await screen.findByRole("dialog");
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
    fireEvent.click(within(dialog).getByRole("button", { name: "Eliminar lista" }));
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/v1/faces/watchlists/w1")).toBe(true));
    expect(await within(dialog).findByText(/used by 1 alarm rule/)).toBeInTheDocument();
  });
});
