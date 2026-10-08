import { ChevronsLeft, ChevronsRight, CircleHelp, Download, Pause, Play, SkipBack, SkipForward, StepBack, StepForward, X } from "lucide-react";
import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import { DayCalendar } from "@/components/DayCalendar";
import { DayTimeline, type TimelineCamera, type TimelineEvent } from "@/components/DayTimeline";
import { LiveExportModal } from "@/components/LiveExportModal";
import { Button, Select } from "@/components/ui";
import { fmtDateTime } from "@/lib/format";
import { moveToDay, REC_LIVE_EDGE_S, REC_SPEEDS, stepEvent } from "@/lib/liveRec";
import type { RecTransport } from "@/lib/useRecPlayback";

const STEPS = [
  { label: "Retroceder 1 minuto", seconds: -60, Icon: ChevronsLeft },
  { label: "Retroceder 10 segundos", seconds: -10, Icon: StepBack },
  { label: "Avanzar 10 segundos", seconds: 10, Icon: StepForward },
  { label: "Avanzar 1 minuto", seconds: 60, Icon: ChevronsRight },
] as const;

/** True for targets where typing must not trigger the shortcuts below. */
function isTyping(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  if (!el?.tagName) return false;
  return el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName) || !!el.closest("[role=dialog]");
}

const pad = (n: number) => String(n).padStart(2, "0");
const clockTime = (t: number) => {
  const d = new Date(t * 1000);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};
const durationLabel = (s: number) => (s < 60 ? `${Math.round(s)}s` : `${Math.floor(s / 60)}m ${Math.round(s % 60)}s`);

/**
 * LiveRecDock is the transport of the Live REC mode: play/pause, speed, steps, event
 * navigation, "Ahora", the day picker and the zoomable day timeline. It also provides
 * [ IN --- OUT ] range selection for multi-camera video export.
 */
export function LiveRecDock({
  transport,
  cameras,
  events,
  now,
  selectedId,
  onSelectCamera,
  compact = false,
}: {
  transport: RecTransport;
  cameras: TimelineCamera[];
  events: TimelineEvent[];
  now: number;
  selectedId?: string;
  onSelectCamera?: (cameraId: string) => void;
  /** Phone variant: play, ±10 s, speed, "Ahora", day and clock only, scrollable, no keyboard help. */
  compact?: boolean;
}) {
  const { seek, play, pause, playing, speed, setSpeed, day, getPosition, subscribePosition } = transport;
  const position = useSyncExternalStore(subscribePosition, getPosition);
  const eventTimes = useMemo(() => events.map((e) => e.time), [events]);

  const [inTime, setInTime] = useState<number | undefined>();
  const [outTime, setOutTime] = useState<number | undefined>();
  const [exportOpen, setExportOpen] = useState(false);

  const selection = useMemo(() => {
    if (inTime === undefined || outTime === undefined || inTime === outTime) return undefined;
    return {
      start: Math.min(inTime, outTime),
      end: Math.max(inTime, outTime),
    };
  }, [inTime, outTime]);

  const clearSelection = () => {
    setInTime(undefined);
    setOutTime(undefined);
  };

  const latestRecorded = useMemo(() => {
    const ends = cameras.flatMap((c) => c.spans.map((s) => s.end));
    return ends.length ? Math.max(...ends) : undefined;
  }, [cameras]);
  const liveEdge = now - REC_LIVE_EDGE_S;
  const goNow = () => seek(latestRecorded !== undefined && latestRecorded - 5 < liveEdge ? latestRecorded - 5 : liveEdge);
  const step = (seconds: number) => seek(Math.min(position + seconds, liveEdge), { play: playing });
  const toEvent = (dir: 1 | -1) => {
    const t = stepEvent(eventTimes, position, dir);
    if (t !== undefined) seek(t);
  };

  // Keyboard map (documented in the help below). Ignored while typing or when a dialog is open.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.altKey || event.ctrlKey || event.metaKey || isTyping(event.target)) return;
      const target = event.target as HTMLElement | null;
      if (event.key === " ") {
        // A focused button keeps its own Space behaviour.
        if (target?.tagName === "BUTTON") return;
        event.preventDefault();
        if (playing) pause();
        else play();
      } else if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        const sign = event.key === "ArrowLeft" ? -1 : 1;
        step(sign * (event.shiftKey ? 60 : 10));
      } else if (event.key === "i" || event.key === "I") {
        event.preventDefault();
        setInTime(position);
      } else if (event.key === "o" || event.key === "O") {
        event.preventDefault();
        setOutTime(position);
      } else if (event.key === "Escape" && (inTime !== undefined || outTime !== undefined)) {
        event.preventDefault();
        clearSelection();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  return (
    <section aria-label="Controles de grabación" className={compact ? "flex max-h-[42dvh] shrink-0 flex-col gap-2 overflow-y-auto rounded-m3-xl bg-surface-1 p-2" : "flex flex-col gap-2 max-h-[40vh] rounded-xl border border-line bg-surface p-2"}>
      <div className="flex flex-wrap items-center gap-1.5">
        <Button variant="primary" aria-label={playing ? "Pausar" : "Reproducir"} title={playing ? "Pausar (Espacio)" : "Reproducir (Espacio)"} onClick={() => (playing ? pause() : play())}>
          {playing ? <Pause className="size-4" aria-hidden /> : <Play className="size-4" aria-hidden />}
        </Button>
        {(compact ? STEPS.filter((s) => Math.abs(s.seconds) === 10) : STEPS).map(({ label, seconds, Icon }) => (
          <Button key={label} aria-label={label} title={label} className="px-2" onClick={() => step(seconds)}>
            <Icon className="size-4" aria-hidden />
          </Button>
        ))}
        {!compact && <Button aria-label="Evento anterior" title="Evento anterior" className="px-2" onClick={() => toEvent(-1)} disabled={stepEvent(eventTimes, position, -1) === undefined}>
          <SkipBack className="size-4" aria-hidden />
        </Button>}
        {!compact && <Button aria-label="Evento siguiente" title="Evento siguiente" className="px-2" onClick={() => toEvent(1)} disabled={stepEvent(eventTimes, position, 1) === undefined}>
          <SkipForward className="size-4" aria-hidden />
        </Button>}
        <Select aria-label="Velocidad" className={compact ? "min-h-11 w-auto text-xs" : "w-auto py-1 text-xs"} value={String(speed)} onChange={(e) => setSpeed(Number(e.target.value))}>
          {REC_SPEEDS.map((s) => (
            <option key={s} value={s}>
              {s}×
            </option>
          ))}
        </Select>
        <Button onClick={goNow} title="Ir al último momento grabado">
          Ahora
        </Button>

        {/* IN / OUT Range Selection Controls */}
        <div className="inline-flex items-center gap-1 border-l border-line/60 pl-1.5 ml-0.5">
          <Button
            variant={inTime !== undefined ? "tonal" : "outlined"}
            className="px-2 py-1 font-mono text-xs font-semibold"
            aria-label="Marcar punto inicial IN"
            title="Marcar punto inicial IN (tecla I)"
            onClick={() => setInTime(position)}
          >
            IN
          </Button>
          <Button
            variant={outTime !== undefined ? "tonal" : "outlined"}
            className="px-2 py-1 font-mono text-xs font-semibold"
            aria-label="Marcar punto final OUT"
            title="Marcar punto final OUT (tecla O)"
            onClick={() => setOutTime(position)}
          >
            OUT
          </Button>

          {(inTime !== undefined || outTime !== undefined) && (
            <div className="inline-flex items-center gap-1 rounded-full bg-surface-2 px-2.5 py-1 text-xs font-mono border border-line/60">
              {inTime !== undefined && <span>IN: {clockTime(inTime)}</span>}
              {inTime !== undefined && outTime !== undefined && <span className="text-muted">·</span>}
              {outTime !== undefined && <span>OUT: {clockTime(outTime)}</span>}
              {selection && <span className="text-primary font-semibold">({durationLabel(selection.end - selection.start)})</span>}
              <button
                type="button"
                onClick={clearSelection}
                className="ml-1 rounded-full p-0.5 hover:bg-on-surface/10 text-muted hover:text-ink"
                title="Limpiar selección (Esc)"
                aria-label="Limpiar selección"
              >
                <X className="size-3" />
              </button>
            </div>
          )}

          {selection && (
            <Button
              variant="filled"
              className="gap-1 px-2.5 py-1 text-xs"
              title="Exportar tramo seleccionado"
              onClick={() => setExportOpen(true)}
            >
              <Download className="size-3.5" />
              Exportar
            </Button>
          )}
        </div>

        <DayCalendar value={new Date(day * 1000)} max={new Date(now * 1000)} onChange={(d) => seek(moveToDay(position, d, liveEdge), { play: playing })} />
        <span className="ml-auto font-mono text-xs" aria-live="off" data-testid="rec-clock">
          {fmtDateTime(new Date(position * 1000))}
        </span>
        {!compact && <details className="relative">
          <summary className="flex cursor-pointer list-none items-center rounded p-1 text-muted hover:text-ink" aria-label="Atajos de teclado" title="Atajos de teclado">
            <CircleHelp className="size-4" aria-hidden />
          </summary>
          <div className="absolute right-0 bottom-full z-30 mb-1 w-64 rounded-lg border border-line bg-surface p-2 text-xs shadow-lg">
            <p className="mb-1 font-medium">Atajos</p>
            <ul className="flex flex-col gap-0.5 text-muted">
              <li>Espacio: reproducir / pausar</li>
              <li>← / →: ±10 s</li>
              <li>Mayús + ← / →: ±1 min</li>
              <li>I: marcar inicio (IN)</li>
              <li>O: marcar fin (OUT)</li>
              <li>Esc: limpiar selección de tramo</li>
              <li>Rueda sobre la línea: acercar / alejar</li>
              <li>Arrastrar: desplazar · Clic: ir a ese momento</li>
              <li>Mayús + arrastrar (o el cabezal): buscar</li>
            </ul>
          </div>
        </details>}
      </div>
      <DayTimeline
        day={day}
        cameras={cameras}
        events={events}
        position={position}
        now={now}
        onSeek={(t, detection) => seek(Math.min(t, liveEdge), detection?.id ? { poster: { cameraId: detection.cameraId, eventId: detection.id } } : undefined)}
        selectedId={selectedId}
        onSelectCamera={onSelectCamera}
        selection={selection}
      />
      {selection && (
        <LiveExportModal
          open={exportOpen}
          onClose={() => setExportOpen(false)}
          range={selection}
          cameras={cameras}
          selectedCameraId={selectedId}
        />
      )}
    </section>
  );
}
