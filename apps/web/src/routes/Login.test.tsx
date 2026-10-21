import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

  it("explains a login whose session the browser did not keep instead of silently staying", async () => {
    vi.stubGlobal("fetch", vi.fn(stubApi({
      "/api/v1/auth/login": () => json({ mfa_required: false }),
      "/api/v1/me": () => json({ code: "unauthorized", message: "no session" }, 401),
    })));
    const router = renderLogin();
    fireEvent.change(await screen.findByLabelText("Usuario"), { target: { value: "admin" } });
    fireEvent.change(screen.getByLabelText("Contraseña"), { target: { value: "New-Password-22" } });
    fireEvent.click(screen.getByRole("button", { name: "Ingresar" }));
    expect(await screen.findByText(/no guardó la sesión/)).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
  });

  describe("password recovery", () => {
    let calls: { path: string; body: unknown }[];
    const stubRecovery = (confirm: () => Response = () => new Response(null, { status: 204 })) => {
      calls = [];
      const routes = stubApi({
        "/api/v1/auth/recovery": () => json({ message: "Si la cuenta puede recuperarse, se generó un código." }, 202),
        "/api/v1/auth/recovery/confirm": confirm,
      });
      vi.stubGlobal("fetch", vi.fn(async (input: Request) => {
        const path = new URL(input.url).pathname;
        if (path.startsWith("/api/v1/auth/recovery")) calls.push({ path, body: await input.clone().json() });
        return routes(input);
      }));
    };
    const startRecovery = async (username = "admin") => {
      fireEvent.click(await screen.findByRole("button", { name: "¿Olvidaste la contraseña?" }));
      fireEvent.change(screen.getByLabelText("Usuario"), { target: { value: username } });
      fireEvent.click(screen.getByRole("button", { name: "Generar código" }));
      expect(await screen.findByText("Si la cuenta puede recuperarse, se generó un código.")).toBeInTheDocument();
    };
    const fillCode = (code: string, password: string, confirmation = password) => {
      fireEvent.change(screen.getByLabelText(/^Código de recuperación/), { target: { value: code } });
      fireEvent.change(screen.getByLabelText("Nueva contraseña"), { target: { value: password } });
      fireEvent.change(screen.getByLabelText("Repetir contraseña"), { target: { value: confirmation } });
      fireEvent.click(screen.getByRole("button", { name: "Cambiar contraseña" }));
    };

    it("asks for a logged code and sets the new password with it", async () => {
      stubRecovery();
      renderLogin();
      await startRecovery();
      expect(screen.getByText(/docker compose logs api/)).toBeInTheDocument();
      fillCode("482913", "New-Password-22");
      expect(await screen.findByText("Contraseña actualizada. Ingresá con la nueva.")).toBeInTheDocument();
      expect(calls).toEqual([
        { path: "/api/v1/auth/recovery", body: { username: "admin" } },
        { path: "/api/v1/auth/recovery/confirm", body: { username: "admin", code: "482913", new_password: "New-Password-22" } },
      ]);
      // Back on the sign-in form, ready for the new password.
      expect(screen.getByRole("button", { name: "Ingresar" })).toBeInTheDocument();
      expect((screen.getByLabelText("Usuario") as HTMLInputElement).value).toBe("admin");
    });

    it("does not send mismatched passwords", async () => {
      stubRecovery();
      renderLogin();
      await startRecovery();
      fillCode("482913", "New-Password-22", "Other-Password-22");
      expect(await screen.findByText("Las contraseñas no coinciden.")).toBeInTheDocument();
      expect(calls.map((c) => c.path)).toEqual(["/api/v1/auth/recovery"]);
    });

    it("shows why a code was rejected", async () => {
      stubRecovery(() => json({ code: "invalid_recovery_code", message: "El código no es válido o venció. Pedí uno nuevo." }, 401));
      renderLogin();
      await startRecovery();
      fillCode("000000", "New-Password-22");
      expect(await screen.findByText("El código no es válido o venció. Pedí uno nuevo.")).toBeInTheDocument();
      await waitFor(() => expect(screen.getByRole("button", { name: "Cambiar contraseña" })).toBeEnabled());
    });
  });

  function renderLogin() {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const router = createRouter({ routeTree, history: createMemoryHistory({ initialEntries: ["/login"] }), context: { queryClient: client } });
    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>,
    );
    return router;
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
