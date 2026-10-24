import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Car,
  CheckCircle2,
  Clock,
  Copy,
  Eye,
  Info,
  Radio,
  RefreshCw,
  Send,
  Server,
  ShieldCheck,
  Sparkles,
  Tag,
  XCircle,
} from "lucide-react";
import { api, requestJson, type Schemas, unwrap } from "@/api/client";
import { Icon } from "@/components/Icon";
import { type ConfigureResult, ProvisionConfigureResult } from "@/components/provision/ProvisionConfigureResult";
import { ProvisionIngestionCard } from "@/components/provision/ProvisionIngestionCard";
import { ProvisionRegistryCard } from "@/components/provision/ProvisionRegistryCard";
import { Button, ErrorNote, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";

interface ProvisionLprSectionProps {
  camera: Schemas["Camera"];
  onCameraUpdate?: () => void;
}

interface LPRRecentEvent {
  event_type?: string;
  source?: string;
  camera_id?: string;
  camera_ip?: string;
  plate: string;
  event_time?: string;
  received_time?: string;
  country_or_region?: string;
  confidence?: number;
  list_status?: string;
  panoramic_image?: string;
  plate_crop?: string;
  vehicle_color?: string;
  vehicle_brand?: string;
  vehicle_model?: string;
  vehicle_type?: string;
  speed?: number;
  direction?: string;
}

interface ProvisionStatusResponse {
  camera_id: string;
  camera_ip: string;
  online: boolean;
  last_seen?: string;
  keepalive_count: number;
  event_count: number;
  last_plate?: string;
  last_event_time?: string;
  target_port: number;
  keepalive_path: string;
  event_path: string;
  recent_events?: LPRRecentEvent[];
  recent_plates?: {
    id: string;
    event_id?: string;
    plate: string;
    seen_at: string;
    score?: number;
    vehicle_type?: string;
    vehicle_color?: string;
  }[];
}

export function isProvisionCamera(cam?: Schemas["Camera"] | null): boolean {
  if (!cam) return false;
  if (cam.tags?.includes("provision")) return true;

  const caps = cam.onvif_capabilities;
  const mfg = (caps?.manufacturer ?? "").toLowerCase();
  const model = (caps?.model ?? "").toLowerCase();
  const ep = (cam.onvif_endpoint ?? "").toLowerCase();
  const remote = (cam.remote_name ?? "").toLowerCase();
  const display = (cam.display_name ?? "").toLowerCase();

  return (
    mfg.includes("provision") ||
    model.includes("provision") ||
    model.startsWith("pr-") ||
    model.startsWith("di-") ||
    model.startsWith("i4-") ||
    model.startsWith("i2-") ||
    model.startsWith("i6-") ||
    ep.includes("provision") ||
    remote.includes("provision") ||
    display.includes("provision")
  );
}

export function ProvisionLprSection({ camera, onCameraUpdate }: ProvisionLprSectionProps) {
  const qc = useQueryClient();
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const [selectedImage, setSelectedImage] = useState<string | null>(null);

  // Configuration form state
  const [cameraIp, setCameraIp] = useState("");
  const [targetHost, setTargetHost] = useState("");
  const [targetPort, setTargetPort] = useState("7419");
  const [configResult, setConfigResult] = useState<ConfigureResult | null>(null);
  const [showManualGuide, setShowManualGuide] = useState(false);

  const isForced = camera.tags?.includes("provision") ?? false;
  const onvifMfg = camera.onvif_capabilities?.manufacturer;
  const onvifModel = camera.onvif_capabilities?.model;
  const isDetectedViaOnvif =
    Boolean(onvifMfg && onvifMfg.toLowerCase().includes("provision")) ||
    Boolean(onvifModel && (onvifModel.toLowerCase().includes("provision") || onvifModel.toLowerCase().startsWith("pr-")));

  // Polling query for live status & recent events
  const statusQuery = useQuery<ProvisionStatusResponse>({
    queryKey: ["camera-provision-status", camera.id],
    queryFn: async () => {
      const res = await fetch(`/api/v1/cameras/${camera.id}/provision-lpr/status`, {
        headers: { Accept: "application/json" },
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      return (await res.json()) as ProvisionStatusResponse;
    },
    refetchInterval: 3000,
  });

  const status = statusQuery.data;

  // Toggle force provision tag
  const toggleForceProvision = useMutation({
    mutationFn: async (force: boolean) => {
      const currentTags = new Set<string>(camera.tags ?? []);
      if (force) {
        currentTags.add("provision");
      } else {
        currentTags.delete("provision");
      }
      return unwrap(
        await api.PATCH("/api/v1/cameras/{cameraId}", {
          params: { path: { cameraId: camera.id } },
          body: { tags: Array.from(currentTags) },
        })
      );
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["cameras", camera.id] });
      await qc.invalidateQueries({ queryKey: ["cameras"] });
      onCameraUpdate?.();
    },
  });

  // Enable LPR on camera
  const toggleLprMode = useMutation({
    mutationFn: async (enable: boolean) => {
      const currentTags = new Set<string>(camera.tags ?? []);
      if (enable) currentTags.add("lpr");
      else currentTags.delete("lpr");

      return unwrap(
        await api.PATCH("/api/v1/cameras/{cameraId}", {
          params: { path: { cameraId: camera.id } },
          body: { lpr: enable, tags: Array.from(currentTags) },
        })
      );
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["cameras", camera.id] });
      await qc.invalidateQueries({ queryKey: ["cameras"] });
      onCameraUpdate?.();
    },
  });

  // Apply configuration on camera via CGI
  const configureCamera = useMutation({
    mutationFn: async () => {
      setConfigResult(null);
      // The camera's credentials are not sent: nothing is applied to the camera remotely.
      return requestJson<ConfigureResult>("POST", `/api/v1/cameras/${camera.id}/provision-lpr/configure`, {
        camera_ip: cameraIp.trim() || undefined,
        target_host: targetHost.trim() || undefined,
        target_port: targetPort ? parseInt(targetPort, 10) : 7419,
        interval_second: 20,
      });
    },
    onSuccess: (data) => {
      setConfigResult(data);
      if (!data.success) {
        setShowManualGuide(true);
      }
      void statusQuery.refetch();
    },
  });

  const copyToClipboard = (key: string, val: string) => {
    void navigator.clipboard.writeText(val);
    setCopiedKey(key);
    setTimeout(() => setCopiedKey(null), 2000);
  };

  const recentEvents = status?.recent_events ?? [];
  const latestEvent = recentEvents[0];

  return (
    <div className="flex flex-col gap-6">
      {/* Header Banner */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 rounded-m3-2xl border border-primary/30 bg-primary/5 p-4">
        <div className="flex items-center gap-3">
          <div className="flex size-11 items-center justify-center rounded-m3-xl bg-primary/10 text-primary">
            <Icon icon={Car} size="md" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h2 className="text-base font-bold text-on-surface">Plugin Provision-ISR LPR</h2>
              {isDetectedViaOnvif && (
                <span className="inline-flex items-center gap-1 rounded-m3-xs bg-ok/15 px-2 py-0.5 text-[11px] font-bold text-ok">
                  <Icon icon={ShieldCheck} size="xs" /> Detectada ONVIF
                </span>
              )}
              {isForced && !isDetectedViaOnvif && (
                <span className="inline-flex items-center gap-1 rounded-m3-xs bg-primary/20 px-2 py-0.5 text-[11px] font-bold text-primary">
                  <Icon icon={Tag} size="xs" /> Forzada manualmente
                </span>
              )}
            </div>
            <p className="text-xs text-muted">
              {onvifMfg ? `${onvifMfg} ${onvifModel ?? ""}` : "Reconocimiento y captura directa de patentes de cámaras Provision-ISR"}
            </p>
          </div>
        </div>

        {/* Tilde para forzar que es provision */}
        <label className="flex items-center gap-2.5 cursor-pointer select-none rounded-m3-xl border border-outline-variant/60 bg-surface-1 px-3 py-2 text-xs font-semibold text-on-surface hover:bg-surface-2 transition-colors">
          <input
            type="checkbox"
            className="size-4 rounded accent-primary cursor-pointer"
            checked={isForced}
            disabled={toggleForceProvision.isPending}
            onChange={(e) => toggleForceProvision.mutate(e.target.checked)}
          />
          <span>Forzar compatibilidad Provision-ISR</span>
        </label>
      </div>

      {/* Connection & Heartbeat Status Card */}
      <div className="rounded-m3-2xl border border-outline-variant/50 bg-surface-1 p-5 shadow-sm">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-outline-variant/30 pb-4">
          <div className="flex items-center gap-2.5">
            <span
              className={cn(
                "relative flex size-3",
                status?.online ? "text-ok" : "text-warn"
              )}
            >
              {status?.online && (
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-ok opacity-75" />
              )}
              <span
                className={cn(
                  "relative inline-flex size-3 rounded-full",
                  status?.online ? "bg-ok" : "bg-warn"
                )}
              />
            </span>
            <span className="font-bold text-sm text-on-surface">
              {status?.online
                ? "Cámara en línea · Recibiendo datos LPR"
                : "Esperando comunicación LPR de la cámara"}
            </span>
          </div>

          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="outlined"
              onClick={() => void statusQuery.refetch()}
              disabled={statusQuery.isFetching}
            >
              <Icon icon={RefreshCw} size="xs" className={statusQuery.isFetching ? "animate-spin" : ""} />
              {statusQuery.isFetching ? "Actualizando…" : "Actualizar estado"}
            </Button>
          </div>
        </div>

        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 pt-4">
          <div className="flex flex-col rounded-m3-xl bg-surface-2 p-3">
            <span className="text-[11px] font-bold uppercase tracking-wider text-muted">Keepalives Recibidos</span>
            <span className="font-mono text-xl font-bold text-on-surface mt-1">
              {status?.keepalive_count ?? 0}
            </span>
            <span className="text-[10px] text-muted mt-0.5">Latidos cada 20s</span>
          </div>

          <div className="flex flex-col rounded-m3-xl bg-surface-2 p-3">
            <span className="text-[11px] font-bold uppercase tracking-wider text-muted">Lecturas LPR</span>
            <span className="font-mono text-xl font-bold text-primary mt-1">
              {status?.event_count ?? 0}
            </span>
            <span className="text-[10px] text-muted mt-0.5">Eventos procesados</span>
          </div>

          <div className="flex flex-col rounded-m3-xl bg-surface-2 p-3">
            <span className="text-[11px] font-bold uppercase tracking-wider text-muted">IP de la Cámara</span>
            <span className="font-mono text-sm font-bold text-on-surface mt-1 truncate" title={status?.camera_ip || "No detectada"}>
              {status?.camera_ip || "Auto / ONVIF"}
            </span>
            <span className="text-[10px] text-muted mt-0.5">Origen HTTP POST</span>
          </div>

          <div className="flex flex-col rounded-m3-xl bg-surface-2 p-3">
            <span className="text-[11px] font-bold uppercase tracking-wider text-muted">Último Contacto</span>
            <span className="font-mono text-xs font-semibold text-on-surface mt-1 truncate">
              {status?.last_seen ? new Date(status.last_seen).toLocaleTimeString() : "Sin actividad"}
            </span>
            <span className="text-[10px] text-muted mt-0.5">
              {status?.last_seen ? "Conectado recientemente" : "Pendiente de configuración"}
            </span>
          </div>
        </div>
      </div>

      {/* Activar Servicio LPR y Configuración en Cámara */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 items-start">
        {/* Panel 1: Activar servicio y Enviar Configuración */}
        <div className="flex flex-col gap-4 rounded-m3-2xl border border-outline-variant/50 bg-surface-1 p-5 shadow-sm">
          <div className="flex items-center justify-between border-b border-outline-variant/30 pb-3">
            <div className="flex items-center gap-2">
              <Icon icon={Server} size="sm" className="text-primary" />
              <h3 className="text-sm font-bold text-on-surface">Activar y Aplicar LPR en Cámara</h3>
            </div>
            {/* Switch LPR */}
            <label className="flex items-center gap-2 cursor-pointer select-none">
              <span className="text-xs font-medium text-muted">Servicio LPR:</span>
              <button
                type="button"
                role="switch"
                aria-checked={camera.lpr}
                disabled={toggleLprMode.isPending}
                onClick={() => toggleLprMode.mutate(!camera.lpr)}
                className={cn(
                  "relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none",
                  camera.lpr ? "bg-primary" : "bg-outline-variant"
                )}
              >
                <span
                  className={cn(
                    "pointer-events-none inline-block size-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out",
                    camera.lpr ? "translate-x-5" : "translate-x-0"
                  )}
                />
              </button>
            </label>
          </div>

          <p className="text-xs text-muted">
            Configure los parámetros para que la cámara Provision envíe los eventos de reconocimiento de patentes y latidos directo al agente de OpenVMS.
          </p>

          <form
            onSubmit={(e) => {
              e.preventDefault();
              configureCamera.mutate();
            }}
            className="flex flex-col gap-3"
          >
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-2.5">
              <div className="sm:col-span-2">
                <label className="block text-[11px] font-bold text-muted uppercase mb-1">IP Cámara</label>
                <TextInput
                  value={cameraIp}
                  onChange={(e) => setCameraIp(e.target.value)}
                  placeholder={status?.camera_ip || "Ej: 172.22.148.100"}
                />
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-3 gap-2.5">
              <div className="sm:col-span-2">
                <label className="block text-[11px] font-bold text-muted uppercase mb-1">IP Destino (Agente / Servidor)</label>
                <TextInput
                  value={targetHost}
                  onChange={(e) => setTargetHost(e.target.value)}
                  placeholder="Auto-detectada del servidor"
                />
              </div>
              <div>
                <label className="block text-[11px] font-bold text-muted uppercase mb-1">Puerto Destino</label>
                <TextInput
                  value={targetPort}
                  onChange={(e) => setTargetPort(e.target.value)}
                  placeholder="7419"
                />
              </div>
            </div>

            <Button
              type="submit"
              disabled={configureCamera.isPending}
              className="mt-2 w-full justify-center"
            >
              <Icon icon={Send} size="xs" className={configureCamera.isPending ? "animate-spin" : ""} />
              {configureCamera.isPending ? "Registrando…" : "Registrar cámara y preparar configuración"}
            </Button>
          </form>

          {configureCamera.error && (
            <ErrorNote error={configureCamera.error instanceof Error ? configureCamera.error : new Error(String(configureCamera.error))} />
          )}

          {configResult && <ProvisionConfigureResult result={configResult} />}
        </div>

        {/* Panel 2: Parámetros del Servidor y Guía de Configuración Web */}
        <div className="flex flex-col gap-4 rounded-m3-2xl border border-outline-variant/50 bg-surface-1 p-5 shadow-sm">
          <div className="flex items-center justify-between border-b border-outline-variant/30 pb-3">
            <div className="flex items-center gap-2">
              <Icon icon={Info} size="sm" className="text-muted" />
              <h3 className="text-sm font-bold text-on-surface">Ruta en Interfaz Web de la Cámara</h3>
            </div>
            <button
              type="button"
              onClick={() => setShowManualGuide(!showManualGuide)}
              className="text-xs font-semibold text-primary hover:underline"
            >
              {showManualGuide ? "Ocultar guía" : "Ver parámetros"}
            </button>
          </div>

          <div className="rounded-m3-xl bg-surface-2 p-3 text-xs flex flex-col gap-2">
            <div className="font-semibold text-on-surface">Ubicación en el menú web de Provision-ISR:</div>
            <div className="font-mono text-xs rounded bg-surface-3 px-2 py-1.5 text-primary break-words">
              Configuración ➔ Red ➔ Configuración de Servidor ➔ HTTP POST / API
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <span className="text-[11px] font-bold text-muted uppercase">Parámetros a ingresar en la cámara:</span>
            <div className="flex flex-col gap-1.5">
              {[
                { label: "Dirección IP / Dominio", val: configResult?.parameters?.["Server IP / Domain"] || targetHost || "IP de este servidor", key: "host" },
                { label: "Puerto del Servidor", val: configResult?.parameters?.["Server Port"] || targetPort || "7419", key: "port" },
                { label: "Ruta Keepalive / Latido", val: configResult?.parameters?.["Keepalive Endpoint"] || "/SendKeepalive", key: "keepalive" },
                { label: "Ruta de Evento LPR", val: configResult?.parameters?.["LPR Event Endpoint"] || "/v1/provision-lpr/event", key: "event" },
                { label: "Intervalo de Latido", val: configResult?.parameters?.["Heartbeat Interval"] || "20 s", key: "interval" },
                { label: "Protocolo", val: "HTTP POST", key: "proto" },
              ].map((p) => (
                <div key={p.key} className="flex items-center justify-between rounded-m3-lg bg-surface-2 px-3 py-2 text-xs">
                  <span className="font-medium text-muted">{p.label}:</span>
                  <div className="flex items-center gap-2 font-mono font-bold text-on-surface">
                    <span>{p.val}</span>
                    <button
                      type="button"
                      title="Copiar"
                      onClick={() => copyToClipboard(p.key, p.val)}
                      className="text-muted hover:text-primary transition-colors p-1"
                    >
                      <Icon icon={copiedKey === p.key ? CheckCircle2 : Copy} size="xs" />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>

      <ProvisionRegistryCard serverId={camera.server_id} />
      <ProvisionIngestionCard serverId={camera.server_id} />

      {/* Monitor en Vivo: Imagen, Chapa y Atributos de Vehículo */}
      <div className="flex flex-col gap-4 rounded-m3-2xl border border-outline-variant/50 bg-surface-1 p-5 shadow-sm">
        <div className="flex items-center justify-between border-b border-outline-variant/30 pb-3">
          <div className="flex items-center gap-2">
            <Icon icon={Sparkles} size="sm" className="text-primary" />
            <h3 className="text-sm font-bold text-on-surface">Monitor LPR en Vivo de la Cámara</h3>
          </div>
          <span className="inline-flex items-center gap-1.5 text-xs font-semibold text-muted">
            <Radio size={14} className={cn("text-ok", status?.online ? "animate-pulse" : "")} />
            Transmisión en tiempo real
          </span>
        </div>

        {/* Latest Reading Highlight Box */}
        {latestEvent ? (
          <div className="rounded-m3-2xl border border-primary/40 bg-surface-2 p-5 flex flex-col md:flex-row gap-6 items-start">
            {/* Visual Images (Plate Crop & Panorama) */}
            <div className="flex flex-col gap-2 w-full md:w-72 shrink-0">
              {latestEvent.plate_crop ? (
                <div
                  className="group relative cursor-pointer overflow-hidden rounded-m3-xl border border-outline-variant/60 bg-black"
                  onClick={() => setSelectedImage(`data:image/jpeg;base64,${latestEvent.plate_crop}`)}
                >
                  <img
                    src={`data:image/jpeg;base64,${latestEvent.plate_crop}`}
                    alt={`Recorte de patente ${latestEvent.plate}`}
                    className="h-24 w-full object-contain"
                  />
                  <div className="absolute inset-0 bg-black/40 opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center text-white text-xs font-semibold gap-1">
                    <Icon icon={Eye} size="xs" /> Ver recorte
                  </div>
                </div>
              ) : latestEvent.panoramic_image ? (
                <div
                  className="group relative cursor-pointer overflow-hidden rounded-m3-xl border border-outline-variant/60 bg-black"
                  onClick={() => setSelectedImage(`data:image/jpeg;base64,${latestEvent.panoramic_image}`)}
                >
                  <img
                    src={`data:image/jpeg;base64,${latestEvent.panoramic_image}`}
                    alt={`Escena patente ${latestEvent.plate}`}
                    className="h-28 w-full object-cover"
                  />
                  <div className="absolute inset-0 bg-black/40 opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center text-white text-xs font-semibold gap-1">
                    <Icon icon={Eye} size="xs" /> Ver escena
                  </div>
                </div>
              ) : (
                <div className="flex h-24 w-full items-center justify-center rounded-m3-xl border border-outline-variant/40 bg-surface-3 text-xs text-muted">
                  Sin imagen adjunta
                </div>
              )}

              {latestEvent.panoramic_image && latestEvent.plate_crop && (
                <button
                  type="button"
                  onClick={() => setSelectedImage(`data:image/jpeg;base64,${latestEvent.panoramic_image}`)}
                  className="text-center text-xs font-semibold text-primary hover:underline flex items-center justify-center gap-1"
                >
                  <Icon icon={Eye} size="xs" /> Ver foto panorámica de escena
                </button>
              )}
            </div>

            {/* Plate & Attributes Details */}
            <div className="flex flex-1 flex-col justify-between gap-4 w-full">
              <div className="flex flex-col gap-2">
                <span className="text-[11px] font-bold uppercase tracking-wider text-muted">Última Chapa Detectada</span>
                <div className="flex flex-wrap items-center gap-3">
                  {/* License Plate Graphic Badge */}
                  <div className="inline-flex items-center justify-center rounded-m3-md border-2 border-black/80 bg-white px-5 py-2 shadow-md">
                    <span className="font-mono text-3xl font-black tracking-widest text-black uppercase">
                      {latestEvent.plate}
                    </span>
                  </div>

                  {latestEvent.confidence !== undefined && (
                    <span className="rounded-m3-full bg-ok/15 px-3 py-1 text-xs font-bold text-ok">
                      {latestEvent.confidence.toFixed(1)}% confianza
                    </span>
                  )}
                  {latestEvent.list_status && (
                    <span className="rounded-m3-full bg-primary/20 px-3 py-1 text-xs font-bold text-primary">
                      {latestEvent.list_status}
                    </span>
                  )}
                </div>
              </div>

              {/* Attributes (Color, Marca, Modelo, Tipo) */}
              <div className="flex flex-wrap items-center gap-2 pt-2 border-t border-outline-variant/30">
                {latestEvent.vehicle_color && (
                  <div className="flex items-center gap-1.5 rounded-m3-lg bg-surface-3 px-3 py-1.5 text-xs font-semibold text-on-surface">
                    <span className="text-muted">Color:</span>
                    <span className="font-bold text-primary">{latestEvent.vehicle_color}</span>
                  </div>
                )}
                {latestEvent.vehicle_brand && (
                  <div className="flex items-center gap-1.5 rounded-m3-lg bg-surface-3 px-3 py-1.5 text-xs font-semibold text-on-surface">
                    <span className="text-muted">Marca:</span>
                    <span className="font-bold text-primary">{latestEvent.vehicle_brand}</span>
                  </div>
                )}
                {latestEvent.vehicle_model && (
                  <div className="flex items-center gap-1.5 rounded-m3-lg bg-surface-3 px-3 py-1.5 text-xs font-semibold text-on-surface">
                    <span className="text-muted">Modelo:</span>
                    <span className="font-bold text-primary">{latestEvent.vehicle_model}</span>
                  </div>
                )}
                {latestEvent.vehicle_type && (
                  <div className="flex items-center gap-1.5 rounded-m3-lg bg-surface-3 px-3 py-1.5 text-xs font-semibold text-on-surface">
                    <span className="text-muted">Tipo:</span>
                    <span className="font-bold text-primary">{latestEvent.vehicle_type}</span>
                  </div>
                )}
                {latestEvent.speed !== undefined && (
                  <div className="flex items-center gap-1.5 rounded-m3-lg bg-surface-3 px-3 py-1.5 text-xs font-semibold text-on-surface">
                    <span className="text-muted">Velocidad:</span>
                    <span className="font-bold text-on-surface">{latestEvent.speed} km/h</span>
                  </div>
                )}
                {latestEvent.direction && (
                  <div className="flex items-center gap-1.5 rounded-m3-lg bg-surface-3 px-3 py-1.5 text-xs font-semibold text-on-surface">
                    <span className="text-muted">Dirección:</span>
                    <span className="font-bold text-on-surface">{latestEvent.direction}</span>
                  </div>
                )}
              </div>

              <div className="flex items-center gap-1.5 text-xs text-muted pt-1">
                <Icon icon={Clock} size="xs" />
                <span>
                  {latestEvent.event_time ? new Date(latestEvent.event_time).toLocaleString() : "Hora reciente"}
                </span>
              </div>
            </div>
          </div>
        ) : (
          <div className="flex flex-col items-center justify-center gap-2 rounded-m3-2xl border border-dashed border-outline-variant/60 p-10 text-center">
            <Icon icon={Car} size="lg" className="text-muted/60" />
            <span className="text-sm font-semibold text-muted">Aún no se han recibido lecturas de patentes de esta cámara</span>
            <p className="text-xs text-muted max-w-sm">
              Una vez que apliques la configuración o la cámara envíe vehículos por la ruta HTTP POST, las chapas aparecerán aquí en tiempo real con sus fotos y atributos.
            </p>
          </div>
        )}

        {/* History Table of Recent Readings */}
        {recentEvents.length > 1 && (
          <div className="flex flex-col gap-2 pt-2">
            <span className="text-xs font-bold uppercase tracking-wider text-muted">Historial de Lecturas Recientes</span>
            <div className="flex flex-col gap-2">
              {recentEvents.slice(1).map((ev, i) => (
                <div
                  key={`${ev.plate}-${ev.event_time ?? i}`}
                  className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 rounded-m3-xl border border-outline-variant/40 bg-surface-2 p-3 hover:bg-surface-3 transition-colors"
                >
                  <div className="flex items-center gap-3">
                    {ev.plate_crop ? (
                      <img
                        src={`data:image/jpeg;base64,${ev.plate_crop}`}
                        alt={ev.plate}
                        className="h-9 w-20 object-contain rounded bg-black shrink-0 cursor-pointer"
                        onClick={() => setSelectedImage(`data:image/jpeg;base64,${ev.plate_crop}`)}
                      />
                    ) : (
                      <div className="flex h-9 w-20 items-center justify-center rounded bg-surface-3 text-[10px] text-muted shrink-0">
                        Sin foto
                      </div>
                    )}
                    <span className="font-mono text-base font-bold text-on-surface uppercase">
                      {ev.plate}
                    </span>
                    {ev.vehicle_color && (
                      <span className="rounded bg-surface-3 px-2 py-0.5 text-xs font-medium text-muted">
                        {ev.vehicle_color}
                      </span>
                    )}
                    {ev.vehicle_brand && (
                      <span className="rounded bg-surface-3 px-2 py-0.5 text-xs font-medium text-muted">
                        {ev.vehicle_brand} {ev.vehicle_model ?? ""}
                      </span>
                    )}
                  </div>

                  <div className="flex items-center gap-3 text-xs text-muted">
                    {ev.confidence !== undefined && <span>{ev.confidence.toFixed(0)}% conf</span>}
                    <span>{ev.event_time ? new Date(ev.event_time).toLocaleTimeString() : ""}</span>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Modal for viewing image in full detail */}
      {selectedImage && (
        <div
          role="dialog"
          aria-modal="true"
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4"
          onClick={() => setSelectedImage(null)}
        >
          <div className="relative max-h-[90vh] max-w-[90vw] overflow-hidden rounded-m3-2xl bg-black p-2">
            <img src={selectedImage} alt="Detalle LPR" className="max-h-[85vh] max-w-full object-contain rounded-m3-xl" />
            <button
              type="button"
              className="absolute top-4 right-4 rounded-full bg-black/70 p-2 text-white hover:bg-black transition-colors"
              onClick={() => setSelectedImage(null)}
            >
              <Icon icon={XCircle} size="sm" />
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
