import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { json, stubApi } from "@/test-utils";
import { DetectionTab } from "../detection/DetectionTab";
import { CameraChangeProvider } from "../CameraChangeProvider";
import { AdvancedTab } from "./AdvancedTab";
import { YamlEntryProvider } from "./YamlEntry";

afterEach(() => vi.unstubAllGlobals());

const state = { desired_revision: 4, applied_revision: 4, sync_state: "synced", last_error: "", drift: [] };
const frigate = { available: true, version: "0.16", sections: ["detect", "record"], managed_sections: ["detect"], desired_revision: 4, applied_revision: 4, sync_state: "synced" };
const onvif = { configured: true, endpoint: "http://10.0.0.5", manufacturer: "Acme", model: "X1", profiles: [{ token: "p1", name: "Main", encoding: "H264", width: 1920, height: 1080, fps: 25, bitrate: 4096, stream_uri: "rtsp://10.0.0.5/main" }], capabilities: { has_ptz: true }, services: [], reported: { Hostname: "cam-1" } };
const provision = { evidence: true, tags: ["provision"], manufacturer: "Acme", link: { reachable: true, registered: true, online: true, event_count: 3 } };
const entry = (over: Record<string, unknown>) => ({ id: 1, time: "2026-01-02T03:04:05Z", user: "ana", origin: "ui", result: "ok", changes: [{ path: "detection.enabled", from: false, to: true }], rollbackable: false, ...over });

function mount(opts: { advanced?: unknown; history?: unknown[]; rollback?: () => Response; props?: Partial<React.ComponentProps<typeof AdvancedTab>> } = {}) {
  const calls: Array<{ url: string; body: unknown }> = [];
  const h = stubApi({
    "/api/v1/cameras/c1/advanced": () => json(opts.advanced ?? { camera_id: "c1", frigate, onvif: { ...onvif, configured: false }, provision: { ...provision, evidence: false } }),
    "/api/v1/cameras/c1/normalized-config/history": () => json(opts.history ?? []),
    "/api/v1/cameras/c1/normalized-config": () => json({ state }),
    "/api/v1/cameras/c1/frigate-yaml": () => json({ camera_id: "c1", section: "x", yaml: "a: 1\n", exists: true, secrets_visible: true, managed_paths: [], revision: 4 }),
    ...(opts.rollback ? { "/api/v1/cameras/c1/normalized-config/rollback": opts.rollback } : {}),
  });
  vi.stubGlobal("fetch", async (req: Request) => {
    calls.push({ url: new URL(req.url).pathname, body: req.method === "POST" ? await req.clone().json() : undefined });
    return h(req);
  });
  return calls;
}
const wrap = (ui: React.ReactNode) => render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>{ui}</QueryClientProvider>);
const tab = (props: Partial<React.ComponentProps<typeof AdvancedTab>> = {}) => <AdvancedTab cameraId="c1" canAdvanced canYaml canRollback {...props} />;

describe("AdvancedTab", () => {
  it("shows only the engine panels that have data, Frigate by default", async () => {
    mount();
    wrap(tab());
    expect(await screen.findByRole("region", { name: "Panel Frigate" })).toBeInTheDocument();
    const nav = within(screen.getByRole("navigation", { name: "Motor" }));
    expect(nav.getAllByRole("button").map((b) => b.textContent)).toEqual(["Frigate"]);
    expect(screen.getByRole("region", { name: "Editar YAML de esta cámara" })).toBeInTheDocument();
  });

  it("renders the ONVIF panel read-only with the redacted URI as received, and the Provision panel", async () => {
    mount({ advanced: { camera_id: "c1", frigate, onvif, provision } });
    wrap(tab({ engine: "onvif" }));
    expect(await screen.findByText("rtsp://10.0.0.5/main")).toBeInTheDocument();
    expect(screen.getByText("has_ptz: sí")).toBeInTheDocument();
    expect(screen.getByText("cam-1")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Provision" }));
    expect(await screen.findByText("En línea")).toBeInTheDocument();
  });

  it("explains the missing permission instead of calling the advanced endpoint", async () => {
    const calls = mount();
    wrap(tab({ canAdvanced: false }));
    expect(await screen.findByText(/cameras.config.advanced/)).toBeInTheDocument();
    expect(calls.some((c) => c.url.endsWith("/advanced"))).toBe(false);
  });

  it("hides the editor with the reason when the user lacks cameras.config.yaml", async () => {
    mount();
    wrap(tab({ canYaml: false }));
    expect(await screen.findByText(/requiere el permiso cameras.config.yaml/)).toBeInTheDocument();
    expect(screen.queryByLabelText("YAML de detect")).toBeNull();
  });

  it("opens the YAML editor preselected from a contextual entry point in the Detección tab", async () => {
    mount();
    wrap(
      <CameraChangeProvider cameraId="c1">
        <Host />
      </CameraChangeProvider>,
    );
    fireEvent.click(await screen.findByText("Avanzado de detección (Frigate)"));
    fireEvent.click(screen.getByRole("button", { name: "Editar YAML de detección" }));
    expect(await screen.findByLabelText("Sección")).toHaveValue("detect");
    expect(await screen.findByLabelText("YAML de detect")).toBeInTheDocument();
  });

  it("has no contextual entry point without a provider (no cameras.config.yaml)", async () => {
    mount();
    wrap(
      <CameraChangeProvider cameraId="c1">
        <DetectionTab settings={{ motion: {}, objects: {}, zones: [] }} onMotion={vi.fn()} onObjects={vi.fn()} onOpenZones={vi.fn()} legacy={<p>legacy</p>} />
      </CameraChangeProvider>,
    );
    await screen.findByText("Avanzado de detección (Frigate)");
    expect(screen.queryByRole("button", { name: "Editar YAML de detección" })).toBeNull();
  });

  it("lists the change history and rolls back a rollbackable entry after confirming", async () => {
    const calls = mount({
      history: [entry({ id: 9, rollbackable: true }), entry({ id: 8, result: "error", rollbackable: false, error: { message: "No se pudo iniciar el stream de grabación.", technical: "ffmpeg exited with code 1" } }), entry({ id: 7, origin: "yaml", section: "detect", rollbackable: false, rollback_reason: "es una edición de YAML" })],
      rollback: () => json({ rolled_back: 9, result: { result: "ok", state, review: { changes: [], impact: [], unsupported: [] } } }),
    });
    wrap(tab());
    const list = await screen.findByRole("region", { name: "Historial de cambios" });
    expect((await within(list).findAllByText("detection.enabled: OFF → ON", { selector: "li" })).length).toBeGreaterThan(0);
    expect(within(list).getByText("No se pudo iniciar el stream de grabación.")).toBeInTheDocument();
    expect(within(list).queryByText("ffmpeg exited with code 1")).toBeNull();
    expect(within(list).getByText(/es una edición de YAML/)).toBeInTheDocument();
    const buttons = within(list).getAllByRole("button", { name: "Restaurar configuración anterior" });
    expect(buttons).toHaveLength(1);
    fireEvent.click(buttons[0]!);
    const dialog = await screen.findByRole("dialog", { name: "Restaurar configuración anterior" });
    expect(within(dialog).getByText("detection.enabled: ON → OFF")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Restaurar" }));
    expect(await within(list).findByText("Configuración restaurada.")).toBeInTheDocument();
    expect(calls.find((c) => c.url.endsWith("/rollback"))?.body).toEqual({ expected_revision: 4, audit_id: 9 });
  });
});

function Host() {
  const [req, setReq] = useStateReq();
  return (
    <YamlEntryProvider openYaml={(section) => setReq({ section })}>
      <DetectionTab settings={{ motion: {}, objects: {}, zones: [] }} onMotion={vi.fn()} onObjects={vi.fn()} onOpenZones={vi.fn()} legacy={<p>legacy</p>} />
      {req && <AdvancedTab cameraId="c1" yamlRequest={req} canAdvanced canYaml canRollback />}
    </YamlEntryProvider>
  );
}
import { useState } from "react";
function useStateReq() {
  return useState<{ section: string } | undefined>();
}
