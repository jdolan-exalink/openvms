import { useState } from "react";
import { Layers, X } from "lucide-react";
import { ArPlate } from "@/components/plates/ArPlate";
import { EventSnapshot } from "@/components/visionai/EventSnapshot";
import { IconButton } from "@/components/ui";
import { cn } from "@/lib/cn";
import { fmtTime } from "@/lib/format";
import type { VisionAISlot } from "@/lib/visionAi";
import { ObjectIcon } from "@/lib/visionAiIcons";
import { slotToView, useTabEvents, type EventsTab, type VisionEventView } from "@/lib/visionEvents";

const TAB_LABELS: Record<EventsTab, string> = { active: "Activos", history: "Históricos", lpr: "LPR", faces: "Faciales" };
const EMPTY: Record<EventsTab, string> = {
  active: "Sin eventos activos",
  history: "Sin eventos históricos",
  lpr: "Sin lecturas de patentes",
  faces: "Sin detecciones faciales",
};

/**
 * The Vision AI "Eventos" side bar: active events (the ranked slots) plus the historic, LPR and
 * face tabs. Choosing a row hands its view to the caller, which shows it in the popup.
 */
export function EventsSidebar({
  activeSlots,
  canSnapshots,
  canEvents,
  canLpr,
  selectedKey,
  onSelect,
  onClose,
}: {
  activeSlots: VisionAISlot[];
  canSnapshots: boolean;
  canEvents: boolean;
  canLpr: boolean;
  selectedKey?: string;
  onSelect: (view: VisionEventView) => void;
  onClose: () => void;
}) {
  const tabs: EventsTab[] = ["active", ...(canEvents ? (["history"] as const) : []), ...(canLpr ? (["lpr"] as const) : []), ...(canEvents ? (["faces"] as const) : [])];
  const [chosen, setChosen] = useState<EventsTab>("active");
  const tab = tabs.includes(chosen) ? chosen : "active";
  const other = useTabEvents(tab, canSnapshots);

  return (
    <aside
      data-testid="vision-events-sidebar"
      className="flex min-h-0 w-80 shrink-0 flex-col overflow-hidden rounded-m3-lg border border-line bg-surface"
    >
      <div className="flex items-center justify-between border-b border-line p-3">
        <div className="flex items-center gap-2 text-sm font-semibold text-ink">
          <Layers className="size-4 text-primary" />
          <span>Eventos ({activeSlots.length})</span>
        </div>
        <IconButton icon={X} title="Cerrar panel" aria-label="Cerrar panel" size="sm" onClick={onClose} />
      </div>

      <div role="tablist" aria-label="Tipo de eventos" className="flex gap-1 border-b border-line px-2 py-1.5">
        {tabs.map((t) => (
          <button
            key={t}
            type="button"
            role="tab"
            aria-selected={t === tab}
            onClick={() => setChosen(t)}
            className={cn(
              "rounded-full px-2.5 py-1 text-xs font-semibold transition-colors focus-visible:outline-2 focus-visible:outline-primary",
              t === tab ? "bg-primary text-on-primary" : "text-on-surface-variant hover:bg-on-surface/8",
            )}
          >
            {TAB_LABELS[t]}
          </button>
        ))}
      </div>

      <div role="tabpanel" aria-label={TAB_LABELS[tab]} className="flex-1 divide-y divide-line/40 overflow-y-auto">
        {tab === "active" ? (
          activeSlots.length === 0 ? (
            <p className="p-4 text-center text-xs text-muted">{EMPTY.active}</p>
          ) : (
            activeSlots.map((sl) => {
              const view = slotToView(sl, canSnapshots);
              return (
                <div
                  key={sl.slot}
                  onClick={() => onSelect(view)}
                  aria-current={selectedKey === view.key ? "true" : undefined}
                  className={cn("flex cursor-pointer flex-col gap-1.5 p-3 transition-colors hover:bg-surface-2", selectedKey === view.key && "bg-surface-2")}
                >
                  <EventSnapshot slot={sl} snapshots={canSnapshots} />
                  <div className="flex items-center justify-between">
                    <span className="truncate text-xs font-semibold text-ink">{sl.camera_name}</span>
                    {sl.score !== undefined && (
                      <span className="rounded-m3-sm bg-primary/10 px-1.5 py-0.5 font-mono text-xs font-bold text-primary">Score {Math.round(sl.score)}</span>
                    )}
                  </div>
                  <div className="flex items-center justify-between text-[11px] text-muted">
                    <span className="inline-flex items-center gap-1 font-medium uppercase text-emerald-600">
                      <ObjectIcon object={sl.object} className="size-3" />
                      {sl.object}
                      {sl.confidence !== undefined && ` (${Math.round(sl.confidence * 100)}%)`}
                    </span>
                    <span>{sl.zone}</span>
                  </div>
                  {sl.plate && (
                    <div className="self-start rounded-m3-sm bg-surface-3 px-2 py-0.5 font-mono text-xs font-bold tracking-widest text-ink">{sl.plate}</div>
                  )}
                </div>
              );
            })
          )
        ) : other.error ? (
          <p role="alert" className="p-4 text-center text-xs text-bad">No se pudieron cargar los eventos</p>
        ) : other.loading ? (
          <p className="p-4 text-center text-xs text-muted">Cargando…</p>
        ) : other.views.length === 0 ? (
          <p className="p-4 text-center text-xs text-muted">{EMPTY[tab]}</p>
        ) : (
          other.views.map((view) => <EventRow key={view.key} view={view} selected={selectedKey === view.key} onSelect={() => onSelect(view)} />)
        )}
      </div>
    </aside>
  );
}

function EventRow({ view, selected, onSelect }: { view: VisionEventView; selected: boolean; onSelect: () => void }) {
  const [failed, setFailed] = useState(false);
  const src = view.images.at(-1);
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-current={selected ? "true" : undefined}
      className={cn("flex w-full items-center gap-2 p-2.5 text-left transition-colors hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-primary", selected && "bg-surface-2")}
    >
      {src && !failed ? (
        <img src={src} alt="" loading="lazy" onError={() => setFailed(true)} className="h-10 w-16 shrink-0 rounded-m3-sm bg-surface-3 object-cover" />
      ) : (
        <span aria-hidden="true" className="flex h-10 w-16 shrink-0 items-center justify-center rounded-m3-sm bg-surface-3 text-muted">
          <ObjectIcon object={view.object} className="size-4 opacity-60" />
        </span>
      )}
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-xs font-semibold text-ink">{view.cameraName}</span>
        {view.plate ? (
          <ArPlate plate={view.plate} />
        ) : (
          <span className="truncate text-[11px] uppercase text-muted">{view.person ?? view.object}</span>
        )}
        {view.at && <span className="font-mono text-[10px] tabular-nums text-muted">{fmtTime(view.at)}</span>}
      </span>
    </button>
  );
}
