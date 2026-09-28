import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage, stubApi } from "@/test-utils";
import { Events } from "./Events";

afterEach(() => vi.unstubAllGlobals());

/** meResponse builds a /api/v1/me response allowing exactly the given permissions. */
function meResponse(...permissions: string[]) {
  return json({
    id: "u1",
    username: "u1",
    display_name: "u1",
    mfa_enabled: false,
    must_change_password: false,
    auth_method: "session",
    tenant_id: "t1",
    grants: permissions.map((permission) => ({ permission, effect: "allow" as const, scope_type: "platform" as const })),
  });
}

const noCatalogs = {
  "/api/v1/sites": () => json({ items: [] }),
  "/api/v1/cameras": () => json({ items: [] }),
  "/api/v1/camera-groups": () => json({ items: [] }),
};

function makeEvent(id: string, overrides: Partial<Schemas["Event"]> = {}): Schemas["Event"] {
  return {
    id,
    tenant_id: "t1",
    site_id: "site1",
    site_name: "Site One",
    server_id: "srv1",
    server_name: "Server One",
    camera_id: "cam1",
    camera_name: "Camera One",
    remote_id: id,
    severity: "detection",
    labels: ["car"],
    sub_labels: [],
    zones: [],
    plates: [],
    start_time: "2024-01-01T10:00:00Z",
    end_time: null,
    reviewed: false,
    has_thumbnail: false,
    has_snapshot: false,
    has_preview: false,
    ...overrides,
  };
}

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
          "/api/v1/me": () => meResponse("events.search"),
          ...noCatalogs,
          "/api/v1/camera-groups": () =>
            json({ items: [{ id: "g1", tenant_id: "t1", name: "Perimeter", description: "", camera_ids: [] }] }),
        })(input);
      }),
    );

    renderPage(Events);

    // Wait for the initial (unfiltered) events fetch before filling the form.
    expect(await screen.findByText("No hay eventos que coincidan.")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Zona"), { target: { value: "entrada" } });
    fireEvent.change(screen.getByLabelText("Sub-etiqueta"), { target: { value: "placa_reconocida" } });
    fireEvent.click(screen.getByRole("button", { name: "Buscar" }));

    // The submit re-triggers the same "no results" text, so waiting for it alone does not
    // guarantee the filtered fetch (as opposed to the initial one) has landed yet: wait for the
    // fetch mock to actually be called with the filtered query string.
    await waitFor(() => {
      expect(lastEventsUrl?.searchParams.getAll("zone")).toEqual(["entrada"]);
      expect(lastEventsUrl?.searchParams.getAll("sub_label")).toEqual(["placa_reconocida"]);
    });
  });

  it("sends camera_group_id as a query param", async () => {
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
          "/api/v1/me": () => meResponse("events.search"),
          ...noCatalogs,
          "/api/v1/camera-groups": () =>
            json({ items: [{ id: "g1", tenant_id: "t1", name: "Perimeter", description: "", camera_ids: [] }] }),
        })(input);
      }),
    );

    renderPage(Events);

    expect(await screen.findByText("No hay eventos que coincidan.")).toBeInTheDocument();
    // Wait for the "Perimeter" option itself (loaded async from /api/v1/camera-groups), not
    // just the select element, so fireEvent.change below does not race the option's render.
    await screen.findByRole("option", { name: "Perimeter" });
    fireEvent.change(screen.getByLabelText("Grupo de cámaras"), { target: { value: "g1" } });
    fireEvent.click(screen.getByRole("button", { name: "Buscar" }));

    await waitFor(() => {
      expect(lastEventsUrl?.searchParams.getAll("camera_group_id")).toEqual(["g1"]);
    });
  });

  it("sends every filter field as query params", async () => {
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
          "/api/v1/me": () => meResponse("events.search", "lpr.search"),
          "/api/v1/sites": () => json({ items: [{ id: "site1", name: "Site One" }] }),
          "/api/v1/cameras": () => json({ items: [{ id: "cam1", display_name: "Camera One", site_id: "site1" }] }),
          "/api/v1/camera-groups": () =>
            json({ items: [{ id: "g1", tenant_id: "t1", name: "Perimeter", description: "", camera_ids: [] }] }),
        })(input);
      }),
    );

    renderPage(Events);

    expect(await screen.findByText("No hay eventos que coincidan.")).toBeInTheDocument();
    // Both options load async from their own catalog fetches; wait for both before touching the
    // selects that depend on them, to avoid racing their render (see the camera_group_id test).
    await screen.findByRole("option", { name: "Perimeter" });
    await screen.findByRole("option", { name: "Camera One" });

    fireEvent.change(screen.getByLabelText("Sitio"), { target: { value: "site1" } });
    fireEvent.change(screen.getByLabelText("Cámara"), { target: { value: "cam1" } });
    fireEvent.change(screen.getByLabelText("Grupo de cámaras"), { target: { value: "g1" } });
    fireEvent.change(screen.getByLabelText("Objeto"), { target: { value: "car" } });
    fireEvent.change(screen.getByLabelText("Zona"), { target: { value: "entrada" } });
    fireEvent.change(screen.getByLabelText("Sub-etiqueta"), { target: { value: "placa_reconocida" } });
    fireEvent.change(screen.getByLabelText("Tipo"), { target: { value: "alert" } });
    fireEvent.change(screen.getByLabelText("Desde"), { target: { value: "2024-01-01T10:00" } });
    fireEvent.change(screen.getByLabelText("Hasta"), { target: { value: "2024-01-02T10:00" } });
    fireEvent.change(screen.getByLabelText("Patente"), { target: { value: "ab123cd" } });
    fireEvent.click(screen.getByLabelText("Solo sin revisar"));
    fireEvent.click(screen.getByLabelText("Con snapshot"));
    fireEvent.click(screen.getByLabelText("Con preview"));
    fireEvent.click(screen.getByRole("button", { name: "Buscar" }));

    await waitFor(() => {
      const p = lastEventsUrl?.searchParams;
      expect(p?.getAll("site_id")).toEqual(["site1"]);
      expect(p?.getAll("camera_id")).toEqual(["cam1"]);
      expect(p?.getAll("camera_group_id")).toEqual(["g1"]);
      expect(p?.getAll("label")).toEqual(["car"]);
      expect(p?.getAll("zone")).toEqual(["entrada"]);
      expect(p?.getAll("sub_label")).toEqual(["placa_reconocida"]);
      expect(p?.get("severity")).toBe("alert");
      expect(p?.get("plate")).toBe("AB123CD");
      expect(p?.get("from")).toBe(new Date("2024-01-01T10:00").toISOString());
      expect(p?.get("to")).toBe(new Date("2024-01-02T10:00").toISOString());
      expect(p?.get("reviewed")).toBe("false");
      expect(p?.get("has_snapshot")).toBe("true");
      expect(p?.get("has_preview")).toBe("true");
      expect(p?.get("limit")).toBe("48");
    });
  });

  it("hides the Patente filter without lpr.search", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/events") return json({ items: [] });
        return stubApi({ "/api/v1/me": () => meResponse("events.search"), ...noCatalogs })(input);
      }),
    );

    renderPage(Events);

    await screen.findByText("No hay eventos que coincidan.");
    expect(screen.queryByLabelText("Patente")).not.toBeInTheDocument();
  });

  it('loads the next page via cursor when "Cargar más" is clicked', async () => {
    const eventsCalls: URL[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/events") {
          eventsCalls.push(url);
          if (!url.searchParams.get("cursor")) {
            return json({ items: [makeEvent("e1")], next_cursor: "page2" });
          }
          return json({ items: [makeEvent("e2")] });
        }
        return stubApi({ "/api/v1/me": () => meResponse("events.search"), ...noCatalogs })(input);
      }),
    );

    renderPage(Events);

    await screen.findByRole("button", { name: "Cargar más" });
    expect(screen.getAllByRole("listitem")).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "Cargar más" }));

    await waitFor(() => {
      expect(eventsCalls.some((u) => u.searchParams.get("cursor") === "page2")).toBe(true);
    });
    await waitFor(() => {
      expect(screen.getAllByRole("listitem")).toHaveLength(2);
    });
  });
});
