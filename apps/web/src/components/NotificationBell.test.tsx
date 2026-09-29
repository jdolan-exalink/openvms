import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage } from "@/test-utils";
import { NotificationBell } from "./NotificationBell";

afterEach(() => vi.unstubAllGlobals());

function stub(unread: number) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      if (url.pathname === "/api/v1/notifications") {
        return json({
          unread_count: unread,
          items: [{ id: "n1", tenant_id: "t1", title: "Cámara caída", body: "Patio sin señal", severity: "critical", created_at: "2026-09-29T10:00:00Z", read_at: null, link: "/cameras" }],
        });
      }
      if (req.method === "POST") {
        calls.push(`POST ${url.pathname}`);
        return json({ updated: 1 });
      }
      return json({}, 404);
    }),
  );
  return calls;
}

describe("NotificationBell", () => {
  it("shows the unread badge", async () => {
    stub(3);
    renderPage(() => <NotificationBell />);
    expect(await screen.findByRole("button", { name: "Notificaciones, 3 sin leer" })).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("hides the badge when everything is read", async () => {
    stub(0);
    renderPage(() => <NotificationBell />);
    expect(await screen.findByRole("button", { name: "Notificaciones" })).toBeInTheDocument();
    expect(screen.queryByText("0")).not.toBeInTheDocument();
  });

  it("opens the panel and marks all as read", async () => {
    const calls = stub(1);
    renderPage(() => <NotificationBell />);
    fireEvent.click(await screen.findByRole("button", { name: /Notificaciones, 1 sin leer/ }));
    expect(await screen.findByText("Cámara caída")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Marcar todas como leídas" }));
    await waitFor(() => expect(calls).toContain("POST /api/v1/notifications/read-all"));
  });
});
