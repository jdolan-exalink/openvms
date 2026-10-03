import { ChevronsLeft, ChevronsRight, CircleHelp, Pause, Play, SkipBack, SkipForward, StepBack, StepForward } from "lucide-react";
import { useEffect, useMemo, useSyncExternalStore } from "react";
import { DayCalendar } from "@/components/DayCalendar";
import { DayTimeline, type TimelineCamera, type TimelineEvent } from "@/components/DayTimeline";
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

/**
 * LiveRecDock is the transport of the Live REC mode: play/pause, speed, steps, event
 * navigation, "Ahora", the day picker and the zoomable day timeline. It only asks the
 * transport to seek; the tiles and the drift correction do the rest.
 */
export function LiveRecDock({
  transport,
  cameras,
  events,
  now,
  selectedId,
  onSelectCamera,
}: {
  transport: RecTransport;
  cameras: TimelineCamera[];
  events: TimelineEvent[];
  now: number;
  selectedId?: string;
  onSelectCamera?: (cameraId: string) => void;
}) {
  const { seek, play, pause, playing, speed, setSpeed, day, getPosition, subscribePosition } = transport;
  const position = useSyncExternalStore(subscribePosition, getPosition);
  const eventTimes = useMemo(() => events.map((e) => e.time), [events]);

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
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  return (
    <section aria-label="Controles de grabación" className="flex flex-col gap-2 max-h-[40vh] rounded-xl border border-line bg-surface p-2">
      <div className="flex flex-wrap items-center gap-1.5">
        <Button variant="primary" aria-label={playing ? "Pausar" : "Reproducir"} title={playing ? "Pausar (Espacio)" : "Reproducir (Espacio)"} onClick={() => (playing ? pause() : play())}>
          {playing ? <Pause className="size-4" aria-hidden /> : <Play className="size-4" aria-hidden />}
        </Button>
        {STEPS.map(({ label, seconds, Icon }) => (
          <Button key={label} aria-label={label} title={label} className="px-2" onClick={() => step(seconds)}>
            <Icon className="size-4" aria-hidden />
          </Button>
        ))}
        <Button aria-label="Evento anterior" title="Evento anterior" className="px-2" onClick={() => toEvent(-1)} disabled={stepEvent(eventTimes, position, -1) === undefined}>
          <SkipBack className="size-4" aria-hidden />
        </Button>
        <Button aria-label="Evento siguiente" title="Evento siguiente" className="px-2" onClick={() => toEvent(1)} disabled={stepEvent(eventTimes, position, 1) === undefined}>
          <SkipForward className="size-4" aria-hidden />
        </Button>
        <Select aria-label="Velocidad" className="w-auto py-1 text-xs" value={String(speed)} onChange={(e) => setSpeed(Number(e.target.value))}>
          {REC_SPEEDS.map((s) => (
            <option key={s} value={s}>
              {s}×
            </option>
          ))}
        </Select>
        <Button onClick={goNow} title="Ir al último momento grabado">
          Ahora
        </Button>
        <DayCalendar value={new Date(day * 1000)} max={new Date(now * 1000)} onChange={(d) => seek(moveToDay(position, d, liveEdge), { play: playing })} />
        <span className="ml-auto font-mono text-xs" aria-live="off" data-testid="rec-clock">
          {fmtDateTime(new Date(position * 1000))}
        </span>
        <details className="relative">
          <summary className="flex cursor-pointer list-none items-center rounded p-1 text-muted hover:text-ink" aria-label="Atajos de teclado" title="Atajos de teclado">
            <CircleHelp className="size-4" aria-hidden />
          </summary>
          <div className="absolute right-0 bottom-full z-30 mb-1 w-64 rounded-lg border border-line bg-surface p-2 text-xs shadow-lg">
            <p className="mb-1 font-medium">Atajos</p>
            <ul className="flex flex-col gap-0.5 text-muted">
              <li>Espacio: reproducir / pausar</li>
              <li>← / →: ±10 s</li>
              <li>Mayús + ← / →: ±1 min</li>
              <li>Rueda sobre la línea: acercar / alejar</li>
              <li>Arrastrar: desplazar · Clic: ir a ese momento</li>
              <li>Mayús + arrastrar (o el cabezal): buscar</li>
            </ul>
          </div>
        </details>
      </div>
      <DayTimeline day={day} cameras={cameras} events={events} position={position} now={now} onSeek={(t, detection) => seek(Math.min(t, liveEdge), detection?.id ? { poster: { cameraId: detection.cameraId, eventId: detection.id } } : undefined)} selectedId={selectedId} onSelectCamera={onSelectCamera} />
    </section>
  );
}
