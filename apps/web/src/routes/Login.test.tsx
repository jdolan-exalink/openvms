import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { routeTree } from "@/router";

afterEach(() => vi.unstubAllGlobals());

function meResponse() {
  return json({
    id: "u1",
    username: "operator",
    display_name: "Operator",
    mfa_enabled: false,
    must_change_password: false,
    auth_method: "session",
    tenant_id: "t1",
    grants: [{ permission: "live.view", effect: "allow" as const, scope_type: "platform" as const }],
  });
}

describe("Login", () => {
  it("always lands on Live (En vivo), the default landing page, after a successful login", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        stubApi({
          "/api/v1/auth/login": () => json({ mfa_required: false }),
          "/api/v1/me": meResponse,
          "/api/v1/cameras": () => json({ items: [] }),
          "/api/v1/sites": () => json({ items: [] }),
          "/api/v1/servers": () => json({ items: [] }),
          "/api/v1/views": () => json({ items: [] }),
        }),
      ),
    );

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/login"] }), context: { queryClient: client } });

    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );

    fireEvent.change(await screen.findByLabelText("Usuario"), { target: { value: "operator" } });
    fireEvent.change(screen.getByLabelText("Contraseña"), { target: { value: "s3cret!" } });
    fireEvent.click(screen.getByRole("button", { name: "Ingresar" }));

    expect(await screen.findByRole("heading", { name: "En vivo" }, { timeout: 5000 })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/live");
  });

  function renderLogin() {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/login"] }), context: { queryClient: client } });
    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
  }

  it("shows the OpenVMS version read from /version.json", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request | string) => {
        const url = typeof input === "string" ? input : input.url;
        if (new URL(url, "http://localhost").pathname === "/version.json") return json({ version: "0.4.0", commit: "abc1234", buildTime: "2026-10-19T00:00:00Z" });
        return json({ code: "unauthorized", message: "no" }, 401);
      }),
    );
    renderLogin();
    expect(await screen.findByText("OpenVMS v0.4.0")).toBeInTheDocument();
  });

  it("shows no version and no error when /version.json is missing or not a release", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("<html></html>", { status: 200, headers: { "Content-Type": "text/html" } })));
    renderLogin();
    expect(await screen.findByLabelText("Usuario")).toBeInTheDocument();
    expect(screen.queryByText(/OpenVMS v/)).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows nothing for the dev placeholder version", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => json({ version: "dev", commit: "unknown", buildTime: "unknown" })));
    renderLogin();
    expect(await screen.findByLabelText("Usuario")).toBeInTheDocument();
    expect(screen.queryByText(/OpenVMS v/)).not.toBeInTheDocument();
  });
});
