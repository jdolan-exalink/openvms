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

  it("shows applied-filter chips, a result count, and removes one filter without dropping the rest", async () => {
    let lastEventsUrl: URL | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/events") {
          lastEventsUrl = url;
          return json({ items: [makeEvent("e1")], next_cursor: "page2" });
        }
        return stubApi({ "/api/v1/me": () => meResponse("events.search"), ...noCatalogs })(input);
      }),
    );

    renderPage(Events);
    await screen.findByRole("button", { name: "Cargar más" });
    expect(screen.getByRole("status")).toHaveTextContent("1+ eventos");
    expect(screen.queryByRole("list", { name: "Filtros aplicados" })).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Zona"), { target: { value: "entrada" } });
    fireEvent.change(screen.getByLabelText("Tipo"), { target: { value: "alert" } });
    // Editing without submitting must not show a chip: chips reflect the applied search.
    expect(screen.queryByRole("list", { name: "Filtros aplicados" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Buscar" }));

    const chips = await screen.findByRole("list", { name: "Filtros aplicados" });
    expect(chips).toHaveTextContent("Zona: entrada");
    expect(chips).toHaveTextContent("Tipo: Solo alertas");

    fireEvent.click(screen.getByRole("button", { name: "Quitar filtro Zona: entrada" }));
    await waitFor(() => {
      expect(lastEventsUrl?.searchParams.getAll("zone")).toEqual([]);
      expect(lastEventsUrl?.searchParams.get("severity")).toBe("alert");
    });
    expect((screen.getByLabelText("Zona") as HTMLInputElement).value).toBe("");
    expect(screen.getByRole("list", { name: "Filtros aplicados" })).not.toHaveTextContent("Zona");

    fireEvent.click(screen.getByRole("button", { name: "Limpiar filtros" }));
    await waitFor(() => expect(lastEventsUrl?.searchParams.get("severity")).toBeNull());
    expect(screen.queryByRole("list", { name: "Filtros aplicados" })).not.toBeInTheDocument();
  });
  /** stubEvents serves one event plus the given /me permissions; PATCH is delegated to onPatch. */
  function stubEventsApi(permissions: string[], event: Schemas["Event"], onPatch?: () => Response | Promise<Response>) {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/events" && input.method === "GET") return json({ items: [event] });
        if (url.pathname === `/api/v1/events/${event.id}` && input.method === "PATCH" && onPatch) return onPatch();
        return stubApi({ "/api/v1/me": () => meResponse(...permissions), ...noCatalogs })(input);
      }),
    );
  }

  it("marks review state and severity on each card in text, not only color", async () => {
    stubEventsApi(["events.search"], makeEvent("e1", { severity: "alert" }));
    renderPage(Events);
    const card = await screen.findByRole("button", { name: /Camera One/ });
    expect(card).toHaveTextContent("Alerta");
    expect(card).toHaveTextContent("Sin revisar");
  });

  it("opens the detail as a dialog that closes with Escape and restores focus to the card", async () => {
    stubEventsApi(["events.search"], makeEvent("e1"));
    renderPage(Events);
    const card = await screen.findByRole("button", { name: /Camera One/ });
    card.focus();
    fireEvent.click(card);
    expect(await screen.findByRole("dialog", { name: "Detalle del evento" })).toBeInTheDocument();
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(card).toHaveFocus();
  });

  it("gates the playback link on recordings.view and points it at the event camera and time", async () => {
    stubEventsApi(["events.search", "recordings.view"], makeEvent("e1"));
    renderPage(Events);
    fireEvent.click(await screen.findByRole("button", { name: /Camera One/ }));
    const link = await screen.findByRole("link", { name: /Ver grabación/ });
    expect(link.getAttribute("href")).toContain("camera=cam1");
  });

  it("hides the playback link and review action without their permissions", async () => {
    stubEventsApi(["events.search"], makeEvent("e1"));
    renderPage(Events);
    fireEvent.click(await screen.findByRole("button", { name: /Camera One/ }));
    await screen.findByRole("dialog");
    expect(screen.queryByRole("link", { name: /Ver grabación/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Marcar revisado/ })).not.toBeInTheDocument();
  });

  it("shows pending then success feedback when marking reviewed, and refreshes the card", async () => {
    let release: (r: Response) => void = () => {};
    const gate = new Promise<Response>((r) => (release = r));
    stubEventsApi(["events.search", "events.review"], makeEvent("e1"), () => gate);
    renderPage(Events);
    fireEvent.click(await screen.findByRole("button", { name: /Camera One/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Marcar revisado" }));
    const pending = await screen.findByRole("button", { name: "Guardando…" });
    expect(pending).toBeDisabled();
    release(json(makeEvent("e1", { reviewed: true })));
    expect(await screen.findByText("Evento marcado como revisado.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Marcar sin revisar" })).toBeEnabled();
  });

  it("surfaces a review failure without changing the review state", async () => {
    stubEventsApi(["events.search", "events.review"], makeEvent("e1"), () => json({ error: { code: "boom", message: "No se pudo" } }, 500));
    renderPage(Events);
    fireEvent.click(await screen.findByRole("button", { name: /Camera One/ }));
    fireEvent.click(await screen.findByRole("button", { name: "Marcar revisado" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Marcar revisado" })).toBeEnabled();
  });

  describe("filters in the URL", () => {
    /** stubEventsUrlApi records the last GET /events URL. */
    function stubEventsUrlApi() {
      const seen: { url?: URL } = {};
      vi.stubGlobal(
        "fetch",
        vi.fn(async (input: Request) => {
          const url = new URL(input.url);
          if (url.pathname === "/api/v1/events") {
            seen.url = url;
            return json({ items: [] });
          }
          return stubApi({ "/api/v1/me": () => meResponse("events.search"), ...noCatalogs })(input);
        }),
      );
      return seen;
    }

    it("applies the filters found in the URL on load, in the form and the chips", async () => {
      const seen = stubEventsUrlApi();
      renderPage(Events, "/?zone=entrada&pending=true&severity=alert");
      await waitFor(() => {
        expect(seen.url?.searchParams.getAll("zone")).toEqual(["entrada"]);
        expect(seen.url?.searchParams.get("reviewed")).toBe("false");
        expect(seen.url?.searchParams.get("severity")).toBe("alert");
      });
      expect((screen.getByLabelText("Zona") as HTMLInputElement).value).toBe("entrada");
      expect(await screen.findByRole("list", { name: "Filtros aplicados" })).toHaveTextContent("Zona: entrada");
    });

    it("writes the applied filters to the URL on submit and clears them on Limpiar", async () => {
      stubEventsUrlApi();
      const { router } = renderPage(Events);
      await screen.findByText("No hay eventos que coincidan.");
      fireEvent.change(screen.getByLabelText("Zona"), { target: { value: "entrada" } });
      // Draft edits alone never touch the URL.
      expect(router.state.location.search).toEqual({});
      fireEvent.click(screen.getByRole("button", { name: "Buscar" }));
      await waitFor(() => expect(router.state.location.search).toEqual({ zone: "entrada" }));
      fireEvent.click(screen.getByRole("button", { name: "Limpiar" }));
      await waitFor(() => expect(router.state.location.search).toEqual({}));
    });
  });

  describe("bulk review", () => {
    const two = [makeEvent("e1"), makeEvent("e2", { camera_name: "Camera Two" })];

    function stubBulkApi(permissions: string[], onReview?: (body: unknown) => Response | Promise<Response>) {
      vi.stubGlobal(
        "fetch",
        vi.fn(async (input: Request) => {
          const url = new URL(input.url);
          if (url.pathname === "/api/v1/events" && input.method === "GET") return json({ items: two });
          if (url.pathname === "/api/v1/events/review" && input.method === "POST" && onReview) return onReview(await input.json());
          return stubApi({ "/api/v1/me": () => meResponse(...permissions), ...noCatalogs })(input);
        }),
      );
    }

    it("offers no selection without events.review", async () => {
      stubBulkApi(["events.search"]);
      renderPage(Events);
      await screen.findByRole("button", { name: /Camera One/ });
      expect(screen.queryByRole("checkbox", { name: /Seleccionar evento/ })).not.toBeInTheDocument();
    });

    it("marks the selected events reviewed in one request with pending then success feedback", async () => {
      let release: (r: Response) => void = () => {};
      const gate = new Promise<Response>((r) => (release = r));
      let body: unknown;
      stubBulkApi(["events.search", "events.review"], (b) => {
        body = b;
        return gate;
      });
      renderPage(Events);
      fireEvent.click(await screen.findByRole("checkbox", { name: /Seleccionar evento.*Camera One/ }));
      fireEvent.click(screen.getByRole("checkbox", { name: /Seleccionar evento.*Camera Two/ }));
      fireEvent.click(screen.getByRole("button", { name: "Marcar 2 como revisados" }));
      expect(await screen.findByRole("button", { name: "Guardando…" })).toBeDisabled();
      release(json({ items: [makeEvent("e1", { reviewed: true }), makeEvent("e2", { reviewed: true })] }));
      expect(await screen.findByText("2 eventos marcados como revisados.")).toBeInTheDocument();
      expect(body).toEqual({ ids: ["e1", "e2"], reviewed: true });
      // The selection is spent once applied.
      expect(screen.queryByRole("button", { name: /Marcar \d+ como/ })).not.toBeInTheDocument();
    });

    it("can mark the selection as not reviewed", async () => {
      let body: unknown;
      stubBulkApi(["events.search", "events.review"], (b) => {
        body = b;
        return json({ items: [makeEvent("e1")] });
      });
      renderPage(Events);
      fireEvent.click(await screen.findByRole("checkbox", { name: /Seleccionar evento.*Camera One/ }));
      fireEvent.click(screen.getByRole("button", { name: "Marcar 1 como sin revisar" }));
      expect(await screen.findByText("1 evento marcado como sin revisar.")).toBeInTheDocument();
      expect(body).toEqual({ ids: ["e1"], reviewed: false });
    });

    it("keeps the selection and shows the error when the server refuses the batch", async () => {
      stubBulkApi(["events.search", "events.review"], () => json({ error: { code: "forbidden", message: "Sin permiso" } }, 403));
      renderPage(Events);
      fireEvent.click(await screen.findByRole("checkbox", { name: /Seleccionar evento.*Camera One/ }));
      fireEvent.click(screen.getByRole("button", { name: "Marcar 1 como revisados" }));
      expect(await screen.findByRole("alert")).toBeInTheDocument();
      expect(screen.getByRole("checkbox", { name: /Seleccionar evento.*Camera One/ })).toBeChecked();
      expect(screen.getByRole("button", { name: "Marcar 1 como revisados" })).toBeEnabled();
    });
  });
});
