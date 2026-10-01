import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { routeTree } from "@/router";

vi.mock("@/components/maps/canvas/MapCanvas", () => ({
  MapCanvas: () => <div data-testid="mock-map-canvas">Mock Map Canvas</div>,
}));

afterEach(() => vi.unstubAllGlobals());

describe("Maps route", () => {
  it("shows restricted access when the user lacks maps.view permission", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": () =>
            json({
              id: "u1",
              username: "operator",
              display_name: "Operator",
              tenant_id: "t1",
              mfa_enabled: false,
              must_change_password: false,
              auth_method: "session",
              grants: [{ permission: "live.view", effect: "allow", scope_type: "platform" }],
            }),
        }),
      ),
    );

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({
      routeTree,
      history: createMemoryHistory({ initialEntries: ["/maps"] }),
      context: { queryClient: client },
    });

    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    expect(await screen.findByText("Acceso restringido")).toBeInTheDocument();
    expect(screen.getByText(/maps.view/)).toBeInTheDocument();
  });

  it("transitions from loading to ready without changing MapShell hook order", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/me": () =>
            json({
              id: "u1",
              username: "admin",
              display_name: "Admin",
              tenant_id: "t1",
              mfa_enabled: false,
              must_change_password: false,
              auth_method: "session",
              grants: [
                { permission: "maps.view", effect: "allow", scope_type: "platform" },
                { permission: "maps.edit", effect: "allow", scope_type: "platform" },
              ],
            }),
          "/api/v1/maps/config": () =>
            json({
              provider: {
                id: "protomaps-local",
                kind: "pmtiles",
                tiles: ["/tiles/base.pmtiles"],
                attribution: "© OpenStreetMap contributors",
                max_zoom: 18,
                offline: true,
              },
              default_center: { lat: -34.6037, lng: -58.3816 },
              default_zoom: 12,
            }),
          "/api/v1/maps/overview": () =>
            json({
              items: [
                {
                  id: "11111111-1111-1111-1111-111111111111",
                  name: "Sede Central",
                  lat: -34.6037,
                  lng: -58.3816,
                  default_zoom: 14,
                  camera_count: 10,
                  online_cameras: 9,
                  offline_cameras: 1,
                  degraded_cameras: 0,
                  alarm_count: 0,
                },
              ],
            }),
        }),
      ),
    );

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({
      routeTree,
      history: createMemoryHistory({ initialEntries: ["/maps"] }),
      context: { queryClient: client },
    });

    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    expect(await screen.findByTestId("mock-map-canvas")).toBeInTheDocument();
    expect(screen.getByText("Sede Central")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /editor/i })).toBeInTheDocument();
  });
});
