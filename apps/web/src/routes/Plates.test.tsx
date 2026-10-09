import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage, stubApi } from "@/test-utils";
import { Plates } from "./Plates";

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
  "/api/v1/camera-groups": () => json({ items: [] }),
};

function makePlateRead(id: string, overrides: Partial<Schemas["PlateRead"]> = {}): Schemas["PlateRead"] {
  return {
    id,
    site_id: "site1",
    site_name: "Site One",
    server_id: "srv1",
    server_name: "Server One",
    camera_id: "cam1",
    camera_name: "Camera One",
    plate: "AB123CD",
    plate_normalized: "AB123CD",
    score: 0.9,
    label: "car",
    zones: [],
    seen_at: "2024-01-01T10:00:00Z",
    event_id: null,
    ...overrides,
  };
}

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
          "/api/v1/me": () => meResponse("lpr.search"),
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

  it("sends every filter field as query params", async () => {
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
          "/api/v1/me": () => meResponse("lpr.search"),
          "/api/v1/sites": () => json({ items: [{ id: "site1", name: "Site One" }] }),
          "/api/v1/camera-groups": () =>
            json({ items: [{ id: "g1", tenant_id: "t1", name: "Perimeter", description: "", camera_ids: [] }] }),
        })(input);
      }),
    );

    renderPage(Plates);

    expect(await screen.findByText("No hay lecturas que coincidan.")).toBeInTheDocument();
    await screen.findByRole("option", { name: "Perimeter" });

    fireEvent.change(screen.getByLabelText("Patente"), { target: { value: "ab123cd" } });
    fireEvent.click(screen.getByRole("switch", { name: "Coincidencia exacta" }));
    fireEvent.change(screen.getByLabelText("Sitio"), { target: { value: "site1" } });
    fireEvent.change(screen.getByLabelText("Grupo de cámaras"), { target: { value: "g1" } });
    fireEvent.change(screen.getByLabelText("Desde"), { target: { value: "2024-01-01T10:00" } });
    fireEvent.change(screen.getByLabelText("Hasta"), { target: { value: "2024-01-02T10:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Buscar" }));

    await waitFor(() => {
      const p = lastReadsUrl?.searchParams;
      expect(p?.get("plate")).toBe("AB123CD");
      expect(p?.get("exact")).toBe("true");
      expect(p?.getAll("site_id")).toEqual(["site1"]);
      expect(p?.getAll("camera_group_id")).toEqual(["g1"]);
      expect(p?.get("from")).toBe(new Date("2024-01-01T10:00").toISOString());
      expect(p?.get("to")).toBe(new Date("2024-01-02T10:00").toISOString());
    });
  });

  it("disables the Patente filter without lpr.search", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/lpr/reads") return json({ items: [] });
        return stubApi({ "/api/v1/me": () => meResponse(), ...noCatalogs })(input);
      }),
    );

    renderPage(Plates);

    await screen.findByText("No hay lecturas que coincidan.");
    expect(screen.getByLabelText("Patente")).toBeDisabled();
  });

  it("enables the Patente filter with lpr.search", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/lpr/reads") return json({ items: [] });
        return stubApi({ "/api/v1/me": () => meResponse("lpr.search"), ...noCatalogs })(input);
      }),
    );

    renderPage(Plates);

    await screen.findByText("No hay lecturas que coincidan.");
    expect(screen.getByLabelText("Patente")).toBeEnabled();
  });

  it("shows the plate photo on hover with snapshots.view and lpr.view", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/lpr/reads") return json({ items: [makePlateRead("r1")] });
        return stubApi({ "/api/v1/me": () => meResponse("lpr.search", "snapshots.view", "lpr.view"), ...noCatalogs })(input);
      }),
    );

    renderPage(Plates);

    await screen.findByText("AB123CD");
    const img = screen.getByRole("img", { name: "Lectura de patente AB123CD" });
    expect(img).toHaveAttribute("src", "/media/v1/lpr/reads/r1/snapshot.jpg?crop=1&quality=55");
  });

  it("does not show the plate photo preview without both snapshots.view and lpr.view", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/lpr/reads") return json({ items: [makePlateRead("r1")] });
        // Has lpr.search and snapshots.view but not lpr.view: the design requires BOTH
        // snapshots.view and lpr.view for the preview, since the photo shows the plate.
        return stubApi({ "/api/v1/me": () => meResponse("lpr.search", "snapshots.view"), ...noCatalogs })(input);
      }),
    );

    renderPage(Plates);

    await screen.findByText("AB123CD");
    expect(screen.queryByRole("img", { name: /Lectura de patente/ })).not.toBeInTheDocument();
  });

  it("opens the plate detail modal with photo, clip and watermark overlay", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/lpr/reads") return json({ items: [makePlateRead("r1")] });
        if (url.pathname === "/api/v1/tenants/t1/branding") {
          return json({
            tenant_id: "t1",
            owner_name: "Municipalidad de Helvecia",
            // PDW-7: America/Argentina/Buenos_Aires is -03:00 year-round (no DST); seen_at
            // 10:00:00Z -> 07:00:00 -03:00.
            timezone: "America/Argentina/Buenos_Aires",
            has_logo: false,
            updated_at: "2024-01-01T00:00:00Z",
          });
        }
        return stubApi({ "/api/v1/me": () => meResponse("lpr.search", "lpr.view", "snapshots.view", "recordings.view"), ...noCatalogs })(input);
      }),
    );

    renderPage(Plates);
    fireEvent.click(await screen.findByRole("button", { name: /AB123CD/ }));

    const dialog = await screen.findByRole("dialog", { name: "Patente AB123CD" });
    expect(dialog).toHaveAttribute("aria-modal", "true");
    // PDW-8: photo/clip are tabs now, photo selected by default — no <video> mounted yet.
    expect(screen.getByRole("img", { name: "Foto de la lectura de patente AB123CD" })).toHaveAttribute(
      "src",
      "/media/v1/lpr/reads/r1/snapshot.jpg?quality=100",
    );
    expect(dialog).toHaveTextContent("AB 123 CD");
    expect(dialog.querySelector("video")).not.toBeInTheDocument();
    const photoOverlay = await screen.findByText(/Municipalidad de Helvecia/, { selector: "span" });
    expect(photoOverlay).toHaveTextContent("2024-01-01 07:00:00 -03:00");

    fireEvent.click(screen.getByRole("tab", { name: "Clip" }));
    const video = dialog.querySelector("video");
    expect(video).toHaveAttribute("src", "/media/v1/lpr/reads/r1/clip.mp4");
    const clipOverlay = await screen.findByText(/Municipalidad de Helvecia/, { selector: "span" });
    expect(clipOverlay).toHaveTextContent("2024-01-01 07:00:00 -03:00");

    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("does not offer detail without lpr.view", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/lpr/reads") return json({ items: [makePlateRead("r1")] });
        return stubApi({ "/api/v1/me": () => meResponse("lpr.search", "snapshots.view"), ...noCatalogs })(input);
      }),
    );

    renderPage(Plates);
    await screen.findByText("AB123CD");
    expect(screen.queryByRole("button", { name: "Ver detalle" })).not.toBeInTheDocument();
  });

  it("loads the next page via cursor when Siguiente is clicked", async () => {
    const readsCalls: URL[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/lpr/reads") {
          readsCalls.push(url);
          if (!url.searchParams.get("cursor")) {
            return json({ items: [makePlateRead("r1")], next_cursor: "page2" });
          }
          return json({ items: [makePlateRead("r2", { plate_normalized: "XY987ZZ" })] });
        }
        return stubApi({ "/api/v1/me": () => meResponse("lpr.search"), ...noCatalogs })(input);
      }),
    );

    renderPage(Plates);

    await screen.findByText("AB123CD");
    expect(screen.queryByText("XY987ZZ")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Siguiente" }));

    await waitFor(() => {
      expect(readsCalls.some((u) => u.searchParams.get("cursor") === "page2")).toBe(true);
    });
    await waitFor(() => {
      expect(screen.getByText("XY987ZZ")).toBeInTheDocument();
    });
    expect(screen.queryByText("AB123CD")).not.toBeInTheDocument();
  });

  it("filters the list as each plate character is typed", async () => {
    let lastReadsUrl: URL | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/lpr/reads") {
          lastReadsUrl = url;
          return json({ items: [] });
        }
        return stubApi({ "/api/v1/me": () => meResponse("lpr.search"), ...noCatalogs })(input);
      }),
    );
    renderPage(Plates);
    await screen.findByText("No hay lecturas que coincidan.");
    fireEvent.change(screen.getByLabelText("Patente"), { target: { value: "ab1" } });
    await waitFor(() => expect(lastReadsUrl?.searchParams.get("plate")).toBe("AB1"));
    fireEvent.change(screen.getByLabelText("Patente"), { target: { value: "ab12" } });
    await waitFor(() => expect(lastReadsUrl?.searchParams.get("plate")).toBe("AB12"));
  });

  it("shows applied-filter chips and a result count, and clears them", async () => {
    let lastReadsUrl: URL | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/lpr/reads") {
          lastReadsUrl = url;
          return json({ items: [makePlateRead("r1"), makePlateRead("r2")] });
        }
        return stubApi({ "/api/v1/me": () => meResponse("lpr.search"), ...noCatalogs })(input);
      }),
    );

    renderPage(Plates);
    await screen.findByRole("list", { name: "Lecturas LPR" });
    expect(screen.getByRole("status")).toHaveTextContent("2 lecturas");
    expect(screen.queryByRole("list", { name: "Filtros aplicados" })).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Patente"), { target: { value: "ab12" } });
    fireEvent.click(screen.getByRole("switch", { name: "Coincidencia exacta" }));
    fireEvent.click(screen.getByRole("button", { name: "Buscar" }));

    const chips = await screen.findByRole("list", { name: "Filtros aplicados" });
    expect(chips).toHaveTextContent("Patente: AB12");
    expect(chips).toHaveTextContent("Coincidencia exacta");

    fireEvent.click(screen.getByRole("button", { name: "Quitar filtro Coincidencia exacta" }));
    await waitFor(() => {
      expect(lastReadsUrl?.searchParams.get("plate")).toBe("AB12");
      expect(lastReadsUrl?.searchParams.get("exact")).toBeNull();
    });

    fireEvent.click(screen.getByRole("button", { name: "Limpiar filtros" }));
    await waitFor(() => expect(lastReadsUrl?.searchParams.get("plate")).toBeNull());
    expect((screen.getByLabelText("Patente") as HTMLInputElement).value).toBe("");
    expect(screen.queryByRole("list", { name: "Filtros aplicados" })).not.toBeInTheDocument();
  });
});
