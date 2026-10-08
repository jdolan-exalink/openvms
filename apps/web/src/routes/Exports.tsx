import { useState } from "react";
import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  Ban,
  Camera,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clock,
  Copy,
  Download,
  Film,
  HardDrive,
  Pencil,
  Play,
  RotateCw,
  Share2,
  ShieldCheck,
  Trash2,
  User,
} from "lucide-react";
import { api, unwrap } from "@/api/client";
import { exportJobsQuery, exportsQuery } from "@/api/queries";
import { EditExportModal } from "@/components/EditExportModal";
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
  Summary,
  Table,
  Th,
  type Tone,
} from "@/components/ui";
import { fmtBytes, fmtDateTime, fmtDuration } from "@/lib/format";

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
  const [editJob, setEditJob] = useState<any | null>(null);
  const [copiedHash, setCopiedHash] = useState<string | null>(null);

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

  const copyHash = (hash: string, id: string) => {
    navigator.clipboard.writeText(hash);
    setCopiedHash(id);
    setTimeout(() => setCopiedHash(null), 2000);
  };

  const hasJobs = (jobs.data && jobs.data.length > 0) || (legacyExports.data && legacyExports.data.length > 0);
  const error = jobs.error ?? legacyExports.error ?? deleteJob.error ?? cancelJob.error ?? retryJob.error;

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader title={t("nav.exports")} description={t("settings.exports")} />
      <ErrorNote error={error} />

      {!hasJobs && !jobs.isLoading && <Empty>Todavía no hay exportaciones registradas.</Empty>}

      {/* Multi-camera Orchestrated Export Jobs */}
      {!!jobs.data?.length && (
        <div className="flex flex-col gap-4">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-semibold tracking-wide text-muted uppercase">
              Trabajos de exportación y evidencia ({jobs.data.length})
            </h2>
            <Summary>{jobs.data.length === 1 ? "1 trabajo" : `${jobs.data.length} trabajos`}</Summary>
          </div>

          <div className="flex flex-col gap-4">
            {jobs.data.map((job) => {
              const isExpanded = !!expandedJobs[job.id];
              const inProgress = ["queued", "preparing", "transferring", "processing"].includes(job.status);
              const cfg = statusConfig[job.status] ?? { label: job.status, tone: "neutral" as Tone };

              return (
                <div
                  key={job.id}
                  className="flex flex-col gap-3 rounded-m3-xl bg-surface-1 border border-outline-variant/60 p-4 sm:p-5 shadow-sm hover:border-outline/80 transition-all"
                >
                  {/* Top Header: Title, Badges & Action Toolbar */}
                  <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                    {/* Title & Status Badges */}
                    <div className="flex flex-wrap items-center gap-2.5">
                      <div className="flex items-center gap-2">
                        <span className="text-base font-semibold text-on-surface tracking-tight">
                          {job.name}
                        </span>
                        <StatusBadge status={job.status} tone={cfg.tone} label={cfg.label} />
                      </div>

                      {job.protected ? (
                        <span className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-semibold bg-primary/10 text-primary border border-primary/20">
                          <Icon icon={ShieldCheck} size="xs" />
                          <span>Evidencia protegida</span>
                        </span>
                      ) : job.expires_at ? (
                        <span
                          className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-medium bg-warn-container text-on-warn-container border border-warn/30"
                          title={`Se eliminará automáticamente el ${fmtDateTime(job.expires_at)}`}
                        >
                          <Icon icon={Clock} size="xs" />
                          <span>
                            Auto-borrado en{" "}
                            {Math.max(
                              0,
                              Math.ceil(
                                (new Date(job.expires_at).getTime() - Date.now()) / (1000 * 60 * 60 * 24)
                              )
                            )}{" "}
                            días
                          </span>
                        </span>
                      ) : null}

                      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-m3-sm text-xs font-medium bg-surface-2 text-on-surface-variant">
                        <Icon icon={Camera} size="xs" />
                        <span>{job.camera_count} {job.camera_count === 1 ? "cámara" : "cámaras"}</span>
                      </span>

                      {job.status === "ready" && job.total_bytes > 0 && (
                        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-m3-sm text-xs font-mono font-medium bg-surface-2 text-on-surface-variant">
                          <Icon icon={HardDrive} size="xs" />
                          <span>{fmtBytes(job.total_bytes)}</span>
                        </span>
                      )}
                    </div>

                    {/* Action Toolbar with generous spacing */}
                    <div className="flex items-center gap-2 self-end sm:self-auto flex-wrap">
                      <Button
                        variant="outlined"
                        size="sm"
                        onClick={() => setEditJob(job)}
                        title="Editar nombre y proteger evidencia forense"
                      >
                        <Icon icon={Pencil} size="xs" />
                        <span>Editar</span>
                      </Button>

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
                            title="Compartir enlace seguro de evidencia"
                          >
                            <Icon icon={Share2} size="xs" />
                            <span>Compartir</span>
                          </Button>

                          <LinkButton
                            variant="outlined"
                            size="sm"
                            href={`/media/v1/export-jobs/${job.id}/download`}
                            title={job.camera_count > 1 ? "Descargar paquete ZIP con manifest" : "Descargar video MP4"}
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
                          title="Cancelar trabajo de exportación"
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

                  {/* Metadata Facts Strip: Video range, Requester & Progress */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-3 p-3 bg-surface-2/60 rounded-m3-md border border-outline-variant/40 text-xs">
                    {/* Fact 1: Video Range & Duration */}
                    <div className="flex flex-col gap-0.5">
                      <span className="text-[11px] font-semibold text-muted uppercase tracking-wider flex items-center gap-1">
                        <Icon icon={Film} size="xs" />
                        <span>Rango de video</span>
                      </span>
                      <div className="font-mono text-on-surface text-xs leading-relaxed">
                        <span>{fmtDateTime(job.start_time)}</span>
                        <span className="text-muted mx-1.5">➔</span>
                        <span>{fmtDateTime(job.end_time)}</span>
                      </div>
                      <span className="text-[11px] text-muted">
                        Duración: {fmtDuration(job.start_time, job.end_time)}
                      </span>
                    </div>

                    {/* Fact 2: Requester & Creation date */}
                    <div className="flex flex-col gap-0.5">
                      <span className="text-[11px] font-semibold text-muted uppercase tracking-wider flex items-center gap-1">
                        <Icon icon={User} size="xs" />
                        <span>Solicitud</span>
                      </span>
                      <span className="font-medium text-on-surface">
                        {job.requested_by_name}
                      </span>
                      <span className="text-[11px] text-muted flex items-center gap-1">
                        <Icon icon={Clock} size="xs" />
                        <span>Creado el {fmtDateTime(job.created_at)}</span>
                      </span>
                    </div>

                    {/* Fact 3: Progress bar / Forensic integrity */}
                    <div className="flex flex-col gap-1">
                      <span className="text-[11px] font-semibold text-muted uppercase tracking-wider">
                        {inProgress ? "Transferencia en progreso" : "Integridad"}
                      </span>

                      {inProgress ? (
                        <div className="flex flex-col gap-1 w-full">
                          <div className="flex items-center justify-between text-[11px] font-mono">
                            <span className="font-semibold text-primary">{Math.round(job.progress)}%</span>
                            <span className="text-muted">{fmtBytes(job.transferred_bytes)}</span>
                          </div>
                          <div className="h-2 w-full bg-surface-3 rounded-full overflow-hidden">
                            <div
                              className="h-full bg-primary rounded-full transition-all duration-300"
                              style={{ width: `${Math.min(100, Math.max(5, job.progress))}%` }}
                            />
                          </div>
                          <div className="flex items-center justify-between text-[10px] text-muted font-mono">
                            <span>{job.speed_bps > 0 ? fmtSpeed(job.speed_bps) : "Iniciando..."}</span>
                            <span>{job.eta_seconds > 0 ? fmtETA(job.eta_seconds) : ""}</span>
                          </div>
                        </div>
                      ) : job.status === "ready" ? (
                        <div className="flex items-center gap-1.5 text-ok text-xs">
                          <Icon icon={CheckCircle2} size="xs" />
                          <span>Paquete generado y verificado con SHA-256</span>
                        </div>
                      ) : job.error ? (
                        <div role="alert" className="text-xs text-bad flex items-start gap-1 font-mono">
                          <Icon icon={AlertCircle} size="xs" />
                          <span>{job.error}</span>
                        </div>
                      ) : (
                        <span className="text-muted text-xs">—</span>
                      )}
                    </div>
                  </div>

                  {/* Expand / Collapse Camera Clips Breakdown */}
                  {job.items && job.items.length > 0 && (
                    <div className="flex flex-col gap-2 pt-0.5">
                      <button
                        type="button"
                        onClick={() => toggleExpand(job.id)}
                        className="self-start inline-flex items-center gap-1.5 text-xs font-medium text-on-surface-variant hover:text-primary transition-colors py-1 px-2 rounded-m3-sm hover:bg-surface-2"
                      >
                        <Icon icon={isExpanded ? ChevronUp : ChevronDown} size="xs" />
                        <span>
                          {isExpanded
                            ? "Ocultar detalle de cámaras"
                            : `Ver ${job.items.length} ${job.items.length === 1 ? "cámara" : "cámaras"} y hashes SHA-256`}
                        </span>
                      </button>

                      {isExpanded && (
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-2.5 pt-2 border-t border-outline-variant/40">
                          {job.items.map((it) => {
                            const itemCfg = statusConfig[it.status] ?? { label: it.status, tone: "neutral" as Tone };
                            const isCopied = copiedHash === it.id;

                            return (
                              <div
                                key={it.id}
                                className="flex flex-col gap-2 p-3 bg-surface-2/40 rounded-m3-md border border-outline-variant/40 text-xs"
                              >
                                <div className="flex items-center justify-between gap-2">
                                  <div className="flex items-center gap-2">
                                    <Icon icon={Camera} size="xs" className="text-muted" />
                                    <span className="font-semibold text-on-surface">{it.camera_name}</span>
                                    {it.server_name && (
                                      <span className="text-[11px] text-muted font-mono">({it.server_name})</span>
                                    )}
                                  </div>

                                  <div className="flex items-center gap-2">
                                    {it.total_bytes > 0 && (
                                      <span className="text-muted font-mono">{fmtBytes(it.total_bytes)}</span>
                                    )}
                                    <StatusBadge status={it.status} tone={itemCfg.tone} label={itemCfg.label} />
                                  </div>
                                </div>

                                {it.sha256_hash && (
                                  <div className="flex items-center justify-between gap-2 pt-1 border-t border-outline-variant/20 font-mono text-[11px]">
                                    <span className="text-muted truncate">
                                      SHA-256: {it.sha256_hash.slice(0, 16)}...{it.sha256_hash.slice(-8)}
                                    </span>
                                    <button
                                      type="button"
                                      onClick={() => copyHash(it.sha256_hash!, it.id)}
                                      className="inline-flex items-center gap-1 text-primary hover:underline text-[11px] whitespace-nowrap"
                                      title="Copiar hash SHA-256 completo"
                                    >
                                      <Icon icon={isCopied ? Check : Copy} size="xs" />
                                      <span>{isCopied ? "Copiado" : "Copiar"}</span>
                                    </button>
                                  </div>
                                )}

                                {it.status === "ready" && (
                                  <div className="flex items-center justify-end gap-2 pt-1">
                                    <Button
                                      variant="filled"
                                      size="sm"
                                      onClick={() => {
                                        setPlaybackJob({
                                          ...job,
                                          name: `${job.name} · ${it.camera_name}`,
                                          items: [it],
                                        });
                                      }}
                                      title={`Reproducir evidencia de ${it.camera_name}`}
                                    >
                                      <Icon icon={Play} size="xs" />
                                      <span>Reproducir</span>
                                    </Button>

                                    <LinkButton
                                      variant="outlined"
                                      size="sm"
                                      href={`/media/v1/export-jobs/${job.id}/items/${it.id}/download`}
                                      title={`Descargar clip de ${it.camera_name}`}
                                    >
                                      <Icon icon={Download} size="xs" />
                                      <span>Descargar clip MP4</span>
                                    </LinkButton>
                                  </div>
                                )}

                                {it.error && (
                                  <div className="text-[11px] text-bad flex items-center gap-1 font-mono">
                                    <Icon icon={AlertCircle} size="xs" />
                                    <span>Error: {it.error}</span>
                                  </div>
                                )}
                              </div>
                            );
                          })}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* Legacy Single-Camera Exports Section */}
      {!!legacyExports.data?.length && (
        <div className="flex flex-col gap-3 mt-4">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-semibold tracking-wide text-muted uppercase">
              Exportaciones individuales ({legacyExports.data.length})
            </h2>
            <Summary>{legacyExports.data.length === 1 ? "1 exportación" : `${legacyExports.data.length} exportaciones`}</Summary>
          </div>

          <Table label="Exportaciones individuales">
            <thead>
              <tr>
                <Th>Nombre</Th>
                <Th>Cámara</Th>
                <Th>Rango de video</Th>
                <Th>Pedida por</Th>
                <Th>Estado</Th>
                <Th className="text-right">Acciones</Th>
              </tr>
            </thead>
            <tbody>
              {legacyExports.data.map((x) => (
                <tr key={x.id} className="border-t border-outline-variant align-middle">
                  <td className="font-semibold text-sm">{x.name}</td>
                  <td className="text-sm font-medium">{x.camera_name}</td>
                  <td className="font-mono text-xs whitespace-nowrap">
                    <div>{fmtDateTime(x.start_time)}</div>
                    <div className="text-muted">{fmtDateTime(x.end_time)}</div>
                  </td>
                  <td className="text-xs">
                    <span className="font-medium">{x.requested_by_name}</span>
                    <div className="text-muted">{fmtDateTime(x.created_at)}</div>
                  </td>
                  <td className="text-sm">
                    <div className="flex items-center gap-2">
                      <StatusBadge
                        status={x.status}
                        tone={x.status === "ready" ? "ok" : x.status === "failed" ? "bad" : "warn"}
                        label={statusConfig[x.status]?.label ?? x.status}
                      />
                      {x.status === "running" && x.progress > 0 && (
                        <span className="font-mono text-xs text-muted">{Math.round(x.progress)}%</span>
                      )}
                    </div>
                    {x.error && <div role="alert" className="max-w-56 text-xs text-bad mt-1 font-mono">{x.error}</div>}
                  </td>
                  <td className="text-right whitespace-nowrap">
                    <div className="inline-flex items-center gap-2">
                      {x.status === "ready" && (
                        <LinkButton variant="filled" size="sm" href={`/media/v1/exports/${x.id}/download`}>
                          <Icon icon={Download} size="xs" />
                          <span>Descargar</span>
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

      {editJob && (
        <EditExportModal job={editJob} onClose={() => setEditJob(null)} />
      )}
    </div>
  );
}
