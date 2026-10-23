import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { Faces } from "@/routes/Faces";
import { json, renderPage } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const sightingID = "4b03f6ef-e00e-4a17-8d33-7f2d583a4ccd";
const sightingID2 = "11f15391-cea3-4263-9827-ced0ecb84276";
const groupID = "8690c9eb-1d61-4254-8739-7a79c6ef72e1";
const sighting: Schemas["FaceReviewSighting"] = {
  id: sightingID, identity_id: sightingID, server_id: "s1", server_name: "Frigate 1", site_id: "site1",
  first_seen_at: "2026-10-22T10:00:00Z", seen_at: "2026-10-22T09:59:00Z", score: 0.41,
  crop_available: true,
};
const secondSighting: Schemas["FaceReviewSighting"] = { ...sighting, id: sightingID2, seen_at: "2026-10-22T10:01:00Z" };

describe("Faces review inbox", () => {
  it("shows a stable ID and uses it for image, assignment and rejection", async () => {
    const calls: { method: string; path: string; body?: unknown }[] = [];
    vi.stubGlobal("fetch", vi.fn(async (raw: Request) => {
      const url = new URL(raw.url);
      const call = { method: raw.method, path: url.pathname };
      if (raw.method !== "GET") {
        const body = await raw.clone().text();
        calls.push({ ...call, body: body ? JSON.parse(body) : undefined });
      } else calls.push(call);
      if (url.pathname === "/api/v1/me") return json({ id: "u1", username: "u1", display_name: "u1", mfa_enabled: false, must_change_password: false, auth_method: "session", tenant_id: "t1", grants: ["faces.view", "faces.manage"].map((permission) => ({ permission, effect: "allow", scope_type: "platform" })) });
      if (url.pathname === "/api/v1/faces/persons") return json({ items: [{ id: "p1", display_name: "Ana" }] });
      if (url.pathname === "/api/v1/faces/review-inbox") return json({ items: [sighting] });
      if (url.pathname === `/api/v1/faces/review-inbox/${sightingID}/assign`) return json({ person: { id: "p1", display_name: "Ana" }, image: {}, attempt_removed: true }, 201);
      if (url.pathname === `/api/v1/faces/review-inbox/${sightingID}` && raw.method === "DELETE") return new Response(null, { status: 204 });
      if (url.pathname.startsWith("/api/v1/faces/review-inbox/")) return new Response(null, { status: 200, headers: { "Content-Type": "image/png" } });
      return json({ code: "not_found", message: "not found" }, 404);
    }));

    renderPage(Faces);
    fireEvent.click(await screen.findByRole("tab", { name: "Entrenar" }));

    expect(await screen.findAllByText(new RegExp(sightingID))).toHaveLength(2);
    const image = screen.getByRole("img", { name: /intento de reconocimiento/i });
    expect(image).toHaveAttribute("src", `/api/v1/faces/review-inbox/${sightingID}/image`);

    fireEvent.change(screen.getByLabelText("Asignar a"), { target: { value: "p1" } });
    fireEvent.click(screen.getByRole("button", { name: "Asignar" }));
    await waitFor(() => expect(calls).toContainEqual({
      method: "POST", path: `/api/v1/faces/review-inbox/${sightingID}/assign`, body: { person_id: "p1" },
    }));

    fireEvent.click(screen.getByRole("button", { name: "Rechazar" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Rechazar rostro" }));
    await waitFor(() => expect(calls).toContainEqual({ method: "DELETE", path: `/api/v1/faces/review-inbox/${sightingID}` }));
  });

  it("groups two selected sightings only after the manager confirms the action", async () => {
    const calls: { method: string; path: string; body?: unknown }[] = [];
    let grouped = false;
    vi.stubGlobal("fetch", vi.fn(async (raw: Request) => {
      const url = new URL(raw.url);
      const call = { method: raw.method, path: url.pathname };
      if (raw.method !== "GET") {
        const body = await raw.clone().text();
        calls.push({ ...call, body: body ? JSON.parse(body) : undefined });
      }
      if (url.pathname === "/api/v1/me") return json({ id: "u1", username: "u1", display_name: "u1", mfa_enabled: false, must_change_password: false, auth_method: "session", tenant_id: "t1", grants: ["faces.view", "faces.manage"].map((permission) => ({ permission, effect: "allow", scope_type: "platform" })) });
      if (url.pathname === "/api/v1/faces/persons") return json({ items: [] });
      if (url.pathname === "/api/v1/faces/review-inbox") return json({ items: grouped
        ? [{ ...sighting, identity_id: groupID, group_id: groupID }, { ...secondSighting, identity_id: groupID, group_id: groupID }]
        : [sighting, secondSighting] });
      if (url.pathname === "/api/v1/faces/review-inbox/groups" && raw.method === "POST") {
        grouped = true;
        return json({ id: groupID, member_sighting_ids: [sightingID, sightingID2], created_at: "2026-10-22T10:02:00Z" }, 201);
      }
      if (url.pathname.startsWith("/api/v1/faces/review-inbox/")) return new Response(new Uint8Array(), { status: 200, headers: { "Content-Type": "image/png" } });
      return json({ code: "not_found", message: "not found" }, 404);
    }));

    renderPage(Faces);
    fireEvent.click(await screen.findByRole("tab", { name: "Entrenar" }));
    const first = await screen.findByRole("checkbox", { name: new RegExp(sightingID) });
    const second = screen.getByRole("checkbox", { name: new RegExp(sightingID2) });
    const groupButton = screen.getByRole("button", { name: "Agrupar recortes seleccionados" });
    expect(groupButton).toBeDisabled();
    fireEvent.click(first);
    expect(groupButton).toBeDisabled();
    fireEvent.click(second);
    expect(groupButton).toBeEnabled();
    fireEvent.click(groupButton);
    await waitFor(() => expect(calls).toContainEqual({
      method: "POST", path: "/api/v1/faces/review-inbox/groups", body: { sighting_ids: [sightingID, sightingID2] },
    }));
    expect(await screen.findAllByText(new RegExp(groupID))).toHaveLength(2);
  });
  it("rejects every selected sighting only after the manager confirms", async () => {
    const calls: { method: string; path: string }[] = [];
    let remaining = [sighting, secondSighting];
    vi.stubGlobal("fetch", vi.fn(async (raw: Request) => {
      const url = new URL(raw.url);
      if (raw.method === "DELETE") {
        calls.push({ method: raw.method, path: url.pathname });
        remaining = remaining.filter((s) => url.pathname !== `/api/v1/faces/review-inbox/${s.id}`);
        return new Response(null, { status: 204 });
      }
      if (url.pathname === "/api/v1/me") return json({ id: "u1", username: "u1", display_name: "u1", mfa_enabled: false, must_change_password: false, auth_method: "session", tenant_id: "t1", grants: ["faces.view", "faces.manage"].map((permission) => ({ permission, effect: "allow", scope_type: "platform" })) });
      if (url.pathname === "/api/v1/faces/persons") return json({ items: [] });
      if (url.pathname === "/api/v1/faces/review-inbox") return json({ items: remaining });
      if (url.pathname.startsWith("/api/v1/faces/review-inbox/")) return new Response(new Uint8Array(), { status: 200, headers: { "Content-Type": "image/png" } });
      return json({ code: "not_found", message: "not found" }, 404);
    }));

    renderPage(Faces);
    fireEvent.click(await screen.findByRole("tab", { name: "Entrenar" }));
    const rejectSelected = await screen.findByRole("button", { name: "Rechazar seleccionados" });
    expect(rejectSelected).toBeDisabled();
    fireEvent.click(await screen.findByRole("checkbox", { name: new RegExp(sightingID) }));
    fireEvent.click(screen.getByRole("checkbox", { name: new RegExp(sightingID2) }));
    expect(rejectSelected).toBeEnabled();
    fireEvent.click(rejectSelected);
    const dialog = await screen.findByRole("dialog");
    expect(calls).toEqual([]);
    fireEvent.click(within(dialog).getByRole("button", { name: "Rechazar rostros" }));
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(calls).toContainEqual({ method: "DELETE", path: `/api/v1/faces/review-inbox/${sightingID}` });
    expect(calls).toContainEqual({ method: "DELETE", path: `/api/v1/faces/review-inbox/${sightingID2}` });
    await waitFor(() => expect(screen.queryByRole("checkbox", { name: new RegExp(sightingID) })).toBeNull());
  });

  it("offers a similarity suggestion that groups only when the manager accepts it", async () => {
    const calls: { method: string; path: string; body?: unknown }[] = [];
    vi.stubGlobal("fetch", vi.fn(async (raw: Request) => {
      const url = new URL(raw.url);
      if (raw.method !== "GET") {
        const body = await raw.clone().text();
        calls.push({ method: raw.method, path: url.pathname, body: body ? JSON.parse(body) : undefined });
      }
      if (url.pathname === "/api/v1/me") return json({ id: "u1", username: "u1", display_name: "u1", mfa_enabled: false, must_change_password: false, auth_method: "session", tenant_id: "t1", grants: ["faces.view", "faces.manage"].map((permission) => ({ permission, effect: "allow", scope_type: "platform" })) });
      if (url.pathname === "/api/v1/faces/persons") return json({ items: [] });
      if (url.pathname === "/api/v1/faces/review-inbox") return json({ items: [
        { ...sighting, suggestions: [{ identity_id: sightingID2, similarity: 0.874 }] },
        { ...secondSighting, identity_id: sightingID2, suggestions: [{ identity_id: sightingID, similarity: 0.874 }] },
      ] });
      if (url.pathname === "/api/v1/faces/review-inbox/groups" && raw.method === "POST") return json({ group_id: groupID, member_count: 2 });
      if (url.pathname.startsWith("/api/v1/faces/review-inbox/")) return new Response(new Uint8Array(), { status: 200, headers: { "Content-Type": "image/png" } });
      return json({ code: "not_found", message: "not found" }, 404);
    }));

    renderPage(Faces);
    fireEvent.click(await screen.findByRole("tab", { name: "Entrenar" }));
    const matches = await screen.findAllByText(/Posible coincidencia/);
    expect(matches).toHaveLength(2);
    expect(matches[0]).toHaveTextContent("87%");
    expect(calls).toEqual([]);

    fireEvent.click(screen.getAllByRole("button", { name: "No es la misma persona" })[1] as HTMLElement);
    expect(screen.getAllByText(/Posible coincidencia/)).toHaveLength(1);
    await waitFor(() => expect(calls).toContainEqual({
      method: "POST", path: "/api/v1/faces/review-inbox/suggestion-decisions",
      body: { identity_id: sightingID2, other_identity_id: sightingID, decision: "different", similarity: 0.874 },
    }));

    fireEvent.click(screen.getByRole("button", { name: "Agrupar juntos" }));
    await waitFor(() => expect(calls).toContainEqual({
      method: "POST", path: "/api/v1/faces/review-inbox/groups", body: { sighting_ids: [sightingID, sightingID2] },
    }));
    await waitFor(() => expect(calls).toContainEqual({
      method: "POST", path: "/api/v1/faces/review-inbox/suggestion-decisions",
      body: { identity_id: sightingID, other_identity_id: sightingID2, decision: "same", similarity: 0.874 },
    }));
  });
});
