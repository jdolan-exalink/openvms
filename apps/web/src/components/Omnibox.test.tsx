import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage } from "@/test-utils";
import { Omnibox } from "./Omnibox";

afterEach(() => vi.unstubAllGlobals());

type SearchResult = Schemas["SearchResult"];

function makeSearchResult(overrides: Partial<SearchResult> = {}): SearchResult {
  return {
    cameras: [],
    sites: [],
    servers: [],
    events: [],
    plates: [],
    ...overrides,
  };
}

describe("Omnibox", () => {
  it("does not render when isOpen is false", () => {
    renderPage(() => <Omnibox isOpen={false} onClose={() => {}} />);
    expect(screen.queryByRole("dialog", { name: "Búsqueda global" })).not.toBeInTheDocument();
  });

  it("renders search dialog, input and closes on Escape", async () => {
    const onClose = vi.fn();
    renderPage(() => <Omnibox isOpen={true} onClose={onClose} />);

    const dialog = await screen.findByRole("dialog", { name: "Búsqueda global" });
    expect(dialog).toHaveAttribute("aria-modal", "true");

    const input = await screen.findByRole("combobox", { name: "Buscar en OpenVMS" });
    expect(input).toBeInTheDocument();

    fireEvent.keyDown(input, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("shows prompt when query length < 2", async () => {
    renderPage(() => <Omnibox isOpen={true} onClose={() => {}} />);
    expect(
      await screen.findByText(/Escribí al menos 2 caracteres para buscar en OpenVMS/i),
    ).toBeInTheDocument();
  });

  it("searches and renders grouped results and handles keyboard navigation", async () => {
    const mockData = makeSearchResult({
      cameras: [
        {
          id: "c1",
          display_name: "Cámara Acceso Norte",
          site_id: "s1",
          site_name: "Sitio Central",
          location: "Portón 1",
          status: "ok",
        },
      ],
      sites: [
        {
          id: "s1",
          name: "Sitio Central",
          camera_count: 5,
        },
      ],
      plates: [
        {
          plate: "ABC-123",
          camera_id: "c1",
          camera_name: "Cámara Acceso Norte",
          site_name: "Sitio Central",
          seen_at: "2026-09-29T10:00:00Z",
        },
      ],
    });

    vi.stubGlobal(
      "fetch",
      vi.fn(async (req: Request) => {
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/search") {
          return json(mockData);
        }
        return json({}, 404);
      }),
    );

    const onClose = vi.fn();
    const { router } = renderPage(() => <Omnibox isOpen={true} onClose={onClose} />);

    const input = await screen.findByRole("combobox", { name: "Buscar en OpenVMS" });
    fireEvent.change(input, { target: { value: "Acceso" } });

    // Wait for debounced search results
    await waitFor(() => {
      expect(screen.getByText("Cámaras")).toBeInTheDocument();
    });

    expect(screen.getByText("Cámara Acceso Norte")).toBeInTheDocument();
    expect(screen.getByText("Sitio Central • Portón 1")).toBeInTheDocument();
    expect(screen.getByText("Sitios")).toBeInTheDocument();
    expect(screen.getByText("5 cámaras")).toBeInTheDocument();
    expect(screen.getByText("Patentes")).toBeInTheDocument();
    expect(screen.getByText("ABC-123")).toBeInTheDocument();

    // Test ArrowDown navigation
    fireEvent.keyDown(input, { key: "ArrowDown" });
    const options = screen.getAllByRole("option");
    expect(options[1]).toHaveAttribute("aria-selected", "true");

    // Test Enter key selects item and navigates
    const navigateSpy = vi.spyOn(router, "navigate");
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onClose).toHaveBeenCalled();
    expect(navigateSpy).toHaveBeenCalledWith(
      expect.objectContaining({
        to: "/cameras",
        search: { site_id: "s1" },
      }),
    );
  });

  it("shows empty state when no results match", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (req: Request) => {
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/search") {
          return json(makeSearchResult());
        }
        return json({}, 404);
      }),
    );

    renderPage(() => <Omnibox isOpen={true} onClose={() => {}} />);

    const input = await screen.findByRole("combobox", { name: "Buscar en OpenVMS" });
    fireEvent.change(input, { target: { value: "nonexistent" } });

    await waitFor(() => {
      expect(screen.getByText(/No se encontraron resultados para/i)).toBeInTheDocument();
    });
  });
});
