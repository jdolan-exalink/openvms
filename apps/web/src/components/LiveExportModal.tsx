import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Download, Film, ShieldCheck } from "lucide-react";
import { useId, useMemo, useState } from "react";
import { api, unwrap } from "@/api/client";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Button, Checkbox, ErrorNote, Field, TextInput } from "@/components/ui";
import { fmtDateTime } from "@/lib/format";
import type { TimelineCamera } from "@/components/DayTimeline";

export interface LiveExportModalProps {
  open: boolean;
  onClose: () => void;
  range: { start: number; end: number };
  cameras: TimelineCamera[];
  selectedCameraId?: string;
}

const pad = (n: number) => String(n).padStart(2, "0");

function formatDuration(seconds: number): string {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) {
    return `${h}h ${pad(m)}m ${pad(s)}s`;
  }
  return `${m}m ${pad(s)}s`;
}

export function LiveExportModal({ open, onClose, range, cameras, selectedCameraId }: LiveExportModalProps) {
  const qc = useQueryClient();
  const descId = useId();

  const startDate = useMemo(() => new Date(range.start * 1000), [range.start]);
  const endDate = useMemo(() => new Date(range.end * 1000), [range.end]);
  const durationSec = Math.max(0, range.end - range.start);

  const defaultName = useMemo(() => {
    const d = startDate;
    const dateStr = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
    const timeStr = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
    return cameras.length > 1
      ? `Exportación ${dateStr} ${timeStr} (${cameras.length} cámaras)`
      : `Exportación ${dateStr} ${timeStr}`;
  }, [startDate, cameras.length]);

  const [name, setName] = useState(defaultName);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => {
    if (selectedCameraId && cameras.some((c) => c.id === selectedCameraId)) {
      return new Set([selectedCameraId]);
    }
    return new Set(cameras.map((c) => c.id));
  });
  const [isProtected, setIsProtected] = useState(false);
  const [successNotice, setSuccessNotice] = useState<string | null>(null);

  const createJob = useMutation({
    mutationFn: async () => {
      const res = await api.POST("/api/v1/export-jobs", {
        body: {
          camera_ids: Array.from(selectedIds),
          start_time: startDate.toISOString(),
          end_time: endDate.toISOString(),
          name: name.trim() || undefined,
          protected: isProtected,
        },
      });
      return unwrap(res);
    },
    onSuccess: (job) => {
      void qc.invalidateQueries({ queryKey: ["export-jobs"] });
      void qc.invalidateQueries({ queryKey: ["exports"] });
      setSuccessNotice(`Exportación iniciada: ${job.name}. Podés seguir trabajando.`);
      setTimeout(() => {
        onClose();
      }, 1200);
    },
  });

  if (!open) return null;

  const toggleCamera = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const selectAll = () => setSelectedIds(new Set(cameras.map((c) => c.id)));
  const selectNone = () => setSelectedIds(new Set());

  return (
    <Modal title="Exportar evidencia de video" onClose={onClose} className="max-w-2xl">
      <div className="flex flex-col gap-4">
        {/* Range Summary Card */}
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 rounded-m3-xl bg-surface-2 p-3.5 border border-line/60">
          <div>
            <span className="text-[11px] font-medium uppercase tracking-wider text-muted">Desde (IN)</span>
            <p className="font-mono text-xs font-semibold text-ink mt-0.5">{fmtDateTime(startDate)}</p>
          </div>
          <div>
            <span className="text-[11px] font-medium uppercase tracking-wider text-muted">Hasta (OUT)</span>
            <p className="font-mono text-xs font-semibold text-ink mt-0.5">{fmtDateTime(endDate)}</p>
          </div>
          <div>
            <span className="text-[11px] font-medium uppercase tracking-wider text-muted">Duración</span>
            <p className="font-mono text-xs font-semibold text-primary mt-0.5">{formatDuration(durationSec)}</p>
          </div>
        </div>

        {/* Camera Selector */}
        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium">
              Cámaras a exportar ({selectedIds.size} de {cameras.length})
            </span>
            <div className="flex items-center gap-2 text-xs">
              <button type="button" onClick={selectAll} className="text-primary hover:underline font-medium">
                Todas en grid
              </button>
              <span className="text-muted">·</span>
              <button type="button" onClick={selectNone} className="text-muted hover:underline">
                Ninguna
              </button>
            </div>
          </div>

          <div className="max-h-48 overflow-y-auto rounded-m3-lg border border-line bg-surface-2 p-2 flex flex-col gap-1">
            {cameras.map((cam) => (
              <label
                key={cam.id}
                className="flex items-center gap-2.5 rounded-m3-md px-2.5 py-1.5 hover:bg-surface-3/80 cursor-pointer text-sm"
              >
                <input
                  type="checkbox"
                  checked={selectedIds.has(cam.id)}
                  onChange={() => toggleCamera(cam.id)}
                  className="size-4 shrink-0 rounded accent-primary"
                />
                <Icon icon={Film} size="xs" className="text-muted shrink-0" />
                <span className="truncate flex-1 font-medium">{cam.name}</span>
                {cam.id === selectedCameraId && (
                  <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[10px] font-semibold text-primary">
                    Activa
                  </span>
                )}
              </label>
            ))}
          </div>
        </div>

        {/* Job Name */}
        <Field label="Nombre de la exportación">
          <TextInput
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Ej: Registro forense sector A"
          />
        </Field>

        {/* Evidence Protection */}
        <div className="rounded-m3-lg border border-line/60 bg-surface-2 p-3">
          <Checkbox
            checked={isProtected}
            onChange={setIsProtected}
            label={
              <span className="flex items-center gap-1.5 font-medium">
                <Icon icon={ShieldCheck} size="xs" className="text-ok" />
                Marcar como evidencia protegida
              </span>
            }
            description="Las exportaciones protegidas quedan resguardadas permanentemente y no son purgadas por políticas de retención automática."
          />
        </div>

        {/* Notice on low priority background execution */}
        <p id={descId} className="text-xs text-muted leading-relaxed">
          Esta exportación se convertirá en un trabajo asíncrono transferido al orquestador en segundo plano con ancho de banda controlado (tráfico de baja prioridad). No interrumpirá la visualización en vivo ni las grabaciones de Frigate.
        </p>

        {successNotice && (
          <div role="status" className="rounded-m3-lg border border-ok/40 bg-ok/10 p-3 text-sm text-ok font-medium">
            {successNotice}
          </div>
        )}

        {createJob.error && <ErrorNote error={createJob.error} />}

        {/* Action Buttons */}
        <div className="flex items-center justify-end gap-2 pt-2 border-t border-line">
          <Button variant="outlined" onClick={onClose} disabled={createJob.isPending}>
            Cancelar
          </Button>
          <Button
            variant="filled"
            onClick={() => createJob.mutate()}
            disabled={selectedIds.size === 0 || durationSec <= 0 || createJob.isPending || !!successNotice}
            className="gap-2"
          >
            <Icon icon={Download} size="xs" />
            {createJob.isPending ? "Iniciando..." : "Iniciar exportación"}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
