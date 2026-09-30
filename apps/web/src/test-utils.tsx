import type React from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, RouterProvider } from "@tanstack/react-router";
import { render } from "@testing-library/react";


/** renderPage mounts a screen at "/" inside a router and a fresh query client. */
export function renderPage(Page: () => React.ReactNode, entry = "/") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const root = createRootRoute();
  const paths = ["/sites", "/servers", "/cameras", "/cameras/$cameraId/frigate", "/login"].map((path) =>
    createRoute({ getParentRoute: () => root, path, component: () => null }),
  );
  const page = createRoute({ getParentRoute: () => root, path: "/", component: Page });
  const router = createRouter({
    routeTree: root.addChildren([page, ...paths]),
    history: createMemoryHistory({ initialEntries: [entry] }),
  });
  const result = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  return { ...result, router };
}

export function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

/** stubApi answers fetches by pathname; unknown paths get 404. */
export function stubApi(routes: Record<string, () => Response>) {
  return async (input: Request) => {
    const handler = routes[new URL(input.url).pathname];
    return handler ? handler() : json({ code: "not_found", message: "not found" }, 404);
  };
}
