import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, renderPage } from "@/test-utils";
import { LiveExportModal } from "./LiveExportModal";
import type { TimelineCamera } from "./DayTimeline";

afterEach(() => vi.unstubAllGlobals());

const mockCameras: TimelineCamera[] = [
  { id: "cam-1", name: "Front Gate", spans: [{ start: 100, end: 500 }] },
  { id: "cam-2", name: "Backyard", spans: [{ start: 100, end: 500 }] },
  { id: "cam-3", name: "Parking Lot", spans: [{ start: 100, end: 500 }] },
];

describe("LiveExportModal", () => {
  it("renders range, duration and camera selections", async () => {
    const range = { start: 1775600000, end: 1775600600 }; // 600s = 10m
    renderPage(() => (
      <LiveExportModal
        open={true}
        onClose={() => {}}
        range={range}
        cameras={mockCameras}
        selectedCameraId="cam-1"
      />
    ));

    expect(await screen.findByText("Exportar evidencia de video")).toBeInTheDocument();
    expect(screen.getByText("10m 00s")).toBeInTheDocument();
    expect(screen.getByText("Front Gate")).toBeInTheDocument();
    expect(screen.getByText("Backyard")).toBeInTheDocument();
    expect(screen.getByText("Parking Lot")).toBeInTheDocument();
    expect(screen.getByText("Activa")).toBeInTheDocument();
  });

  it("submits asynchronous export job to /api/v1/export-jobs", async () => {
    let capturedBody: Record<string, unknown> | null = null;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const req = input instanceof Request ? input : new Request(input, init);
        const url = new URL(req.url);
        if (url.pathname === "/api/v1/export-jobs" && req.method === "POST") {
          capturedBody = await req.json();
          return json(
            {
              id: "job-123",
              tenant_id: "t1",
              name: "Test Export",
              status: "queued",
              progress: 0,
              created_at: new Date().toISOString(),
              updated_at: new Date().toISOString(),
            },
            201,
          );
        }
        return json({ code: "not_found" }, 404);
      }),
    );

    const onClose = vi.fn();
    const range = { start: 1775600000, end: 1775600300 }; // 300s = 5m

    renderPage(() => (
      <LiveExportModal
        open={true}
        onClose={onClose}
        range={range}
        cameras={mockCameras}
        selectedCameraId="cam-1"
      />
    ));

    expect(await screen.findByText("Exportar evidencia de video")).toBeInTheDocument();

    // Select all cameras
    fireEvent.click(screen.getByText("Todas en grid"));

    // Check protected checkbox
    const protectedCheckbox = screen.getByLabelText(/Marcar como evidencia protegida/i);
    fireEvent.click(protectedCheckbox);

    // Submit
    const submitBtn = screen.getByText("Iniciar exportación");
    fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(capturedBody).not.toBeNull();
    });

    expect(capturedBody).toMatchObject({
      camera_ids: ["cam-1", "cam-2", "cam-3"],
      protected: true,
    });
    expect(await screen.findByText(/Exportación iniciada/i)).toBeInTheDocument();
  });
});
