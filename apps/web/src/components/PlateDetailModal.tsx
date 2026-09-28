import { useMutation, useQuery } from "@tanstack/react-query";
import { Download } from "lucide-react";
import { useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { brandingQuery, meQuery } from "@/api/queries";
import { Button, ErrorNote } from "@/components/ui";
import { fmtWatermarkTimestamp } from "@/lib/format";
import { can } from "@/lib/perm";
import { Modal } from "./Modal";

const downloadLinkClass = "inline-flex items-center gap-2 rounded border border-line px-3 py-1.5 text-sm hover:bg-raised";

const clipJobStatusText: Record<string, string> = { queued: "En cola", running: "Generando", done: "Lista", failed: "Falló" };

/**
 * ClipWatermarkDownload (PDW-5): starts a clip watermark job (PDW-4), polls its status while
 * queued/running, and offers the download once it is done — the same queued/running/done
 * UX as Exports.tsx, scoped to this one plate read instead of a list.
 */
function ClipWatermarkDownload({ readId }: { readId: string }) {
  const [jobId, setJobId] = useState<string | null>(null);
  const create = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/api/v1/lpr/reads/{readId}/clip-watermark-jobs", { params: { path: { readId } } })),
    onSuccess: (job) => setJobId(job.id),
  });
  const job = useQuery({
    queryKey: ["clip-watermark-job", readId, jobId],
    queryFn: async () =>
      unwrap(
        await api.GET("/api/v1/lpr/reads/{readId}/clip-watermark-jobs/{jobId}", {
          params: { path: { readId, jobId: jobId as string } },
        }),
      ),
    enabled: jobId !== null,
    refetchInterval: (query) => (query.state.data && (query.state.data.status === "done" || query.state.data.status === "failed") ? false : 1500),
  });

  if (jobId === null) {
    return (
      <Button onClick={() => create.mutate()} disabled={create.isPending}>
        <Download className="size-4" aria-hidden /> Preparar clip con marca de agua
      </Button>
    );
  }

  const status = job.data?.status ?? "queued";
  return (
    <div className="flex flex-col gap-1">
      {status === "done" && (
        <a href={`/api/v1/lpr/reads/${readId}/clip-watermark-jobs/${jobId}/download`} className={downloadLinkClass}>
          <Download className="size-4" aria-hidden /> Descargar clip
        </a>
      )}
      {status !== "done" && (
        <p className="text-sm text-muted" role="status">
          {status === "failed" ? "No se pudo generar el clip." : `Preparando clip… (${clipJobStatusText[status]})`}
        </p>
      )}
      {status === "failed" && job.data?.error && <p className="text-xs text-bad">{job.data.error}</p>}
      <ErrorNote error={create.error ?? job.error} />
    </div>
  );
}

/**
 * WatermarkOverlay renders the same date/time + owner name/logo shown burned into a
 * download (PDW-3/PDW-4, Go image/draw and ffmpeg drawtext respectively) as a CSS overlay
 * over the on-screen photo/clip — no re-encode, so it costs nothing to render.
 */
function WatermarkOverlay({ tenantId, hasLogo, ownerName, seenAt }: { tenantId: string; hasLogo: boolean; ownerName: string; seenAt: string }) {
  return (
    <div className="pointer-events-none absolute top-0 left-0 flex items-center gap-1.5 bg-black/60 px-2 py-1 text-xs text-white">
      {hasLogo && <img src={`/api/v1/tenants/${tenantId}/branding/logo`} alt="" className="h-4 w-4 object-contain" />}
      <span>
        {fmtWatermarkTimestamp(seenAt)}
        {ownerName ? ` · ${ownerName}` : ""}
      </span>
    </div>
  );
}

/**
 * PlateDetailModal (PDW-2): the detection photo at maximum quality and a playable/seekable
 * clip, both proxied through the media gateway (internal/media/gateway.go
 * lprReadSnapshot/lprReadClip), each with the CSS watermark overlay above. Photo needs
 * lpr.view + snapshots.view; clip needs lpr.view + recordings.view — sections the caller
 * lacks permission for show a message instead of a broken image/player. Downloads (PDW-5)
 * need the view permission plus snapshots.download (photo, synchronous Go burn-in, PDW-3)
 * or exports.create/exports.download (clip, async ffmpeg job polled to done, PDW-4).
 */
export function PlateDetailModal({ read, onClose }: { read: Schemas["PlateRead"]; onClose: () => void }) {
  const me = useQuery(meQuery);
  const tenantId = me.data?.tenant_id ?? "";
  const branding = useQuery(brandingQuery(tenantId));
  const canViewPhoto = can(me.data, "lpr.view") && can(me.data, "snapshots.view");
  const canViewClip = can(me.data, "lpr.view") && can(me.data, "recordings.view");
  const canDownloadPhoto = canViewPhoto && can(me.data, "snapshots.download");
  const canRequestClip = canViewClip && can(me.data, "exports.create");
  const ownerName = branding.data?.owner_name ?? "";
  const hasLogo = branding.data?.has_logo ?? false;

  return (
    <Modal title={`Patente ${read.plate_normalized}`} onClose={onClose}>
      <div className="flex flex-col gap-4">
        <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-4">
          <div>
            <dt className="text-xs text-muted">Fecha</dt>
            <dd>{fmtWatermarkTimestamp(read.seen_at)}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted">Cámara</dt>
            <dd>{read.camera_name}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted">Sitio</dt>
            <dd>{read.site_name}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted">Confianza</dt>
            <dd>{read.score != null ? `${Math.round(read.score * 100)}%` : "—"}</dd>
          </div>
        </dl>

        {canViewPhoto ? (
          <div className="relative overflow-hidden rounded border border-line">
            <img
              src={`/media/v1/lpr/reads/${read.id}/snapshot.jpg`}
              alt={`Foto de la lectura de patente ${read.plate_normalized}`}
              className="w-full"
            />
            <WatermarkOverlay tenantId={tenantId} hasLogo={hasLogo} ownerName={ownerName} seenAt={read.seen_at} />
          </div>
        ) : (
          <p className="text-sm text-muted">No tenés permiso para ver la foto de esta lectura.</p>
        )}
        {canDownloadPhoto && (
          <a href={`/media/v1/lpr/reads/${read.id}/snapshot.jpg?download=1`} className={downloadLinkClass}>
            <Download className="size-4" aria-hidden /> Descargar foto con marca de agua
          </a>
        )}

        {canViewClip ? (
          <div className="relative overflow-hidden rounded border border-line">
            <video controls preload="metadata" className="w-full bg-black" src={`/media/v1/lpr/reads/${read.id}/clip.mp4`}>
              Tu navegador no puede reproducir este video.
            </video>
            <WatermarkOverlay tenantId={tenantId} hasLogo={hasLogo} ownerName={ownerName} seenAt={read.seen_at} />
          </div>
        ) : (
          <p className="text-sm text-muted">No tenés permiso para ver el clip de esta lectura.</p>
        )}
        {canRequestClip && <ClipWatermarkDownload readId={read.id} />}
      </div>
    </Modal>
  );
}
