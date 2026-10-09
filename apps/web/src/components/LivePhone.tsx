import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { ArrowLeft, ChevronDown, ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { cameraFoldersQuery, camerasQuery, meQuery, serversQuery, sitesQuery } from "@/api/queries";
import type { Schemas } from "@/api/client";
import { setContextSidebarCollapsed } from "@/components/AppShell";
import { MsePlayer } from "@/components/MsePlayer";
import { LiveModeToggle } from "@/components/LiveModeToggle";
import { LiveRecDock } from "@/components/LiveRecDock";
import { PhoneCameraSheet } from "@/components/PhoneCameraSheet";
import { RecTile, type RecTileState } from "@/components/RecTile";
import { IconButton } from "@/components/ui";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";
import { usePlayerSession } from "@/lib/live/PlayerSessionProvider";
import { can } from "@/lib/perm";
import type { RecTransport } from "@/lib/useRecPlayback";
import { useRecSession } from "@/lib/useRecSession";

type Camera = Schemas["Camera"];

/** Cameras per page: the only ones streaming at once, so a phone never decodes more than 4 videos. */
export const PHONE_PAGE_SIZE = 4;
/** Horizontal travel (px) a drag needs to count as a page swipe. */
const SWIPE_THRESHOLD = 50;

const statusDot = (status?: string) => (status === "online" ? "bg-ok" : status === "offline" ? "bg-bad" : "bg-warn");

/** Where the gateway serves the latest frame; it sits under the player so a card is never a black box. */
const snapshotUrl = (id: string) => `/media/v1/cameras/${id}/snapshot.jpg?h=360`;

/** What a card shows in GRABADO: the recorded tile of the shared transport instead of the live player. */
type RecView = { state: RecTileState; transport: RecTransport; isMaster: boolean };

function CameraCard({ camera, playing, rec, onOpen }: { camera: Camera; playing: boolean; rec?: RecView; onOpen: () => void }) {
  const tr = useT();
  return (
    <li className="min-w-0">
      <div className="relative aspect-video w-full overflow-hidden bg-video">
        <img src={snapshotUrl(camera.id)} alt="" draggable={false} className="absolute inset-0 size-full object-contain" />
        {rec ? <RecTile cameraId={camera.id} name={camera.display_name} state={rec.state} transport={rec.transport} isMaster={rec.isMaster} /> : playing && <MsePlayer persistent cameraId={camera.id} quality="sub" serverId={camera.server_id} className="absolute inset-0 size-full" />}
        <div className="pointer-events-none absolute inset-x-0 bottom-0 z-[3] flex items-center gap-1.5 p-2 text-xs">
          <span className="flex min-w-0 items-center gap-1.5 rounded-full bg-surface-1/80 px-2 py-0.5 text-on-surface backdrop-blur">
            <span className={cn("size-2 shrink-0 rounded-full", statusDot(camera.status))} aria-hidden />
            <span className="truncate font-medium">{camera.display_name}</span>
          </span>
        </div>
        <button type="button" aria-label={tr("live.phoneViewCamera", { name: camera.display_name })} onClick={onOpen} className="absolute inset-0 z-[4] min-h-11 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary" />
      </div>
    </li>
  );
}

/**
 * SingleCamera opens instantly: the sub session of the list (already playing, same persistent
 * session) is shown at once while the main session warms in a layer on top. A session's
 * `<video>` stays transparent until its decoder has a picture (PlayerSession.revealFrame), so
 * the sub picture shows through until main reveals its first frame and then main covers it.
 * The sub session stays mounted underneath so going back to the list is instant.
 */
function SingleCamera({ camera }: { camera: Camera }) {
  const main = usePlayerSession(camera.id, "main", camera.server_id);
  const stage = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = stage.current;
    if (!main || !el) return;
    main.setMuted(true);
    main.attach(el);
    return () => main.detach();
  }, [main]);
  return (
    <div className="relative aspect-video w-full overflow-hidden bg-video">
      <img src={snapshotUrl(camera.id)} alt="" draggable={false} className="absolute inset-0 size-full object-contain" />
      <MsePlayer persistent cameraId={camera.id} quality="sub" serverId={camera.server_id} className="absolute inset-0 size-full" />
      <div ref={stage} data-testid="phone-main-layer" className="absolute inset-0" />
    </div>
  );
}

/** SingleRecording is the single view in GRABADO: that camera's recording on the shared transport. */
function SingleRecording({ camera, rec }: { camera: Camera; rec: RecView }) {
  return (
    <div className="relative aspect-video w-full overflow-hidden bg-video">
      <img src={snapshotUrl(camera.id)} alt="" draggable={false} className="absolute inset-0 size-full object-contain" />
      <RecTile cameraId={camera.id} name={camera.display_name} state={rec.state} transport={rec.transport} isMaster />
    </div>
  );
}

/**
 * LivePhone is Live below the `md` breakpoint: pages of 4 camera cards (2x2, only the current
 * page streams, on the sub stream over its snapshot) and a single-camera view that swaps sub to main.
 * Leaving a page unmounts its players; usePlayerSession releases the sessions, which the
 * PlayerSessionManager keeps WARM for its TTL (30 s), so going back soon is instant.
 * The open camera lives in `?camera=<id>`, so the system back button and Maps handoffs work.
 */
export function LivePhone() {
  const tr = useT();
  const cameras = useQuery(camerasQuery({}));
  const sites = useQuery(sitesQuery);
  const servers = useQuery(serversQuery);
  const folders = useQuery(cameraFoldersQuery);
  const me = useQuery(meQuery);
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as Record<string, unknown>;
  const [siteId, setSiteId] = useState<string | null>(null);
  const [page, setPage] = useState(0);
  const [sheetOpen, setSheetOpen] = useState(false);
  const scopeButton = useRef<HTMLButtonElement>(null);
  const gesture = useRef<{ x: number; y: number } | null>(null);
  const swiped = useRef(false);

  useEffect(() => {
    setContextSidebarCollapsed(true);
    return () => setContextSidebarCollapsed(false);
  }, []);

  const siteName = useMemo(() => new Map((sites.data ?? []).map((site) => [site.id, site.name])), [sites.data]);
  const all = useMemo(
    () =>
      (cameras.data ?? [])
        .filter((camera) => camera.enabled)
        .sort((a, b) => (siteName.get(a.site_id) ?? "").localeCompare(siteName.get(b.site_id) ?? "") || a.display_name.localeCompare(b.display_name)),
    [cameras.data, siteName],
  );
  const shown = useMemo(() => (siteId ? all.filter((camera) => camera.site_id === siteId) : all), [all, siteId]);

  const pageCount = Math.max(1, Math.ceil(shown.length / PHONE_PAGE_SIZE));
  const current = Math.min(page, pageCount - 1);
  const pageCameras = useMemo(() => shown.slice(current * PHONE_PAGE_SIZE, (current + 1) * PHONE_PAGE_SIZE), [shown, current]);
  const go = (delta: number) => setPage(Math.min(pageCount - 1, Math.max(0, current + delta)));
  const pickSite = (id: string | null) => {
    setSiteId(id);
    setPage(0);
  };
  const scopeName = siteId ? (siteName.get(siteId) ?? tr("live.phoneAllSites")) : tr("live.phoneAllSites");

  const onPointerDown = (event: PointerEvent) => {
    swiped.current = false;
    gesture.current = { x: event.clientX, y: event.clientY };
  };
  const onPointerUp = (event: PointerEvent) => {
    const start = gesture.current;
    gesture.current = null;
    if (!start) return;
    const dx = event.clientX - start.x;
    const dy = event.clientY - start.y;
    if (Math.abs(dx) < SWIPE_THRESHOLD || Math.abs(dx) < Math.abs(dy) * 1.5) return;
    swiped.current = true;
    go(dx < 0 ? 1 : -1);
  };
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "ArrowRight") go(1);
    else if (event.key === "ArrowLeft") go(-1);
  };

  const requested = typeof search.camera === "string" ? search.camera : undefined;
  const selected = requested ? all.find((camera) => camera.id === requested) : undefined;

  const open = (id: string) => void navigate({ to: ".", search: ((previous: Record<string, unknown>) => ({ ...previous, camera: id })) as never });
  const close = () => void navigate({ to: ".", replace: true, search: ((previous: Record<string, unknown>) => ({ ...previous, camera: undefined })) as never });

  // GRABADO: the same session as the desktop grid (transport, coverage, master, URL time), fed with
  // the cameras on screen: the 4 of the current page, or only the open one in the single view.
  const cameraById = useMemo(() => new Map((cameras.data ?? []).map((camera) => [camera.id, camera])), [cameras.data]);
  const visibleIds = useMemo(() => (selected ? [selected.id] : pageCameras.map((camera) => camera.id)), [selected, pageCameras]);
  const session = useRecSession({ canRec: can(me.data, "recordings.view"), cameraIds: visibleIds, focusedId: selected?.id, selectedId: selected?.id, cameraById });
  const { rec, transport, recData, denied, recLimited, hasCoverage, masterId } = session;
  // Tiles that mount for a new page or the single view start from the window offset, so they
  // reopen the window at the current shared time instead of where it was first opened.
  const visibleKey = visibleIds.join("|");
  const [anchoredKey, setAnchoredKey] = useState(visibleKey);
  if (anchoredKey !== visibleKey) {
    setAnchoredKey(visibleKey);
    if (rec) transport.reanchor();
  }
  const recView = (id: string): RecView | undefined => {
    if (!rec) return undefined;
    const state: RecTileState = denied.has(id) ? "denied" : recLimited.has(id) ? "limited" : recData.loaded.includes(id) && !hasCoverage(id) ? "empty" : "player";
    return { state, transport, isMaster: id === masterId };
  };
  const modeToggle = session.canRec ? <LiveModeToggle rec={rec} onChange={session.setMode} touch /> : null;

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
      <h1 className="sr-only">{tr("live.phoneCameras")}</h1>
      <div hidden={!!selected} className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
        <div className="flex shrink-0 items-center gap-2">
          <button
            ref={scopeButton}
            type="button"
            aria-haspopup="dialog"
            aria-expanded={sheetOpen}
            aria-label={`${tr("live.phoneSites")}: ${scopeName}`}
            onClick={() => setSheetOpen(true)}
            className="m3-press inline-flex h-11 min-w-0 flex-1 items-center justify-between gap-2 rounded-full bg-surface-1 px-4 text-sm font-semibold text-on-surface focus-visible:outline-2 focus-visible:outline-primary"
          >
            <span className="truncate">{scopeName}</span>
            <ChevronDown className="size-4 shrink-0 text-on-surface-variant" aria-hidden />
          </button>
          {modeToggle}
        </div>
        <div
          data-testid="live-phone-pages"
          tabIndex={0}
          onPointerDown={onPointerDown}
          onPointerUp={onPointerUp}
          onPointerCancel={() => { gesture.current = null; }}
          onClickCapture={(event) => {
            // A swipe that ends on a card must not also open it.
            if (swiped.current) {
              swiped.current = false;
              event.stopPropagation();
              event.preventDefault();
            }
          }}
          onKeyDown={onKeyDown}
          className="flex min-h-0 flex-1 touch-pan-y flex-col overflow-y-auto outline-none focus-visible:ring-2 focus-visible:ring-primary"
        >
          <ul className="flex w-full flex-col gap-px">
            {pageCameras.map((camera) => (
              <CameraCard key={camera.id} camera={camera} playing={camera.id !== selected?.id} rec={recView(camera.id)} onOpen={() => open(camera.id)} />
            ))}
          </ul>
        </div>
        <div className="flex shrink-0 items-center justify-center gap-3">
          <IconButton icon={ChevronLeft} aria-label={tr("live.phonePrevPage")} title={tr("live.phonePrevPage")} disabled={current === 0} onClick={() => go(-1)} />
          <span data-testid="phone-page-status" aria-live="polite" className="min-w-16 text-center text-xs text-on-surface-variant">
            <span className="sr-only">{tr("live.phonePageOf", { page: current + 1, total: pageCount })}</span>
            <span aria-hidden>{current + 1} / {pageCount}</span>
          </span>
          <IconButton icon={ChevronRight} aria-label={tr("live.phoneNextPage")} title={tr("live.phoneNextPage")} disabled={current >= pageCount - 1} onClick={() => go(1)} />
        </div>
      </div>
      {selected && (
        <section data-testid="live-phone-single" aria-label={selected.display_name} className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
          <div className="flex shrink-0 items-center gap-2">
            <IconButton icon={ArrowLeft} aria-label={tr("live.phoneBack")} title={tr("live.phoneBack")} onClick={close} />
            <span className="min-w-0 flex-1 truncate text-base font-semibold">{selected.display_name}</span>
            <span className="inline-flex shrink-0 items-center gap-1.5 text-xs text-on-surface-variant" role="status">
              <span className={cn("size-2 rounded-full", statusDot(selected.status))} aria-hidden />
              {selected.status}
            </span>
            {modeToggle}
          </div>
          {rec ? <SingleRecording camera={selected} rec={recView(selected.id)!} /> : <SingleCamera camera={selected} />}
        </section>
      )}
      {rec && (
        <LiveRecDock compact transport={transport} cameras={session.timelineCameras} events={recData.events} now={session.now} selectedId={selected?.id} />
      )}
      {sheetOpen && (
        <PhoneCameraSheet
          cameras={cameras.data ?? []}
          sites={sites.data ?? []}
          servers={servers.data ?? []}
          folders={folders.data?.items ?? []}
          siteId={siteId}
          currentCameraId={selected?.id}
          returnFocusTo={scopeButton}
          onAll={() => { pickSite(null); setSheetOpen(false); }}
          onSite={(id) => { pickSite(id); setSheetOpen(false); }}
          onCamera={(id) => { open(id); setSheetOpen(false); }}
          onClose={() => setSheetOpen(false)}
        />
      )}
    </div>
  );
}
