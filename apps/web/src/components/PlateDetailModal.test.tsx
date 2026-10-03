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
  // PDW-8: photo/clip moved from two always-visible sections into a "Foto"/"Clip" tablist,
  // photo selected by default, with the clip's <video> only ever mounted while its tab is
  // active (so the browser never starts fetching/buffering the clip just from opening the
  // modal).
  describe("tabs (PDW-8)", () => {
    it("shows the photo tab selected by default, with no video in the DOM at all", async () => {
      renderModal("lpr.view", "snapshots.view", "recordings.view");
      await screen.findByRole("img", { name: /Foto de la lectura de patente/ });

      const photoTab = screen.getByRole("tab", { name: "Foto" });
      const clipTab = screen.getByRole("tab", { name: "Clip" });
      expect(photoTab).toHaveAttribute("aria-selected", "true");
      expect(clipTab).toHaveAttribute("aria-selected", "false");
      expect(document.querySelector("video")).not.toBeInTheDocument();
    });

    it("mounts the video only after switching to the Clip tab, and unmounts the photo", async () => {
      renderModal("lpr.view", "snapshots.view", "recordings.view");
      await screen.findByRole("img", { name: /Foto de la lectura de patente/ });

      fireEvent.click(screen.getByRole("tab", { name: "Clip" }));

      const video = document.querySelector("video");
      expect(video).toHaveAttribute("src", "/media/v1/lpr/reads/r1/clip.mp4");
      expect(screen.getByRole("tab", { name: "Clip" })).toHaveAttribute("aria-selected", "true");
      expect(screen.getByRole("tab", { name: "Foto" })).toHaveAttribute("aria-selected", "false");
      expect(screen.queryByRole("img", { name: /Foto de la lectura de patente/ })).not.toBeInTheDocument();
    });

    it("navigates between tabs with the arrow keys", async () => {
      renderModal("lpr.view", "snapshots.view", "recordings.view");
      const photoTab = await screen.findByRole("tab", { name: "Foto" });
      photoTab.focus();

      fireEvent.keyDown(photoTab, { key: "ArrowRight" });
      const clipTab = screen.getByRole("tab", { name: "Clip" });
      expect(clipTab).toHaveAttribute("aria-selected", "true");
      expect(clipTab).toHaveFocus();

      fireEvent.keyDown(clipTab, { key: "ArrowLeft" });
      expect(screen.getByRole("tab", { name: "Foto" })).toHaveAttribute("aria-selected", "true");
      expect(screen.getByRole("tab", { name: "Foto" })).toHaveFocus();
    });

    it("each tab keeps its own watermark overlay and download control", async () => {
      renderModal("lpr.view", "snapshots.view", "recordings.view", "snapshots.download", "exports.create");
      await screen.findByRole("img", { name: /Foto de la lectura de patente/ });
      expect(screen.getByRole("link", { name: /Descargar foto/ })).toBeInTheDocument();
      expect(screen.getAllByText(/2024-01-01/, { selector: "span" })).toHaveLength(1);

      fireEvent.click(screen.getByRole("tab", { name: "Clip" }));
      expect(screen.getByRole("button", { name: "Descargar clip con marca de agua" })).toBeInTheDocument();
      expect(screen.getAllByText(/2024-01-01/, { selector: "span" })).toHaveLength(1);
    });
  });

  it("shows the photo download link only with snapshots.download", async () => {
    renderModal("lpr.view", "snapshots.view", "recordings.view");
    await screen.findByRole("img", { name: /Foto de la lectura de patente/ });
    expect(screen.queryByRole("link", { name: /Descargar foto/ })).not.toBeInTheDocument();
  });

  it("shows the photo download link with snapshots.download", async () => {
    renderModal("lpr.view", "snapshots.view", "snapshots.download");
    const link = await screen.findByRole("link", { name: /Descargar foto/ });
    expect(link).toHaveAttribute("href", "/media/v1/lpr/reads/r1/snapshot.jpg?quality=100&download=1");
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

    // The "Fecha" field (dd) always shows it; the active tab's overlay (a span) shows it too —
    // PDW-8 mounts only the active tab's panel, so only one overlay exists at a time (see the
    // "each tab keeps its own watermark overlay" test for both tabs individually).
    expect(await screen.findByText("2024-01-01 11:00:00 +01:00", { selector: "dd" })).toBeInTheDocument();
    expect(await screen.findAllByText("2024-01-01 11:00:00 +01:00", { selector: "span" })).toHaveLength(1);

    fireEvent.click(screen.getByRole("tab", { name: "Clip" }));
    expect(await screen.findAllByText("2024-01-01 11:00:00 +01:00", { selector: "span" })).toHaveLength(1);
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
    fireEvent.click(await screen.findByRole("tab", { name: "Clip" }));

    const startButton = await screen.findByRole("button", { name: "Descargar clip con marca de agua" });
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
    fireEvent.click(await screen.findByRole("tab", { name: "Clip" }));

    fireEvent.click(await screen.findByRole("button", { name: "Descargar clip con marca de agua" }));

    expect(await screen.findByText("No se pudo generar el clip.")).toBeInTheDocument();
    expect(await screen.findByText("ffmpeg: boom")).toBeInTheDocument();
  });
});
