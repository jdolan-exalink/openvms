import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Plates } from "./Plates";

afterEach(() => vi.unstubAllGlobals());

describe("Plates", () => {
  it("sends camera_group_id as a query param", async () => {
    let lastReadsUrl: URL | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/lpr/reads") {
          lastReadsUrl = url;
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
              grants: [{ permission: "lpr.search", effect: "allow", scope_type: "platform" }],
            }),
          "/api/v1/sites": () => json({ items: [] }),
          "/api/v1/camera-groups": () =>
            json({ items: [{ id: "g1", tenant_id: "t1", name: "Perimeter", description: "", camera_ids: [] }] }),
        })(input);
      }),
    );

    renderPage(Plates);

    expect(await screen.findByText("No hay lecturas que coincidan.")).toBeInTheDocument();
    // Wait for the "Perimeter" option itself (loaded async from /api/v1/camera-groups), not
    // just the select element, so fireEvent.change below does not race the option's render.
    await screen.findByRole("option", { name: "Perimeter" });
    fireEvent.change(screen.getByLabelText("Grupo de cámaras"), { target: { value: "g1" } });
    fireEvent.click(screen.getByRole("button", { name: "Buscar" }));

    await waitFor(() => {
      expect(lastReadsUrl?.searchParams.getAll("camera_group_id")).toEqual(["g1"]);
    });
  });
});
