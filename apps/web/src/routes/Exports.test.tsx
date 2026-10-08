import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage } from "@/test-utils";
import { Exports } from "./Exports";

afterEach(() => vi.unstubAllGlobals());

describe("Exports", () => {
  it("renders empty state when no export jobs or exports exist", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const req = input instanceof Request ? input : new Request(input, init);
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/export-jobs") {
          return json({ items: [] });
        }
        if (url.pathname === "/api/v1/exports") {
          return json({ items: [] });
        }
        return json({});
      }),
    );

    renderPage(() => <Exports />);

    expect(await screen.findByText("Todavía no hay exportaciones.")).toBeInTheDocument();
  });

  it("renders multi-camera export job with progress, speed, ETA, and expanded camera details", async () => {
    const mockJobs = [
      {
        id: "job-123",
        name: "Investigación Portón Principal",
        start_time: "2026-10-06T10:14:20Z",
        end_time: "2026-10-06T10:24:20Z",
        status: "transferring",
        progress: 72,
        error: "",
        total_bytes: 2540000000,
        transferred_bytes: 1820000000,
        speed_bps: 8600000,
        eta_seconds: 86,
        camera_count: 2,
        protected: true,
        requested_by_name: "admin",
        created_at: "2026-10-06T10:25:00Z",
        items: [
          {
            id: "item-1",
            job_id: "job-123",
            camera_id: "cam-1",
            camera_name: "Acceso Norte",
            server_name: "Frigate H01",
            status: "ready",
            progress: 100,
            error: "",
            total_bytes: 1000000000,
            transferred_bytes: 1000000000,
            sha256_hash: "a7f5e3b1c9d8a4f2e0b6c8d7e9f1a3b5c7d9e1f3a5b7c9d1e3f5a7b9c1d3e5f7",
            created_at: "2026-10-06T10:25:00Z",
          },
          {
            id: "item-2",
            job_id: "job-123",
            camera_id: "cam-2",
            camera_name: "Plaza Centro",
            server_name: "Frigate H01",
            status: "transferring",
            progress: 44,
            error: "",
            total_bytes: 1540000000,
            transferred_bytes: 820000000,
            sha256_hash: "",
            created_at: "2026-10-06T10:25:00Z",
          },
        ],
      },
    ];

    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const req = input instanceof Request ? input : new Request(input, init);
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/export-jobs") {
          return json({ items: mockJobs });
        }
        if (url.pathname === "/api/v1/exports") {
          return json({ items: [] });
        }
        return json({});
      }),
    );

    renderPage(() => <Exports />);

    // Job name and evidence protection badge
    expect(await screen.findByText("Investigación Portón Principal")).toBeInTheDocument();
    expect(screen.getByText("Evidencia protegida")).toBeInTheDocument();

    // Cameras count button
    const camCountBtn = screen.getByText("2 cámaras");
    expect(camCountBtn).toBeInTheDocument();

    // Progress percentage, speed, and ETA
    expect(screen.getByText("72%")).toBeInTheDocument();
    expect(screen.getByText("8.2 Mbps")).toBeInTheDocument();
    expect(screen.getByText("ETA 01:26")).toBeInTheDocument();

    // Cancel action is available for transferring job
    expect(screen.getByTitle("Cancelar exportación")).toBeInTheDocument();

    // Expand cameras accordion
    fireEvent.click(camCountBtn);

    // Shows included cameras
    expect(await screen.findByText("Cámaras incluidas (2)")).toBeInTheDocument();
    expect(screen.getByText("Acceso Norte")).toBeInTheDocument();
    expect(screen.getByText("Plaza Centro")).toBeInTheDocument();

    // Forensic SHA-256 hash indicator
    expect(screen.getByText(/SHA-256: a7f5e3b1c9/)).toBeInTheDocument();
  });

  it("renders ready multi-camera job with ZIP download link", async () => {
    const mockJobs = [
      {
        id: "job-ready",
        name: "Incidente Seguridad",
        start_time: "2026-10-06T10:00:00Z",
        end_time: "2026-10-06T10:10:00Z",
        status: "ready",
        progress: 100,
        error: "",
        total_bytes: 524288000, // 500 MB
        transferred_bytes: 524288000,
        speed_bps: 0,
        eta_seconds: 0,
        camera_count: 3,
        protected: false,
        requested_by_name: "operador",
        created_at: "2026-10-06T10:15:00Z",
      },
    ];

    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const req = input instanceof Request ? input : new Request(input, init);
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/export-jobs") {
          return json({ items: mockJobs });
        }
        if (url.pathname === "/api/v1/exports") {
          return json({ items: [] });
        }
        return json({});
      }),
    );

    renderPage(() => <Exports />);

    expect(await screen.findByText("Incidente Seguridad")).toBeInTheDocument();
    expect(screen.getByText("Lista")).toBeInTheDocument();
    expect(screen.getByText("500.00 MB")).toBeInTheDocument();

    const downloadBtn = screen.getByRole("link", { name: /Descargar ZIP/i });
    expect(downloadBtn).toHaveAttribute("href", "/media/v1/export-jobs/job-ready/download");
  });
});
