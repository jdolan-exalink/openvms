import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import { Audit } from "./Audit";

afterEach(() => vi.unstubAllGlobals());

const entry = (id: number, action: string, actor_id: string, actor_name: string, details: Record<string, unknown> = {}) => ({
  id,
  tenant_id: "t1",
  actor_type: "user",
  actor_id,
  actor_name,
  action,
  target_type: "camera",
  target_id: "c1",
  occurred_at: "2026-09-29T10:00:00Z",
  ip: "10.1.1.50",
  details,
});

function setup() {
  const queryLog: string[] = [];
  const routes = stubApi({
    "/api/v1/users": () =>
      json({
        items: [
          { id: "u-1", username: "admin", display_name: "Administrador" },
          { id: "u-2", username: "operador", display_name: "Operador 1" },
        ],
      }),
    "/api/v1/audit": () =>
      json({
        items: [
          entry(1, "CAMERA_UPDATED", "u-1", "Administrador", { name: "Entrada Norte", enabled: true }),
          entry(2, "LOGIN_SUCCESS", "u-2", "Operador 1", { method: "password" }),
        ],
      }),
  });

  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      queryLog.push(url.search);
      return routes(req);
    }),
  );

  renderPage(Audit);
  return { queryLog };
}

describe("Audit", () => {
  it("renders PageHeader, Summary count, and readable details", async () => {
    setup();
    expect(await screen.findByRole("heading", { name: "Auditoría" })).toBeInTheDocument();
    expect(await screen.findByText("2 registros")).toBeInTheDocument();

    // Check actions and user names within table
    const table = screen.getByRole("table", { name: "Auditoría" });
    expect(within(table).getByText("Cámara modificada")).toBeInTheDocument();
    expect(within(table).getByText("Ingreso")).toBeInTheDocument();
    expect(within(table).getByText("Administrador")).toBeInTheDocument();

    // Check readable details
    expect(within(table).getByText("name:")).toBeInTheDocument();
    expect(within(table).getByText("Entrada Norte")).toBeInTheDocument();
  });

  it("filters by actor_id and action", async () => {
    const { queryLog } = setup();
    await screen.findByText("2 registros");

    fireEvent.change(screen.getByLabelText("Usuario"), { target: { value: "u-1" } });
    fireEvent.change(screen.getByLabelText("Acción"), { target: { value: "CAMERA_UPDATED" } });
    fireEvent.click(screen.getByRole("button", { name: "Filtrar" }));

    await waitFor(() => {
      const lastSearch = queryLog[queryLog.length - 1] ?? "";
      expect(lastSearch).toContain("actor_id=u-1");
      expect(lastSearch).toContain("action=CAMERA_UPDATED");
    });
  });
});
