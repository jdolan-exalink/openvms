import { useState } from "react";
import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  Ban,
  Camera,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Download,
  Play,
  RotateCw,
  Share2,
  ShieldCheck,
  Trash2,
} from "lucide-react";
import { api, unwrap } from "@/api/client";
import { exportJobsQuery, exportsQuery } from "@/api/queries";
import { EvidencePlayerModal } from "@/components/EvidencePlayerModal";
import { Icon } from "@/components/Icon";
import { ShareExportModal } from "@/components/ShareExportModal";
import {
  Button,
  Empty,
  ErrorNote,
  IconButton,
  LinkButton,
  PageHeader,
  StatusBadge,
  Table,
  Th,
  type Tone,
} from "@/components/ui";
import { fmtDateTime } from "@/lib/format";

const statusConfig: Record<string, { label: string; tone: Tone }> = {
  queued: { label: "En cola", tone: "warn" },
  preparing: { label: "Preparando", tone: "warn" },
  transferring: { label: "Transfiriendo", tone: "info" },
  processing: { label: "Procesando", tone: "info" },
  ready: { label: "Lista", tone: "ok" },
  failed: { label: "Falló", tone: "bad" },
  cancelled: { label: "Cancelada", tone: "neutral" },
  expired: { label: "Expirada", tone: "neutral" },
  // Legacy single export statuses
  pending: { label: "En cola", tone: "warn" },
  running: { label: "Generando", tone: "warn" },
};

function fmtBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 1 ? 2 : 0)} ${units[i]}`;
}

function fmtSpeed(bps: number): string {
  if (!bps || bps <= 0) return "";
  const mbps = (bps / (1024 * 1024)).toFixed(1);
  return `${mbps} Mbps`;
}

function fmtETA(seconds: number): string {
  if (!seconds || seconds <= 0) return "";
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `ETA ${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
}

export function Exports() {
  const t = useT();
  const qc = useQueryClient();
  const [expandedJobs, setExpandedJobs] = useState<Record<string, boolean>>({});
  const [playbackJob, setPlaybackJob] = useState<any | null>(null);
  const [shareJob, setShareJob] = useState<any | null>(null);

  const jobs = useQuery(exportJobsQuery);
  const legacyExports = useQuery(exportsQuery);

  const deleteJob = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.DELETE("/api/v1/export-jobs/{jobId}", { params: { path: { jobId: id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["export-jobs"] }),
  });

  const cancelJob = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.POST("/api/v1/export-jobs/{jobId}/cancel", { params: { path: { jobId: id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["export-jobs"] }),
  });

  const retryJob = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.POST("/api/v1/export-jobs/{jobId}/retry", { params: { path: { jobId: id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["export-jobs"] }),
  });

  const removeLegacy = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.DELETE("/api/v1/exports/{exportId}", { params: { path: { exportId: id } } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["exports"] }),
  });

  const toggleExpand = (id: string) => {
    setExpandedJobs((prev) => ({ ...prev, [id]: !prev[id] }));
  };

  const hasJobs = (jobs.data && jobs.data.length > 0) || (legacyExports.data && legacyExports.data.length > 0);
  const error = jobs.error ?? legacyExports.error ?? deleteJob.error ?? cancelJob.error ?? retryJob.error;

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader title={t("nav.exports")} description={t("settings.exports")} />
      <ErrorNote error={error} />

      {!hasJobs && !jobs.isLoading && <Empty>Todavía no hay exportaciones.</Empty>}

      {/* Multi-camera Orchestrated Export Jobs */}
      {!!jobs.data?.length && (
        <div className="flex flex-col gap-3">
          <h2 className="text-sm font-semibold tracking-wide text-muted uppercase">
            Trabajos de exportación y evidencia ({jobs.data.length})
          </h2>
          <Table label="Trabajos de exportación">
            <thead>
              <tr>
                <Th>Nombre / Evidencia</Th>
                <Th>Cámaras</Th>
                <Th>Rango de video</Th>
                <Th>Solicitud</Th>
                <Th>Estado y progreso</Th>
                <Th className="text-right">Acciones</Th>
              </tr>
            </thead>
            <tbody>
              {jobs.data.map((job) => {
                const isExpanded = !!expandedJobs[job.id];
                const inProgress = ["queued", "preparing", "transferring", "processing"].includes(job.status);
                const cfg = statusConfig[job.status] ?? { label: job.status, tone: "neutral" as Tone };

                return (
                  <tr key={job.id} className="border-t border-outline-variant align-top">
                    <td colSpan={6} className="p-0">
                      <div className="p-3">
                        <div className="grid grid-cols-1 md:grid-cols-6 items-start gap-3">
                          {/* Col 1: Name, Evidence Protected */}
                          <div className="col-span-1 md:col-span-1 flex flex-col gap-1">
                            <span className="font-semibold text-sm leading-snug">{job.name}</span>
                            {job.protected && (
                              <span className="inline-flex items-center gap-1 text-[11px] font-medium text-primary">
                                <Icon icon={ShieldCheck} size="xs" />
                                <span>Evidencia protegida</span>
                              </span>
                            )}
                          </div>

                          {/* Col 2: Cameras count & expand button */}
                          <div className="col-span-1 md:col-span-1 flex flex-col gap-1">
                            <button
                              type="button"
                              onClick={() => toggleExpand(job.id)}
                              className="inline-flex items-center gap-1.5 text-xs text-on-surface-variant hover:text-primary transition-colors text-left"
                            >
                              <Icon icon={Camera} size="xs" />
                              <span>{job.camera_count} {job.camera_count === 1 ? "cámara" : "cámaras"}</span>
                              <Icon icon={isExpanded ? ChevronUp : ChevronDown} size="xs" />
                            </button>
                          </div>

                          {/* Col 3: Video Range */}
                          <div className="col-span-1 md:col-span-1 font-mono text-xs whitespace-nowrap">
                            <div className="text-on-surface">{fmtDateTime(job.start_time)}</div>
                            <div className="text-muted">{fmtDateTime(job.end_time)}</div>
                          </div>

                          {/* Col 4: Requested By */}
                          <div className="col-span-1 md:col-span-1 text-xs">
                            <span className="font-medium">{job.requested_by_name}</span>
                            <div className="text-muted">{fmtDateTime(job.created_at)}</div>
                          </div>

                          {/* Col 5: Status, Progress Bar, Speed, ETA */}
                          <div className="col-span-1 md:col-span-1 flex flex-col gap-1.5">
                            <div className="flex items-center gap-2">
                              <StatusBadge status={job.status} tone={cfg.tone} label={cfg.label} />
                              {inProgress && job.progress > 0 && (
                                <span className="font-mono text-xs font-semibold">{Math.round(job.progress)}%</span>
                              )}
                            </div>

                            {/* Progress bar and metrics when transferring or preparing */}
                            {inProgress && (
                              <div className="flex flex-col gap-1 w-full max-w-xs">
                                <div className="h-1.5 w-full bg-surface-3 rounded-full overflow-hidden">
                                  <div
                                    className="h-full bg-primary rounded-full transition-all duration-300"
                                    style={{ width: `${Math.min(100, Math.max(5, job.progress))}%` }}
                                  />
                                </div>
                                <div className="flex flex-wrap items-center justify-between text-[10px] text-muted font-mono">
                                  <span>{fmtBytes(job.transferred_bytes)}</span>
                                  {job.speed_bps > 0 && <span>{fmtSpeed(job.speed_bps)}</span>}
                                  {job.eta_seconds > 0 && <span>{fmtETA(job.eta_seconds)}</span>}
                                </div>
                              </div>
                            )}

                            {job.status === "ready" && job.total_bytes > 0 && (
                              <span className="text-xs text-muted font-mono">{fmtBytes(job.total_bytes)}</span>
                            )}

                            {job.error && (
                              <div role="alert" className="text-xs text-bad flex items-start gap-1">
                                <Icon icon={AlertCircle} size="xs" />
                                <span>{job.error}</span>
                              </div>
                            )}
                          </div>

                          {/* Col 6: Actions */}
                          <div className="col-span-1 md:col-span-1 flex items-center justify-end gap-1.5 whitespace-nowrap">
                            {job.status === "ready" && (
                              <>
                                <Button
                                  variant="filled"
                                  size="sm"
                                  onClick={() => setPlaybackJob(job)}
                                  title="Reproducir evidencia multi-cámara sincronizada"
                                >
                                  <Icon icon={Play} size="xs" />
                                  <span>Reproducir</span>
                                </Button>

                                <Button
                                  variant="outlined"
                                  size="sm"
                                  onClick={() => setShareJob(job)}
                                  title="Compartir evidencia de forma segura"
                                >
                                  <Icon icon={Share2} size="xs" />
                                  <span>Compartir</span>
                                </Button>

                                <LinkButton
                                  variant="outlined"
                                  size="sm"
                                  href={`/media/v1/export-jobs/${job.id}/download`}
                                  title={job.camera_count > 1 ? "Descargar paquete ZIP" : "Descargar video MP4"}
                                >
                                  <Icon icon={Download} size="xs" />
                                  <span>{job.camera_count > 1 ? "Descargar ZIP" : "Descargar"}</span>
                                </LinkButton>
                              </>
                            )}

                            {inProgress && (
                              <Button
                                variant="outlined"
                                size="sm"
                                onClick={() => cancelJob.mutate(job.id)}
                                title="Cancelar exportación"
                              >
                                <Icon icon={Ban} size="xs" />
                                <span>Cancelar</span>
                              </Button>
                            )}

                            {(job.status === "failed" || job.status === "cancelled") && (
                              <Button
                                variant="outlined"
                                size="sm"
                                onClick={() => retryJob.mutate(job.id)}
                                title="Reintentar exportación"
                              >
                                <Icon icon={RotateCw} size="xs" />
                                <span>Reintentar</span>
                              </Button>
                            )}

                            <IconButton
                              icon={Trash2}
                              disabled={job.protected}
                              onClick={() => {
                                if (!job.protected && confirm("¿Eliminar este trabajo de exportación?")) {
                                  deleteJob.mutate(job.id);
                                }
                              }}
                              aria-label="Eliminar exportación"
                              title={job.protected ? "Evidencia protegida contra eliminación" : "Eliminar exportación"}
                            />
                          </div>
                        </div>

                        {/* Collapsible camera items and forensic integrity */}
                        {isExpanded && job.items && job.items.length > 0 && (
                          <div className="mt-3 pt-3 border-t border-outline-variant/60 flex flex-col gap-2 bg-surface-2/40 rounded-m3-md p-3">
                            <span className="text-xs font-semibold text-on-surface-variant">
                              Cámaras incluidas ({job.items.length})
                            </span>
                            <div className="flex flex-col gap-2">
                              {job.items.map((it) => {
                                const itemCfg = statusConfig[it.status] ?? { label: it.status, tone: "neutral" as Tone };
                                return (
                                  <div
                                    key={it.id}
                                    className="flex flex-col gap-1 p-2 bg-surface-1 rounded-m3-sm border border-outline-variant/40 text-xs"
                                  >
                                    <div className="flex flex-wrap items-center justify-between gap-2">
                                      <div className="flex items-center gap-2">
                                        <Icon icon={Camera} size="xs" className="text-muted" />
                                        <span className="font-medium text-on-surface">{it.camera_name}</span>
                                        {it.server_name && (
                                          <span className="text-[11px] text-muted">({it.server_name})</span>
                                        )}
                                      </div>

                                      <div className="flex items-center gap-3">
                                        {it.sha256_hash ? (
                                          <span
                                            className="inline-flex items-center gap-1 text-[11px] text-ok font-mono"
                                            title={`SHA-256: ${it.sha256_hash}`}
                                          >
                                            <Icon icon={CheckCircle2} size="xs" />
                                            <span>SHA-256: {it.sha256_hash.slice(0, 10)}...</span>
                                          </span>
                                        ) : null}

                                        {it.total_bytes > 0 && (
                                          <span className="text-muted font-mono">{fmtBytes(it.total_bytes)}</span>
                                        )}

                                        <StatusBadge status={it.status} tone={itemCfg.tone} label={itemCfg.label} />

                                        {it.status === "ready" && (
                                          <LinkButton
                                            variant="text"
                                            size="sm"
                                            href={`/media/v1/export-jobs/${job.id}/items/${it.id}/download`}
                                            title={`Descargar ${it.camera_name}`}
                                          >
                                            <Icon icon={Download} size="xs" />
                                            <span>Clip</span>
                                          </LinkButton>
                                        )}
                                      </div>
                                    </div>

                                    {it.error && (
                                      <div className="text-[11px] text-bad flex items-center gap-1 font-mono mt-0.5">
                                        <Icon icon={AlertCircle} size="xs" />
                                        <span>Error: {it.error}</span>
                                      </div>
                                    )}
                                  </div>
                                );
                              })}
                            </div>
                          </div>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </Table>
        </div>
      )}

      {/* Legacy Single-Camera Exports Section */}
      {!!legacyExports.data?.length && (
        <div className="flex flex-col gap-3 mt-4">
          <h2 className="text-sm font-semibold tracking-wide text-muted uppercase">
            Exportaciones individuales ({legacyExports.data.length})
          </h2>
          <Table label="Exportaciones individuales">
            <thead>
              <tr>
                <Th>Nombre</Th>
                <Th>Cámara</Th>
                <Th>Rango</Th>
                <Th>Pedida por</Th>
                <Th>Estado</Th>
                <Th className="text-right" />
              </tr>
            </thead>
            <tbody>
              {legacyExports.data.map((x) => (
                <tr key={x.id} className="border-t border-outline-variant align-top">
                  <td className="font-medium text-sm">{x.name}</td>
                  <td className="text-sm">{x.camera_name}</td>
                  <td className="font-mono text-xs whitespace-nowrap">
                    {fmtDateTime(x.start_time)}
                    <br />
                    {fmtDateTime(x.end_time)}
                  </td>
                  <td className="text-xs">
                    {x.requested_by_name}
                    <div className="text-muted">{fmtDateTime(x.created_at)}</div>
                  </td>
                  <td className="text-sm">
                    <StatusBadge
                      status={x.status}
                      tone={x.status === "ready" ? "ok" : x.status === "failed" ? "bad" : "warn"}
                      label={statusConfig[x.status]?.label ?? x.status}
                    />
                    {x.status === "running" && x.progress > 0 && (
                      <span className="ml-1 text-xs text-muted">{Math.round(x.progress)}%</span>
                    )}
                    {x.error && <div role="alert" className="max-w-56 text-xs text-bad">{x.error}</div>}
                  </td>
                  <td className="text-right whitespace-nowrap">
                    <div className="inline-flex items-center gap-2">
                      {x.status === "ready" && (
                        <LinkButton variant="filled" size="sm" href={`/media/v1/exports/${x.id}/download`}>
                          <Icon icon={Download} size="xs" /> Descargar
                        </LinkButton>
                      )}
                      <IconButton
                        icon={Trash2}
                        onClick={() => removeLegacy.mutate(x.id)}
                        aria-label="Quitar de la lista"
                        title="Quitar de la lista"
                      />
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      )}

      {playbackJob && (
        <EvidencePlayerModal job={playbackJob} onClose={() => setPlaybackJob(null)} />
      )}

      {shareJob && (
        <ShareExportModal job={shareJob} onClose={() => setShareJob(null)} />
      )}
    </div>
  );
}
