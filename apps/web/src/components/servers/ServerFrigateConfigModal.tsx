import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  AlertTriangle,
  Bot,
  CheckCircle2,
  Code2,
  Cpu,
  Database,
  Eye,
  HardDrive,
  History,
  Radio,
  RefreshCw,
  RotateCcw,
  Save,
  ShieldAlert,
  ShieldCheck,
  Sliders,
  Sparkles,
  Zap,
} from "lucide-react";
import { api, type Schemas, unwrap } from "@/api/client";
import { objectCatalogQuery, serverAgentQuery, serverFrigateRawQuery, serverFrigateRevisionsQuery } from "@/api/queries";
import { setSectionScalar } from "@/lib/frigateGroups";
import { AccelerationPanel, DefaultsPanel, ModelPanel, ObjectCatalogView, OtherPanel } from "./FrigateGlobalPanels";
import {
  FRIGATE_OBJECT_CATALOG,
  FRIGATE_OBJECT_CATEGORIES,
  isFrigateSupportedObject,
  parseTrackedObjects,
  toggleTrackedObject,
} from "./frigateObjects";
import {
  calculateAttainableDays,
  calculatePerCameraDays,
  calculateUsableStorage,
  DEFAULT_BITRATE_MBPS,
  DEFAULT_SAFETY_BUFFER_PERCENT,
  extractCamerasFromYaml,
  formatBytes,
  patchRetentionInYaml,
  type CameraPriority,
} from "./storageRetention";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { YamlDiff } from "@/components/frigate/YamlDiff";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Button, ErrorNote, Field, Select, Switch, TextInput } from "@/components/ui";

interface ServerFrigateConfigModalProps {
  server: Schemas["Server"];
  open: boolean;
  onClose: () => void;
  canRestart: boolean;
  /** False renders the screen read-only (no saving, no acceleration change). Defaults to true. */
  canEdit?: boolean;
}

type ConfigTab = "mqtt" | "detectors" | "model" | "objects" | "record" | "accel" | "defaults" | "birdseye" | "other" | "yaml" | "history";

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
  canEdit = true,
}: ServerFrigateConfigModalProps) {
  const qc = useQueryClient();
  const [activeTab, setActiveTab] = useState<ConfigTab>("mqtt");
  const [restartAfterSave, setRestartAfterSave] = useState(true);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const [confirmRollback, setConfirmRollback] = useState<string | null>(null);
  const [dynamicRetentionEnabled, setDynamicRetentionEnabled] = useState(true);
  const [selectedBitrate, setSelectedBitrate] = useState<number>(DEFAULT_BITRATE_MBPS);
  const [cameraPriorities, setCameraPriorities] = useState<Record<string, CameraPriority>>({});

  // Queries
  const rawQuery = useQuery(serverFrigateRawQuery(server.id));
  const revisionsQuery = useQuery(serverFrigateRevisionsQuery(server.id));
  const agentQuery = useQuery(serverAgentQuery(server.id));
  const catalogQuery = useQuery(objectCatalogQuery("frigate", server.id));

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

  const yamlCameras = useMemo(() => extractCamerasFromYaml(yamlText), [yamlText]);
  const cameraList = useMemo(() => {
    if (yamlCameras.length > 0) {
      return yamlCameras.map((id) => ({
        id,
        name: id.replace(/_/g, " "),
        priority: cameraPriorities[id] ?? ("normal" as CameraPriority),
      }));
    }
    const count = Math.max(1, server.camera_count || 1);
    return Array.from({ length: count }, (_, i) => {
      const id = `camera_${i + 1}`;
      return {
        id,
        name: `Cámara ${i + 1}`,
        priority: cameraPriorities[id] ?? ("normal" as CameraPriority),
      };
    });
  }, [yamlCameras, server.camera_count, cameraPriorities]);

  const storageStats = useMemo(() => {
    const agent = agentQuery.data;
    const cctvTotal = agent?.cctv_total_bytes ?? 1_000_000_000_000;
    const cctvFree = agent?.cctv_free_bytes ?? 500_000_000_000;
    const dbTotal = agent?.database_total_bytes ?? 100_000_000_000;
    const dbFree = agent?.database_free_bytes ?? 50_000_000_000;
    const bufferPercent = agent?.storage_buffer_percent ?? DEFAULT_SAFETY_BUFFER_PERCENT;
    const watchdogEnabled = agent?.storage_watchdog ?? true;
    const storageOver85 = agent?.storage_over_85_percent ?? false;
    const dbCritical = agent?.database_critical ?? false;
    const prunedCount = agent?.pruned_segments_count ?? 0;

    const { usableBytes, reservedBytes } = calculateUsableStorage(cctvTotal, bufferPercent);
    const attainableDays = calculateAttainableDays({
      usableBytes,
      cameraCount: cameraList.length,
      bitrateMbps: selectedBitrate,
    });

    const perCameraDays = calculatePerCameraDays(cameraList, attainableDays);

    const cctvUsed = Math.max(0, cctvTotal - cctvFree);
    const cctvUsedPercent = cctvTotal > 0 ? (cctvUsed / cctvTotal) * 100 : 0;
    const usablePercent = 100 - bufferPercent;

    return {
      cctvTotal,
      cctvFree,
      cctvUsed,
      cctvUsedPercent,
      dbTotal,
      dbFree,
      bufferPercent,
      watchdogEnabled,
      storageOver85,
      dbCritical,
      prunedCount,
      usableBytes,
      reservedBytes,
      attainableDays,
      perCameraDays,
      usablePercent,
    };
  }, [agentQuery.data, cameraList, selectedBitrate]);

  // Parsed quick values from YAML
  const mqttHost = useMemo(() => extractYamlValue(yamlText, ["mqtt", "host"]) ?? "", [yamlText]);
  const mqttPort = useMemo(() => extractYamlValue(yamlText, ["mqtt", "port"]) ?? "1883", [yamlText]);
  const mqttUser = useMemo(() => extractYamlValue(yamlText, ["mqtt", "user"]) ?? "", [yamlText]);
  const mqttTopic = useMemo(() => extractYamlValue(yamlText, ["mqtt", "topic_prefix"]) ?? "frigate", [yamlText]);

  const birdseyeEnabled = useMemo(() => extractYamlValue(yamlText, ["birdseye", "enabled"]) === "True" || extractYamlValue(yamlText, ["birdseye", "enabled"]) === "true", [yamlText]);
  const birdseyeMode = useMemo(() => extractYamlValue(yamlText, ["birdseye", "mode"]) ?? "motion", [yamlText]);
  const trackedObjects = useMemo(() => parseTrackedObjects(yamlText), [yamlText]);

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
            Objetos disponibles
          </Button>

          <Button
            size="sm"
            variant={activeTab === "model" ? "primary" : "text"}
            onClick={() => setActiveTab("model")}
          >
            <Icon icon={Bot} size="xs" />
            Modelo
          </Button>

          <Button
            size="sm"
            variant={activeTab === "record" ? "primary" : "text"}
            onClick={() => setActiveTab("record")}
          >
            <Icon icon={HardDrive} size="xs" />
            Grabación & Almacenamiento
          </Button>

          <Button
            size="sm"
            variant={activeTab === "accel" ? "primary" : "text"}
            onClick={() => setActiveTab("accel")}
          >
            <Icon icon={Zap} size="xs" />
            Aceleración
          </Button>

          <Button
            size="sm"
            variant={activeTab === "defaults" ? "primary" : "text"}
            onClick={() => setActiveTab("defaults")}
          >
            <Icon icon={Sliders} size="xs" />
            Defaults de cámaras
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
            variant={activeTab === "other" ? "primary" : "text"}
            onClick={() => setActiveTab("other")}
          >
            <Icon icon={Database} size="xs" />
            Otros parámetros
          </Button>

          <Button
            size="sm"
            variant={activeTab === "yaml" ? "primary" : "text"}
            onClick={() => setActiveTab("yaml")}
          >
            <Icon icon={Code2} size="xs" />
            Editor YAML (avanzado)
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
            {activeTab === "model" && <ModelPanel yaml={yamlText} catalog={catalogQuery.data} />}
            {activeTab === "accel" && (
              <AccelerationPanel
                yaml={yamlText}
                canEdit={canEdit}
                onChange={(preset) => setYamlText((prev) => setSectionScalar(prev, "ffmpeg", "hwaccel_args", preset))}
              />
            )}
            {activeTab === "defaults" && <DefaultsPanel yaml={yamlText} onOpenYaml={() => setActiveTab("yaml")} />}
            {activeTab === "other" && <OtherPanel yaml={yamlText} />}

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
              <div className="flex flex-col gap-5">
                <ObjectCatalogView catalog={catalogQuery.data} loading={catalogQuery.isLoading} failed={catalogQuery.isError} />
                <div className="flex flex-col gap-1">
                  <div className="flex items-center justify-between">
                    <h3 className="text-sm font-bold text-on-surface flex items-center gap-2">
                      <Icon icon={Sparkles} size="xs" className="text-primary" />
                      Objetos a Rastrear Globalmente
                    </h3>
                    <span className="text-xs text-muted font-mono">
                      {trackedObjects.length} seleccionados
                    </span>
                  </div>
                  <p className="text-xs text-muted">
                    Catálogo de objetos COCO soportados nativamente por los modelos de inferencia de Frigate. Activa o desactiva las clases que deseas que el servidor detecte en todas las cámaras.
                  </p>
                </div>

                {/* Categorized groups */}
                <div className="flex flex-col gap-4">
                  {FRIGATE_OBJECT_CATEGORIES.map((cat) => {
                    const items = FRIGATE_OBJECT_CATALOG.filter((o) => o.category === cat.id);
                    const trackedCount = items.filter((item) => trackedObjects.includes(item.tag)).length;
                    return (
                      <div key={cat.id} className="rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-3.5 flex flex-col gap-2.5">
                        <div className="flex items-center justify-between">
                          <span className="text-xs font-bold text-on-surface uppercase tracking-wider flex items-center gap-2">
                            {cat.label}
                            {trackedCount > 0 && (
                              <span className="rounded-full bg-primary/10 text-primary text-[10px] px-2 py-0.5 font-bold">
                                {trackedCount} activos
                              </span>
                            )}
                          </span>
                        </div>

                        <div className="flex flex-wrap gap-2">
                          {items.map((item) => {
                            const isTracked = trackedObjects.includes(item.tag);
                            return (
                              <Button
                                key={item.tag}
                                size="sm"
                                variant={isTracked ? "primary" : "outlined"}
                                className="gap-2"
                                onClick={() => {
                                  setYamlText((prev) => toggleTrackedObject(prev, item.tag));
                                }}
                                title={item.description}
                              >
                                <Icon icon={item.icon} size="xs" />
                                <span>{item.label}</span>
                                {isTracked && <Icon icon={CheckCircle2} size="xs" className="text-on-primary/90" />}
                              </Button>
                            );
                          })}
                        </div>
                      </div>
                    );
                  })}
                </div>

                {/* Custom / Non-standard objects check */}
                {trackedObjects.some((tag) => !isFrigateSupportedObject(tag)) && (
                  <div className="rounded-m3-lg bg-warn/10 border border-warn/30 p-3 text-xs text-warn flex items-center gap-2">
                    <Icon icon={AlertTriangle} size="xs" />
                    <span>
                      Hay objetos configurados que no forman parte del dataset estándar de Frigate (COCO 80):{" "}
                      <strong>
                        {trackedObjects.filter((t) => !isFrigateSupportedObject(t)).join(", ")}
                      </strong>. Asegúrate de tener un modelo custom cargado en el detector.
                    </span>
                  </div>
                )}

                <div className="flex flex-col gap-2">
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
                {/* Switch for Dynamic Optimization and 15% Buffer Watchdog */}
                <div className="flex flex-col gap-3 rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4">
                  <div className="flex items-center justify-between">
                    <div className="flex flex-col gap-0.5">
                      <span className="text-sm font-semibold text-ink flex items-center gap-2">
                        <Icon icon={ShieldCheck} size="sm" className="text-primary" />
                        Optimización Dinámica y Protección de Almacenamiento (Watchdog 15% Buffer)
                      </span>
                      <span className="text-xs text-muted">
                        Calcula retención óptima por cámara manteniendo intacto el 15% de reserva anticorrupción en disco CCTV y base de datos.
                      </span>
                    </div>
                    <Switch
                      label="Activar Protección & Retención Dinámica"
                      checked={dynamicRetentionEnabled}
                      onChange={(on) => setDynamicRetentionEnabled(on)}
                    />
                  </div>

                  {!dynamicRetentionEnabled && (
                    <div className="rounded-m3-lg bg-surface-3 p-3 text-xs text-muted flex items-center gap-2 border border-outline-variant/40">
                      <Icon icon={AlertTriangle} size="xs" className="text-warn flex-shrink-0" />
                      <span>
                        Cálculo dinámico y protección de almacenamiento desactivados. La retención se gestionará manualmente mediante los valores fijos especificados abajo sin optimización automática de buffer.
                      </span>
                    </div>
                  )}
                </div>

                {dynamicRetentionEnabled && (
                  <>
                    {/* Storage & Safety Gauge Grid */}
                    <div className="grid gap-4 sm:grid-cols-3">
                      {/* CCTV Storage Card */}
                      <div className="flex flex-col gap-2 rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4 sm:col-span-2">
                        <div className="flex items-center justify-between">
                          <span className="text-xs font-bold text-muted uppercase tracking-wider flex items-center gap-1.5">
                            <Icon icon={HardDrive} size="xs" className="text-primary" />
                            Almacenamiento CCTV & Buffer de Seguridad
                          </span>
                          <span className="text-xs font-semibold text-ink">
                            Total: {formatBytes(storageStats.cctvTotal)}
                          </span>
                        </div>

                        {/* Visual Storage Bar */}
                        <div className="flex flex-col gap-1.5 mt-1">
                          <div className="h-3 w-full rounded-full bg-surface-3 overflow-hidden flex">
                            <div
                              className="bg-primary transition-all duration-300"
                              style={{ width: `${Math.min(storageStats.usablePercent, storageStats.cctvUsedPercent)}%` }}
                              title={`Uso actual: ${formatBytes(storageStats.cctvUsed)}`}
                            />
                            <div
                              className="bg-primary/25 flex-1"
                              title={`Disponible útil: ${formatBytes(storageStats.cctvFree)}`}
                            />
                            <div
                              className="bg-amber-500/80 w-[15%]"
                              title={`15% Reserva de Seguridad: ${formatBytes(storageStats.reservedBytes)}`}
                            />
                          </div>
                          <div className="flex flex-wrap items-center justify-between text-[11px] text-muted gap-2">
                            <span>Usado: {formatBytes(storageStats.cctvUsed)} ({storageStats.cctvUsedPercent.toFixed(1)}%)</span>
                            <span className="font-semibold text-ok">{formatBytes(storageStats.usableBytes)} Útiles (85%)</span>
                            <span className="font-semibold text-amber-400 flex items-center gap-1">
                              <Icon icon={ShieldCheck} size="xs" />
                              15% Reserva de Seguridad ({formatBytes(storageStats.reservedBytes)} Reservados)
                            </span>
                          </div>
                        </div>

                        {storageStats.storageOver85 && (
                          <div className="mt-2 rounded-m3-lg bg-amber-500/15 border border-amber-500/30 p-2.5 text-xs text-amber-200 flex items-center gap-2">
                            <Icon icon={AlertTriangle} size="xs" className="text-amber-400 flex-shrink-0" />
                            <span>
                              ¡Atención: Uso superior al 85%! El Edge Agent Watchdog está podando los segmentos más antiguos para garantizar el 15% libre.
                            </span>
                          </div>
                        )}
                      </div>

                      {/* Database & Watchdog Status Card */}
                      <div className="flex flex-col gap-2 rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4">
                        <span className="text-xs font-bold text-muted uppercase tracking-wider flex items-center gap-1.5">
                          <Icon icon={Database} size="xs" className="text-primary" />
                          Base de Datos & Watchdog
                        </span>

                        <div className="flex flex-col gap-2 text-xs mt-1">
                          {storageStats.dbCritical ? (
                            <div className="rounded-m3-lg bg-danger/15 border border-danger/30 p-2 text-danger flex items-center gap-1.5 font-semibold">
                              <Icon icon={ShieldAlert} size="xs" className="flex-shrink-0" />
                              ¡Alerta Crítica BD! Espacio inferior al 15%
                            </div>
                          ) : (
                            <div className="rounded-m3-lg bg-ok/15 border border-ok/30 p-2 text-ok flex items-center gap-1.5 font-semibold">
                              <Icon icon={ShieldCheck} size="xs" className="flex-shrink-0" />
                              Base de Datos Protegida
                            </div>
                          )}

                          <div className="flex items-center justify-between text-muted">
                            <span>Espacio BD Libre:</span>
                            <span className="font-semibold text-ink">{formatBytes(storageStats.dbFree)} / {formatBytes(storageStats.dbTotal)}</span>
                          </div>

                          <div className="flex items-center justify-between text-muted">
                            <span>Watchdog Agente:</span>
                            <span className="font-semibold text-ok">Activo (15% Buffer)</span>
                          </div>

                          {storageStats.prunedCount > 0 && (
                            <div className="text-[11px] text-muted">
                              Segmentos antiguos liberados: <span className="font-semibold text-ink">{storageStats.prunedCount}</span>
                            </div>
                          )}
                        </div>
                      </div>
                    </div>

                    {/* Retention Calculator Banner */}
                    <div className="flex flex-col gap-3 rounded-m3-xl border border-primary/30 bg-primary/5 p-4">
                      <div className="flex flex-wrap items-center justify-between gap-3">
                        <div className="flex items-center gap-3">
                          <div className="size-10 rounded-full bg-primary/20 flex items-center justify-center text-primary font-bold">
                            <Icon icon={Sliders} size="sm" />
                          </div>
                          <div className="flex flex-col">
                            <span className="text-sm font-bold text-ink">
                              Días Estimados de Retención: {storageStats.attainableDays} días
                            </span>
                            <span className="text-xs text-muted">
                              Calculado para {cameraList.length} cámaras sobre {formatBytes(storageStats.usableBytes)} útiles (15% reservado).
                            </span>
                          </div>
                        </div>

                        <div className="flex items-center gap-2">
                          <span className="text-xs text-muted font-medium">Bitrate promedio:</span>
                          <Select
                            value={String(selectedBitrate)}
                            onChange={(e) => setSelectedBitrate(Number(e.target.value))}
                            className="text-xs h-8 py-1"
                          >
                            <option value="1.5">1.5 Mbps (~16.2 GB/d por cámara)</option>
                            <option value="2.0">2.0 Mbps (~21.6 GB/d por cámara)</option>
                            <option value="4.0">4.0 Mbps (~43.2 GB/d por cámara)</option>
                          </Select>
                          <Button
                            variant="primary"
                            onClick={() => {
                              const patched = patchRetentionInYaml(yamlText, storageStats.attainableDays);
                              setYamlText(patched);
                            }}
                          >
                            Aplicar Retención Calculada al YAML
                          </Button>
                        </div>
                      </div>

                      {/* Per-camera priority redistribution */}
                      <div className="flex flex-col gap-2 mt-2 pt-3 border-t border-primary/20">
                        <span className="text-xs font-bold text-muted uppercase tracking-wider">
                          Prioridad y Distribución por Cámara:
                        </span>
                        <div className="grid gap-2 sm:grid-cols-2 md:grid-cols-3">
                          {cameraList.map((cam) => {
                            const days = storageStats.perCameraDays[cam.id] ?? storageStats.attainableDays;
                            return (
                              <div
                                key={cam.id}
                                className="flex flex-col gap-2 rounded-m3-lg border border-outline-variant/60 bg-surface-1 p-3"
                              >
                                <div className="flex items-center justify-between">
                                  <span className="text-xs font-semibold text-ink truncate" title={cam.name}>
                                    {cam.name}
                                  </span>
                                  <span className="text-xs font-bold text-primary">
                                    {days} días
                                  </span>
                                </div>
                                <div className="flex items-center justify-between gap-2">
                                  <span className="text-[11px] text-muted">Prioridad:</span>
                                  <Select
                                    value={cam.priority}
                                    onChange={(e) => {
                                      const p = e.target.value as CameraPriority;
                                      setCameraPriorities((prev) => ({ ...prev, [cam.id]: p }));
                                    }}
                                    className="text-xs h-7 py-0.5"
                                  >
                                    <option value="low">Baja (0.5x)</option>
                                    <option value="normal">Normal (1.0x)</option>
                                    <option value="high">Alta (1.5x)</option>
                                    <option value="critical">Crítica (2.0x)</option>
                                  </Select>
                                </div>
                              </div>
                            );
                          })}
                        </div>
                      </div>
                    </div>
                  </>
                )}

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
                      <h4 className="text-sm font-bold flex items-center gap-2">
                        <Icon icon={Eye} size="xs" className="text-primary" />
                        Canal de Stream OpenVMS: BirdEye
                      </h4>
                      <p className="text-xs text-muted">Genera un canal de stream unificado en el servidor que conmuta dinámicamente las cámaras con actividad relevante o las muestra en mosaico continuo.</p>
                    </div>
                    <Switch
                      label="Habilitar BirdEye"
                      checked={birdseyeEnabled}
                      onChange={(on) => {
                        const block = `birdseye:\n  enabled: ${on ? "True" : "False"}\n  mode: ${birdseyeMode}`;
                        patchYamlBlock("birdseye", block);
                      }}
                    />
                  </div>

                  <div className="flex items-center gap-2 text-xs">
                    <span className="font-semibold text-muted">Estado del Canal:</span>
                    {birdseyeEnabled ? (
                      <span className="inline-flex items-center gap-1 text-emerald-400 font-semibold">
                        <Icon icon={CheckCircle2} size="xs" /> Activo en Live Grid (Canal &quot;BirdEye&quot;)
                      </span>
                    ) : (
                      <span className="text-muted">Desactivado</span>
                    )}
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
                {isDirty && (
                  <section aria-label="Vista previa de cambios" className="flex flex-col gap-2">
                    <h3 className="text-sm font-bold">Vista previa de cambios</h3>
                    <YamlDiff before={initialYaml} after={yamlText} />
                  </section>
                )}
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
              disabled={saveMutation.isPending || !isDirty || !canEdit}
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
