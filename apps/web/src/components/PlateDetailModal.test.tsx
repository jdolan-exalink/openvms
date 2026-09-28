import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage, stubApi } from "@/test-utils";
import { PlateDetailModal } from "./PlateDetailModal";

afterEach(() => vi.unstubAllGlobals());

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

function brandingResponse(timezone = "America/Argentina/Buenos_Aires") {
  return json({ tenant_id: "t1", owner_name: "", timezone, has_logo: false, updated_at: "2024-01-01T00:00:00Z" });
}

const read: Schemas["PlateRead"] = {
  id: "r1",
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
};

function renderModal(...permissions: string[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request) => {
      const url = new URL(input.url);
      if (url.pathname === "/api/v1/tenants/t1/branding") return brandingResponse();
      return stubApi({ "/api/v1/me": () => meResponse(...permissions) })(input);
    }),
  );
  renderPage(() => <PlateDetailModal read={read} onClose={() => {}} />);
}

describe("PlateDetailModal", () => {
  it("shows the photo download link only with snapshots.download", async () => {
    renderModal("lpr.view", "snapshots.view", "recordings.view");
    await screen.findByRole("img", { name: /Foto de la lectura de patente/ });
    expect(screen.queryByRole("link", { name: /Descargar foto/ })).not.toBeInTheDocument();
  });

  it("shows the photo download link with snapshots.download", async () => {
    renderModal("lpr.view", "snapshots.view", "snapshots.download");
    const link = await screen.findByRole("link", { name: /Descargar foto/ });
    expect(link).toHaveAttribute("href", "/media/v1/lpr/reads/r1/snapshot.jpg?download=1");
  });

  // PDW-7: the watermark's date/time must render in the tenant's configured branding.timezone
  // (fetched via brandingQuery), not a fixed UTC — and it must be the *real* seasonal offset
  // for that zone, proving the value flows all the way from the branding API response through
  // fmtWatermarkTimestamp to the rendered "Fecha" field. read.seen_at is
  // "2024-01-01T10:00:00Z"; Europe/Madrid is CET (+01:00) in January, so the local time is
  // 11:00:00.
  it("renders the watermark timestamp in the tenant's configured time zone", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/tenants/t1/branding") return brandingResponse("Europe/Madrid");
        return stubApi({ "/api/v1/me": () => meResponse("lpr.view", "snapshots.view", "recordings.view") })(input);
      }),
    );
    renderPage(() => <PlateDetailModal read={read} onClose={() => {}} />);

    // The same text appears three times: the "Fecha" field (dd) and both overlays (photo +
    // clip, each a span) — matching PDW-2's own prior finding for this exact shape. Scope to
    // the dd to assert the "Fecha" field specifically, and separately confirm both overlay
    // spans got it too.
    expect(await screen.findByText("2024-01-01 11:00:00 +01:00", { selector: "dd" })).toBeInTheDocument();
    expect(await screen.findAllByText("2024-01-01 11:00:00 +01:00", { selector: "span" })).toHaveLength(2);
  });

  it("requests, polls and offers the download of a clip watermark job", async () => {
    let jobStatus: "queued" | "running" | "done" = "queued";
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/tenants/t1/branding") return brandingResponse();
        if (url.pathname === "/api/v1/lpr/reads/r1/clip-watermark-jobs" && input.method === "POST") {
          return json({ id: "job1", lpr_read_id: "r1", status: "queued", error: "", created_at: "2024-01-01T00:00:00Z", updated_at: "2024-01-01T00:00:00Z" });
        }
        if (url.pathname === "/api/v1/lpr/reads/r1/clip-watermark-jobs/job1") {
          const status = jobStatus;
          jobStatus = status === "queued" ? "running" : "done";
          return json({ id: "job1", lpr_read_id: "r1", status, error: "", created_at: "2024-01-01T00:00:00Z", updated_at: "2024-01-01T00:00:00Z" });
        }
        return stubApi({ "/api/v1/me": () => meResponse("lpr.view", "recordings.view", "exports.create") })(input);
      }),
    );
    renderPage(() => <PlateDetailModal read={read} onClose={() => {}} />);

    const startButton = await screen.findByRole("button", { name: "Preparar clip con marca de agua" });
    fireEvent.click(startButton);

    await waitFor(() => {
      expect(screen.getByRole("status")).toHaveTextContent("Preparando clip");
    });

    const link = await screen.findByRole("link", { name: /Descargar clip/ }, { timeout: 5000 });
    expect(link).toHaveAttribute("href", "/api/v1/lpr/reads/r1/clip-watermark-jobs/job1/download");
  });

  it("shows an error message when the clip watermark job fails", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: Request) => {
        const url = new URL(input.url);
        if (url.pathname === "/api/v1/tenants/t1/branding") return brandingResponse();
        if (url.pathname === "/api/v1/lpr/reads/r1/clip-watermark-jobs" && input.method === "POST") {
          return json({ id: "job1", lpr_read_id: "r1", status: "queued", error: "", created_at: "2024-01-01T00:00:00Z", updated_at: "2024-01-01T00:00:00Z" });
        }
        if (url.pathname === "/api/v1/lpr/reads/r1/clip-watermark-jobs/job1") {
          return json({ id: "job1", lpr_read_id: "r1", status: "failed", error: "ffmpeg: boom", created_at: "2024-01-01T00:00:00Z", updated_at: "2024-01-01T00:00:00Z" });
        }
        return stubApi({ "/api/v1/me": () => meResponse("lpr.view", "recordings.view", "exports.create") })(input);
      }),
    );
    renderPage(() => <PlateDetailModal read={read} onClose={() => {}} />);

    fireEvent.click(await screen.findByRole("button", { name: "Preparar clip con marca de agua" }));

    expect(await screen.findByText("No se pudo generar el clip.")).toBeInTheDocument();
    expect(await screen.findByText("ffmpeg: boom")).toBeInTheDocument();
  });
});
