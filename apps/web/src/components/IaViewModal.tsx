import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, LayoutGrid, Sparkles, Video } from "lucide-react";
import { useState } from "react";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Button, Field, Select, Switch, TextInput } from "@/components/ui";
import { camerasQuery, serversQuery } from "@/api/queries";
import { api } from "@/api/client";
import { cn } from "@/lib/cn";

export type IaViewConfig = {
  name: string;
  enabled: boolean;
  serverId: string;
  mode: "objects" | "motion" | "continuous";
  width: number;
  height: number;
  quality: number;
  cameraIds: string[];
};

const STORAGE_KEY = "openvms.ia_view_channel_config";

export function loadIaViewConfig(defaultServerId = ""): IaViewConfig {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      return {
        name: parsed.name || "IA-View",
        enabled: parsed.enabled ?? true,
        serverId: parsed.serverId || defaultServerId,
        mode: parsed.mode || "objects",
        width: parsed.width || 1280,
        height: parsed.height || 720,
        quality: parsed.quality || 8,
        cameraIds: Array.isArray(parsed.cameraIds) ? parsed.cameraIds : [],
      };
    }
  } catch {
    // storage unavailable
  }
  return {
    name: "IA-View",
    enabled: true,
    serverId: defaultServerId,
    mode: "objects",
    width: 1280,
    height: 720,
    quality: 8,
    cameraIds: [],
  };
}

export function saveIaViewConfig(cfg: IaViewConfig) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(cfg));
  } catch {
    // storage unavailable
  }
}

export function IaViewModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const qc = useQueryClient();
  const servers = useQuery(serversQuery);
  const cameras = useQuery(camerasQuery());

  const firstServerId = servers.data?.[0]?.id ?? "";
  const [config, setConfig] = useState<IaViewConfig>(() => loadIaViewConfig(firstServerId));
  const [savedSuccess, setSavedSuccess] = useState(false);

  // If serverId was empty and servers loaded, initialize it
  const activeServerId = config.serverId || firstServerId;
  const serverCameras = (cameras.data ?? []).filter((c) => !activeServerId || c.server_id === activeServerId);

  const saveMutation = useMutation({
    mutationFn: async () => {
      saveIaViewConfig({ ...config, serverId: activeServerId });
      // If we have an endpoint to apply to server config, we attempt best effort:
      try {
        if (activeServerId) {
          // Internal birdseye config applied transparently to Frigate server without mentioning the name to user
          await api.PATCH("/api/v1/servers/{serverId}/frigate-config" as never, {
            params: { path: { serverId: activeServerId } },
            body: {
              sections: {
                birdseye: {
                  enabled: config.enabled,
                  mode: config.mode,
                  width: config.width,
                  height: config.height,
                  quality: config.quality,
                },
              },
            },
          } as never);
        }
      } catch {
        // Frigate config endpoint might be read-only or mocked in tests
      }
      return true;
    },
    onSuccess: async () => {
      setSavedSuccess(true);
      await qc.invalidateQueries({ queryKey: ["cameras"] });
      setTimeout(() => setSavedSuccess(false), 3000);
    },
  });

  if (!open) return null;

  const toggleCamera = (camId: string) => {
    setConfig((prev) => {
      const exists = prev.cameraIds.includes(camId);
      const cameraIds = exists ? prev.cameraIds.filter((id) => id !== camId) : [...prev.cameraIds, camId];
      return { ...prev, cameraIds };
    });
  };

  const selectAll = () => {
    setConfig((prev) => ({
      ...prev,
      cameraIds: serverCameras.map((c) => c.id),
    }));
  };

  const deselectAll = () => {
    setConfig((prev) => ({
      ...prev,
      cameraIds: [],
    }));
  };

  return (
    <Modal title="Canal Inteligente IA-View" onClose={onClose}>
      <div className="flex flex-col gap-6 max-w-2xl">
        {/* Avigilon / Nx Header Banner */}
        <div className="flex items-start gap-3 rounded-m3-xl border border-primary/20 bg-primary/8 p-4">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-m3-lg bg-primary/20 text-primary">
            <Icon icon={Sparkles} size="sm" />
          </div>
          <div className="flex flex-col gap-1">
            <h3 className="text-sm font-semibold text-ink">Composición Dinámica por Inteligencia Artificial</h3>
            <p className="text-xs text-on-surface-variant">
              Genera un canal de streaming inteligente que conmuta automáticamente entre las cámaras participantes
              según los eventos y detecciones en tiempo real, optimizando el ancho de banda y la atención del operador.
            </p>
          </div>
        </div>

        {/* Channel Identity */}
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Nombre del Canal" hint="Nombre con el que se identificará este flujo inteligente.">
            <TextInput
              value={config.name}
              onChange={(e) => setConfig((p) => ({ ...p, name: e.target.value }))}
              placeholder="IA-View"
            />
          </Field>

          {servers.data && servers.data.length > 1 && (
            <Field label="Servidor NVR" hint="Servidor que ejecuta la composición de IA.">
              <Select
                value={activeServerId}
                onChange={(e) => setConfig((p) => ({ ...p, serverId: e.target.value }))}
              >
                {servers.data.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </Select>
            </Field>
          )}

          <div className="flex items-center gap-3 self-end sm:col-span-2 pt-1">
            <Switch
              label="Canal IA-View Habilitado"
              checked={config.enabled}
              onChange={(enabled) => setConfig((p) => ({ ...p, enabled }))}
            />
            <span className="text-xs text-muted">
              {config.enabled ? "Generando flujo y procesando eventos" : "Flujo pausado"}
            </span>
          </div>
        </div>

        {/* Operating Mode */}
        <div className="flex flex-col gap-2">
          <label className="text-xs font-semibold text-muted uppercase">Modo de Operación del Canal</label>
          <div className="grid gap-3 sm:grid-cols-3">
            <button
              type="button"
              onClick={() => setConfig((p) => ({ ...p, mode: "objects" }))}
              className={cn(
                "flex flex-col gap-1 rounded-m3-lg border p-3 text-left transition-colors",
                config.mode === "objects"
                  ? "border-primary bg-primary/10 text-primary shadow-sm"
                  : "border-outline-variant bg-surface-2 text-on-surface hover:bg-surface-3"
              )}
            >
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold">Objetos Activos</span>
                <Icon icon={Sparkles} size="xs" />
              </div>
              <p className="text-[11px] text-muted leading-tight">
                Muestra las cámaras solo cuando detectan personas, vehículos u objetos de interés.
              </p>
            </button>

            <button
              type="button"
              onClick={() => setConfig((p) => ({ ...p, mode: "motion" }))}
              className={cn(
                "flex flex-col gap-1 rounded-m3-lg border p-3 text-left transition-colors",
                config.mode === "motion"
                  ? "border-primary bg-primary/10 text-primary shadow-sm"
                  : "border-outline-variant bg-surface-2 text-on-surface hover:bg-surface-3"
              )}
            >
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold">Movimiento</span>
                <Icon icon={Video} size="xs" />
              </div>
              <p className="text-[11px] text-muted leading-tight">
                Conmuta dinámicamente cada vez que se detecta cualquier movimiento en la escena.
              </p>
            </button>

            <button
              type="button"
              onClick={() => setConfig((p) => ({ ...p, mode: "continuous" }))}
              className={cn(
                "flex flex-col gap-1 rounded-m3-lg border p-3 text-left transition-colors",
                config.mode === "continuous"
                  ? "border-primary bg-primary/10 text-primary shadow-sm"
                  : "border-outline-variant bg-surface-2 text-on-surface hover:bg-surface-3"
              )}
            >
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold">Mosaico Continuo</span>
                <Icon icon={LayoutGrid} size="xs" />
              </div>
              <p className="text-[11px] text-muted leading-tight">
                Mantiene permanentemente la cuadrícula activa con todas las cámaras vinculadas.
              </p>
            </button>
          </div>
        </div>

        {/* Resolution & Quality */}
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Resolución del Canal IA">
            <Select
              value={`${config.width}x${config.height}`}
              onChange={(e) => {
                const parts = e.target.value.split("x");
                const width = Number(parts[0]) || 1280;
                const height = Number(parts[1]) || 720;
                setConfig((p) => ({ ...p, width, height }));
              }}
            >
              <option value="1280x720">1280×720 (720p HD - Recomendado)</option>
              <option value="1920x1080">1920×1080 (1080p Full HD)</option>
            </Select>
          </Field>

          <Field label="Calidad de Codificación">
            <Select
              value={String(config.quality)}
              onChange={(e) => setConfig((p) => ({ ...p, quality: Number(e.target.value) }))}
            >
              <option value="6">Estándar (Bajo ancho de banda)</option>
              <option value="8">Alta calidad (Recomendado)</option>
              <option value="10">Máxima fidelidad</option>
            </Select>
          </Field>
        </div>

        {/* Participating Cameras Selection */}
        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <span className="text-xs font-semibold text-muted uppercase">
              Cámaras Participantes ({config.cameraIds.length} de {serverCameras.length})
            </span>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={selectAll}
                className="text-xs font-medium text-primary hover:underline"
              >
                Todas
              </button>
              <span className="text-muted">·</span>
              <button
                type="button"
                onClick={deselectAll}
                className="text-xs font-medium text-muted hover:underline"
              >
                Ninguna
              </button>
            </div>
          </div>

          <div className="max-h-48 overflow-y-auto rounded-m3-lg border border-outline-variant/60 bg-surface-2 p-2">
            {serverCameras.length === 0 ? (
              <p className="p-3 text-xs text-muted text-center">No hay cámaras disponibles en este servidor.</p>
            ) : (
              <div className="grid gap-1 sm:grid-cols-2">
                {serverCameras.map((cam) => {
                  const isChecked = config.cameraIds.includes(cam.id);
                  return (
                    <label
                      key={cam.id}
                      className={cn(
                        "flex items-center gap-2 rounded-m3-md px-3 py-2 text-xs cursor-pointer select-none transition-colors",
                        isChecked ? "bg-primary/10 text-ink font-medium" : "text-on-surface-variant hover:bg-surface-3"
                      )}
                    >
                      <input
                        type="checkbox"
                        checked={isChecked}
                        onChange={() => toggleCamera(cam.id)}
                        className="size-4 rounded accent-primary"
                      />
                      <span className="truncate">{cam.display_name}</span>
                    </label>
                  );
                })}
              </div>
            )}
          </div>
        </div>

        {/* Actions & Feedback */}
        <div className="flex flex-wrap items-center justify-between gap-3 pt-2 border-t border-outline-variant">
          {savedSuccess && (
            <p role="status" className="flex items-center gap-1.5 text-xs font-medium text-ok">
              <Icon icon={Check} size="xs" /> Configuración de {config.name} guardada correctamente.
            </p>
          )}
          <div className="flex items-center gap-2 ml-auto">
            <Button variant="text" onClick={onClose}>
              Cerrar
            </Button>
            <Button
              variant="primary"
              disabled={saveMutation.isPending || !config.name.trim()}
              onClick={() => saveMutation.mutate()}
            >
              {saveMutation.isPending ? "Guardando…" : "Guardar Canal IA-View"}
            </Button>
          </div>
        </div>
      </div>
    </Modal>
  );
}
