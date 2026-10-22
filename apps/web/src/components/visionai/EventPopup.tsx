import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { Crosshair, X } from "lucide-react";
import { ArPlate } from "@/components/plates/ArPlate";
import { IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import { fmtDateTime } from "@/lib/format";
import type { VisionEventView } from "@/lib/visionEvents";
import { ObjectIcon } from "@/lib/visionAiIcons";

/**
 * Popup shown above the Vision AI grid for one event: image, camera, object, zone, time and the
 * plate when there is one. It never blocks the grid underneath beyond its own box.
 */
export function EventPopup({ event, onClose, onPin }: { event: VisionEventView; onClose: () => void; onPin?: () => void }) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  const subject = event.person ?? event.object ?? "evento";
  return (
    <div className="pointer-events-none absolute inset-x-0 top-2 z-30 flex justify-center px-2">
      <section
        role="dialog"
        aria-label="Detalle del evento"
        className="pointer-events-auto flex w-full max-w-5xl flex-col overflow-hidden rounded-xl border border-cyan-400/50 bg-slate-950/85 text-slate-100 shadow-[0_0_32px_rgba(34,211,238,0.3)] backdrop-blur-md sm:flex-row"
      >
        <EventImage key={event.key} event={event} />
        <div className="flex min-w-0 flex-1 flex-col gap-2 p-3">
          <header className="flex items-start justify-between gap-2">
            <div className="flex min-w-0 flex-col">
              <span className="truncate text-sm font-bold text-cyan-200">{event.cameraName}</span>
              {event.at && <span className="font-mono text-[11px] tabular-nums text-slate-300/80">{fmtDateTime(event.at)}</span>}
            </div>
            <IconButton icon={X} title="Cerrar evento" aria-label="Cerrar evento" size="sm" onClick={onClose} className="shrink-0 text-cyan-200 hover:bg-cyan-300/15" />
          </header>

          <div className="flex flex-wrap items-center gap-2 text-xs">
            <span className="inline-flex items-center gap-1 rounded-full border border-cyan-400/40 bg-cyan-400/10 px-2 py-0.5 font-semibold uppercase text-cyan-200">
              <ObjectIcon object={event.object} className="size-3" />
              {subject}
              {event.confidence !== undefined && <span className="opacity-75">· {Math.round(event.confidence * 100)}%</span>}
            </span>
            {event.zone && <span className="rounded-full bg-white/10 px-2 py-0.5 text-slate-200">{event.zone}</span>}
          </div>

          {event.plate && (
            <div className="flex items-center gap-2">
              <ArPlate plate={event.plate} large />
            </div>
          )}

          {event.slot && onPin && (
            <button
              type="button"
              onClick={onPin}
              className="mt-auto inline-flex items-center gap-1.5 self-start rounded-full border border-cyan-400/40 px-3 py-1 text-xs font-semibold text-cyan-200 hover:bg-cyan-300/15 focus-visible:outline-2 focus-visible:outline-cyan-300"
            >
              <Crosshair aria-hidden="true" className="size-3.5" />
              Fijar en foco
            </button>
          )}
        </div>
      </section>
    </div>
  );
}

const MAX_ZOOM = 8;
const ZOOM_STEP = 1.25;

type Zoom = { scale: number; x: number; y: number };
const UNZOOMED: Zoom = { scale: 1, x: 0, y: 0 };

/** Keeps the zoomed photo covering its frame: it can be dragged, never out of view. */
function clampPan(z: Zoom, width: number, height: number): Zoom {
  const maxX = (width * (z.scale - 1)) / 2;
  const maxY = (height * (z.scale - 1)) / 2;
  return { scale: z.scale, x: Math.min(maxX, Math.max(-maxX, z.x)), y: Math.min(maxY, Math.max(-maxY, z.y)) };
}

/** Zooms by `factor` keeping the point under the cursor (offset from the frame center) in place. */
function zoomAt(z: Zoom, factor: number, cx: number, cy: number, width: number, height: number): Zoom {
  const scale = Math.min(MAX_ZOOM, Math.max(1, z.scale * factor));
  if (scale < 1.01) return UNZOOMED;
  const ratio = scale / z.scale;
  return clampPan({ scale, x: cx - (cx - z.x) * ratio, y: cy - (cy - z.y) * ratio }, width, height);
}

/**
 * The first image that loads, in the order the view lists them; a placeholder when none does.
 * The wheel zooms around the cursor, a drag pans a zoomed photo and a double click resets it.
 */
function EventImage({ event }: { event: VisionEventView }) {
  const [index, setIndex] = useState(0);
  const [zoom, setZoom] = useState<Zoom>(UNZOOMED);
  const frame = useRef<HTMLDivElement>(null);
  const drag = useRef<{ x: number; y: number } | null>(null);
  const src = event.images[index];

  // Native listener: React registers wheel as passive, which cannot stop the page from scrolling.
  useEffect(() => {
    const el = frame.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const rect = el.getBoundingClientRect();
      const cx = e.clientX - rect.left - rect.width / 2;
      const cy = e.clientY - rect.top - rect.height / 2;
      setZoom((z) => zoomAt(z, e.deltaY < 0 ? ZOOM_STEP : 1 / ZOOM_STEP, cx, cy, rect.width, rect.height));
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, []);

  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (zoom.scale === 1) return;
    drag.current = { x: e.clientX, y: e.clientY };
    e.currentTarget.setPointerCapture?.(e.pointerId);
  };
  const onPointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const from = drag.current;
    if (!from) return;
    drag.current = { x: e.clientX, y: e.clientY };
    const rect = e.currentTarget.getBoundingClientRect();
    setZoom((z) => clampPan({ scale: z.scale, x: z.x + e.clientX - from.x, y: z.y + e.clientY - from.y }, rect.width, rect.height));
  };

  return (
    <div
      ref={frame}
      data-testid="event-popup-photo"
      title="Rueda para acercar · doble clic para restablecer"
      onDoubleClick={() => setZoom(UNZOOMED)}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={() => { drag.current = null; }}
      onPointerCancel={() => { drag.current = null; }}
      className={cn("aspect-video w-full shrink-0 touch-none select-none overflow-hidden bg-black sm:w-[26rem] lg:w-[36rem]", zoom.scale > 1 ? "cursor-grab active:cursor-grabbing" : "cursor-zoom-in")}
    >
      <div
        data-testid="event-popup-zoom"
        style={{ transform: `translate(${zoom.x}px, ${zoom.y}px) scale(${zoom.scale})` }}
        className="size-full origin-center will-change-transform"
      >
        {src ? (
          <img
            src={src}
            alt={`Detección: ${event.object ?? "evento"} en ${event.cameraName}`}
            draggable={false}
            className="size-full object-contain"
            onError={() => setIndex((i) => i + 1)}
          />
        ) : (
          <div data-testid="event-popup-placeholder" className="flex size-full items-center justify-center text-slate-400">
            <ObjectIcon object={event.object} className="size-8 opacity-50" />
          </div>
        )}
      </div>
    </div>
  );
}
