import { useQuery } from "@tanstack/react-query";
import type { Schemas } from "@/api/client";
import { brandingQuery, meQuery } from "@/api/queries";
import { fmtWatermarkTimestamp } from "@/lib/format";
import { can } from "@/lib/perm";
import { Modal } from "./Modal";

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
 * lacks permission for show a message instead of a broken image/player. Download buttons
 * (photo burn-in, clip watermark job) are wired in PDW-3/PDW-4/PDW-5.
 */
export function PlateDetailModal({ read, onClose }: { read: Schemas["PlateRead"]; onClose: () => void }) {
  const me = useQuery(meQuery);
  const tenantId = me.data?.tenant_id ?? "";
  const branding = useQuery(brandingQuery(tenantId));
  const canViewPhoto = can(me.data, "lpr.view") && can(me.data, "snapshots.view");
  const canViewClip = can(me.data, "lpr.view") && can(me.data, "recordings.view");
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
      </div>
    </Modal>
  );
}
