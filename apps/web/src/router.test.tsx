import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { routeTree } from "./router";

afterEach(() => vi.unstubAllGlobals());

function meResponse() {
  return json({
    id: "u1",
    username: "u1",
    display_name: "u1",
    mfa_enabled: false,
    must_change_password: false,
    auth_method: "session",
    tenant_id: "t1",
    grants: [{ permission: "live.view", effect: "allow" as const, scope_type: "platform" as const }],
  });
}

function stubLiveRoutes() {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      stubApi({
        "/api/v1/me": meResponse,
        "/api/v1/cameras": () => json({ items: [] }),
        "/api/v1/sites": () => json({ items: [] }),
        "/api/v1/servers": () => json({ items: [] }),
        "/api/v1/views": () => json({ items: [] }),
      }),
    ),
  );
}

describe("router", () => {
  it('resolves "/" to Live, the operational landing page', async () => {
    stubLiveRoutes();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/"] }), context: { queryClient: client } });

    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    expect(await screen.findByRole("heading", { name: "En vivo" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/live");
  });
});

describe("router: client station", () => {
  it("resolves /client-station inside the settings layout", async () => {
    stubLiveRoutes();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/client-station"] }), context: { queryClient: client } });

    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    expect(await screen.findByRole("heading", { name: "Estación cliente", level: 1 })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/client-station");
  });
});

describe("router: faces", () => {
  it("resolves /faces and tells a user without faces.view they cannot see it", async () => {
    stubLiveRoutes();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/faces"] }), context: { queryClient: client } });

    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    expect(await screen.findByRole("heading", { name: "Rostros", level: 1 })).toBeInTheDocument();
    expect(await screen.findByText("No tienes permiso para ver el reconocimiento facial.")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/faces");
  });
});

describe("router: totem", () => {
  const mount = (path: string, routes: Record<string, () => Response> = {}) => {
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/me": meResponse,
      "/api/v1/lpr/reads": () => json({ items: [] }),
      "/api/v1/events": () => json({ items: [] }),
      ...routes,
    })));
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: [path] }), context: { queryClient: client } });
    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    return router;
  };

  it.each([
    ["/totem/lpr", "lpr"],
    ["/totem/faces", "faces"],
  ])("renders %s fullscreen without the app navigation", async (path, kind) => {
    mount(path);
    const channel = await screen.findByTestId("detection-channel");
    expect(channel.getAttribute("data-channel")).toBe(kind);
    expect(screen.getByTestId("totem")).toBeInTheDocument();
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
  });

  it("explains an unknown totem instead of rendering a channel", async () => {
    mount("/totem/nope");
    expect(await screen.findByText("Tótem desconocido")).toBeInTheDocument();
    expect(screen.queryByTestId("detection-channel")).not.toBeInTheDocument();
  });

  it("sends a signed-out visitor to the login", async () => {
    const router = mount("/totem/lpr", { "/api/v1/me": () => json({ code: "unauthorized", message: "no" }, 401) });
    await vi.waitFor(() => expect(router.state.location.pathname).toBe("/login"));
  });
});
