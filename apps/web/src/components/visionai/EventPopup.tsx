import { useEffect, useState } from "react";
import { Crosshair, X } from "lucide-react";
import { ArPlate } from "@/components/plates/ArPlate";
import { IconButton } from "@/components/ui";
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
        className="pointer-events-auto flex w-full max-w-xl flex-col overflow-hidden rounded-xl border border-cyan-400/50 bg-slate-950/85 text-slate-100 shadow-[0_0_32px_rgba(34,211,238,0.3)] backdrop-blur-md sm:flex-row"
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

/** The first image that loads, in the order the view lists them; a placeholder when none does. */
function EventImage({ event }: { event: VisionEventView }) {
  const [index, setIndex] = useState(0);
  const src = event.images[index];
  return (
    <div className="aspect-video w-full shrink-0 bg-black sm:w-60">
      {src ? (
        <img
          src={src}
          alt={`Detección: ${event.object ?? "evento"} en ${event.cameraName}`}
          className="size-full object-cover"
          onError={() => setIndex((i) => i + 1)}
        />
      ) : (
        <div data-testid="event-popup-placeholder" className="flex size-full items-center justify-center text-slate-400">
          <ObjectIcon object={event.object} className="size-8 opacity-50" />
        </div>
      )}
    </div>
  );
}
