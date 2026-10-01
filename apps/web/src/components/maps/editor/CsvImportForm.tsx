import { useState } from "react";
import type { Schemas } from "@/api/client";
import { importPlacements } from "@/lib/maps/import";

export interface CsvImportFormProps {
  siteId: string;
  onClose: () => void;
  /**
   * Fired after an apply actually wrote placements: the caller refetches the site
   * entities and the unplaced tray. Dry runs and rejected applies never call it.
   */
  onApplied?: () => void;
}

type Report = Schemas["MapImportReport"];

/**
 * CsvImportForm is the M-B8 CSV import: paste a camera,lat,lng table, validate it as a
 * dry run first, then apply. The backend applies atomically, so a report with errors
 * means nothing was written — the form stays open with the offending lines to fix.
 */
export function CsvImportForm({ siteId, onClose, onApplied }: CsvImportFormProps) {
  const [csv, setCsv] = useState("");
  const [report, setReport] = useState<Report>();
  const [error, setError] = useState<string>();
  const [pending, setPending] = useState(false);

  const run = async (dryRun: boolean) => {
    if (pending || csv.trim().length === 0) return;
    setPending(true);
    setError(undefined);
    try {
      const result = await importPlacements(siteId, csv, dryRun);
      setReport(result);
      // A clean apply changed the server: refresh the lists and step out of the way.
      if (!result.dry_run && result.errors.length === 0) {
        onApplied?.();
        onClose();
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setPending(false);
    }
  };

  return (
    <section aria-label="Importar CSV" className="w-80 rounded border border-line bg-surface p-3 shadow-sm">
      <header className="mb-2 flex items-center justify-between">
        <h2 className="text-sm font-semibold">Importar posiciones (CSV)</h2>
        <button
          type="button"
          aria-label="Cerrar importación"
          onClick={onClose}
          className="rounded p-1 text-muted hover:bg-raised hover:text-ink"
        >
          ×
        </button>
      </header>

      <p className="mb-2 text-xs text-muted">
        Columnas camera,lat,lng (bearing,fov,range son opcionales). Validá antes de aplicar:
        la importación es por sitio y reemplaza las posiciones existentes.
      </p>

      <textarea
        aria-label="CSV de posiciones"
        rows={6}
        value={csv}
        onChange={event => setCsv(event.target.value)}
        placeholder={"camera,lat,lng\nAcceso Norte,-34.6,-58.4"}
        className="mb-2 w-full rounded border border-line bg-bg px-2 py-1 font-mono text-xs text-ink"
      />

      {error && <p role="alert" className="mb-2 text-xs text-bad">{error}</p>}

      {report && (
        <div className="mb-2 rounded border border-line bg-bg p-2 text-xs">
          <p className="text-ink">
            {report.rows} filas
            {report.dry_run ? " (solo validación)" : `, ${report.upserted} aplicadas`}
          </p>
          {report.errors.length === 0 ? (
            <p className="text-muted">Sin errores</p>
          ) : (
            <ul className="mt-1 list-inside list-disc text-bad">
              {report.errors.map(item => (
                <li key={`${item.line}:${item.message}`}>línea {item.line}: {item.message}</li>
              ))}
            </ul>
          )}
        </div>
      )}

      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          disabled={pending || csv.trim().length === 0}
          onClick={() => void run(true)}
          className="rounded border border-line px-2 py-1 text-xs text-ink disabled:opacity-50"
        >
          Validar
        </button>
        <button
          type="button"
          disabled={pending || csv.trim().length === 0}
          onClick={() => void run(false)}
          className="rounded bg-accent px-2 py-1 text-xs font-medium text-white disabled:opacity-50"
        >
          Importar
        </button>
        <button
          type="button"
          onClick={onClose}
          className="rounded border border-line px-2 py-1 text-xs text-ink"
        >
          Cancelar
        </button>
      </div>
    </section>
  );
}
