import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { routeTree } from "@/router";

afterEach(() => vi.unstubAllGlobals());

describe("primary navigation and context header", () => {
  it("hides the tenant notification bell for the tenant-less platform admin", async () => {
    const fetchMock = vi.fn(stubApi({
      "/api/v1/me": () => json({
        id: "u1", username: "admin", display_name: "Admin", tenant_id: null,
        mfa_enabled: false, must_change_password: false, auth_method: "session",
        grants: [{ permission: "events.view", effect: "allow", scope_type: "platform" }],
      }),
    }));
    vi.stubGlobal("fetch", fetchMock);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/events"] }), context: { queryClient: client } });
    render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>);

    await screen.findByRole("navigation", { name: "Navegación principal" });
    // Notifications are tenant-scoped: the platform admin has no tenant, so polling the
    // endpoint would only ever produce a 403 in the console.
    expect(screen.queryByRole("button", { name: "Notificaciones" })).not.toBeInTheDocument();
    expect(fetchMock.mock.calls.some(([req]) => String((req as Request).url).includes("/notifications"))).toBe(false);
  });

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
    // No rail on mobile: the bottom navigation takes over below md.
    expect(rail).toHaveClass("hidden", "md:flex");
    const eventsLink = within(rail).getByRole("link", { name: "Eventos" });
    expect(eventsLink).toHaveAttribute("aria-current", "page");
    // Icon-only rail: the label is the accessible name and a tooltip, never visible text.
    expect(eventsLink).toHaveAttribute("aria-label", "Eventos");
    expect(eventsLink).not.toHaveTextContent("Eventos");
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.focus(eventsLink);
    const tip = await screen.findByRole("tooltip");
    expect(tip).toHaveTextContent("Eventos");
    expect(eventsLink).toHaveAttribute("aria-describedby", tip.id);
    fireEvent.keyDown(eventsLink, { key: "Escape" });
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.blur(eventsLink);
    expect(screen.getByLabelText("Encabezado de página")).toHaveTextContent(/Investigación.*Eventos/);
    fireEvent.click(screen.getByRole("button", { name: "Cuenta" }));
    expect(screen.getByRole("menuitem", { name: "Cerrar sesión" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Cambiar contraseña" })).toBeInTheDocument();
    expect(screen.getByRole("radiogroup", { name: "Tema" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("menuitemradio", { name: "EN" }));
    expect(screen.getByRole("menuitem", { name: "Sign out" })).toBeInTheDocument();
    expect(within(rail).getByRole("link", { name: "Events" })).toBeInTheDocument();
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

    const rail = await screen.findByRole("complementary", { name: "Primary Nav Rail" });
    const configurationLink = within(rail).getByRole("link", { name: "Configuración" });
    expect(configurationLink).toHaveAttribute("aria-current", "page");
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

  const meWith = (...permissions: string[]) => json({
    id: "u1", username: "operator", display_name: "Operator", tenant_id: "t1",
    mfa_enabled: false, must_change_password: false, auth_method: "session",
    grants: permissions.map((permission) => ({ permission, effect: "allow", scope_type: "platform" })),
  });

  async function renderAt(path: string, permissions: string[]) {
    vi.stubGlobal("fetch", vi.fn(stubApi({ "/api/v1/me": () => meWith(...permissions), "/api/v1/sites": () => json({ items: [] }) })));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }), context: { queryClient: client } });
    render(<QueryClientProvider client={client}><RouterProvider router={router} /></QueryClientProvider>);
    await screen.findByLabelText("Encabezado de página");
    await screen.findByRole("navigation", { name: "Navegación móvil" });
  }

  it("replaces the hamburger drawer with a bottom navigation of at most four destinations plus More", async () => {
    await renderAt("/events", ["live.view", "events.view", "servers.view", "alarms.view", "lpr.view"]);
    expect(screen.queryByRole("button", { name: "Abrir menú de navegación" })).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Saltar al contenido" })).toHaveAttribute("href", "#main-content");

    const bar = screen.getByRole("navigation", { name: "Navegación móvil" });
    expect(bar.closest('[data-shell-region="bottom-nav"]')).toHaveClass("md:hidden");
    const links = within(bar).getAllByRole("link");
    expect(links.map((link) => link.textContent)).toEqual(["En vivo", "Eventos", "Servidores", "Alarmas"]);
    expect(within(bar).getByRole("link", { name: "Eventos" })).toHaveAttribute("aria-current", "page");
    expect(within(bar).getByRole("link", { name: "En vivo" })).not.toHaveAttribute("aria-current");
    expect(within(bar).getByRole("button", { name: "Más" })).toHaveAttribute("aria-expanded", "false");
  });

  it("opens a More sheet with the remaining permitted destinations and closes it with Escape returning focus", async () => {
    await renderAt("/events", ["live.view", "events.view", "servers.view", "alarms.view", "lpr.view"]);
    const more = screen.getByRole("button", { name: "Más" });
    more.focus();
    fireEvent.click(more);

    const sheet = screen.getByRole("dialog", { name: "Más destinos" });
    expect(sheet).toHaveAttribute("aria-modal", "true");
    expect(more).toHaveAttribute("aria-expanded", "true");
    expect(within(sheet).getByRole("link", { name: "Patentes" })).toBeInTheDocument();
    expect(within(sheet).getByRole("link", { name: "Configuración" })).toBeInTheDocument();
    // Primary destinations stay in the bar only.
    expect(within(sheet).queryByRole("link", { name: "Eventos" })).not.toBeInTheDocument();
    expect(within(sheet).queryByRole("link", { name: "En vivo" })).not.toBeInTheDocument();
    expect(sheet.contains(document.activeElement)).toBe(true);

    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("dialog", { name: "Más destinos" })).not.toBeInTheDocument();
    expect(more).toHaveFocus();
  });

  it("closes the More sheet from the close button and marks the active sheet destination", async () => {
    await renderAt("/plates", ["live.view", "events.view", "servers.view", "alarms.view", "lpr.view"]);
    fireEvent.click(screen.getByRole("button", { name: "Más" }));
    const sheet = screen.getByRole("dialog", { name: "Más destinos" });
    expect(within(sheet).getByRole("link", { name: "Patentes" })).toHaveAttribute("aria-current", "page");
    fireEvent.click(within(sheet).getByRole("button", { name: "Cerrar menú" }));
    expect(screen.queryByRole("dialog", { name: "Más destinos" })).not.toBeInTheDocument();
  });

  it("never offers permission-filtered destinations in the rail, the bar or the sheet", async () => {
    await renderAt("/events", ["events.view"]);
    fireEvent.click(screen.getByRole("button", { name: "Más" }));
    for (const name of ["En vivo", "Mapas", "Servidores", "Alarmas", "Patentes", "Usuarios"]) {
      expect(screen.queryByRole("link", { name })).not.toBeInTheDocument();
    }
    expect(within(screen.getByRole("dialog", { name: "Más destinos" })).getByRole("link", { name: "Configuración" })).toBeInTheDocument();
  });
});
