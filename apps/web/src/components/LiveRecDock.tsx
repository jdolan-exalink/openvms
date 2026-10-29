import { ChevronsLeft, ChevronsRight, CircleHelp, Pause, Play, SkipBack, SkipForward, StepBack, StepForward } from "lucide-react";
import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import { DayCalendar } from "@/components/DayCalendar";
import { useRecordingDays } from "@/lib/useRecordingDays";
import { DayTimeline, type TimelineCamera, type TimelineEvent } from "@/components/DayTimeline";
import { LiveExportModal } from "@/components/LiveExportModal";
import { RangeExportBox } from "@/components/RangeExportBox";
import { TimelineLegend } from "@/components/TimelineLegend";
import { Button, Select } from "@/components/ui";
import { fmtDateTime } from "@/lib/format";
import { escapeMarks, exportRange, pressEnd, pressStart, shownMarks, type ExportMarks, type MarkPress } from "@/lib/exportMarks";
import { moveToDay, REC_LIVE_EDGE_S, REC_NOW_BACK_S, REC_SPEEDS, stepEvent } from "@/lib/liveRec";
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

  const recordingDays = useRecordingDays(selectedId ?? cameras[0]?.id, new Date(day * 1000));

  const [marks, setMarks] = useState<ExportMarks>({});
  const [refused, setRefused] = useState<MarkPress["refused"]>();
  const [exportOpen, setExportOpen] = useState(false);

  const selection = useMemo(() => exportRange(marks), [marks]);
  // What the timeline draws: a mark being placed follows the cursor.
  const shown = shownMarks(marks, position);
  const timelineMarks = useMemo(
    () => ({ start: shown.start, end: shown.end, placing: marks.placing }),
    [shown.start, shown.end, marks.placing],
  );
  const timelineSelection = useMemo(
    () => (shown.start !== undefined && shown.end !== undefined && shown.end > shown.start ? { start: shown.start, end: shown.end } : undefined),
    [shown.start, shown.end],
  );

  const apply = (r: MarkPress) => {
    setMarks(r.marks);
    setRefused(r.refused);
  };
  const markStart = () => apply(pressStart(marks, position));
  const markEnd = () => apply(pressEnd(marks, position));
  const clearSelection = () => apply({ marks: {} });

  const liveEdge = now - REC_LIVE_EDGE_S;
  // "Ahora" goes to today, a quarter of an hour before the current time.
  const goNow = () => seek(Math.min(now - REC_NOW_BACK_S, liveEdge));
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
        markStart();
      } else if (event.key === "o" || event.key === "O") {
        event.preventDefault();
        markEnd();
      } else if (event.key === "Escape" && (marks.start !== undefined || marks.end !== undefined || marks.placing)) {
        event.preventDefault();
        apply({ marks: escapeMarks(marks) });
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
        <Button onClick={goNow} title="Ir a hoy, 15 minutos antes de la hora actual">
          Ahora
        </Button>
        <DayCalendar value={new Date(day * 1000)} max={new Date(now * 1000)} marked={recordingDays.marked} onMonthChange={recordingDays.onMonthChange} onChange={(d) => seek(moveToDay(position, d, liveEdge), { play: playing })} />

        <RangeExportBox marks={marks} refused={refused} onPressStart={markStart} onPressEnd={markEnd} onClear={clearSelection} onExport={() => setExportOpen(true)} />

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
              <li>I: marcar inicio (otra vez para fijarlo)</li>
              <li>O: marcar fin (otra vez para fijarlo)</li>
              <li>Esc: dejar de mover la marca; otra vez, limpiar</li>
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
        onSeek={(t, detection) => seek(Math.min(t, liveEdge), { play: playing, ...(detection?.id ? { poster: { cameraId: detection.cameraId, eventId: detection.id } } : {}) })}
        selectedId={selectedId}
        onSelectCamera={onSelectCamera}
        selection={timelineSelection}
        marks={timelineMarks}
      />
      <TimelineLegend />
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
