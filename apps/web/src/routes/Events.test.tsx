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
  "/api/v1/servers": () => json({ items: [] }),
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
  it("sends the chosen server and asks for a page of 100", async () => {
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
          "/api/v1/servers": () => json({ items: [{ id: "srv1", name: "Casa" }] }),
        })(input);
      }),
    );

    renderPage(Events);
    expect(await screen.findByText("No hay eventos que coincidan.")).toBeInTheDocument();
    await screen.findByRole("option", { name: "Casa" });
    fireEvent.change(screen.getByLabelText("Servidor"), { target: { value: "srv1" } });
    fireEvent.click(screen.getByRole("button", { name: "Buscar" }));

    await waitFor(() => {
      expect(lastEventsUrl?.searchParams.getAll("server_id")).toEqual(["srv1"]);
      expect(lastEventsUrl?.searchParams.get("limit")).toBe("100");
    });
  });

  it("starts the day at 00:00 and ends it at 23:59", async () => {
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
    expect((screen.getByLabelText("Desde") as HTMLInputElement).value.endsWith("T00:00")).toBe(true);
    expect((screen.getByLabelText("Hasta") as HTMLInputElement).value.endsWith("T23:59")).toBe(true);
    expect(screen.queryByLabelText("Zona")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Sub-etiqueta")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Grupo de cámaras")).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Eventos" })).not.toBeInTheDocument();
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
          "/api/v1/servers": () => json({ items: [{ id: "srv1", name: "Casa" }] }),
          "/api/v1/cameras": () => json({ items: [{ id: "cam1", display_name: "Camera One", site_id: "site1", server_id: "srv1" }] }),
        })(input);
      }),
    );

    renderPage(Events);

    expect(await screen.findByText("No hay eventos que coincidan.")).toBeInTheDocument();
    await screen.findByRole("option", { name: "Casa" });
    await screen.findByRole("option", { name: "Camera One" });

    fireEvent.change(screen.getByLabelText("Servidor"), { target: { value: "srv1" } });
    fireEvent.change(screen.getByLabelText("Cámara"), { target: { value: "cam1" } });
    fireEvent.change(screen.getByLabelText("Objeto"), { target: { value: "car" } });
    fireEvent.change(screen.getByLabelText("Tipo"), { target: { value: "alert" } });
    fireEvent.change(screen.getByLabelText("Desde"), { target: { value: "2024-01-01T10:00" } });
    fireEvent.change(screen.getByLabelText("Hasta"), { target: { value: "2024-01-02T10:00" } });
    fireEvent.change(screen.getByLabelText("Patente"), { target: { value: "ab123cd" } });
    fireEvent.click(screen.getByRole("button", { name: "Sin revisar" }));
    fireEvent.click(screen.getByRole("button", { name: "Con snapshot" }));
    fireEvent.click(screen.getByRole("button", { name: "Con preview" }));
    fireEvent.click(screen.getByRole("button", { name: "Buscar" }));

    await waitFor(() => {
      const p = lastEventsUrl?.searchParams;
      expect(p?.getAll("server_id")).toEqual(["srv1"]);
      expect(p?.getAll("camera_id")).toEqual(["cam1"]);
      expect(p?.getAll("label")).toEqual(["car"]);
      expect(p?.get("severity")).toBe("alert");
      expect(p?.get("plate")).toBe("AB123CD");
      expect(p?.get("from")).toBe(new Date("2024-01-01T10:00").toISOString());
      expect(p?.get("to")).toBe(new Date("2024-01-02T10:00").toISOString());
      expect(p?.get("reviewed")).toBe("false");
      expect(p?.get("has_snapshot")).toBe("true");
      expect(p?.get("has_preview")).toBe("true");
      expect(p?.get("limit")).toBe("100");
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

  it("loads the next page of 100 when the pager advances", async () => {
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
          return json({ items: [makeEvent("e2", { camera_name: "Camera Two" })] });
        }
        return stubApi({ "/api/v1/me": () => meResponse("events.search"), ...noCatalogs })(input);
      }),
    );

    renderPage(Events);

    await screen.findByRole("button", { name: "Página siguiente" });
    expect(screen.getAllByRole("listitem")).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "Página siguiente" }));

    await waitFor(() => {
      expect(eventsCalls.some((u) => u.searchParams.get("cursor") === "page2")).toBe(true);
    });
    await waitFor(() => {
      expect(screen.getByRole("button", { name: /Camera Two/ })).toBeInTheDocument();
    });
    expect(screen.queryByRole("button", { name: /Camera One/ })).not.toBeInTheDocument();
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
    await screen.findByRole("button", { name: "Página siguiente" });
    expect(screen.queryByText(/eventos$/)).not.toBeInTheDocument();
    expect(screen.queryByRole("list", { name: "Filtros aplicados" })).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Objeto"), { target: { value: "person" } });
    fireEvent.change(screen.getByLabelText("Tipo"), { target: { value: "alert" } });
    expect(screen.queryByRole("list", { name: "Filtros aplicados" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Buscar" }));

    const chips = await screen.findByRole("list", { name: "Filtros aplicados" });
    expect(chips).toHaveTextContent("Objeto: Persona");
    expect(chips).toHaveTextContent("Tipo: Solo alertas");

    fireEvent.click(screen.getByRole("button", { name: "Quitar filtro Objeto: Persona" }));
    await waitFor(() => {
      expect(lastEventsUrl?.searchParams.getAll("label")).toEqual([]);
      expect(lastEventsUrl?.searchParams.get("severity")).toBe("alert");
    });
    expect((screen.getByLabelText("Objeto") as HTMLSelectElement).value).toBe("");
    expect(screen.getByRole("list", { name: "Filtros aplicados" })).not.toHaveTextContent("Objeto");

    fireEvent.click(screen.getByRole("button", { name: "Limpiar" }));
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
      renderPage(Events, "/?label=person&pending=true&severity=alert");
      await waitFor(() => {
        expect(seen.url?.searchParams.getAll("label")).toEqual(["person"]);
        expect(seen.url?.searchParams.get("reviewed")).toBe("false");
        expect(seen.url?.searchParams.get("severity")).toBe("alert");
      });
      expect((screen.getByLabelText("Objeto") as HTMLSelectElement).value).toBe("person");
      expect(screen.getByRole("button", { name: "Sin revisar" })).toHaveAttribute("aria-pressed", "true");
      expect(await screen.findByRole("list", { name: "Filtros aplicados" })).toHaveTextContent("Objeto: Persona");
    });

    it("writes the applied filters to the URL on submit and clears them on Limpiar", async () => {
      stubEventsUrlApi();
      const { router } = renderPage(Events);
      await screen.findByText("No hay eventos que coincidan.");
      fireEvent.change(screen.getByLabelText("Objeto"), { target: { value: "person" } });
      expect(router.state.location.search).toEqual({});
      fireEvent.click(screen.getByRole("button", { name: "Buscar" }));
      await waitFor(() => expect(router.state.location.search).toEqual({ label: "person" }));
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
      fireEvent.click(screen.getByRole("button", { name: "Marcar como revisado" }));
      expect(await screen.findByRole("button", { name: "Guardando…" })).toBeDisabled();
      release(json({ items: [makeEvent("e1", { reviewed: true }), makeEvent("e2", { reviewed: true })] }));
      expect(await screen.findByText("2 eventos marcados como revisados.")).toBeInTheDocument();
      expect(body).toEqual({ ids: ["e1", "e2"], reviewed: true });
      // The selection is spent once applied.
      expect(screen.queryByRole("button", { name: /Marcar \d+ como/ })).not.toBeInTheDocument();
    });

    it("keeps mark-reviewed out of the bar until more than one event is selected", async () => {
      stubBulkApi(["events.search", "events.review"]);
      renderPage(Events);
      fireEvent.click(await screen.findByRole("checkbox", { name: /Seleccionar evento.*Camera One/ }));
      expect(screen.queryByRole("button", { name: "Marcar como revisado" })).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Seleccionar todas" }));
      expect(screen.getByRole("button", { name: "Marcar como revisado" })).toBeInTheDocument();
      expect(screen.getByRole("checkbox", { name: /Seleccionar evento.*Camera Two/ })).toBeChecked();
    });

    it("keeps the selection and shows the error when the server refuses the batch", async () => {
      stubBulkApi(["events.search", "events.review"], () => json({ error: { code: "forbidden", message: "Sin permiso" } }, 403));
      renderPage(Events);
      fireEvent.click(await screen.findByRole("checkbox", { name: /Seleccionar evento.*Camera One/ }));
      fireEvent.click(screen.getByRole("checkbox", { name: /Seleccionar evento.*Camera Two/ }));
      fireEvent.click(screen.getByRole("button", { name: "Marcar como revisado" }));
      expect(await screen.findByRole("alert")).toBeInTheDocument();
      expect(screen.getByRole("checkbox", { name: /Seleccionar evento.*Camera One/ })).toBeChecked();
      expect(screen.getByRole("button", { name: "Marcar como revisado" })).toBeEnabled();
    });
  });
});
