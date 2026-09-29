import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { routeTree } from "@/router";

afterEach(() => vi.unstubAllGlobals());

describe("primary navigation and context header", () => {
  it("uses a compact icon-first rail with an active route and context-aware heading", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": () => json({
        id: "u1", username: "operator", display_name: "Operator", tenant_id: "t1",
        mfa_enabled: false, must_change_password: false, auth_method: "session",
        grants: [{ permission: "events.view", effect: "allow", scope_type: "platform" }],
      }),
    })));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/events"] }), context: { queryClient: client } });
    render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>);

    await screen.findByRole("navigation", { name: "Navegación principal" });
    const rail = screen.getByRole("complementary", { name: "Primary Nav Rail" });
    expect(rail).toHaveClass("w-16");
    const eventsLink = screen.getByRole("link", { name: "Eventos" });
    expect(eventsLink).toHaveAttribute("aria-current", "page");
    expect(eventsLink).toHaveClass("bg-accent/15");
    expect(screen.getByLabelText("Encabezado de página")).toHaveTextContent(/Investigación.*Eventos/);
    expect(screen.getByRole("button", { name: "Cerrar sesión" })).toBeInTheDocument();
    const themeToggle = screen.getByRole("button", { name: /modo claro/i });
    expect(themeToggle).toBeInTheDocument();
    expect(themeToggle).not.toHaveTextContent(/Modo claro|Modo oscuro/);
  });

  it("marks the settings destination active on an unprefixed settings route", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": () => json({
        id: "u1", username: "operator", display_name: "Operator", tenant_id: "t1",
        mfa_enabled: false, must_change_password: false, auth_method: "session", grants: [],
      }),
      "/api/v1/sites": () => json({ items: [] }),
    })));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/sites"] }), context: { queryClient: client } });
    render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>);

    const configurationLink = await screen.findByRole("link", { name: "Configuración" });
    expect(configurationLink).toHaveAttribute("aria-current", "page");
    expect(configurationLink).toHaveClass("bg-accent/15");
  });

  it("uses the settings landing label for the settings route", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": () => json({
        id: "u1", username: "operator", display_name: "Operator", tenant_id: "t1",
        mfa_enabled: false, must_change_password: false, auth_method: "session", grants: [],
      }),
    })));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/settings"] }), context: { queryClient: client } });
    render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>);

    const header = await screen.findByLabelText("Encabezado de página");
    expect(header).toHaveTextContent(/Resumen/);
  });
  it("opens the app-wide /ws realtime feed once the authenticated shell renders", async () => {
    const sockets: string[] = [];
    class FakeSocket {
      constructor(url: string) {
        sockets.push(url);
      }
      close() {}
    }
    vi.stubGlobal("WebSocket", FakeSocket);
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": () => json({
        id: "u1", username: "operator", display_name: "Operator", tenant_id: "t1",
        mfa_enabled: false, must_change_password: false, auth_method: "session", grants: [],
      }),
    })));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/settings"] }), context: { queryClient: client } });
    render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>);

    await screen.findByLabelText("Encabezado de página");
    expect(sockets).toHaveLength(1);
    expect(sockets[0]).toMatch(/^wss?:\/\/.+\/ws$/);
  });
});
