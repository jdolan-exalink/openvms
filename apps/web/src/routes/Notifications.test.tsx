import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage } from "@/test-utils";
import { Notifications } from "./Notifications";

afterEach(() => vi.unstubAllGlobals());

function make(id: string, overrides: Partial<Schemas["Notification"]> = {}): Schemas["Notification"] {
  return {
    id, tenant_id: "t1", title: `Título ${id}`, body: `Detalle ${id}`, severity: "warning",
    created_at: "2026-09-29T10:00:00Z", read_at: null, ...overrides,
  };
}

function stub(items: Schemas["Notification"][]) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      if (url.pathname === "/api/v1/notifications") {
        calls.push(`GET ${url.search}`);
        const unreadOnly = url.searchParams.get("unread_only") === "true";
        const shown = unreadOnly ? items.filter((i) => !i.read_at) : items;
        return json({ items: shown, unread_count: items.filter((i) => !i.read_at).length });
      }
      if (req.method === "POST") {
        calls.push(`POST ${url.pathname}`);
        return json(url.pathname.endsWith("read-all") ? { updated: 2 } : make("n1", { read_at: "2026-09-29T11:00:00Z" }));
      }
      return json({}, 404);
    }),
  );
  return calls;
}

describe("Notifications route", () => {
  it("lists notifications and marks one as read", async () => {
    const calls = stub([make("n1"), make("n2", { read_at: "2026-09-29T10:30:00Z" })]);
    renderPage(() => <Notifications />);
    expect(await screen.findByText("Título n1")).toBeInTheDocument();
    expect(screen.getByText("Título n2")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /Marcar como leída/ })).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: /Marcar como leída/ }));
    await waitFor(() => expect(calls).toContain("POST /api/v1/notifications/n1/read"));
  });

  it("filters unread only", async () => {
    const calls = stub([make("n1"), make("n2", { read_at: "2026-09-29T10:30:00Z" })]);
    renderPage(() => <Notifications />);
    await screen.findByText("Título n2");
    fireEvent.click(screen.getByRole("checkbox", { name: /Solo sin leer/ }));
    await waitFor(() => expect(screen.queryByText("Título n2")).not.toBeInTheDocument());
    expect(calls.some((c) => c.includes("unread_only=true"))).toBe(true);
  });

  it("marks all as read", async () => {
    const calls = stub([make("n1"), make("n2")]);
    renderPage(() => <Notifications />);
    await screen.findByText("Título n1");
    fireEvent.click(screen.getByRole("button", { name: "Marcar todas como leídas" }));
    await waitFor(() => expect(calls).toContain("POST /api/v1/notifications/read-all"));
  });

  it("shows an empty state", async () => {
    stub([]);
    renderPage(() => <Notifications />);
    expect(await screen.findByText(/No hay notificaciones/)).toBeInTheDocument();
  });
});
