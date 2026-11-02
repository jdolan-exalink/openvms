import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Faces } from "@/routes/Faces";
import { json, renderPage } from "@/test-utils";

afterEach(() => vi.unstubAllGlobals());

function install() {
  const calls: { path: string; search: URLSearchParams }[] = [];
  vi.stubGlobal("fetch", vi.fn(async (raw: Request) => {
    const url = new URL(raw.url);
    calls.push({ path: url.pathname, search: url.searchParams });
    if (url.pathname === "/api/v1/me") return json({ id: "u1", username: "u1", display_name: "u1", mfa_enabled: false, must_change_password: false, auth_method: "session", tenant_id: "t1", grants: [{ permission: "faces.view", effect: "allow", scope_type: "platform" }] });
    if (url.pathname === "/api/v1/faces/recognitions") {
      return json({ items: [{ event_id: "e1", person_id: "p1", person_name: "Marcos Díaz", camera_id: "c1", camera_name: "Acceso principal", site_id: "s1", site_name: "Sede", server_name: "h01", start_time: "2026-11-02T10:05:00Z", has_thumbnail: true, has_snapshot: false }] });
    }
    if (url.pathname === "/api/v1/faces/review-inbox") {
      return json({ items: [{ id: "u1", identity_id: "u1", server_id: "v1", server_name: "Frigate 1", site_id: "s1", first_seen_at: "2026-11-02T10:08:00Z", crop_available: true }] });
    }
    return json({ items: [] });
  }));
  return calls;
}

describe("Live faces", () => {
  it("opens on the live tab with recognized and unknown faces on one timeline, newest first", async () => {
    const calls = install();
    renderPage(Faces);
    const list = await screen.findByRole("list");
    const rows = await within(list).findAllByRole("listitem");
    expect(rows).toHaveLength(2);
    expect(within(rows[0]!).getByText("Rostro sin nombre")).toBeInTheDocument();
    expect(within(rows[0]!).getByText("Desconocido")).toBeInTheDocument();
    expect(within(rows[0]!).getByText(/Frigate 1/)).toBeInTheDocument();
    expect(within(rows[1]!).getByText("Marcos Díaz")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("Reconocido")).toBeInTheDocument();
    expect(within(rows[1]!).getByText(/Acceso principal · Sede/)).toBeInTheDocument();
    expect(calls.some((c) => c.path === "/api/v1/faces/recognitions")).toBe(true);
  });

  it("enlarges a face from the timeline", async () => {
    install();
    renderPage(Faces);
    fireEvent.click(await screen.findByRole("button", { name: "Ampliar foto: Marcos Díaz" }));
    const dialog = screen.getByRole("dialog", { name: "Marcos Díaz" });
    expect(dialog.querySelector("img")).toHaveAttribute("src", "/api/v1/events/e1/thumbnail");
  });

  it("filters by state", async () => {
    install();
    renderPage(Faces);
    await screen.findByText("Marcos Díaz");
    fireEvent.click(screen.getByRole("button", { name: "Reconocidos" }));
    expect(screen.getByText("Marcos Díaz")).toBeInTheDocument();
    expect(screen.queryByText("Rostro sin nombre")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Desconocidos" }));
    expect(screen.queryByText("Marcos Díaz")).not.toBeInTheDocument();
    expect(screen.getByText("Rostro sin nombre")).toBeInTheDocument();
  });

  it("pauses the list and resumes it", async () => {
    install();
    renderPage(Faces);
    await screen.findByText("Marcos Díaz");
    const pause = screen.getByRole("button", { name: "Pausar" });
    fireEvent.click(pause);
    expect(screen.getByRole("button", { name: "Reanudar" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "Reanudar" }));
    expect(screen.getByRole("button", { name: "Pausar" })).toHaveAttribute("aria-pressed", "false");
  });
});
