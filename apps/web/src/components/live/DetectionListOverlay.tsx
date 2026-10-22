import { useEffect, useState } from "react";
import { CarFront, ChevronLeft, ChevronRight, ScanFace, type LucideIcon } from "lucide-react";
import { ArPlate } from "@/components/plates/ArPlate";
import { cn } from "@/lib/cn";
import type { ChannelItem, ChannelKind } from "@/lib/detectionChannels";
import { useCameraFaces, useCameraPlates, useFittingRows, useLprOverlayCollapsed } from "@/lib/detectionOverlay";
import { detectionAgeSeconds, formatDetectionAge } from "@/lib/visionAi";

/** Ticks so "hace Ns" keeps moving without refetching. */
function useNow(intervalMs = 5000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

/** Tab order, and the kind shown first when a camera has both. */
const KIND_ORDER: readonly ChannelKind[] = ["lpr", "faces"];

type Look = {
  tab: string;
  title: string;
  section: string;
  list: string;
  show: string;
  hide: string;
  Icon: LucideIcon;
  panel: string;
  corner: string;
  scan: string;
  dot: string;
  headerBorder: string;
  accent: string;
  accentButton: string;
  tabOn: string;
  collapsed: string;
  rowLatest: string;
  rowSub: string;
  rowEmpty: string;
};

// Tailwind needs literal class names, so each kind spells out its own colors: cyan for plates,
// fuchsia for faces, so the tab you are on is recognizable at a glance.
const LOOK: Record<ChannelKind, Look> = {
  lpr: {
    tab: "LPR",
    title: "LPR",
    section: "Patentes recientes",
    list: "Lecturas de patentes",
    show: "Mostrar lista de patentes",
    hide: "Ocultar lista de patentes",
    Icon: CarFront,
    panel: "border-cyan-400/40 shadow-[0_0_24px_rgba(34,211,238,0.22)]",
    corner: "border-cyan-300",
    scan: "via-cyan-300/10",
    dot: "bg-cyan-300 shadow-[0_0_8px_rgba(103,232,249,0.9)]",
    headerBorder: "border-cyan-400/25",
    accent: "text-cyan-300",
    accentButton: "text-cyan-300/80 hover:bg-cyan-300/15 hover:text-cyan-200 focus-visible:outline-cyan-300",
    tabOn: "bg-cyan-300/20 text-cyan-100 shadow-[0_0_10px_rgba(34,211,238,0.35)]",
    collapsed: "border-cyan-400/50 text-cyan-300 shadow-[0_0_14px_rgba(34,211,238,0.35)] focus-visible:outline-cyan-300",
    rowLatest: "border-cyan-300/70 bg-cyan-400/15 shadow-[0_0_14px_rgba(34,211,238,0.35)]",
    rowSub: "text-cyan-100/70",
    rowEmpty: "text-cyan-200/50",
  },
  faces: {
    tab: "Rostros",
    title: "Rostros",
    section: "Rostros recientes",
    list: "Detecciones faciales",
    show: "Mostrar lista de rostros",
    hide: "Ocultar lista de rostros",
    Icon: ScanFace,
    panel: "border-fuchsia-400/40 shadow-[0_0_24px_rgba(217,70,239,0.22)]",
    corner: "border-fuchsia-300",
    scan: "via-fuchsia-300/10",
    dot: "bg-fuchsia-300 shadow-[0_0_8px_rgba(240,171,252,0.9)]",
    headerBorder: "border-fuchsia-400/25",
    accent: "text-fuchsia-300",
    accentButton: "text-fuchsia-300/80 hover:bg-fuchsia-300/15 hover:text-fuchsia-200 focus-visible:outline-fuchsia-300",
    tabOn: "bg-fuchsia-300/20 text-fuchsia-100 shadow-[0_0_10px_rgba(217,70,239,0.35)]",
    collapsed: "border-fuchsia-400/50 text-fuchsia-300 shadow-[0_0_14px_rgba(217,70,239,0.35)] focus-visible:outline-fuchsia-300",
    rowLatest: "border-fuchsia-300/70 bg-fuchsia-400/15 shadow-[0_0_14px_rgba(217,70,239,0.35)]",
    rowSub: "text-fuchsia-100/70",
    rowEmpty: "text-fuchsia-200/50",
  },
};

/**
 * Detections of one camera as a glass list on the left of the video: plate reads and/or face
 * detections. It is drawn only when the camera has some; with both, tabs pick the list (plates
 * first). The wrapper ignores the pointer so the player underneath keeps working; only the tabs
 * and the collapse button are clickable.
 *
 * `lpr` and `faces` say which kinds the caller wants looked up for this camera; `enabled` switches
 * the whole overlay off.
 */
export function DetectionListOverlay({
  cameraId,
  enabled = true,
  lpr = true,
  faces = false,
  className,
}: {
  cameraId: string;
  enabled?: boolean;
  lpr?: boolean;
  faces?: boolean;
  className?: string;
}) {
  const plates = useCameraPlates(cameraId, enabled && lpr);
  const faceList = useCameraFaces(cameraId, enabled && faces);
  const [collapsed, setCollapsed] = useLprOverlayCollapsed();
  const [chosen, setChosen] = useState<ChannelKind | null>(null);
  const now = useNow();
  const [listRef, rows] = useFittingRows();
  if (!enabled) return null;

  const byKind: Record<ChannelKind, ChannelItem[]> = {
    lpr: lpr ? (plates.data ?? []) : [],
    faces: faces ? (faceList.data ?? []) : [],
  };
  // Only cameras that actually have detections get the panel: nothing while loading, on error or empty.
  const available = KIND_ORDER.filter((kind) => byKind[kind].length > 0);
  const first = available[0];
  if (!first) return null;
  const kind = chosen && available.includes(chosen) ? chosen : first;
  const look = LOOK[kind];
  const items = byKind[kind];

  return (
    <div
      data-testid="detection-overlay"
      data-kind={kind}
      className={cn("pointer-events-none absolute inset-y-0 left-0 z-20 flex max-w-[44%] items-stretch py-2 pl-2", className)}
    >
      {collapsed ? (
        <button
          type="button"
          aria-label={look.show}
          onClick={() => setCollapsed(false)}
          className={cn(
            "pointer-events-auto my-auto flex h-14 w-7 items-center justify-center rounded-r-lg border border-l-0 bg-black/50 backdrop-blur-md hover:bg-black/70 focus-visible:outline-2",
            look.collapsed,
          )}
        >
          <look.Icon aria-hidden="true" className="size-4" />
          <ChevronRight aria-hidden="true" className="size-3" />
        </button>
      ) : (
        <section
          aria-label={look.section}
          className={cn("relative flex w-56 min-w-0 max-w-full flex-col overflow-hidden rounded-xl border bg-slate-950/55 backdrop-blur-md", look.panel)}
        >
          <span aria-hidden="true" className={cn("lpr-scan pointer-events-none absolute inset-x-0 top-0 h-1/3 bg-gradient-to-b from-transparent to-transparent", look.scan)} />
          <span aria-hidden="true" className={cn("pointer-events-none absolute left-0 top-0 size-3 rounded-tl-xl border-l-2 border-t-2", look.corner)} />
          <span aria-hidden="true" className={cn("pointer-events-none absolute bottom-0 right-0 size-3 rounded-br-xl border-b-2 border-r-2", look.corner)} />

          <header className={cn("flex shrink-0 items-center justify-between gap-2 border-b px-2.5 py-1.5", look.headerBorder)}>
            {available.length > 1 ? (
              <div role="tablist" aria-label="Tipo de detección" className="flex min-w-0 items-center gap-1">
                {available.map((k) => {
                  const TabIcon = LOOK[k].Icon;
                  return (
                    <button
                      key={k}
                      type="button"
                      role="tab"
                      aria-selected={k === kind}
                      onClick={() => setChosen(k)}
                      className={cn(
                        "pointer-events-auto inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 font-mono text-[10px] font-bold uppercase tracking-[0.14em] transition-colors focus-visible:outline-2 focus-visible:outline-white/70",
                        k === kind ? LOOK[k].tabOn : "text-white/50 hover:text-white/80",
                      )}
                    >
                      <TabIcon aria-hidden="true" className="size-3" />
                      {LOOK[k].tab}
                    </button>
                  );
                })}
              </div>
            ) : (
              <div className={cn("flex min-w-0 items-center gap-1.5 font-mono text-[10px] font-bold uppercase tracking-[0.2em]", look.accent)}>
                <span aria-hidden="true" className={cn("size-1.5 shrink-0 animate-pulse rounded-full", look.dot)} />
                <span className="truncate">{look.title}</span>
              </div>
            )}
            <button
              type="button"
              aria-label={look.hide}
              onClick={() => setCollapsed(true)}
              className={cn("pointer-events-auto inline-flex size-5 shrink-0 items-center justify-center rounded focus-visible:outline-2", look.accentButton)}
            >
              <ChevronLeft aria-hidden="true" className="size-4" />
            </button>
          </header>

          <ol ref={listRef} aria-label={look.list} className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-hidden p-1.5">
            {items.slice(0, rows).map((item, index) => (
              <OverlayRow key={item.key} item={item} look={look} latest={index === 0} now={now} />
            ))}
          </ol>
        </section>
      )}
    </div>
  );
}

function OverlayRow({ item, look, latest, now }: { item: ChannelItem; look: Look; latest: boolean; now: number }) {
  const [failed, setFailed] = useState(false);
  const seconds = detectionAgeSeconds(item.at, now);
  return (
    <li
      data-latest={latest ? "true" : undefined}
      className={cn(
        "lpr-feed-in flex min-w-0 items-center gap-2 rounded-lg border px-1.5 py-1",
        latest ? look.rowLatest : "border-white/10 bg-white/5 opacity-80",
      )}
    >
      {item.imageUrl && !failed ? (
        <img src={item.imageUrl} alt="" loading="lazy" onError={() => setFailed(true)} className="h-8 w-12 shrink-0 rounded object-cover" />
      ) : (
        <span aria-hidden="true" className={cn("flex h-8 w-12 shrink-0 items-center justify-center rounded bg-black/40", look.rowEmpty)}>
          <look.Icon className="size-4" />
        </span>
      )}
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        {item.kind === "lpr" ? (
          <ArPlate plate={item.label} />
        ) : (
          <span
            className={cn(
              "truncate text-xs font-semibold leading-6",
              item.recognized ? "text-white" : "font-normal italic text-white/60",
            )}
          >
            {item.label}
          </span>
        )}
        <span className={cn("truncate font-mono text-[10px] tabular-nums", look.rowSub)}>
          {seconds === null ? "--" : formatDetectionAge(seconds)}
          {item.zone ? ` · ${item.zone}` : ""}
        </span>
      </div>
    </li>
  );
}
