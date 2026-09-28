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
