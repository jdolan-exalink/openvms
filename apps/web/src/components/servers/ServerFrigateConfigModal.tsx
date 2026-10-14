import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  Bot,
  CheckCircle2,
  Code2,
  Cpu,
  Eye,
  HardDrive,
  History,
  Radio,
  RefreshCw,
  RotateCcw,
  Save,
  Sparkles,
  Zap,
} from "lucide-react";
import { api, type Schemas, unwrap } from "@/api/client";
import { serverAgentQuery, serverFrigateRawQuery, serverFrigateRevisionsQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Button, ErrorNote, Field, Select, Switch, TextInput } from "@/components/ui";

interface ServerFrigateConfigModalProps {
  server: Schemas["Server"];
  open: boolean;
  onClose: () => void;
  canRestart: boolean;
}

type ConfigTab = "mqtt" | "detectors" | "objects" | "record" | "birdseye" | "yaml" | "history";

// Helper regex-based parsers to inspect and patch YAML without requiring a heavyweight client-side YAML parser
function extractYamlSection(yaml: string, sectionKey: string): string | null {
  const lines = yaml.split("\n");
  let inSection = false;
  const sectionLines: string[] = [];

  for (const line of lines) {
    if (!inSection) {
      if (line.startsWith(`${sectionKey}:`)) {
        inSection = true;
        sectionLines.push(line);
      }
    } else {
      // If we encounter another top-level key (starts with non-space non-comment), we exited
      if (/^[a-zA-Z0-9_-]+:/.test(line)) {
        break;
      }
      sectionLines.push(line);
    }
  }

  return inSection ? sectionLines.join("\n") : null;
}

function extractYamlValue(yaml: string, path: string[]): string | undefined {
  const lines = yaml.split("\n");
  let currentDepth = 0;

  for (const line of lines) {
    const match = line.match(/^(\s*)([a-zA-Z0-9_-]+):\s*(.*)$/);
    if (match) {
      const indent = (match[1] ?? "").length;
      const key = match[2];
      const val = (match[3] ?? "").trim();

      const expectedDepth = currentDepth * 2;
      if (indent === expectedDepth && key === path[currentDepth]) {
        if (currentDepth === path.length - 1) {
          return val.replace(/^["']|["']$/g, "");
        }
        currentDepth++;
      }
    }
  }
  return undefined;
}

export function ServerFrigateConfigModal({
  server,
  open,
  onClose,
  canRestart,
}: ServerFrigateConfigModalProps) {
  const qc = useQueryClient();
  const [activeTab, setActiveTab] = useState<ConfigTab>("mqtt");
  const [restartAfterSave, setRestartAfterSave] = useState(true);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const [confirmRollback, setConfirmRollback] = useState<string | null>(null);

  // Queries
  const rawQuery = useQuery(serverFrigateRawQuery(server.id));
  const revisionsQuery = useQuery(serverFrigateRevisionsQuery(server.id));
  const agentQuery = useQuery(serverAgentQuery(server.id));

  const detectedHw = useMemo(() => {
    const data = agentQuery.data;
    if (!data || !data.installed) return null;
    return {
      nvidia: !!data.nvidia,
      coralPcie: !!data.coral_pcie,
      coralUsb: !!data.coral_usb,
      openvino: !!data.openvino,
      hailo: !!data.hailo,
      gpuName: data.gpu_name || (data.gpu_vendor ? `${data.gpu_vendor} GPU` : "NVIDIA GPU"),
    };
  }, [agentQuery.data]);

  // Local YAML state override
  const [customYaml, setCustomYaml] = useState<string | null>(null);
  const initialYaml = rawQuery.data?.yaml ?? "";
  const yamlText = customYaml ?? initialYaml;
  const isDirty = customYaml !== null && customYaml !== initialYaml;

  const setYamlText = (next: string | ((prev: string) => string)) => {
    setCustomYaml((prevCustom) => {
      const current = prevCustom ?? initialYaml;
      return typeof next === "function" ? next(current) : next;
    });
  };

  // Parsed quick values from YAML
  const mqttHost = useMemo(() => extractYamlValue(yamlText, ["mqtt", "host"]) ?? "", [yamlText]);
  const mqttPort = useMemo(() => extractYamlValue(yamlText, ["mqtt", "port"]) ?? "1883", [yamlText]);
  const mqttUser = useMemo(() => extractYamlValue(yamlText, ["mqtt", "user"]) ?? "", [yamlText]);
  const mqttTopic = useMemo(() => extractYamlValue(yamlText, ["mqtt", "topic_prefix"]) ?? "frigate", [yamlText]);

  const birdseyeEnabled = useMemo(() => extractYamlValue(yamlText, ["birdseye", "enabled"]) === "True" || extractYamlValue(yamlText, ["birdseye", "enabled"]) === "true", [yamlText]);
  const birdseyeMode = useMemo(() => extractYamlValue(yamlText, ["birdseye", "mode"]) ?? "motion", [yamlText]);

  const recordRetainDays = useMemo(() => extractYamlValue(yamlText, ["record", "retain", "days"]) ?? "7", [yamlText]);
  const recordRetainMode = useMemo(() => extractYamlValue(yamlText, ["record", "retain", "mode"]) ?? "motion", [yamlText]);

  const snapshotsRetainDays = useMemo(() => extractYamlValue(yamlText, ["snapshots", "retain", "default"]) ?? "14", [yamlText]);

  // Patch a top-level or nested block in YAML text
  const patchYamlBlock = (sectionKey: string, newBlockYaml: string) => {
    const existingSection = extractYamlSection(yamlText, sectionKey);
    if (existingSection) {
      setYamlText((prev) => prev.replace(existingSection, newBlockYaml));
    } else {
      // Append section
      setYamlText((prev) => `${prev.trimEnd()}\n\n${newBlockYaml}\n`);
    }
  };

  // Save mutation
  const saveMutation = useMutation({
    mutationFn: async () => {
      setSaveSuccess(false);
      return unwrap(
        await api.PUT("/api/v1/servers/{serverId}/frigate-config/raw", {
          params: {
            path: { serverId: server.id },
            query: { restart: canRestart ? restartAfterSave : false },
          },
          body: { yaml: yamlText },
        })
      );
    },
    onSuccess: async () => {
      setSaveSuccess(true);
      setCustomYaml(null);
      await qc.invalidateQueries({ queryKey: ["servers", server.id] });
      await qc.invalidateQueries({ queryKey: ["servers", server.id, "frigate-raw"] });
      await qc.invalidateQueries({ queryKey: ["servers", server.id, "frigate-revisions"] });
      setTimeout(() => setSaveSuccess(false), 4000);
    },
  });

  // Rollback mutation
  const rollbackMutation = useMutation({
    mutationFn: async (revisionId: string) => {
      return unwrap(
        await api.POST("/api/v1/servers/{serverId}/frigate-config/revisions/{revisionId}/rollback", {
          params: { path: { serverId: server.id, revisionId } },
        })
      );
    },
    onSuccess: async () => {
      setConfirmRollback(null);
      setCustomYaml(null);
      await qc.invalidateQueries({ queryKey: ["servers", server.id] });
      await qc.invalidateQueries({ queryKey: ["servers", server.id, "frigate-raw"] });
      await qc.invalidateQueries({ queryKey: ["servers", server.id, "frigate-revisions"] });
      await rawQuery.refetch();
    },
  });

  if (!open) return null;

  return (
    <Modal
      title={`Configuración de Servidor Frigate · ${server.name}`}
      onClose={() => {
        if (!saveMutation.isPending) onClose();
      }}
    >
      <div className="flex flex-col gap-5 max-w-4xl min-h-[520px]">
        {/* Navigation Tabs */}
        <div className="flex flex-wrap items-center gap-1.5 border-b border-outline-variant/60 pb-3">
          <Button
            size="sm"
            variant={activeTab === "mqtt" ? "primary" : "text"}
            onClick={() => setActiveTab("mqtt")}
          >
            <Icon icon={Radio} size="xs" />
            MQTT & OpenVMS
          </Button>

          <Button
            size="sm"
            variant={activeTab === "detectors" ? "primary" : "text"}
            onClick={() => setActiveTab("detectors")}
          >
            <Icon icon={Cpu} size="xs" />
            Detectores IA
          </Button>

          <Button
            size="sm"
            variant={activeTab === "objects" ? "primary" : "text"}
            onClick={() => setActiveTab("objects")}
          >
            <Icon icon={Sparkles} size="xs" />
            Objetos Globales
          </Button>

          <Button
            size="sm"
            variant={activeTab === "record" ? "primary" : "text"}
            onClick={() => setActiveTab("record")}
          >
            <Icon icon={HardDrive} size="xs" />
            Grabación & Retención
          </Button>

          <Button
            size="sm"
            variant={activeTab === "birdseye" ? "primary" : "text"}
            onClick={() => setActiveTab("birdseye")}
          >
            <Icon icon={Eye} size="xs" />
            Birdseye
          </Button>

          <Button
            size="sm"
            variant={activeTab === "yaml" ? "primary" : "text"}
            onClick={() => setActiveTab("yaml")}
          >
            <Icon icon={Code2} size="xs" />
            Editor YAML
          </Button>

          <Button
            size="sm"
            variant={activeTab === "history" ? "primary" : "text"}
            onClick={() => setActiveTab("history")}
          >
            <Icon icon={History} size="xs" />
            Historial ({revisionsQuery.data?.length ?? 0})
          </Button>
        </div>

        {rawQuery.isLoading && (
          <div className="flex items-center gap-2 py-12 justify-center text-sm text-muted">
            <Icon icon={RefreshCw} size="sm" className="animate-spin" />
            Cargando configuración global de Frigate…
          </div>
        )}

        {rawQuery.error && (
          <ErrorNote
            error={
              rawQuery.error instanceof Error
                ? rawQuery.error
                : new Error("No se pudo obtener la configuración de Frigate. Verificá que el servidor esté online.")
            }
          />
        )}

        {!rawQuery.isLoading && !rawQuery.error && (
          <div className="flex-1 flex flex-col gap-4">
            {/* 1. Tab MQTT & OpenVMS Integration */}
            {activeTab === "mqtt" && (
              <div className="flex flex-col gap-4">
                <div className="rounded-m3-xl border border-primary/20 bg-primary/5 p-4 flex flex-col gap-1.5">
                  <div className="flex items-center gap-2 font-bold text-sm text-on-surface">
                    <Icon icon={Radio} size="xs" className="text-primary" />
                    Integración de Eventos MQTT con OpenVMS
                  </div>
                  <p className="text-xs text-muted">
                    OpenVMS utiliza la conexión MQTT de Frigate para recibir detecciones en tiempo real, alertas de patentes LPR y telemetría de eventos.
                  </p>
                </div>

                <div className="grid gap-4 sm:grid-cols-2">
                  <Field label="Host / Broker MQTT" hint="IP o hostname del broker MQTT">
                    <TextInput
                      value={mqttHost}
                      placeholder="10.20.0.1 o mosquitto"
                      onChange={(e) => {
                        const newHost = e.target.value;
                        const block = `mqtt:\n  host: ${newHost}\n  port: ${mqttPort || "1883"}\n  user: ${mqttUser || "frigate"}\n  topic_prefix: ${mqttTopic || "frigate"}`;
                        patchYamlBlock("mqtt", block);
                      }}
                    />
                  </Field>

                  <Field label="Puerto MQTT" hint="Puerto TCP estándar (por defecto 1883)">
                    <TextInput
                      type="number"
                      value={mqttPort}
                      placeholder="1883"
                      onChange={(e) => {
                        const newPort = e.target.value;
                        const block = `mqtt:\n  host: ${mqttHost || "10.20.0.1"}\n  port: ${newPort}\n  user: ${mqttUser || "frigate"}\n  topic_prefix: ${mqttTopic || "frigate"}`;
                        patchYamlBlock("mqtt", block);
                      }}
                    />
                  </Field>

                  <Field label="Usuario MQTT" hint="Credenciales configuradas en el broker">
                    <TextInput
                      value={mqttUser}
                      placeholder="frigate"
                      onChange={(e) => {
                        const newUser = e.target.value;
                        const block = `mqtt:\n  host: ${mqttHost || "10.20.0.1"}\n  port: ${mqttPort || "1883"}\n  user: ${newUser}\n  topic_prefix: ${mqttTopic || "frigate"}`;
                        patchYamlBlock("mqtt", block);
                      }}
                    />
                  </Field>

                  <Field label="Topic Prefix" hint="Prefijo de los mensajes publicados (por defecto: frigate)">
                    <TextInput
                      value={mqttTopic}
                      placeholder="frigate"
                      onChange={(e) => {
                        const newTopic = e.target.value;
                        const block = `mqtt:\n  host: ${mqttHost || "10.20.0.1"}\n  port: ${mqttPort || "1883"}\n  user: ${mqttUser || "frigate"}\n  topic_prefix: ${newTopic}`;
                        patchYamlBlock("mqtt", block);
                      }}
                    />
                  </Field>
                </div>

                <div className="flex items-center gap-2 text-xs text-muted mt-2">
                  <Icon icon={CheckCircle2} size="xs" className="text-ok" />
                  <span>Los cambios se guardan directamente en el archivo <code>config.yml</code> de Frigate.</span>
                </div>
              </div>
            )}

            {/* 2. Tab Detectores IA */}
            {activeTab === "detectors" && (
              <div className="flex flex-col gap-4">
                {/* Hardware Probe Banner */}
                <div className="rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4 flex flex-col gap-3">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2 font-bold text-sm text-on-surface">
                      <Icon icon={Cpu} size="xs" className="text-primary" />
                      Capacidades de Hardware Detectadas (Edge Agent)
                    </div>
                    {agentQuery.isLoading && <span className="text-xs text-muted">Consultando hardware...</span>}
                  </div>

                  {detectedHw ? (
                    <div className="flex flex-wrap items-center gap-2">
                      {detectedHw.nvidia && (
                        <span className="inline-flex items-center gap-1.5 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/30 px-3 py-1 text-xs font-semibold">
                          <Icon icon={Activity} size="xs" />
                          {detectedHw.gpuName}
                        </span>
                      )}
                      {detectedHw.coralPcie && (
                        <span className="inline-flex items-center gap-1.5 rounded-full bg-blue-500/10 text-blue-400 border border-blue-500/30 px-3 py-1 text-xs font-semibold">
                          <Icon icon={Bot} size="xs" />
                          Google Coral (PCIe)
                        </span>
                      )}
                      {detectedHw.coralUsb && (
                        <span className="inline-flex items-center gap-1.5 rounded-full bg-blue-500/10 text-blue-400 border border-blue-500/30 px-3 py-1 text-xs font-semibold">
                          <Icon icon={Bot} size="xs" />
                          Google Coral (USB)
                        </span>
                      )}
                      {detectedHw.openvino && (
                        <span className="inline-flex items-center gap-1.5 rounded-full bg-cyan-500/10 text-cyan-400 border border-cyan-500/30 px-3 py-1 text-xs font-semibold">
                          <Icon icon={Sparkles} size="xs" />
                          Intel OpenVINO
                        </span>
                      )}
                      {detectedHw.hailo && (
                        <span className="inline-flex items-center gap-1.5 rounded-full bg-amber-500/10 text-amber-400 border border-amber-500/30 px-3 py-1 text-xs font-semibold">
                          <Icon icon={Zap} size="xs" />
                          Hailo-8 AI
                        </span>
                      )}
                      {!detectedHw.nvidia && !detectedHw.coralPcie && !detectedHw.coralUsb && !detectedHw.openvino && !detectedHw.hailo && (
                        <span className="text-xs text-muted">
                          No se detectaron aceleradores dedicados. Recomendado usar CPU Multithread.
                        </span>
                      )}
                    </div>
                  ) : (
                    <p className="text-xs text-muted">
                      {agentQuery.isLoading
                        ? "Consultando telemetría del edge-agent..."
                        : "El agente no está conectado en este servidor. Se requiere OpenVMS Agent para probe automático de aceleradores."}
                    </p>
                  )}
                </div>

                <div className="rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4 flex flex-col gap-2">
                  <div className="flex items-center gap-2 font-bold text-sm text-on-surface">
                    <Icon icon={Cpu} size="xs" className="text-primary" />
                    Presets de Detectores Acelerados
                  </div>
                  <p className="text-xs text-muted">
                    Selecciona un preset para configurar el bloque <code>detectors</code> en el YAML de Frigate.
                  </p>

                  <div className="grid gap-3 sm:grid-cols-2 mt-2">
                    <Button
                      variant="outlined"
                      className="justify-start text-left h-auto py-3 px-3.5 flex flex-col items-start gap-1"
                      onClick={() => {
                        const block = `detectors:\n  cpu1:\n    type: cpu\n    num_threads: 3`;
                        patchYamlBlock("detectors", block);
                      }}
                    >
                      <div className="flex items-center justify-between w-full">
                        <span className="font-bold text-xs flex items-center gap-1.5">
                          <Icon icon={Cpu} size="xs" /> CPU Standard (Multithread)
                        </span>
                        {(!detectedHw || (!detectedHw.nvidia && !detectedHw.coralPcie && !detectedHw.coralUsb && !detectedHw.openvino && !detectedHw.hailo)) && (
                          <span className="text-[10px] font-bold uppercase tracking-wider text-primary bg-primary/10 px-1.5 py-0.5 rounded">
                            Recomendado
                          </span>
                        )}
                      </div>
                      <span className="text-[11px] text-muted">Inferencia por software para servidores sin TPU/GPU</span>
                    </Button>

                    <Button
                      variant="outlined"
                      className="justify-start text-left h-auto py-3 px-3.5 flex flex-col items-start gap-1"
                      onClick={() => {
                        const block = `detectors:\n  coral:\n    type: edgetpu\n    device: usb`;
                        patchYamlBlock("detectors", block);
                      }}
                    >
                      <div className="flex items-center justify-between w-full">
                        <span className="font-bold text-xs flex items-center gap-1.5">
                          <Icon icon={Bot} size="xs" className="text-primary" /> Google Coral EdgeTPU (USB)
                        </span>
                        {detectedHw?.coralUsb && (
                          <span className="text-[10px] font-bold uppercase tracking-wider text-primary bg-primary/10 px-1.5 py-0.5 rounded">
                            Recomendado
                          </span>
                        )}
                      </div>
                      <span className="text-[11px] text-muted">Acelerador USB Coral (latencia &lt; 10ms por frame)</span>
                    </Button>

                    <Button
                      variant="outlined"
                      className="justify-start text-left h-auto py-3 px-3.5 flex flex-col items-start gap-1"
                      onClick={() => {
                        const block = `detectors:\n  coral:\n    type: edgetpu\n    device: pci:0`;
                        patchYamlBlock("detectors", block);
                      }}
                    >
                      <div className="flex items-center justify-between w-full">
                        <span className="font-bold text-xs flex items-center gap-1.5">
                          <Icon icon={Bot} size="xs" className="text-blue-400" /> Google Coral EdgeTPU (PCIe)
                        </span>
                        {detectedHw?.coralPcie && (
                          <span className="text-[10px] font-bold uppercase tracking-wider text-primary bg-primary/10 px-1.5 py-0.5 rounded">
                            Recomendado
                          </span>
                        )}
                      </div>
                      <span className="text-[11px] text-muted">Módulo PCIe / M.2 Coral (máxima velocidad de bus)</span>
                    </Button>

                    <Button
                      variant="outlined"
                      className="justify-start text-left h-auto py-3 px-3.5 flex flex-col items-start gap-1"
                      onClick={() => {
                        const block = `detectors:\n  hailo:\n    type: hailo8`;
                        patchYamlBlock("detectors", block);
                      }}
                    >
                      <div className="flex items-center justify-between w-full">
                        <span className="font-bold text-xs flex items-center gap-1.5">
                          <Icon icon={Zap} size="xs" className="text-amber-400" /> Hailo-8 AI NPU
                        </span>
                        {detectedHw?.hailo && (
                          <span className="text-[10px] font-bold uppercase tracking-wider text-primary bg-primary/10 px-1.5 py-0.5 rounded">
                            Recomendado
                          </span>
                        )}
                      </div>
                      <span className="text-[11px] text-muted">Inferencia NPU de 26 TOPS para cámaras simultáneas</span>
                    </Button>

                    <Button
                      variant="outlined"
                      className="justify-start text-left h-auto py-3 px-3.5 flex flex-col items-start gap-1"
                      onClick={() => {
                        const block = `detectors:\n  ov:\n    type: openvino\n    device: GPU`;
                        patchYamlBlock("detectors", block);
                      }}
                    >
                      <div className="flex items-center justify-between w-full">
                        <span className="font-bold text-xs flex items-center gap-1.5">
                          <Icon icon={Sparkles} size="xs" className="text-ok" /> Intel OpenVINO (iGPU / Arc)
                        </span>
                        {detectedHw?.openvino && (
                          <span className="text-[10px] font-bold uppercase tracking-wider text-primary bg-primary/10 px-1.5 py-0.5 rounded">
                            Recomendado
                          </span>
                        )}
                      </div>
                      <span className="text-[11px] text-muted">Inferencia rápida en procesadores Intel con gráficos integrados</span>
                    </Button>

                    <Button
                      variant="outlined"
                      className="justify-start text-left h-auto py-3 px-3.5 flex flex-col items-start gap-1"
                      onClick={() => {
                        const block = `detectors:\n  tensorrt:\n    type: tensorrt\n    device: 0`;
                        patchYamlBlock("detectors", block);
                      }}
                    >
                      <div className="flex items-center justify-between w-full">
                        <span className="font-bold text-xs flex items-center gap-1.5">
                          <Icon icon={Activity} size="xs" className="text-warn" /> NVIDIA TensorRT (CUDA)
                        </span>
                        {detectedHw?.nvidia && (
                          <span className="text-[10px] font-bold uppercase tracking-wider text-primary bg-primary/10 px-1.5 py-0.5 rounded">
                            Recomendado
                          </span>
                        )}
                      </div>
                      <span className="text-[11px] text-muted">Aceleración masiva para GPUs NVIDIA GeForce / Quadro</span>
                    </Button>
                  </div>
                </div>

                <div className="flex flex-col gap-2">
                  <span className="text-xs font-bold text-muted uppercase tracking-wider">
                    Definición actual de detectores en YAML:
                  </span>
                  <pre className="font-mono text-xs rounded-m3-lg bg-surface-3 p-3 text-ink overflow-x-auto">
                    {extractYamlSection(yamlText, "detectors") || "# No hay bloque detectors definido (usa CPU por defecto)"}
                  </pre>
                </div>
              </div>
            )}

            {/* 3. Tab Objetos Globales */}
            {activeTab === "objects" && (
              <div className="flex flex-col gap-4">
                <div className="flex flex-col gap-1">
                  <h3 className="text-sm font-bold text-on-surface">Objetos a Rastrear Globalmente</h3>
                  <p className="text-xs text-muted">
                    Clases de IA que Frigate busca y clasifica por defecto en todas las cámaras del servidor.
                  </p>
                </div>

                <div className="flex flex-wrap gap-2">
                  {[
                    { label: "Persona", tag: "person" },
                    { label: "Auto / Vehículo", tag: "car" },
                    { label: "Moto", tag: "motorcycle" },
                    { label: "Bicicleta", tag: "bicycle" },
                    { label: "Colectivo / Bus", tag: "bus" },
                    { label: "Camión", tag: "truck" },
                    { label: "Perro", tag: "dog" },
                    { label: "Gato", tag: "cat" },
                  ].map((item) => {
                    const isTracked = yamlText.includes(`- ${item.tag}`) || yamlText.includes(`- "${item.tag}"`);
                    return (
                      <Button
                        key={item.tag}
                        size="sm"
                        variant={isTracked ? "primary" : "outlined"}
                        onClick={() => {
                          if (isTracked) {
                            // Remove from track
                            setYamlText((prev) =>
                              prev
                                .replace(new RegExp(`\\s*-\\s*["']?${item.tag}["']?`, "g"), "")
                            );
                          } else {
                            // Add to track block
                            const existingTrack = extractYamlSection(yamlText, "objects");
                            if (existingTrack) {
                              const updated = existingTrack.replace(
                                /track:\s*\n/,
                                `track:\n    - ${item.tag}\n`
                              );
                              setYamlText((prev) => prev.replace(existingTrack, updated));
                            } else {
                              const block = `objects:\n  track:\n    - ${item.tag}`;
                              patchYamlBlock("objects", block);
                            }
                          }
                        }}
                      >
                        {item.label}
                      </Button>
                    );
                  })}
                </div>

                <div className="flex flex-col gap-2 mt-3">
                  <span className="text-xs font-bold text-muted uppercase tracking-wider">
                    Definición de objetos en YAML:
                  </span>
                  <pre className="font-mono text-xs rounded-m3-lg bg-surface-3 p-3 text-ink overflow-x-auto">
                    {extractYamlSection(yamlText, "objects") || "# Sin sección objects global"}
                  </pre>
                </div>
              </div>
            )}

            {/* 4. Tab Grabación & Retención */}
            {activeTab === "record" && (
              <div className="flex flex-col gap-5">
                <div className="grid gap-4 sm:grid-cols-2">
                  <div className="flex flex-col gap-3 rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4">
                    <span className="text-xs font-bold text-muted uppercase tracking-wider">
                      Grabación Global de Video
                    </span>
                    <Field label="Días de retención general" hint="Días que se conservan las grabaciones continuas">
                      <TextInput
                        type="number"
                        min={1}
                        max={365}
                        value={recordRetainDays}
                        onChange={(e) => {
                          const val = e.target.value;
                          const block = `record:\n  enabled: true\n  retain:\n    days: ${val}\n    mode: ${recordRetainMode}`;
                          patchYamlBlock("record", block);
                        }}
                      />
                    </Field>
                    <Field label="Modo de retención">
                      <Select
                        value={recordRetainMode}
                        onChange={(e) => {
                          const val = e.target.value;
                          const block = `record:\n  enabled: true\n  retain:\n    days: ${recordRetainDays}\n    mode: ${val}`;
                          patchYamlBlock("record", block);
                        }}
                      >
                        <option value="all">Todo el video continuo</option>
                        <option value="motion">Solo segmentos con movimiento</option>
                        <option value="active_objects">Solo con objetos activos detectados</option>
                      </Select>
                    </Field>
                  </div>

                  <div className="flex flex-col gap-3 rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4">
                    <span className="text-xs font-bold text-muted uppercase tracking-wider">
                      Capturas Instantáneas (Snapshots)
                    </span>
                    <Field label="Días de retención de capturas" hint="Tiempo de almacenamiento de las fotos de eventos">
                      <TextInput
                        type="number"
                        min={1}
                        max={365}
                        value={snapshotsRetainDays}
                        onChange={(e) => {
                          const val = e.target.value;
                          const block = `snapshots:\n  enabled: true\n  retain:\n    default: ${val}`;
                          patchYamlBlock("snapshots", block);
                        }}
                      />
                    </Field>
                  </div>
                </div>

                <div className="flex flex-col gap-2">
                  <span className="text-xs font-bold text-muted uppercase tracking-wider">
                    Bloque de grabación y capturas en YAML:
                  </span>
                  <pre className="font-mono text-xs rounded-m3-lg bg-surface-3 p-3 text-ink overflow-x-auto">
                    {[extractYamlSection(yamlText, "record"), extractYamlSection(yamlText, "snapshots")].filter(Boolean).join("\n\n") || "# Usando valores por defecto de Frigate"}
                  </pre>
                </div>
              </div>
            )}

            {/* 5. Tab Birdseye (Mosaico) */}
            {activeTab === "birdseye" && (
              <div className="flex flex-col gap-4">
                <div className="rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4 flex flex-col gap-3">
                  <div className="flex items-center justify-between">
                    <div>
                      <h4 className="text-sm font-bold">Mosaico Global Birdseye</h4>
                      <p className="text-xs text-muted">Genera un stream unificado que muestra en vivo las cámaras con actividad relevante.</p>
                    </div>
                    <Switch
                      label="Habilitar Birdseye"
                      checked={birdseyeEnabled}
                      onChange={(on) => {
                        const block = `birdseye:\n  enabled: ${on ? "True" : "False"}\n  mode: ${birdseyeMode}`;
                        patchYamlBlock("birdseye", block);
                      }}
                    />
                  </div>

                  <Field label="Modo de activación en pantalla">
                    <Select
                      value={birdseyeMode}
                      onChange={(e) => {
                        const val = e.target.value;
                        const block = `birdseye:\n  enabled: ${birdseyeEnabled ? "True" : "False"}\n  mode: ${val}`;
                        patchYamlBlock("birdseye", block);
                      }}
                    >
                      <option value="objects">Mostrar solo cuando hay objetos detectados</option>
                      <option value="motion">Mostrar cuando hay movimiento o detección</option>
                      <option value="continuous">Mostrar todas las cámaras continuamente</option>
                    </Select>
                  </Field>
                </div>

                <pre className="font-mono text-xs rounded-m3-lg bg-surface-3 p-3 text-ink overflow-x-auto">
                  {extractYamlSection(yamlText, "birdseye") || "# Sin sección birdseye"}
                </pre>
              </div>
            )}

            {/* 6. Tab Editor YAML Completo */}
            {activeTab === "yaml" && (
              <div className="flex flex-col gap-2 flex-1">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-bold text-muted uppercase tracking-wider">
                    Archivo config.yml completo (incluye credenciales y cámaras)
                  </span>
                  <Button
                    size="sm"
                    variant="text"
                    disabled={!isDirty}
                    onClick={() => setCustomYaml(null)}
                  >
                    <Icon icon={RotateCcw} size="xs" />
                    Descartar cambios
                  </Button>
                </div>

                <textarea
                  className="font-mono text-xs w-full h-[360px] rounded-m3-lg border border-outline-variant bg-surface-2 p-3 text-on-surface focus:outline-primary leading-relaxed"
                  value={yamlText}
                  onChange={(e) => setYamlText(e.target.value)}
                  spellCheck={false}
                />
              </div>
            )}

            {/* 7. Tab Historial de Revisiones */}
            {activeTab === "history" && (
              <div className="flex flex-col gap-3">
                <div className="flex items-center justify-between">
                  <h4 className="text-sm font-bold">Revisiones Anteriores del Servidor</h4>
                  <span className="text-xs text-muted">
                    {revisionsQuery.data?.length ?? 0} revisiones registradas
                  </span>
                </div>

                <div className="flex flex-col gap-2 max-h-[360px] overflow-y-auto">
                  {(revisionsQuery.data ?? []).map((rev) => (
                    <div
                      key={rev.id}
                      className="flex items-center justify-between gap-3 rounded-m3-lg border border-outline-variant/60 bg-surface-2 p-3 text-xs"
                    >
                      <div className="flex flex-col gap-0.5">
                        <span className="font-semibold text-on-surface">
                          {rev.actor_name ? `Modificado por ${rev.actor_name}` : "Cambio automático / sistema"}
                        </span>
                        <span className="text-[11px] text-muted">
                          {new Date(rev.created_at).toLocaleString()}{rev.sections && rev.sections.length > 0 ? ` · Secciones: ${rev.sections.join(", ")}` : ""}
                        </span>
                      </div>

                      <Button
                        size="sm"
                        variant="outlined"
                        disabled={rollbackMutation.isPending}
                        onClick={() => setConfirmRollback(rev.id)}
                      >
                        <Icon icon={RotateCcw} size="xs" />
                        Restaurar
                      </Button>
                    </div>
                  ))}
                  {(!revisionsQuery.data || revisionsQuery.data.length === 0) && (
                    <p className="text-xs text-muted py-6 text-center">No hay revisiones previas archivadas para este servidor.</p>
                  )}
                </div>
              </div>
            )}
          </div>
        )}

        {/* Footer Actions */}
        <div className="border-t border-outline-variant/60 pt-4 flex flex-col sm:flex-row items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            {canRestart && (
              <label className="flex items-center gap-2 text-xs cursor-pointer select-none">
                <input
                  type="checkbox"
                  className="size-4 rounded accent-primary cursor-pointer"
                  checked={restartAfterSave}
                  onChange={(e) => setRestartAfterSave(e.target.checked)}
                />
                <span>Reiniciar Frigate después de guardar para aplicar cambios</span>
              </label>
            )}
          </div>

          <div className="flex items-center gap-2 self-end">
            {saveSuccess && (
              <span className="inline-flex items-center gap-1.5 text-xs text-ok font-semibold animate-pulse mr-2">
                <Icon icon={CheckCircle2} size="xs" />
                Configuración guardada exitosamente
              </span>
            )}
            <Button
              variant="text"
              onClick={onClose}
              disabled={saveMutation.isPending}
            >
              Cerrar
            </Button>
            <Button
              variant="primary"
              disabled={saveMutation.isPending || !isDirty}
              onClick={() => saveMutation.mutate()}
            >
              <Icon icon={Save} size="xs" className={saveMutation.isPending ? "animate-spin" : ""} />
              {saveMutation.isPending ? "Guardando…" : "Guardar cambios"}
            </Button>
          </div>
        </div>

        {saveMutation.error && (
          <ErrorNote
            error={
              saveMutation.error instanceof Error
                ? saveMutation.error
                : new Error(String(saveMutation.error))
            }
          />
        )}
      </div>

      {confirmRollback && (
        <ConfirmDialog
          title="Restaurar configuración previa"
          message="¿Confirmas que deseas restaurar esta versión anterior de config.yml? La configuración actual se reemplazará y se generará una nueva revisión."
          confirmLabel="Restaurar ahora"
          onConfirm={() => rollbackMutation.mutate(confirmRollback)}
          onCancel={() => setConfirmRollback(null)}
        />
      )}
    </Modal>
  );
}
