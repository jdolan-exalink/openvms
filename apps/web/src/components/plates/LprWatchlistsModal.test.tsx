import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage } from "@/test-utils";
import { LprWatchlistsModal } from "./LprWatchlistsModal";
import { PlateReadCard } from "./PlateReadCard";

afterEach(() => vi.unstubAllGlobals());
beforeEach(() => sessionStorage.clear());

type Watchlist = Schemas["LprWatchlist"];
type WatchlistItem = Schemas["LprWatchlistItem"];

const mockWatchlists: Watchlist[] = [
  {
    id: "wl-1",
    tenant_id: "t1",
    name: "Autos Buscados",
    description: "Vehículos con pedido de captura",
    color: "#ef4444",
    item_count: 1,
    created_at: "2026-10-31T00:00:00Z",
    updated_at: "2026-10-31T00:00:00Z",
  },
  {
    id: "wl-2",
    tenant_id: "t1",
    name: "VIP",
    description: "Directores y gerencia",
    color: "#10b981",
    item_count: 0,
    created_at: "2026-10-31T00:00:00Z",
    updated_at: "2026-10-31T00:00:00Z",
  },
];

const mockItems: WatchlistItem[] = [
  {
    id: "item-1",
    tenant_id: "t1",
    watchlist_id: "wl-1",
    plate: "AA123BB",
    notes: "Robado en zona norte",
    vehicle_brand: "Toyota",
    vehicle_model: "Corolla",
    vehicle_color: "Gris",
    owner_name: "Juan Perez",
    created_at: "2026-10-31T00:00:00Z",
    updated_at: "2026-10-31T00:00:00Z",
  },
];

describe("LprWatchlistsModal", () => {
  it("renders watchlists and allows adding a new watchlist", async () => {
    let postBody: unknown = null;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (req: Request) => {
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/lpr/watchlists") {
          if (req.method === "GET") {
            return json({ items: mockWatchlists });
          }
          if (req.method === "POST") {
            postBody = JSON.parse(await req.text());
            return json({
              id: "wl-3",
              tenant_id: "t1",
              name: (postBody as { name: string }).name,
              description: "",
              color: "#3b82f6",
              item_count: 0,
              created_at: "2026-10-31T00:00:00Z",
              updated_at: "2026-10-31T00:00:00Z",
            }, 201);
          }
        }
        return json({ error: "not found" }, 404);
      }),
    );

    const onClose = vi.fn();
    renderPage(() => <LprWatchlistsModal canManage={true} onClose={onClose} />);

    expect(await screen.findByText("Autos Buscados")).toBeInTheDocument();
    expect(screen.getByText("VIP")).toBeInTheDocument();

    // Click "Nueva Lista"
    const newBtn = screen.getByRole("button", { name: /nueva lista/i });
    fireEvent.click(newBtn);

    const nameInput = screen.getByLabelText(/nombre de la lista/i);
    fireEvent.change(nameInput, { target: { value: "Acceso Prohibido" } });

    const submitBtn = screen.getByRole("button", { name: /aceptar/i });
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(postBody).toEqual({
        name: "Acceso Prohibido",
        color: "#ef4444",
      });
    });
  });

  it("expands watchlist and displays items and allows adding a plate", async () => {
    let postItemBody: unknown = null;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (req: Request) => {
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/lpr/watchlists") {
          return json({ items: mockWatchlists });
        }
        if (url.pathname === "/api/v1/lpr/watchlists/wl-1/items") {
          if (req.method === "GET") {
            return json({ items: mockItems });
          }
          if (req.method === "POST") {
            postItemBody = JSON.parse(await req.text());
            return json({
              id: "item-2",
              tenant_id: "t1",
              watchlist_id: "wl-1",
              plate: (postItemBody as { plate: string }).plate,
              created_at: "2026-10-31T00:00:00Z",
              updated_at: "2026-10-31T00:00:00Z",
            }, 201);
          }
        }
        return json({ error: "not found" }, 404);
      }),
    );

    const onClose = vi.fn();
    renderPage(() => <LprWatchlistsModal canManage={true} onClose={onClose} />);

    expect(await screen.findByText("Autos Buscados")).toBeInTheDocument();

    // Click to expand items
    const expandBtn = screen.getByRole("button", { name: /patentes en esta lista \(1\)/i });
    fireEvent.click(expandBtn);

    expect(await screen.findByText("AA123BB")).toBeInTheDocument();
    expect(screen.getByText("Toyota · Corolla · Gris")).toBeInTheDocument();

    // Add plate form
    const plateInput = screen.getByPlaceholderText(/ej: aa123bb/i);
    fireEvent.change(plateInput, { target: { value: "bb999cc" } });

    const addBtn = screen.getByRole("button", { name: /agregar patente/i });
    fireEvent.click(addBtn);

    await waitFor(() => {
      expect(postItemBody).toMatchObject({
        plate: "BB999CC",
      });
    });
  });
});

describe("PlateReadCard with watchlists", () => {
  it("renders watchlist badges when present on the plate read", async () => {
    const read: Schemas["PlateRead"] = {
      id: "read-1",
      camera_id: "cam-1",
      camera_name: "Acceso Principal",
      site_id: "site-1",
      site_name: "Planta",
      server_id: "srv-1",
      server_name: "Frigate-1",
      plate: "AA123BB",
      plate_normalized: "AA123BB",
      label: "car",
      zones: [],
      seen_at: "2026-10-31T10:00:00Z",
      watchlists: [
        { id: "wl-1", name: "Alerta Robo", color: "#ef4444" },
      ],
    };

    renderPage(() => <PlateReadCard read={read} showPhoto={false} />);

    expect(await screen.findByText("Alerta Robo")).toBeInTheDocument();
  });
});
