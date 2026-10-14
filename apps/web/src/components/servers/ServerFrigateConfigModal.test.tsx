import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { json, renderPage, stubApi } from "@/test-utils";
import { ServerFrigateConfigModal } from "./ServerFrigateConfigModal";

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

const mockServer: Schemas["Server"] = {
  id: "srv-1",
  name: "Server Main",
  site_id: "site-1",
  tenant_id: "t-1",
  base_url: "http://192.168.1.164:5000",
  status: "online",
  frigate_version: "0.14.1",
  camera_count: 3,
  auth_mode: "none",
  username: "",
  tls_skip_verify: false,
  capabilities: {},
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as Schemas["Server"];

const sampleYaml = `mqtt:
  host: 192.168.1.50
  port: 1883
  topic_prefix: frigate

detectors:
  cpu1:
    type: cpu
    num_threads: 3

objects:
  track:
    - person
    - car

record:
  retain:
    days: 7
    mode: motion

snapshots:
  retain:
    default: 14

birdseye:
  enabled: true
  mode: motion
`;

const mockRevisions = [
  {
    id: "rev-20000000",
    server_id: "srv-1",
    actor_name: "admin",
    created_at: "2026-10-14T18:00:00Z",
    sections: ["mqtt", "detectors"],
    yaml: sampleYaml,
  },
  {
    id: "rev-10000000",
    server_id: "srv-1",
    actor_name: "system",
    created_at: "2026-10-14T17:00:00Z",
    sections: ["mqtt"],
    yaml: "mqtt:\n  host: 192.168.1.10\n",
  },
];

function setupConfigModal(customHandler?: (req: Request) => Promise<Response>) {
  const calls: Request[] = [];
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const urlStr = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    const url = new URL(urlStr, "http://localhost");
    const method = typeof input === "string" || input instanceof URL ? (init?.method ?? "GET") : input.method;
    const request = new Request(url.toString(), {
      method,
      headers: init?.headers,
      body: init?.body,
    });
    calls.push(request);

    if (customHandler) {
      const handled = await customHandler(request);
      if (handled) return handled;
    }

    if (url.pathname.includes("/frigate-config/raw")) {
      if (method === "GET") {
        return json({ yaml: sampleYaml, version: "0.14.1" });
      }
      if (method === "PUT") {
        return json({ success: true });
      }
    }

    if (url.pathname.includes("/frigate-config/revisions")) {
      return json({ items: mockRevisions });
    }

    return stubApi({
      "/api/v1/me": () =>
        json({
          id: "u1",
          username: "admin",
          grants: [{ permission: "servers.manage", effect: "allow" }],
        }),
    })(request);
  });

  vi.stubGlobal("fetch", fetchMock);
  return { calls };
}

describe("ServerFrigateConfigModal", () => {
  it("renders modal with tabs and loads current MQTT configuration", async () => {
    setupConfigModal();
    const onClose = vi.fn();

    renderPage(() => (
      <ServerFrigateConfigModal
        server={mockServer}
        open={true}
        onClose={onClose}
        canRestart={true}
      />
    ));

    expect(await screen.findByText(/Configuración de Servidor Frigate/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /MQTT/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Detectores/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Objetos/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Grabación/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Birdseye/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Editor YAML/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Historial/i })).toBeInTheDocument();

    // MQTT tab is active by default and displays parsed host & port
    expect(await screen.findByDisplayValue("192.168.1.50")).toBeInTheDocument();
    expect(screen.getByDisplayValue("1883")).toBeInTheDocument();
  });

  it("switches tabs and displays detector presets", async () => {
    setupConfigModal();

    renderPage(() => (
      <ServerFrigateConfigModal
        server={mockServer}
        open={true}
        onClose={() => {}}
        canRestart={true}
      />
    ));

    await screen.findByText(/Configuración de Servidor Frigate/i);

    // Switch to Detectores IA
    fireEvent.click(screen.getByRole("button", { name: /Detectores/i }));
    expect(await screen.findByText(/Google Coral EdgeTPU/i)).toBeInTheDocument();
    expect(screen.getByText(/Intel OpenVINO/i)).toBeInTheDocument();
    expect(screen.getByText(/NVIDIA TensorRT/i)).toBeInTheDocument();
  });

  it("switches to Editor YAML and allows editing and saving", async () => {
    const { calls } = setupConfigModal();

    renderPage(() => (
      <ServerFrigateConfigModal
        server={mockServer}
        open={true}
        onClose={() => {}}
        canRestart={true}
      />
    ));

    await screen.findByText(/Configuración de Servidor Frigate/i);

    // Switch to Editor YAML
    fireEvent.click(screen.getByRole("button", { name: /Editor YAML/i }));
    const editor = await screen.findByRole("textbox");
    expect(editor).toHaveValue(sampleYaml);

    // Modify YAML
    const updatedYaml = sampleYaml.replace("192.168.1.50", "192.168.1.99");
    fireEvent.change(editor, { target: { value: updatedYaml } });
    expect(editor).toHaveValue(updatedYaml);

    // Save changes
    const saveBtn = screen.getByRole("button", { name: /Guardar cambios/i });
    fireEvent.click(saveBtn);

    await waitFor(() => {
      const putCall = calls.find(
        (c) =>
          c.method === "PUT" &&
          new URL(c.url).pathname.endsWith("/frigate-config/raw"),
      );
      expect(putCall).toBeDefined();
    });
  });

  it("switches to Historial and displays past revisions", async () => {
    setupConfigModal();

    renderPage(() => (
      <ServerFrigateConfigModal
        server={mockServer}
        open={true}
        onClose={() => {}}
        canRestart={true}
      />
    ));

    await screen.findByText(/Configuración de Servidor Frigate/i);

    // Switch to Historial
    fireEvent.click(screen.getByRole("button", { name: /Historial/i }));
    expect(await screen.findByText(/Modificado por admin/i)).toBeInTheDocument();
    expect(screen.getByText(/Modificado por system/i)).toBeInTheDocument();
  });
});
