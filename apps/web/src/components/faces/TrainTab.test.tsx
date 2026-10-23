import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { Faces } from "@/routes/Faces";
import { json, renderPage } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

const sightingID = "4b03f6ef-e00e-4a17-8d33-7f2d583a4ccd";
const sighting: Schemas["FaceReviewSighting"] = {
  id: sightingID, server_id: "s1", server_name: "Frigate 1", site_id: "site1",
  first_seen_at: "2026-10-22T10:00:00Z", seen_at: "2026-10-22T09:59:00Z", score: 0.41,
  crop_available: true,
};

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

    const id = await screen.findByText(new RegExp(sightingID));
    expect(id).toBeInTheDocument();
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
});
