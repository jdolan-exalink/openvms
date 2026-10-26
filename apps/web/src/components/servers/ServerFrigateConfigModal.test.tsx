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

    if (url.pathname.endsWith("/agent")) {
      return json({
        installed: true,
        version: "1.0.0",
        current_version: "1.0.0",
        outdated: false,
        binary_status: "running",
        binary_upgrade_available: false,
        nvidia: true,
        coral_pcie: true,
        coral_usb: false,
        openvino: true,
        hailo: true,
        gpu_present: true,
        gpu_name: "NVIDIA RTX 4060",
        cctv_total_bytes: 1_000_000_000_000,
        cctv_free_bytes: 400_000_000_000,
        database_total_bytes: 100_000_000_000,
        database_free_bytes: 50_000_000_000,
        storage_buffer_percent: 15,
        storage_watchdog: true,
        storage_over_85_percent: false,
        database_critical: false,
        pruned_segments_count: 0,
      });
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
    expect(await screen.findByRole("button", { name: /Google Coral EdgeTPU \(USB\)/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Intel OpenVINO/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /NVIDIA TensorRT/i })).toBeInTheDocument();
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

    const preview = screen.getByRole("region", { name: /Vista previa de cambios/i });
    expect(preview).toHaveTextContent("- host: 192.168.1.50");
    expect(preview).toHaveTextContent("+ host: 192.168.1.99");
    expect(calls.some((c) => c.method === "PUT" && new URL(c.url).pathname.endsWith("/frigate-config/raw"))).toBe(false);

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
    expect(screen.queryByRole("region", { name: /Vista previa de cambios/i })).not.toBeInTheDocument();
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

  it("displays detected remote AI hardware chips and presets for Coral PCIe and Hailo-8", async () => {
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

    // Verify detected hardware chips
    expect(await screen.findByText("NVIDIA RTX 4060")).toBeInTheDocument();
    expect(screen.getByText("Google Coral (PCIe)")).toBeInTheDocument();
    expect(screen.getByText("Hailo-8 AI")).toBeInTheDocument();
    expect(screen.getByText("Intel OpenVINO")).toBeInTheDocument();

    // Verify presets
    const coralPcieBtn = screen.getByRole("button", { name: /Google Coral EdgeTPU \(PCIe\)/i });
    expect(coralPcieBtn).toBeInTheDocument();

    const hailoBtn = screen.getByRole("button", { name: /Hailo-8 AI NPU/i });
    expect(hailoBtn).toBeInTheDocument();

    // Click Coral PCIe preset and verify detectors block updated
    fireEvent.click(coralPcieBtn);
    expect(screen.getByText(/device: pci:0/i)).toBeInTheDocument();

    // Click Hailo-8 preset and verify detectors block updated
    fireEvent.click(hailoBtn);
    expect(screen.getByText(/type: hailo8/i)).toBeInTheDocument();
  });

  it("switches to Objetos tab, renders categories with icons, and toggles objects", async () => {
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

    // Switch to Objetos tab
    fireEvent.click(screen.getByRole("button", { name: /Objetos/i }));

    // Verify category headers exist
    expect(await screen.findByText(/Vehículos/i)).toBeInTheDocument();
    expect(screen.getByText(/Animales/i)).toBeInTheDocument();
    expect(screen.getByText(/Accesorios & Paquetes/i)).toBeInTheDocument();

    // Verify object buttons exist with labels
    const dogBtn = screen.getByRole("button", { name: /Perro/i });
    expect(dogBtn).toBeInTheDocument();

    // Toggle dog (sampleYaml only has person and car)
    fireEvent.click(dogBtn);
    expect(screen.getByText(/- dog/i)).toBeInTheDocument();
  });

  it("switches to Birdseye tab and updates BirdEye channel mode", async () => {
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

    // Switch to Birdseye tab
    fireEvent.click(screen.getByRole("button", { name: /Birdseye/i }));

    expect(await screen.findByText(/Canal de Stream OpenVMS: BirdEye/i)).toBeInTheDocument();
    expect(screen.getByText(/Activo en Live Grid/i)).toBeInTheDocument();

    // Toggle BirdEye mode
    const select = screen.getByRole("combobox");
    fireEvent.change(select, { target: { value: "continuous" } });
    expect(screen.getByText(/mode: continuous/i)).toBeInTheDocument();
  });

  it("switches to Grabación tab, displays 15% safety buffer and active watchdog by default", async () => {
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

    // Switch to Grabación tab
    fireEvent.click(screen.getByRole("button", { name: /Grabación/i }));

    // Verify dynamic retention title and default active switch
    expect(
      await screen.findByText(/Optimización Dinámica y Protección de Almacenamiento/i)
    ).toBeInTheDocument();

    const toggle = screen.getByRole("switch", {
      name: /Activar Protección & Retención Dinámica/i,
    });
    expect(toggle).toBeChecked();

    // Verify 15% safety buffer and storage gauge
    expect(screen.getByText(/15% Reserva de Seguridad/i)).toBeInTheDocument();
    expect(screen.getAllByText(/850.00 GB Útiles/i)[0]).toBeInTheDocument();
    expect(screen.getByText(/150.00 GB Reservados/i)).toBeInTheDocument();

    // Verify dynamic attainable days calculation
    expect(screen.getByText(/Días Estimados de Retención/i)).toBeInTheDocument();
    expect(screen.getByText(/Base de Datos Protegida/i)).toBeInTheDocument();
  });

  it("allows toggling dynamic retention switch and applying calculated days to YAML", async () => {
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

    // Switch to Grabación tab
    fireEvent.click(screen.getByRole("button", { name: /Grabación/i }));

    const toggle = await screen.findByRole("switch", {
      name: /Activar Protección & Retención Dinámica/i,
    });
    expect(toggle).toBeChecked();

    // Click apply calculated retention to YAML
    const applyBtn = screen.getByRole("button", {
      name: /Aplicar Retención Calculada al YAML/i,
    });
    expect(applyBtn).toBeInTheDocument();
    fireEvent.click(applyBtn);

    // Check YAML preview has updated days (e.g. 13 days for 3 cameras with 850GB at 2Mbps)
    expect(screen.getByText(/days: 13/i)).toBeInTheDocument();

    // Toggle off
    fireEvent.click(toggle);
    expect(toggle).not.toBeChecked();
    expect(
      screen.getByText(/Cálculo dinámico y protección de almacenamiento desactivados/i)
    ).toBeInTheDocument();
  });

  describe("global configuration groups", () => {
    const catalog = {
      engine: "frigate",
      categories: [
        {
          id: "vehicle",
          label: "Vehículos",
          objects: [
            { id: "vehicle.car", label: "Auto", requires_model: false, supported: true },
            { id: "vehicle.pickup", label: "Camioneta", requires_model: true, supported: false },
            { id: "vehicle.bus", label: "Colectivo", requires_model: false, supported: null },
          ],
        },
      ],
    };
    const open = (canEdit = true) => {
      const yaml = `${sampleYaml}ffmpeg:\n  hwaccel_args: preset-vaapi\nmodel:\n  path: /models/custom.tflite\n  width: 320\ngo2rtc:\n  streams: {}\ncameras:\n  patio: {}\n`;
      const { calls } = setupConfigModal(async (req) => {
        const url = new URL(req.url);
        if (url.pathname.includes("/frigate-config/raw") && req.method === "GET") return json({ yaml, version: "0.14.1" });
        if (url.pathname.endsWith("/object-catalog")) return json(catalog);
        return undefined as unknown as Response;
      });
      renderPage(() => <ServerFrigateConfigModal server={mockServer} open={true} onClose={vi.fn()} canRestart={true} canEdit={canEdit} />);
      return calls;
    };

    it("shows the wide catalog with the model support flags", async () => {
      open();
      await screen.findByText(/Configuración de Servidor Frigate/i);
      fireEvent.click(screen.getByRole("button", { name: /Objetos disponibles/i }));
      const group = await screen.findByRole("group", { name: "Catálogo Vehículos" });
      expect(group).toHaveTextContent("Auto Soportado");
      expect(group).toHaveTextContent("Camioneta No soportado por el modelo");
      expect(group).toHaveTextContent("Colectivo Sin verificar");
      expect(screen.getByText(/cada cámara elige solo la selección simplificada/i)).toBeInTheDocument();
    });

    it("shows the model section and the label support counts", async () => {
      open();
      await screen.findByText(/Configuración de Servidor Frigate/i);
      fireEvent.click(screen.getByRole("button", { name: "Modelo" }));
      expect(await screen.findByText("/models/custom.tflite", { selector: "dd" })).toBeInTheDocument();
      expect(await screen.findByText(/1 soportadas · 1 no soportadas · 1 sin verificar/)).toBeInTheDocument();
    });

    it("keeps unclaimed sections reachable under Otros parámetros", async () => {
      open();
      await screen.findByText(/Configuración de Servidor Frigate/i);
      fireEvent.click(screen.getByRole("button", { name: /Otros parámetros/i }));
      expect(await screen.findByRole("region", { name: "go2rtc" })).toHaveTextContent("streams: {}");
      expect(screen.getByRole("region", { name: "cameras" })).toBeInTheDocument();
    });

    it("lists the inherited defaults and edits acceleration through the same save flow", async () => {
      const calls = open();
      await screen.findByText(/Configuración de Servidor Frigate/i);
      fireEvent.click(screen.getByRole("button", { name: /Defaults de cámaras/i }));
      expect(await screen.findByRole("region", { name: /Grabación \(record\)/ })).toHaveTextContent("days: 7");
      fireEvent.click(screen.getByRole("button", { name: "Aceleración" }));
      fireEvent.change(await screen.findByRole("combobox"), { target: { value: "preset-nvidia" } });
      fireEvent.click(screen.getByRole("button", { name: /Editor YAML/i }));
      expect(await screen.findByRole("region", { name: /Vista previa de cambios/i })).toHaveTextContent("+ hwaccel_args: preset-nvidia");
      fireEvent.click(screen.getByRole("button", { name: /Guardar cambios/i }));
      await waitFor(() => expect(calls.some((c) => c.method === "PUT" && new URL(c.url).pathname.endsWith("/frigate-config/raw"))).toBe(true));
    });

    it("is read-only without permission", async () => {
      open(false);
      await screen.findByText(/Configuración de Servidor Frigate/i);
      fireEvent.click(screen.getByRole("button", { name: "Aceleración" }));
      expect(await screen.findByRole("combobox")).toBeDisabled();
      expect(screen.getByRole("button", { name: /Guardar cambios/i })).toBeDisabled();
    });
  });
});
