import { ArrowLeftToLine, ArrowRightToLine, Download, Scissors, X } from "lucide-react";
import { Button } from "@/components/ui";
import { canPressEnd, canPressStart, exportRange, type ExportMarks, type MarkPress } from "@/lib/exportMarks";

const pad = (n: number) => String(n).padStart(2, "0");
const clockTime = (t: number) => {
  const d = new Date(t * 1000);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};
const durationLabel = (s: number) => (s < 60 ? `${Math.round(s)}s` : `${Math.floor(s / 60)}m ${Math.round(s % 60)}s`);

/** The one-line hint under the buttons: what to do next, given what is marked and what is being placed. */
export function exportHint(m: ExportMarks, refused?: MarkPress["refused"]): string {
  if (refused === "end_before_start") return "El fin tiene que ser posterior al inicio: llevá la línea más adelante y tocá Fin de nuevo.";
  if (refused === "start_after_end") return "El inicio tiene que ser anterior al fin: llevá la línea más atrás y tocá Inicio de nuevo.";
  if (m.placing === "start") return "Movés la línea hasta donde empieza el tramo y tocá Inicio de nuevo para fijarlo.";
  if (m.placing === "end") return "Movés la línea hasta donde termina el tramo y tocá Fin de nuevo para fijarlo.";
  if (m.start === undefined) return "Tocá Inicio y llevá la línea de tiempo hasta donde empieza el tramo.";
  if (m.end === undefined) return "Inicio fijado. Ahora tocá Fin para marcar dónde termina el tramo.";
  if (m.end <= m.start) return "Inicio y Fin no están en orden: volvé a marcar alguno.";
  return "Listo: exportá el tramo, o tocá Inicio o Fin para volver a moverlos.";
}

/**
 * RangeExportBox is the framed "Exportación" control of the REC dock. Inicio and Fin work in two presses: the
 * first lets the mark follow the timeline cursor, the second fixes it. Fin needs a fixed Inicio, neither can be
 * pressed while the other is being placed, and Exportar needs both marks fixed.
 */
export function RangeExportBox({
  marks,
  refused,
  onPressStart,
  onPressEnd,
  onClear,
  onExport,
}: {
  marks: ExportMarks;
  refused?: MarkPress["refused"];
  onPressStart: () => void;
  onPressEnd: () => void;
  onClear: () => void;
  onExport: () => void;
}) {
  const range = exportRange(marks);
  const startPlacing = marks.placing === "start";
  const endPlacing = marks.placing === "end";
  const startFixed = marks.start !== undefined && !startPlacing;
  const endFixed = marks.end !== undefined && !endPlacing;
  const startLabel = startPlacing ? "Fijar inicio" : startFixed ? "Mover inicio" : "Marcar inicio";
  const endLabel = endPlacing ? "Fijar fin" : endFixed ? "Mover fin" : "Marcar fin";
  const endBlockedTitle = startPlacing ? "Primero fijá el inicio: tocá Inicio de nuevo" : "Primero marcá el inicio";

  return (
    <fieldset className="flex min-w-0 flex-col gap-1 rounded-lg border border-line/70 px-2 pb-1.5 pt-0.5">
      <legend className="flex items-center gap-1 px-1 text-xs font-medium text-muted">
        <Scissors className="size-3.5" aria-hidden /> Exportación
      </legend>
      <div className="flex flex-wrap items-center gap-1.5">
        <Button
          variant={startPlacing ? "filled" : startFixed ? "tonal" : "outlined"}
          className="gap-1 px-2 py-1 text-xs"
          aria-label={startLabel}
          aria-pressed={startPlacing}
          disabled={!canPressStart(marks)}
          title={
            !canPressStart(marks)
              ? "Fijá primero el fin"
              : startPlacing
                ? "Tocá de nuevo para fijar el inicio donde está la línea (tecla I)"
                : startFixed
                  ? `Inicio fijado a las ${clockTime(marks.start!)}. Tocá para volver a moverlo (tecla I)`
                  : "Marcar inicio: después movés la línea y tocás de nuevo para fijarlo (tecla I)"
          }
          onClick={onPressStart}
        >
          <ArrowRightToLine className="size-3.5" aria-hidden />
          Inicio{startFixed && <span className="font-mono">{clockTime(marks.start!)}</span>}
        </Button>
        <Button
          variant={endPlacing ? "filled" : endFixed ? "tonal" : "outlined"}
          className="gap-1 px-2 py-1 text-xs"
          aria-label={endLabel}
          aria-pressed={endPlacing}
          disabled={!canPressEnd(marks)}
          title={
            !canPressEnd(marks)
              ? endBlockedTitle
              : endPlacing
                ? "Tocá de nuevo para fijar el fin donde está la línea (tecla O)"
                : endFixed
                  ? `Fin fijado a las ${clockTime(marks.end!)}. Tocá para volver a moverlo (tecla O)`
                  : "Marcar fin: después movés la línea y tocás de nuevo para fijarlo (tecla O)"
          }
          onClick={onPressEnd}
        >
          <ArrowLeftToLine className="size-3.5" aria-hidden />
          Fin{endFixed && <span className="font-mono">{clockTime(marks.end!)}</span>}
        </Button>
        {range && <span className="font-mono text-xs font-semibold text-primary" data-testid="export-duration">{durationLabel(range.end - range.start)}</span>}
        {(marks.start !== undefined || marks.end !== undefined || marks.placing) && (
          <button type="button" onClick={onClear} className="rounded-full p-0.5 text-muted hover:bg-on-surface/10 hover:text-ink" title="Limpiar marcas (Esc)" aria-label="Limpiar selección">
            <X className="size-3.5" aria-hidden />
          </button>
        )}
        <Button
          variant="filled"
          className="gap-1 px-2.5 py-1 text-xs"
          disabled={!range}
          title={range ? "Exportar el tramo marcado" : "Fijá Inicio y Fin para poder exportar"}
          onClick={onExport}
        >
          <Download className="size-3.5" aria-hidden />
          Exportar
        </Button>
      </div>
      <p className="text-[11px] leading-tight text-muted" role="status">{exportHint(marks, refused)}</p>
    </fieldset>
  );
}
