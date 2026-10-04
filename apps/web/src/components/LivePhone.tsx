import { useQuery } from "@tanstack/react-query";
import { useNavigate, useSearch } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { camerasQuery, sitesQuery } from "@/api/queries";
import type { Schemas } from "@/api/client";
import { setContextSidebarCollapsed } from "@/components/AppShell";
import { MsePlayer } from "@/components/MsePlayer";
import { Chip, IconButton } from "@/components/ui";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";

/** Simultaneous live players in the list; further visible cards keep their snapshot. */
export const PHONE_MAX_LIVE = 4;
/** A card plays live only while at least this share of it is on screen. */
const VISIBLE_RATIO = 0.5;

type Camera = Schemas["Camera"];

const statusDot = (status?: string) => (status === "online" ? "bg-ok" : status === "offline" ? "bg-bad" : "bg-warn");

/**
 * useVisibleCards watches the cards of the list with one IntersectionObserver and returns the
 * ids at least half visible, top-most first. Without IntersectionObserver every card counts as visible.
 */
function useVisibleCards(order: string[]) {
  const [tops, setTops] = useState<Record<string, number>>({});
  const observer = useRef<IntersectionObserver | null>(null);
  const known = useRef(new Set<Element>());
  const supported = typeof IntersectionObserver !== "undefined";
  useEffect(() => {
    if (!supported) return;
    const io = new IntersectionObserver(
      (entries) => {
        setTops((previous) => {
          const next = { ...previous };
          for (const entry of entries) {
            const id = (entry.target as HTMLElement).dataset.cameraId;
            if (!id) continue;
            if (entry.isIntersecting && entry.intersectionRatio >= VISIBLE_RATIO) next[id] = entry.boundingClientRect.top;
            else delete next[id];
          }
          return next;
        });
      },
      { threshold: [0, VISIBLE_RATIO] },
    );
    observer.current = io;
    known.current.forEach((el) => io.observe(el));
    return () => {
      io.disconnect();
      observer.current = null;
    };
  }, [supported]);
  const watch = useCallback((el: HTMLElement | null) => {
    if (!el) return;
    known.current.add(el);
    observer.current?.observe(el);
  }, []);
  const unwatch = useCallback((el: HTMLElement) => {
    known.current.delete(el);
    observer.current?.unobserve(el);
  }, []);
  const live = useMemo(() => {
    const ids = supported ? order.filter((id) => id in tops).sort((a, b) => (tops[a] ?? 0) - (tops[b] ?? 0)) : order;
    return new Set(ids.slice(0, PHONE_MAX_LIVE));
  }, [order, tops, supported]);
  return { live, watch, unwatch };
}

function CameraCard({ camera, live, watch, unwatch, onOpen }: { camera: Camera; live: boolean; watch: (el: HTMLElement | null) => void; unwatch: (el: HTMLElement) => void; onOpen: () => void }) {
  const tr = useT();
  const host = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = host.current;
    watch(el);
    return () => {
      if (el) unwatch(el);
    };
  }, [watch, unwatch]);
  return (
    <li>
      <div ref={host} data-camera-id={camera.id} className="relative aspect-video w-full overflow-hidden bg-video">
        {live ? (
          <MsePlayer cameraId={camera.id} quality="sub" serverId={camera.server_id} className="size-full" />
        ) : (
          <img src={`/media/v1/cameras/${camera.id}/snapshot.jpg?h=360`} alt="" draggable={false} loading="lazy" className="size-full object-contain" />
        )}
        <div className="pointer-events-none absolute inset-x-0 bottom-0 flex items-center gap-1.5 p-2 text-xs">
          <span className="flex min-w-0 items-center gap-1.5 rounded-full bg-surface-1/80 px-2.5 py-1 text-on-surface backdrop-blur">
            <span className={cn("size-2 shrink-0 rounded-full", statusDot(camera.status))} aria-hidden />
            <span className="truncate font-medium">{camera.display_name}</span>
          </span>
        </div>
        <button type="button" aria-label={tr("live.phoneViewCamera", { name: camera.display_name })} onClick={onOpen} className="absolute inset-0 z-[3] min-h-11 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary" />
      </div>
    </li>
  );
}

/**
 * LivePhone is Live below the `md` breakpoint: a Frigate-style vertical list of camera cards
 * (sub stream while visible, snapshot otherwise) and a single-camera view on the main stream.
 * The open camera lives in `?camera=<id>`, so the system back button and Maps handoffs work.
 */
export function LivePhone() {
  const tr = useT();
  const cameras = useQuery(camerasQuery({}));
  const sites = useQuery(sitesQuery);
  const navigate = useNavigate();
  const search = useSearch({ strict: false }) as Record<string, unknown>;
  const [siteId, setSiteId] = useState<string | null>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const scrollTop = useRef(0);

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
  const siteChips = useMemo(() => {
    const ids = [...new Set(all.map((camera) => camera.site_id))];
    return ids.map((id) => ({ id, name: siteName.get(id) ?? id }));
  }, [all, siteName]);
  const shown = useMemo(() => (siteId ? all.filter((camera) => camera.site_id === siteId) : all), [all, siteId]);
  const order = useMemo(() => shown.map((camera) => camera.id), [shown]);

  const requested = typeof search.camera === "string" ? search.camera : undefined;
  const selected = requested ? all.find((camera) => camera.id === requested) : undefined;
  const { live, watch, unwatch } = useVisibleCards(order);

  // The list stays mounted (hidden) under the single view; display:none resets scroll, so restore it.
  useLayoutEffect(() => {
    if (!selected && listRef.current) listRef.current.scrollTop = scrollTop.current;
  }, [selected]);

  const open = (id: string) => void navigate({ to: ".", search: ((previous: Record<string, unknown>) => ({ ...previous, camera: id })) as never });
  const close = () => void navigate({ to: ".", replace: true, search: ((previous: Record<string, unknown>) => ({ ...previous, camera: undefined })) as never });

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
      <h1 className="sr-only">{tr("live.phoneCameras")}</h1>
      <div hidden={!!selected} className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
        <div role="group" aria-label={tr("live.phoneSites")} className="flex shrink-0 gap-2 overflow-x-auto pb-1">
          <Chip selected={siteId === null} onChange={() => setSiteId(null)} className="shrink-0">{tr("live.phoneAllSites")}</Chip>
          {siteChips.map((site) => (
            <Chip key={site.id} selected={siteId === site.id} onChange={(next) => setSiteId(next ? site.id : null)} className="shrink-0">{site.name}</Chip>
          ))}
        </div>
        <div ref={listRef} data-testid="live-phone-list" onScroll={(event) => { scrollTop.current = event.currentTarget.scrollTop; }} className="min-h-0 flex-1 overflow-y-auto">
          <ul className="flex flex-col gap-2">
            {shown.map((camera) => (
              <CameraCard key={camera.id} camera={camera} live={!selected && live.has(camera.id)} watch={watch} unwatch={unwatch} onOpen={() => open(camera.id)} />
            ))}
          </ul>
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
          </div>
          <div className="relative aspect-video w-full overflow-hidden bg-video">
            <MsePlayer cameraId={selected.id} quality="main" serverId={selected.server_id} className="size-full" />
          </div>
        </section>
      )}
    </div>
  );
}
