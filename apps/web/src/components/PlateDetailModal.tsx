import { useMutation, useQuery } from "@tanstack/react-query";
import { Download } from "lucide-react";
import { type KeyboardEvent, useRef, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { brandingQuery, meQuery } from "@/api/queries";
import { ArPlate } from "@/components/plates/ArPlate";
import { Button, ErrorNote } from "@/components/ui";
import { VehicleFacts } from "@/components/VehicleMark";
import { DEFAULT_WATERMARK_TIMEZONE, fmtWatermarkTimestamp } from "@/lib/format";
import { can } from "@/lib/perm";
import { useT, type MessageKey } from "@/i18n";
import { Modal } from "./Modal";

const downloadLinkClass =
  "m3-press inline-flex h-11 items-center justify-center gap-2 rounded-full border border-outline px-5 text-sm font-bold hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary";

type DetailTab = "photo" | "clip";
const TABS: { id: DetailTab; label: MessageKey }[] = [
  { id: "photo", label: "plates.photo" },
  { id: "clip", label: "plates.clip" },
];

const clipStatusKey: Record<string, MessageKey> = {
  queued: "plates.clipQueued",
  running: "plates.clipRunning",
  done: "plates.clipDone",
  failed: "plates.clipFailed",
};

/**
 * ClipWatermarkDownload (PDW-5): starts a clip watermark job (PDW-4), polls its status while
 * queued/running, and offers the download once it is done — the same queued/running/done
 * UX as Exports.tsx, scoped to this one plate read instead of a list.
 */
function ClipWatermarkDownload({ readId }: { readId: string }) {
  const t = useT();
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
      <Button variant="tonal" onClick={() => create.mutate()} disabled={create.isPending}>
        <Download className="size-4" aria-hidden /> {t("plates.downloadClip")}
      </Button>
    );
  }

  const status = job.data?.status ?? "queued";
  return (
    <div className="flex flex-col gap-1">
      {status === "done" && (
        <a href={`/api/v1/lpr/reads/${readId}/clip-watermark-jobs/${jobId}/download`} className={downloadLinkClass}>
          <Download className="size-4" aria-hidden /> {t("plates.downloadClipReady")}
        </a>
      )}
      {status !== "done" && (
        <p className="text-sm text-muted" role="status">
          {status === "failed" ? t("plates.clipFailedLong") : t("plates.clipPreparing", { status: t(clipStatusKey[status] ?? "plates.clipQueued") })}
        </p>
      )}
      {status === "failed" && job.data?.error && <p role="alert" className="text-xs text-bad">{job.data.error}</p>}
      <ErrorNote error={create.error ?? job.error} />
    </div>
  );
}

/**
 * WatermarkOverlay renders the same date/time + owner name/logo shown burned into a
 * download (PDW-3/PDW-4, Go image/draw and ffmpeg drawtext respectively) as a CSS overlay
 * over the on-screen photo/clip — no re-encode, so it costs nothing to render. timezone
 * (PDW-7) must be the tenant's configured branding.timezone (already defaulted server-side to
 * DefaultTimezone), so the on-screen text matches the burned-in file exactly.
 */
function WatermarkOverlay({
  tenantId,
  hasLogo,
  ownerName,
  seenAt,
  timezone,
}: {
  tenantId: string;
  hasLogo: boolean;
  ownerName: string;
  seenAt: string;
  timezone: string;
}) {
  return (
    <div className="pointer-events-none absolute top-0 left-0 flex items-center gap-1.5 bg-black/60 px-2 py-1 text-xs text-white">
      {hasLogo && <img src={`/api/v1/tenants/${tenantId}/branding/logo`} alt="" className="h-4 w-4 object-contain" />}
      <span>
        {fmtWatermarkTimestamp(seenAt, timezone)}
        {ownerName ? ` · ${ownerName}` : ""}
      </span>
    </div>
  );
}

function tabId(id: DetailTab) {
  return `plate-detail-tab-${id}`;
}
function panelId(id: DetailTab) {
  return `plate-detail-panel-${id}`;
}

/**
 * PlateDetailModal (PDW-2, tabs added in PDW-8): the detection photo at maximum quality and a
 * playable/seekable clip, both proxied through the media gateway (internal/media/gateway.go
 * lprReadSnapshot/lprReadClip), each with its own CSS watermark overlay and download button,
 * shown one at a time behind a "Foto"/"Clip" tablist (photo selected by default). The <video>
 * element is only ever rendered while the Clip tab is the active one — mounting it eagerly
 * would make the browser start fetching/buffering the clip (even with preload="metadata") the
 * instant the modal opens, before the user asked for it. Photo needs lpr.view +
 * snapshots.view; clip needs lpr.view + recordings.view — a tab the caller lacks permission
 * for shows a message instead of a broken image/player, but the tab itself stays reachable
 * (consistent with PDW-2's original per-section behavior, just relocated into the panel).
 * Downloads (PDW-5) need the view permission plus snapshots.download (photo, synchronous Go
 * burn-in, PDW-3) or exports.create/exports.download (clip, async ffmpeg job polled to done,
 * PDW-4).
 */
export function PlateDetailModal({ read, onClose }: { read: Schemas["PlateRead"]; onClose: () => void }) {
  const tr = useT();
  const me = useQuery(meQuery);
  const tenantId = me.data?.tenant_id ?? "";
  const branding = useQuery(brandingQuery(tenantId));
  const canViewPhoto = can(me.data, "lpr.view") && can(me.data, "snapshots.view");
  const canViewClip = can(me.data, "lpr.view") && can(me.data, "recordings.view");
  const canDownloadPhoto = canViewPhoto && can(me.data, "snapshots.download");
  const canRequestClip = canViewClip && can(me.data, "exports.create");
  const ownerName = branding.data?.owner_name ?? "";
  const hasLogo = branding.data?.has_logo ?? false;
  // PDW-7: the tenant's configured watermark time zone; the API already defaults it
  // server-side (branding.DefaultTimezone) once a row exists, but before brandingQuery has
  // loaded (or for a tenant with no row at all) this local fallback keeps the overlay from
  // ever rendering with an undefined time zone. seenAt is always read.seen_at — the same
  // value the backend uses (internal/media/gateway.go's lr.SeenAt, the lpr_reads.seen_at
  // column) for both the photo burn-in and the clip job's frozen watermark text, so the
  // on-screen overlay and the downloaded files always show identical local date/time (PDW-8's
  // own explicit check, confirming PDW-7 actually fixed the "watermark shows UTC" report).
  const timezone = branding.data?.timezone ?? DEFAULT_WATERMARK_TIMEZONE;

  const [tab, setTab] = useState<DetailTab>("photo");
  const tabRefs = useRef<(HTMLButtonElement | null)[]>([]);

  const onTabKeyDown = (e: KeyboardEvent<HTMLButtonElement>, index: number) => {
    if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
    e.preventDefault();
    const dir = e.key === "ArrowRight" ? 1 : -1;
    const next = (index + dir + TABS.length) % TABS.length;
    const nextTab = TABS[next];
    if (!nextTab) return;
    setTab(nextTab.id);
    tabRefs.current[next]?.focus();
  };

  return (
    <Modal title={tr("plates.detailTitle", { plate: read.plate_normalized })} onClose={onClose}>
      <div className="flex flex-col gap-4">
        <ArPlate plate={read.plate_normalized || read.plate} large />
        <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-4">
          <div>
            <dt className="text-xs text-muted">{tr("plates.date")}</dt>
            <dd>{fmtWatermarkTimestamp(read.seen_at, timezone)}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted">{tr("common.camera")}</dt>
            <dd>{read.camera_name}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted">{tr("common.site")}</dt>
            <dd>{read.site_name}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted">{tr("plates.confidence")}</dt>
            <dd>{read.score != null ? `${Math.round(read.score * 100)}%` : "—"}</dd>
          </div>
          <div className="col-span-2">
            <dt className="text-xs text-muted">{tr("plates.vehicle")}</dt>
            <dd className="mt-1"><VehicleFacts labels={read.label ? [read.label] : []} vehicle={read.vehicle} serverName={read.server_name} /></dd>
          </div>
        </dl>

        <div role="tablist" aria-label={tr("plates.tabs")} className="flex gap-1 border-b border-outline-variant">
          {TABS.map((tabItem, index) => (
            <button
              key={tabItem.id}
              ref={(el) => {
                tabRefs.current[index] = el;
              }}
              type="button"
              role="tab"
              id={tabId(tabItem.id)}
              aria-controls={panelId(tabItem.id)}
              aria-selected={tab === tabItem.id}
              tabIndex={tab === tabItem.id ? 0 : -1}
              onClick={() => setTab(tabItem.id)}
              onKeyDown={(e) => onTabKeyDown(e, index)}
              className={`-mb-px min-h-11 border-b-2 px-4 text-sm font-bold focus-visible:outline-2 focus-visible:outline-primary ${
                tab === tabItem.id ? "border-primary text-primary" : "border-transparent text-on-surface-variant hover:text-on-surface"
              }`}
            >
              {tr(tabItem.label)}
            </button>
          ))}
        </div>

        {tab === "photo" && (
          <div id={panelId("photo")} role="tabpanel" aria-labelledby={tabId("photo")} tabIndex={0} className="flex flex-col gap-4">
            {canViewPhoto ? (
              <div className="relative overflow-hidden rounded-m3-lg">
                <img
                  src={`/media/v1/lpr/reads/${read.id}/snapshot.jpg?quality=100`}
                  alt={tr("plates.photoAlt", { plate: read.plate_normalized })}
                  className="max-h-[70vh] w-full bg-video object-contain"
                />
                <WatermarkOverlay tenantId={tenantId} hasLogo={hasLogo} ownerName={ownerName} seenAt={read.seen_at} timezone={timezone} />
              </div>
            ) : (
              <p className="text-sm text-muted">{tr("plates.noPhoto")}</p>
            )}
            {canDownloadPhoto && (
              <a href={`/media/v1/lpr/reads/${read.id}/snapshot.jpg?quality=100&download=1`} className={downloadLinkClass}>
                <Download className="size-4" aria-hidden /> {tr("plates.downloadPhoto")}
              </a>
            )}
          </div>
        )}

        {tab === "clip" && (
          <div id={panelId("clip")} role="tabpanel" aria-labelledby={tabId("clip")} tabIndex={0} className="flex flex-col gap-4">
            {canViewClip ? (
              <div className="relative overflow-hidden rounded-m3-lg">
                <video controls preload="metadata" className="max-h-[70vh] w-full bg-video" src={`/media/v1/lpr/reads/${read.id}/clip.mp4`}>
                  {tr("plates.clipUnsupported")}
                </video>
                <WatermarkOverlay tenantId={tenantId} hasLogo={hasLogo} ownerName={ownerName} seenAt={read.seen_at} timezone={timezone} />
              </div>
            ) : (
              <p className="text-sm text-muted">{tr("plates.noClip")}</p>
            )}
            {canRequestClip && <ClipWatermarkDownload readId={read.id} />}
          </div>
        )}
      </div>
    </Modal>
  );
}
