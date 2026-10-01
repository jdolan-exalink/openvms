import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "../../../test-utils";
import { CsvImportForm } from "./CsvImportForm";

const CSV = "camera,lat,lng\nAcceso Norte,-34.6,-58.4\n";

const importRequests = (fetchSpy: { mock: { calls: unknown[][] } }) =>
  fetchSpy.mock.calls
    .map(([input]) => input as Request)
    .filter((request) => request && typeof request === "object" && request.method === "POST"
      && new URL(request.url).pathname === "/api/v1/maps/placements/import");

function stubImport(report: unknown) {
  const fetchSpy = vi.fn(stubApi({ "POST /api/v1/maps/placements/import": () => json(report) }));
  vi.stubGlobal("fetch", fetchSpy);
  return fetchSpy;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe("CsvImportForm", () => {
  it("validates with dry_run and renders the report without errors", async () => {
    const fetchSpy = stubImport({ dry_run: true, rows: 1, upserted: 0, errors: [] });
    render(<CsvImportForm siteId="site-1" onClose={vi.fn()} />);

    fireEvent.change(screen.getByRole("textbox"), { target: { value: CSV } });
    fireEvent.click(screen.getByRole("button", { name: "Validar" }));

    await screen.findByText(/1 filas/);
    expect(screen.getByText(/sin errores/i)).toBeInTheDocument();
    const [request] = importRequests(fetchSpy);
    expect(request).toBeTruthy();
    const body = JSON.parse(await request!.text()) as Record<string, unknown>;
    expect(body).toMatchObject({ site_id: "site-1", csv: CSV, dry_run: true });
  });

  it("imports with dry_run false and lists the rejected lines", async () => {
    const fetchSpy = stubImport({
      dry_run: false,
      rows: 2,
      upserted: 0,
      errors: [{ line: 3, message: "latitud fuera de rango" }],
    });
    render(<CsvImportForm siteId="site-1" onClose={vi.fn()} />);

    fireEvent.change(screen.getByRole("textbox"), { target: { value: CSV } });
    fireEvent.click(screen.getByRole("button", { name: "Importar" }));

    await screen.findByText(/línea 3/);
    const [request] = importRequests(fetchSpy);
    const body = JSON.parse(await request!.text()) as Record<string, unknown>;
    expect(body).toMatchObject({ dry_run: false });
  });

  it("tells the caller to refresh after an apply wrote placements", async () => {
    stubImport({ dry_run: false, rows: 1, upserted: 1, errors: [] });
    const onApplied = vi.fn();
    render(<CsvImportForm siteId="site-1" onClose={vi.fn()} onApplied={onApplied} />);

    fireEvent.change(screen.getByRole("textbox"), { target: { value: CSV } });
    fireEvent.click(screen.getByRole("button", { name: "Importar" }));

    await waitFor(() => expect(onApplied).toHaveBeenCalledTimes(1));
  });

  it("does not refresh after a dry run", async () => {
    stubImport({ dry_run: true, rows: 1, upserted: 0, errors: [] });
    const onApplied = vi.fn();
    render(<CsvImportForm siteId="site-1" onClose={vi.fn()} onApplied={onApplied} />);

    fireEvent.change(screen.getByRole("textbox"), { target: { value: CSV } });
    fireEvent.click(screen.getByRole("button", { name: "Validar" }));

    await screen.findByText(/1 filas/);
    expect(onApplied).not.toHaveBeenCalled();
  });

  it("closes through the cancel button", () => {
    const onClose = vi.fn();
    render(<CsvImportForm siteId="site-1" onClose={onClose} />);

    fireEvent.click(screen.getByRole("button", { name: "Cancelar" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
