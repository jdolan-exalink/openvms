import { ArrowLeftToLine, ArrowRightToLine, Download, Scissors, X } from "lucide-react";
import { Button } from "@/components/ui";

const pad = (n: number) => String(n).padStart(2, "0");
const clockTime = (t: number) => {
  const d = new Date(t * 1000);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};
const durationLabel = (s: number) => (s < 60 ? `${Math.round(s)}s` : `${Math.floor(s / 60)}m ${Math.round(s % 60)}s`);

/** The one-line hint under the buttons: what to do next, given what is already marked. */
function hint(start?: number, end?: number): string {
  if (start === undefined && end === undefined) return "Ubicá la línea de tiempo donde empieza el tramo y tocá Inicio.";
  if (end === undefined) return "Ahora llevá la línea a donde termina el tramo y tocá Fin.";
  if (start === undefined) return "Ahora llevá la línea a donde empieza el tramo y tocá Inicio.";
  if (end === start) return "Inicio y Fin coinciden: movelos para elegir un tramo.";
  return "Listo: exportá el tramo o ajustá Inicio y Fin.";
}

/**
 * RangeExportBox is the framed "Exportación" control of the REC dock: mark where a clip starts and ends on the
 * timeline, see the chosen range, and export it.
 */
export function RangeExportBox({
  start,
  end,
  selection,
  onMarkStart,
  onMarkEnd,
  onClear,
  onExport,
}: {
  start?: number;
  end?: number;
  selection?: { start: number; end: number };
  onMarkStart: () => void;
  onMarkEnd: () => void;
  onClear: () => void;
  onExport: () => void;
}) {
  return (
    <fieldset className="flex min-w-0 flex-col gap-1 rounded-lg border border-line/70 px-2 pb-1.5 pt-0.5">
      <legend className="flex items-center gap-1 px-1 text-xs font-medium text-muted">
        <Scissors className="size-3.5" aria-hidden /> Exportación
      </legend>
      <div className="flex flex-wrap items-center gap-1.5">
        <Button
          variant={start !== undefined ? "tonal" : "outlined"}
          className="gap-1 px-2 py-1 text-xs"
          aria-label="Marcar inicio"
          title={start !== undefined ? `Inicio marcado a las ${clockTime(start)}. Tocá para moverlo a la posición actual (tecla I)` : "Marcar inicio en la posición actual (tecla I)"}
          onClick={onMarkStart}
        >
          <ArrowRightToLine className="size-3.5" aria-hidden />
          Inicio{start !== undefined && <span className="font-mono">{clockTime(start)}</span>}
        </Button>
        <Button
          variant={end !== undefined ? "tonal" : "outlined"}
          className="gap-1 px-2 py-1 text-xs"
          aria-label="Marcar fin"
          title={end !== undefined ? `Fin marcado a las ${clockTime(end)}. Tocá para moverlo a la posición actual (tecla O)` : "Marcar fin en la posición actual (tecla O)"}
          onClick={onMarkEnd}
        >
          <ArrowLeftToLine className="size-3.5" aria-hidden />
          Fin{end !== undefined && <span className="font-mono">{clockTime(end)}</span>}
        </Button>
        {selection && <span className="font-mono text-xs font-semibold text-primary" data-testid="export-duration">{durationLabel(selection.end - selection.start)}</span>}
        {(start !== undefined || end !== undefined) && (
          <button type="button" onClick={onClear} className="rounded-full p-0.5 text-muted hover:bg-on-surface/10 hover:text-ink" title="Limpiar selección (Esc)" aria-label="Limpiar selección">
            <X className="size-3.5" aria-hidden />
          </button>
        )}
        {selection && (
          <Button variant="filled" className="gap-1 px-2.5 py-1 text-xs" title="Exportar el tramo marcado" onClick={onExport}>
            <Download className="size-3.5" aria-hidden />
            Exportar
          </Button>
        )}
      </div>
      <p className="text-[11px] leading-tight text-muted">{hint(start, end)}</p>
    </fieldset>
  );
}
