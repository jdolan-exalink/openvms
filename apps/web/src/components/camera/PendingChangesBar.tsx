import { Button } from "@/components/ui";

/** Sticky bar shown while the draft differs from the saved config: "3 cambios sin guardar". */
export function PendingChangesBar({ count, errorCount = 0, onDiscard, onReview, onSave }: { count: number; errorCount?: number; onDiscard: () => void; onReview: () => void; onSave: () => void }) {
  if (count === 0) return null;
  const blocked = errorCount > 0;
  return (
    <div className="sticky bottom-0 z-10 flex flex-wrap items-center justify-between gap-2 rounded-m3-xl bg-surface-3 px-5 py-3 shadow-md">
      <span role="status" className="text-sm text-muted">
        {count} {count === 1 ? "cambio" : "cambios"} sin guardar
        {blocked && <span className="text-bad"> · {errorCount} {errorCount === 1 ? "error" : "errores"}</span>}
      </span>
      <div className="flex gap-2">
        <Button variant="text" onClick={onDiscard}>Descartar</Button>
        <Button variant="secondary" disabled={blocked} onClick={onReview}>Revisar cambios</Button>
        <Button variant="primary" disabled={blocked} onClick={onSave}>Guardar</Button>
      </div>
    </div>
  );
}
