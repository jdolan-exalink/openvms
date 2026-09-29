import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Account } from "./Account";

afterEach(() => vi.unstubAllGlobals());

function setup(me = { id: "u-1", username: "juan", display_name: "Juan Perez", must_change_password: false, mfa_enabled: false }) {
  const writes: { method: string; url: string; body: unknown }[] = [];
  const routes = stubApi({
    "/api/v1/me": () => json(me),
  });

  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      if (req.method === "POST") {
        writes.push({
          method: req.method,
          url: new URL(req.url).pathname,
          body: await req.clone().json(),
        });
        return json({ ok: true });
      }
      return routes(req);
    }),
  );

  renderPage(Account);
  return { writes };
}

describe("Account", () => {
  it("renders PageHeader with user details", async () => {
    setup();
    expect(await screen.findByRole("heading", { name: "Mi cuenta" })).toBeInTheDocument();
    expect(await screen.findByText("Juan Perez (juan)")).toBeInTheDocument();
  });

  it("shows warning when must_change_password is true", async () => {
    setup({ id: "u-1", username: "juan", display_name: "Juan Perez", must_change_password: true, mfa_enabled: false });
    expect(await screen.findByText("Un administrador te pidió que cambies la contraseña.")).toBeInTheDocument();
  });

  it("submits password change with matching passwords", async () => {
    const { writes } = setup();
    await screen.findByText("Juan Perez (juan)");

    fireEvent.change(screen.getByLabelText("Contraseña actual"), { target: { value: "oldPassword123!" } });
    fireEvent.change(screen.getByLabelText(/Nueva contraseña/), { target: { value: "newSecurePass123!" } });
    fireEvent.change(screen.getByLabelText("Repetir nueva contraseña"), { target: { value: "newSecurePass123!" } });

    fireEvent.click(screen.getByRole("button", { name: "Cambiar contraseña" }));

    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]).toEqual({
      method: "POST",
      url: "/api/v1/me/password",
      body: {
        current_password: "oldPassword123!",
        new_password: "newSecurePass123!",
      },
    });
    expect(await screen.findByText("Contraseña actualizada. Tus otras sesiones se cerraron.")).toBeInTheDocument();
  });
});
