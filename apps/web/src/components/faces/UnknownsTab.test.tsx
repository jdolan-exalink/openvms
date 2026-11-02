import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { Faces } from "@/routes/Faces";
import { json, renderPage } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const ids = ["4b03f6ef-e00e-4a17-8d33-7f2d583a4cc1", "4b03f6ef-e00e-4a17-8d33-7f2d583a4cc2", "4b03f6ef-e00e-4a17-8d33-7f2d583a4cc3"];
const cluster = (over: Partial<Schemas["FaceUnknownCluster"]> = {}): Schemas["FaceUnknownCluster"] => ({
  id: ids[0]!, sighting_ids: ids, identity_ids: ids, sighting_count: 3, distinct_days: 2,
  first_seen_at: "2026-10-30T10:00:00Z", last_seen_at: "2026-11-01T10:00:00Z", server_ids: ["s1"], server_names: ["Frigate 1"],
  best_sighting_id: ids[1]!, recurrent: true, ...over,
});

type Call = { method: string; path: string; search: URLSearchParams; body?: unknown };

function install(clusters: Schemas["FaceUnknownCluster"][], failAssignAt = -1) {
  const calls: Call[] = [];
  let assigned = 0;
  vi.stubGlobal("fetch", vi.fn(async (raw: Request) => {
    const url = new URL(raw.url);
    const call: Call = { method: raw.method, path: url.pathname, search: url.searchParams };
    if (raw.method !== "GET") {
      const body = await raw.clone().text();
      call.body = body ? JSON.parse(body) : undefined;
    }
    calls.push(call);
    if (url.pathname === "/api/v1/me") return json({ id: "u1", username: "u1", display_name: "u1", mfa_enabled: false, must_change_password: false, auth_method: "session", tenant_id: "t1", grants: ["faces.view", "faces.manage"].map((permission) => ({ permission, effect: "allow", scope_type: "platform" })) });
    if (url.pathname === "/api/v1/faces/persons") return json({ items: [{ id: "p1", display_name: "Ana" }] });
    if (url.pathname === "/api/v1/faces/review-inbox/clusters") return json({ items: clusters });
    if (/^\/api\/v1\/faces\/review-inbox\/[^/]+\/assign$/.test(url.pathname)) {
      const n = assigned++;
      if (n === failAssignAt) return json({ code: "internal", message: "boom" }, 500);
      return json({ person: { id: "p-new", display_name: "Julián" }, image: {}, attempt_removed: true }, 201);
    }
    if (/^\/api\/v1\/faces\/review-inbox\/[^/]+$/.test(url.pathname) && raw.method === "DELETE") return new Response(null, { status: 204 });
    if (url.pathname.startsWith("/api/v1/faces/")) return json({ items: [] });
    return json({ code: "not_found", message: "not found" }, 404);
  }));
  return calls;
}

async function openUnknowns() {
  renderPage(Faces);
  fireEvent.click(await screen.findByRole("tab", { name: "Desconocidos" }));
}

describe("Unknown faces", () => {
  it("shows each group with its size, days and a recurrent flag, and asks for the rule it was given", async () => {
    const calls = install([cluster(), cluster({ id: "other", sighting_ids: ["x"], identity_ids: ["x"], sighting_count: 1, distinct_days: 1, recurrent: false })]);
    await openUnknowns();
    const items = await screen.findAllByRole("listitem");
    expect(within(items[0]!).getByText("3 apariciones")).toBeInTheDocument();
    expect(within(items[0]!).getByText("2 días")).toBeInTheDocument();
    expect(within(items[0]!).getByText("Recurrente")).toBeInTheDocument();
    expect(within(items[1]!).queryByText("Recurrente")).not.toBeInTheDocument();
    expect(within(items[0]!).getByRole("img")).toHaveAttribute("src", `/api/v1/faces/review-inbox/${ids[1]}/image`);
    const ask = calls.find((c) => c.path === "/api/v1/faces/review-inbox/clusters")!;
    expect(ask.search.get("min_appearances")).toBe("3");
    expect(ask.search.get("window_days")).toBe("7");
  });

  it("names a whole group: creates the person once and reuses it for the other appearances", async () => {
    const calls = install([cluster()]);
    await openUnknowns();
    await screen.findByRole("option", { name: "Ana" });
    fireEvent.change(await screen.findByLabelText("Asignar a"), { target: { value: "__new__" } });
    fireEvent.change(screen.getByLabelText("Nombre de la nueva persona"), { target: { value: "Julián" } });
    fireEvent.click(screen.getByRole("button", { name: "Dar de alta" }));
    await waitFor(() => expect(calls.filter((c) => c.path.endsWith("/assign"))).toHaveLength(3));
    const bodies = calls.filter((c) => c.path.endsWith("/assign")).map((c) => c.body);
    expect(bodies).toEqual([{ new_person_display_name: "Julián" }, { person_id: "p-new" }, { person_id: "p-new" }]);
    expect(await screen.findByText("Julián quedó en la base con 3 apariciones.")).toBeInTheDocument();
  });

  it("says how many were assigned when one fails, and does not create a second person", async () => {
    const calls = install([cluster()], 1);
    await openUnknowns();
    await screen.findByRole("option", { name: "Ana" });
    fireEvent.change(await screen.findByLabelText("Asignar a"), { target: { value: "p1" } });
    fireEvent.click(screen.getByRole("button", { name: "Dar de alta" }));
    expect(await screen.findByText("Se asignaron 1 de 3 apariciones; reintentá para el resto.")).toBeInTheDocument();
    expect(calls.filter((c) => c.path.endsWith("/assign"))).toHaveLength(2);
  });

  it("asks before discarding a group, then deletes every appearance", async () => {
    const calls = install([cluster()]);
    await openUnknowns();
    fireEvent.click(await screen.findByRole("button", { name: "Descartar" }));
    expect(screen.getByText("Se borran las fotos de 3 apariciones. No se puede deshacer.")).toBeInTheDocument();
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Descartar" }));
    await waitFor(() => expect(calls.filter((c) => c.method === "DELETE")).toHaveLength(3));
  });

  it("deletes several selected groups only after a confirmation that counts them", async () => {
    const other = ["4b03f6ef-e00e-4a17-8d33-7f2d583a4cd1", "4b03f6ef-e00e-4a17-8d33-7f2d583a4cd2"];
    const calls = install([
      cluster(),
      cluster({ id: "g2", sighting_ids: other, identity_ids: other, sighting_count: 2, best_sighting_id: other[0]!, recurrent: false }),
      cluster({ id: "g3", sighting_ids: ["keep"], identity_ids: ["keep"], sighting_count: 1, best_sighting_id: "keep", recurrent: false }),
    ]);
    await openUnknowns();
    fireEvent.click(await screen.findByRole("button", { name: "Seleccionar" }));
    const boxes = screen.getAllByRole("checkbox", { name: "Seleccionar grupo" });
    fireEvent.click(boxes[0]!);
    fireEvent.click(boxes[1]!);
    expect(screen.getByText("2 seleccionados")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Borrar seleccionados" }));
    expect(screen.getByText("Se borran las fotos de 5 apariciones en 2 grupos. No se puede deshacer.")).toBeInTheDocument();
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Borrar seleccionados" }));
    await waitFor(() => expect(calls.filter((c) => c.method === "DELETE")).toHaveLength(5));
    expect(calls.some((c) => c.method === "DELETE" && c.path.endsWith("/keep"))).toBe(false);
  });

  it("keeps everything when the confirmation is cancelled", async () => {
    const calls = install([cluster()]);
    await openUnknowns();
    fireEvent.click(await screen.findByRole("button", { name: "Seleccionar" }));
    fireEvent.click(screen.getByRole("button", { name: "Seleccionar todo" }));
    fireEvent.click(screen.getByRole("button", { name: "Borrar seleccionados" }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancelar" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
  });

  it("sends the recurrence rule the operator typed", async () => {
    const calls = install([cluster()]);
    await openUnknowns();
    fireEvent.change(await screen.findByLabelText("Apariciones para marcar como recurrente"), { target: { value: "5" } });
    fireEvent.change(screen.getByLabelText("En los últimos (días)"), { target: { value: "14" } });
    await waitFor(() => {
      const last = calls.filter((c) => c.path === "/api/v1/faces/review-inbox/clusters").at(-1)!;
      expect(last.search.get("min_appearances")).toBe("5");
      expect(last.search.get("window_days")).toBe("14");
    });
  });
});
