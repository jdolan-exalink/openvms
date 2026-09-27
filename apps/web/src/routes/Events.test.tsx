import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Events } from "./Events";

afterEach(() => vi.unstubAllGlobals());

describe("Events", () => {
  it("sends zone and sub_label filters as query params", async () => {
    let lastEventsUrl: URL | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/events") {
          lastEventsUrl = url;
          return json({ items: [] });
        }
        return stubApi({
          "/api/v1/me": () =>
            json({
              id: "u1",
              username: "u1",
              display_name: "u1",
              mfa_enabled: false,
              must_change_password: false,
              auth_method: "session",
              tenant_id: "t1",
              grants: [{ permission: "events.search", effect: "allow", scope_type: "platform" }],
            }),
          "/api/v1/sites": () => json({ items: [] }),
          "/api/v1/cameras": () => json({ items: [] }),
        })(input);
      }),
    );

    renderPage(Events);

    // Wait for the initial (unfiltered) events fetch before filling the form.
    expect(await screen.findByText("No hay eventos que coincidan.")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Zona"), { target: { value: "entrada" } });
    fireEvent.change(screen.getByLabelText("Sub-etiqueta"), { target: { value: "placa_reconocida" } });
    fireEvent.click(screen.getByRole("button", { name: "Buscar" }));

    expect(await screen.findByText("No hay eventos que coincidan.")).toBeInTheDocument();
    expect(lastEventsUrl?.searchParams.getAll("zone")).toEqual(["entrada"]);
    expect(lastEventsUrl?.searchParams.getAll("sub_label")).toEqual(["placa_reconocida"]);
  });
});
