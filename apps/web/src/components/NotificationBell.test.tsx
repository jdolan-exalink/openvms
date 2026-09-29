import { fireEvent, screen, waitFor, within } from "@testing-library/react";
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

  it("moves focus into the panel, traps Tab and returns focus to the bell on Escape", async () => {
    stub(1);
    renderPage(() => <NotificationBell />);
    const bell = await screen.findByRole("button", { name: /Notificaciones, 1 sin leer/ });
    bell.focus();
    fireEvent.click(bell);
    const panel = await screen.findByRole("region", { name: "Panel de notificaciones" });
    await screen.findByText("Cámara caída");
    const markAll = within(panel).getByRole("button", { name: "Marcar todas como leídas" });
    await waitFor(() => expect(panel.contains(document.activeElement)).toBe(true));
    expect(bell).not.toHaveFocus();

    const items = Array.from(panel.querySelectorAll<HTMLElement>("button, a[href]")).filter((el) => !el.hasAttribute("disabled"));
    const last = items[items.length - 1] as HTMLElement;
    last.focus();
    fireEvent.keyDown(last, { key: "Tab" });
    expect(items[0]).toHaveFocus();
    fireEvent.keyDown(items[0] as HTMLElement, { key: "Tab", shiftKey: true });
    expect(last).toHaveFocus();
    expect(markAll).toBeInTheDocument();

    fireEvent.keyDown(document.activeElement as Element, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("region", { name: "Panel de notificaciones" })).not.toBeInTheDocument());
    expect(bell).toHaveFocus();
  });
});
