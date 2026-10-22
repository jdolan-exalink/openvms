import { useEffect, useState } from "react";
import { CarFront, ChevronLeft, ChevronRight } from "lucide-react";
import { ArPlate } from "@/components/plates/ArPlate";
import { cn } from "@/lib/cn";
import type { ChannelItem } from "@/lib/detectionChannels";
import { useCameraPlates, useLprOverlayCollapsed } from "@/lib/lprOverlay";
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

/**
 * Plate reads of one camera as a glass list on the left of the video. The wrapper ignores the
 * pointer so the player underneath keeps working; only the collapse button is clickable.
 */
export function LprListOverlay({ cameraId, enabled = true, className }: { cameraId: string; enabled?: boolean; className?: string }) {
  const query = useCameraPlates(cameraId, enabled);
  const [collapsed, setCollapsed] = useLprOverlayCollapsed();
  const now = useNow();
  if (!enabled) return null;
  const items = query.data ?? [];

  return (
    <div
      data-testid="lpr-list-overlay"
      className={cn("pointer-events-none absolute inset-y-0 left-0 z-20 flex max-w-[44%] items-stretch py-2 pl-2", className)}
    >
      {collapsed ? (
        <button
          type="button"
          aria-label="Mostrar lista de patentes"
          onClick={() => setCollapsed(false)}
          className="pointer-events-auto my-auto flex h-14 w-7 items-center justify-center rounded-r-lg border border-l-0 border-cyan-400/50 bg-black/50 text-cyan-300 shadow-[0_0_14px_rgba(34,211,238,0.35)] backdrop-blur-md hover:bg-black/70 focus-visible:outline-2 focus-visible:outline-cyan-300"
        >
          <CarFront aria-hidden="true" className="size-4" />
          <ChevronRight aria-hidden="true" className="size-3" />
        </button>
      ) : (
        <section
          aria-label="Patentes recientes"
          className="relative flex w-56 min-w-0 max-w-full flex-col overflow-hidden rounded-xl border border-cyan-400/40 bg-slate-950/55 shadow-[0_0_24px_rgba(34,211,238,0.22)] backdrop-blur-md"
        >
          <span aria-hidden="true" className="lpr-scan pointer-events-none absolute inset-x-0 top-0 h-1/3 bg-gradient-to-b from-transparent via-cyan-300/10 to-transparent" />
          <span aria-hidden="true" className="pointer-events-none absolute left-0 top-0 size-3 rounded-tl-xl border-l-2 border-t-2 border-cyan-300" />
          <span aria-hidden="true" className="pointer-events-none absolute bottom-0 right-0 size-3 rounded-br-xl border-b-2 border-r-2 border-cyan-300" />

          <header className="flex shrink-0 items-center justify-between gap-2 border-b border-cyan-400/25 px-2.5 py-1.5">
            <div className="flex min-w-0 items-center gap-1.5 font-mono text-[10px] font-bold uppercase tracking-[0.2em] text-cyan-300">
              <span aria-hidden="true" className="size-1.5 shrink-0 animate-pulse rounded-full bg-cyan-300 shadow-[0_0_8px_rgba(103,232,249,0.9)]" />
              <span className="truncate">LPR</span>
            </div>
            <button
              type="button"
              aria-label="Ocultar lista de patentes"
              onClick={() => setCollapsed(true)}
              className="pointer-events-auto inline-flex size-5 shrink-0 items-center justify-center rounded text-cyan-300/80 hover:bg-cyan-300/15 hover:text-cyan-200 focus-visible:outline-2 focus-visible:outline-cyan-300"
            >
              <ChevronLeft aria-hidden="true" className="size-4" />
            </button>
          </header>

          {items.length === 0 ? (
            <p className="px-3 py-4 text-center font-mono text-[11px] uppercase tracking-wider text-cyan-200/60">
              {query.isError ? "Sin conexión LPR" : "Esperando patentes"}
            </p>
          ) : (
            <ol aria-label="Lecturas de patentes" className="flex min-h-0 flex-1 flex-col gap-1.5 overflow-hidden p-1.5">
              {items.slice(0, 6).map((item, index) => (
                <OverlayRow key={item.key} item={item} latest={index === 0} now={now} />
              ))}
            </ol>
          )}
        </section>
      )}
    </div>
  );
}

function OverlayRow({ item, latest, now }: { item: ChannelItem; latest: boolean; now: number }) {
  const [failed, setFailed] = useState(false);
  const seconds = detectionAgeSeconds(item.at, now);
  return (
    <li
      data-latest={latest ? "true" : undefined}
      className={cn(
        "lpr-feed-in flex min-w-0 items-center gap-2 rounded-lg border px-1.5 py-1",
        latest
          ? "border-cyan-300/70 bg-cyan-400/15 shadow-[0_0_14px_rgba(34,211,238,0.35)]"
          : "border-white/10 bg-white/5 opacity-80",
      )}
    >
      {item.imageUrl && !failed ? (
        <img src={item.imageUrl} alt="" loading="lazy" onError={() => setFailed(true)} className="h-8 w-12 shrink-0 rounded object-cover" />
      ) : (
        <span aria-hidden="true" className="flex h-8 w-12 shrink-0 items-center justify-center rounded bg-black/40 text-cyan-200/50">
          <CarFront className="size-4" />
        </span>
      )}
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <ArPlate plate={item.label} />
        <span className="truncate font-mono text-[10px] tabular-nums text-cyan-100/70">
          {seconds === null ? "--" : formatDetectionAge(seconds)}
          {item.zone ? ` · ${item.zone}` : ""}
        </span>
      </div>
    </li>
  );
}
