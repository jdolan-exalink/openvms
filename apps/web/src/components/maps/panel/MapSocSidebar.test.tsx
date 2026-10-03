import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json } from "@/test-utils";
import { MapSocSidebar } from "./MapSocSidebar";

afterEach(() => vi.unstubAllGlobals());

const read = {
  id: "r1",
  site_id: "site1",
  site_name: "Site",
  server_id: "srv1",
  server_name: "Casa",
  camera_id: "cam1",
  camera_name: "Cementerio",
  plate: "AB123CD",
  plate_normalized: "AB123CD",
  score: 0.9,
  label: "car",
  zones: [],
  seen_at: new Date().toISOString(),
  event_id: null,
};

function renderLpr() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MapSocSidebar
        open
        onToggle={vi.fn()}
        alarmCount={0}
        showAlarms
        showLpr
        siteId="site1"
        onSelectCamera={vi.fn()}
        cameras={null}
        alarms={null}
      />
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole("tab", { name: "LPR" }));
}

describe("map LPR list", () => {
  it("asks only for the current local day and keeps the pager under the list", async () => {
    let seen: URL | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/lpr/reads") {
          seen = url;
          return json({ items: [read] });
        }
        return json({ items: [] });
      }),
    );
    renderLpr();
    expect(await screen.findByRole("list", { name: "Lecturas LPR" })).toBeInTheDocument();
    const start = new Date();
    start.setHours(0, 0, 0, 0);
    const end = new Date(start);
    end.setDate(end.getDate() + 1);
    expect(seen?.searchParams.get("from")).toBe(start.toISOString());
    expect(seen?.searchParams.get("to")).toBe(end.toISOString());
    expect(seen?.searchParams.get("limit")).toBe("8");
    expect(screen.getByText(/Hoy · Página 1/)).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Lectura de patente AB123CD" })).toHaveAttribute("src", "/media/v1/lpr/reads/r1/snapshot.jpg?crop=1&quality=55");
  });

  it("shows the snapshot while the pointer is over a read", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json({ items: [read] })));
    renderLpr();
    const card = await screen.findByRole("button", { name: /AB123CD/ });
    fireEvent.mouseEnter(card.closest("li")!);
    const photo = await screen.findByRole("img", { name: "Captura de AB123CD" });
    expect(photo).toHaveAttribute("src", "/media/v1/lpr/reads/r1/snapshot.jpg?crop=1&quality=55");
    fireEvent.mouseLeave(card.closest("li")!);
    await waitFor(() => expect(screen.queryByRole("img", { name: /Captura/ })).not.toBeInTheDocument());
  });
});
