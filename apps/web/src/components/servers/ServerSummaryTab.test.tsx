import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage, stubApi } from "@/test-utils";
import { ServerSummaryTab } from "./ServerSummaryTab";

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

const caps = { review: true, preview: true, exports: false, lpr: true, face_recognition: false, semantic_search: false, audio: true, ptz: false };
const server = (extra: object = {}) => ({
  id: "a", tenant_id: "t", site_id: "s1", name: "frigate-h01", base_url: "https://h01:8971", auth_mode: "credentials", username: "admin",
  tls_skip_verify: false, frigate_version: "0.17.1", capabilities: caps, status: "online", last_error: "", camera_count: 12,
  uptime_seconds: 90061, last_seen_at: "2026-10-26T12:00:00Z", storage: { total_mb: 2000000, free_mb: 500000 },
  agent_status: "online", agent_last_seen_at: "2026-10-26T12:01:00Z", agent_cert_expires_at: "2027-01-01T00:00:00Z",
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", ...extra,
}) as unknown as Schemas["Server"];

const same = { sha256: "1".repeat(64), architecture: "amd64", version: "0.4.4" };
const current = { installed: true, version: "0.4.4", binary_status: "current", binary_outdated: false, binary_upgrade_available: false, binary_observed: same, binary_available: same };
const updatable = { ...current, binary_status: "update_available", binary_outdated: true, binary_upgrade_available: true, binary_available: { ...same, sha256: "2".repeat(64), version: "0.5.0" } };

function stub(agent: object) {
  vi.stubGlobal("fetch", vi.fn(stubApi({ "/api/v1/servers/a/agent": () => json(agent) })));
}

describe("ServerSummaryTab", () => {
  it("shows Frigate, camera count, storage, agent and capabilities", async () => {
    stub(current);
    renderPage(() => <ServerSummaryTab server={server()} onEditCameras={() => {}} />);
    expect(await screen.findByText("0.17.1")).toBeInTheDocument();
    const frigate = screen.getByRole("region", { name: "Frigate" });
    expect(within(frigate).getByText("En línea")).toBeInTheDocument();
    expect(within(frigate).getByText("1d 1h 1m")).toBeInTheDocument();
    expect(screen.getByText("12 cámaras")).toBeInTheDocument();
    const storage = screen.getByRole("region", { name: "Almacenamiento" });
    expect(within(storage).getByText("2.10 TB")).toBeInTheDocument();
    expect(within(storage).getByText("1.57 TB")).toBeInTheDocument();
    expect(within(storage).getByText("524.29 GB")).toBeInTheDocument();
    expect(await screen.findByText("0.4.4")).toHaveAttribute("data-version-tone", "ok");
    const caps = screen.getByRole("list", { name: "Capacidades" });
    expect(caps).toHaveTextContent("Lectura de patentes");
    expect(caps.querySelector('[data-enabled="true"]')).toBeTruthy();
    const disabled = screen.getByText("Exportaciones").closest("li")!;
    expect(disabled).toHaveAttribute("data-enabled", "false");
    expect(screen.getByText("Lectura de patentes").closest("li")).toHaveAttribute("data-enabled", "true");
  });

  it("marks an agent with a newer bundled version as an update", async () => {
    stub(updatable);
    renderPage(() => <ServerSummaryTab server={server()} onEditCameras={() => {}} />);
    expect(await screen.findByText("0.4.4")).toHaveAttribute("data-version-tone", "warn");
  });

  it("says when there is no agent", async () => {
    stub({ installed: false, binary_status: "not_installed", binary_upgrade_available: false });
    renderPage(() => <ServerSummaryTab server={server()} onEditCameras={() => {}} />);
    expect(await screen.findByText("Agente no instalado")).toBeInTheDocument();
  });

  it("lists why a degraded server is degraded", async () => {
    stub(current);
    renderPage(() => <ServerSummaryTab server={server({ status: "degraded", problems: [{ code: "cameras_offline", cameras: ["Escuela422"] }, { code: "storage_high", used_percent: 95 }] })} onEditCameras={() => {}} />);
    const list = await screen.findByRole("list", { name: "Motivos del estado degradado" });
    expect(list).toHaveTextContent("Escuela422");
    expect(list).toHaveTextContent("95");
  });

  it("calls onEditCameras from the Editar cámaras button", async () => {
    stub(current);
    const onEditCameras = vi.fn();
    renderPage(() => <ServerSummaryTab server={server()} onEditCameras={onEditCameras} />);
    fireEvent.click(await screen.findByRole("button", { name: "Editar cámaras" }));
    expect(onEditCameras).toHaveBeenCalledTimes(1);
  });

  it("renders the original-config backup line only when a backup state is passed", async () => {
    stub(current);
    const view = renderPage(() => <ServerSummaryTab server={server()} onEditCameras={() => {}} />);
    await screen.findByText("0.4.4");
    expect(screen.queryByText("Copia de la configuración original")).not.toBeInTheDocument();
    view.unmount();
    renderPage(() => <ServerSummaryTab server={server()} backup={{ present: true }} onEditCameras={() => {}} />);
    expect(await screen.findByText("Copia de la configuración original")).toBeInTheDocument();
    expect(screen.getByText("Guardada")).toBeInTheDocument();
  });
});
