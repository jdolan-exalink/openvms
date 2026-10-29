import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { ArrowLeft, ChevronDown, ChevronLeft, ChevronRight, Maximize, Minimize, SlidersHorizontal } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { cameraFoldersQuery, camerasQuery, meQuery, serversQuery, sitesQuery } from "@/api/queries";
import type { Schemas } from "@/api/client";
import { setContextSidebarCollapsed } from "@/components/AppShell";
import { MsePlayer } from "@/components/MsePlayer";
import { LiveModeToggle } from "@/components/LiveModeToggle";
import { LiveRecDock } from "@/components/LiveRecDock";
import { PhoneCameraSheet } from "@/components/PhoneCameraSheet";
import { PhoneFocus } from "@/components/PhoneFocus";
import { PhoneGrid } from "@/components/PhoneGrid";
import { PhoneLayoutToggle } from "@/components/PhoneLayoutToggle";
import { SingleCamera, stageClass } from "@/components/PhoneSingleCamera";
import { RecTile, type RecTileState } from "@/components/RecTile";
import { IconButton } from "@/components/ui";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";
import type { DigitalZoom } from "@/lib/live/digitalZoom";
import { can } from "@/lib/perm";
import { loadPhoneLayout, phoneLayoutKey, savePhoneLayout, type PhoneLayout } from "@/lib/phoneLayoutPrefs";
import { snapshotUrl, statusDot } from "@/lib/phoneSnapshot";
import { isHorizontalSwipe, useHorizontalSwipe } from "@/lib/useHorizontalSwipe";
import type { RecTransport } from "@/lib/useRecPlayback";
import { useImmersive, useIsLandscape } from "@/lib/useImmersive";
import { useRecSession } from "@/lib/useRecSession";
import { isRecordableCamera } from "@/lib/recordings";
import { parseRecSearch } from "@/lib/liveRec";

type Camera = Schemas["Camera"];

/** Cameras per page: the only ones streaming at once, so a phone never decodes more than 4 videos. */
export const PHONE_PAGE_SIZE = 4;
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

/** SingleRecording is the single view in GRABADO: that camera's recording on the shared transport. */
function SingleRecording({ camera, rec, fill }: { camera: Camera; rec: RecView; fill: boolean }) {
  return (
    <div data-testid="phone-stage" className={stageClass(fill)}>
      <img src={snapshotUrl(camera.id)} alt="" draggable={false} className="absolute inset-0 size-full object-contain" />
      <RecTile cameraId={camera.id} name={camera.display_name} state={rec.state} transport={rec.transport} isMaster />
    </div>
  );
}

/**
 * LivePhone is Live on phones. Three layouts, remembered per user (default grid):
 * - grid: every camera in scope as a snapshot tile, refreshed only while on screen; no live stream.
 * - focus: one large live camera and a strip of snapshots to switch it; one live stream.
 * - list: pages of 4 camera cards (only the current page streams, on the sub stream over its snapshot).
 * GRABADO always uses the list's recorded tiles. A tap opens the single-camera view (sub swaps to main),
 * where a one-finger swipe moves to the next/previous camera in scope.
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
  const { rec: urlRec } = parseRecSearch(search);
  const isRec = Boolean(urlRec && can(me.data, "recordings.view"));
  const [siteId, setSiteId] = useState<string | null>(null);
  const [page, setPage] = useState(0);
  const [sheetOpen, setSheetOpen] = useState(false);
  const scopeButton = useRef<HTMLButtonElement>(null);
  const gesture = useRef<{ x: number; y: number } | null>(null);
  const swiped = useRef(false);
  const singleRef = useRef<HTMLElement>(null);
  const zoomScale = useRef(1);
  const [focusId, setFocusId] = useState<string | undefined>();
  const [recControls, setRecControls] = useState(false);

  useEffect(() => {
    setContextSidebarCollapsed(true);
    return () => setContextSidebarCollapsed(false);
  }, []);

  const siteName = useMemo(() => new Map((sites.data ?? []).map((site) => [site.id, site.name])), [sites.data]);
  const all = useMemo(
    () =>
      (cameras.data ?? [])
        .filter((camera) => camera.enabled && (!isRec || isRecordableCamera(camera)))
        .sort((a, b) => (siteName.get(a.site_id) ?? "").localeCompare(siteName.get(b.site_id) ?? "") || a.display_name.localeCompare(b.display_name)),
    [cameras.data, siteName, isRec],
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
  // Quick site switcher: only sites that have cameras, in name order.
  const siteChips = useMemo(() => {
    const present = new Set(all.map((camera) => camera.site_id));
    return (sites.data ?? []).filter((site) => present.has(site.id)).sort((a, b) => a.name.localeCompare(b.name)).map((site) => ({ id: site.id as string | null, name: site.name }));
  }, [all, sites.data]);
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
    if (!isHorizontalSwipe(dx, dy)) return;
    swiped.current = true;
    go(dx < 0 ? 1 : -1);
  };
  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "ArrowRight") go(1);
    else if (event.key === "ArrowLeft") go(-1);
  };

  // The layout is saved per tenant and user, so it is only known once /me answers.
  const prefsKey = me.data ? phoneLayoutKey(me.data.tenant_id ?? null, me.data.id) : null;
  const [saved, setSaved] = useState<{ key: string | null; layout: PhoneLayout }>({ key: null, layout: "grid" });
  if (prefsKey && saved.key !== prefsKey) setSaved({ key: prefsKey, layout: loadPhoneLayout(prefsKey) });
  const chooseLayout = (layout: PhoneLayout) => {
    setSaved({ key: prefsKey, layout });
    if (prefsKey) savePhoneLayout(prefsKey, layout);
  };

  const requested = typeof search.camera === "string" ? search.camera : undefined;
  const selected = requested ? all.find((camera) => camera.id === requested) : undefined;

  // Immersive = fullscreen toggle, or landscape. It covers the app chrome (bottom bar, headers).
  const landscape = useIsLandscape();
  const { immersive, toggle: toggleImmersive } = useImmersive(singleRef, !!selected);
  const fill = !!selected && (immersive || landscape);

  const open = (id: string) => void navigate({ to: ".", search: ((previous: Record<string, unknown>) => ({ ...previous, camera: id })) as never });
  const close = () => void navigate({ to: ".", replace: true, search: ((previous: Record<string, unknown>) => ({ ...previous, camera: undefined })) as never });

  // Swiping the single view walks the cameras in scope in grid order (the whole list when the camera is outside it).
  // It replaces the history entry so back still returns to the multi view. A zoomed picture pans instead.
  const walk = selected && shown.some((camera) => camera.id === selected.id) ? shown : all;
  const swapCamera = (direction: 1 | -1) => {
    const next = selected ? walk[walk.findIndex((camera) => camera.id === selected.id) + direction] : undefined;
    if (next) void navigate({ to: ".", replace: true, search: ((previous: Record<string, unknown>) => ({ ...previous, camera: next.id })) as never });
  };
  const swipeNav = useHorizontalSwipe(swapCamera, () => zoomScale.current > 1);
  const trackZoom = (zoom: DigitalZoom) => {
    zoomScale.current = zoom.scale;
  };

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
  // GRABADO keeps the list's recorded tiles whatever the saved layout; the layout comes back with LIVE.
  const layout = rec ? "list" : saved.layout;
  const ready = !me.isPending;
  const immersiveRec = fill && rec;
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
          {!rec && ready && <PhoneLayoutToggle value={saved.layout} onChange={chooseLayout} />}
          {modeToggle}
        </div>
        {!rec && ready && saved.layout !== "list" && sites.data && siteChips.length > 1 && (
          <div role="group" aria-label={tr("live.phoneSiteChips")} className="flex shrink-0 gap-1.5 overflow-x-auto pb-0.5">
            {[{ id: null, name: tr("live.phoneAllSites") }, ...siteChips].map((chip) => (
              <button
                key={chip.id ?? "all"}
                type="button"
                aria-pressed={siteId === chip.id}
                onClick={() => pickSite(chip.id)}
                className={cn(
                  "m3-press inline-flex h-9 shrink-0 items-center rounded-full px-3.5 text-sm font-medium focus-visible:outline-2 focus-visible:outline-primary",
                  siteId === chip.id ? "bg-primary text-on-primary" : "bg-surface-1 text-on-surface",
                )}
              >
                {chip.name}
              </button>
            ))}
          </div>
        )}
        {ready && layout === "grid" && <PhoneGrid cameras={shown} landscape={landscape} onOpen={open} />}
        {ready && layout === "focus" && !selected && <PhoneFocus cameras={shown} activeId={focusId} landscape={landscape} onSelect={setFocusId} onOpen={open} />}
        {ready && layout === "list" && (<>
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
        </>)}
      </div>
      {selected && (
        <section
          ref={singleRef}
          data-testid="live-phone-single"
          aria-label={selected.display_name}
          className={cn(
            "flex min-h-0 min-w-0 flex-1 flex-col gap-2",
            // Covers the shell and keeps the picture clear of the notch, side insets and home indicator.
            fill && "fixed inset-0 z-[60] gap-0 bg-black pt-[env(safe-area-inset-top)] pr-[env(safe-area-inset-right)] pb-[env(safe-area-inset-bottom)] pl-[env(safe-area-inset-left)]",
          )}
        >
          <div className={cn(fill ? "relative min-h-0 flex-1" : "contents")}>
            <div
              className={cn(
                "flex shrink-0 items-center gap-2",
                fill && "absolute inset-x-0 top-0 z-10 bg-gradient-to-b from-black/70 to-transparent px-2 py-1 text-white",
              )}
            >
              <IconButton icon={ArrowLeft} aria-label={tr("live.phoneBack")} title={tr("live.phoneBack")} onClick={close} />
              <span className="min-w-0 flex-1 truncate text-base font-semibold">{selected.display_name}</span>
              <span className={cn("inline-flex shrink-0 items-center gap-1.5 text-xs", fill ? "text-white/80" : "text-on-surface-variant")} role="status">
                <span className={cn("size-2 rounded-full", statusDot(selected.status))} aria-hidden />
                {selected.status}
              </span>
              {modeToggle}
              {immersiveRec && (
                <IconButton
                  icon={SlidersHorizontal}
                  aria-label={recControls ? tr("live.phoneHideRecControls") : tr("live.phoneRecControls")}
                  title={recControls ? tr("live.phoneHideRecControls") : tr("live.phoneRecControls")}
                  aria-pressed={recControls}
                  onClick={() => setRecControls((open) => !open)}
                />
              )}
              <IconButton
                icon={immersive ? Minimize : Maximize}
                aria-label={immersive ? tr("live.phoneExitFullscreen") : tr("live.phoneFullscreen")}
                title={immersive ? tr("live.phoneExitFullscreen") : tr("live.phoneFullscreen")}
                aria-pressed={immersive}
                onClick={toggleImmersive}
              />
            </div>
            {/* display: contents keeps the picture box layout while the wrapper listens for swipes bubbling from it. */}
            <div className="contents" {...swipeNav}>
              {rec ? <SingleRecording camera={selected} rec={recView(selected.id)!} fill={fill} /> : <SingleCamera camera={selected} fill={fill} onZoom={trackZoom} />}
            </div>
            {/* The fullscreen/landscape section covers the page dock, so the transport is revealed on demand over the picture. */}
            {immersiveRec && recControls && (
              <div className="absolute inset-x-0 bottom-0 z-10 max-h-[60%] overflow-y-auto bg-surface-1/95 backdrop-blur">
                <LiveRecDock compact transport={transport} cameras={session.timelineCameras} events={recData.events} now={session.now} selectedId={selected.id} />
              </div>
            )}
          </div>
        </section>
      )}
      {rec && !immersiveRec && (
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
