import { Button } from "@/components/ui";

/**
 * Sticky bar shown while a draft differs from the saved config: "3 cambios sin guardar".
 * Two change sources coexist: `count` (Frigate sections) and `cameraCount` (normalized camera
 * config). Revisar cambios / Guardar open the review of the Frigate source when it has changes,
 * otherwise the camera one; with both pending, "Revisar cambios de la cámara" opens the other
 * dialog so neither source is ever applied silently.
 */
export function PendingChangesBar({ count, cameraCount = 0, errorCount = 0, onDiscard, onReview, onSave, onReviewCamera }: { count: number; cameraCount?: number; errorCount?: number; onDiscard: () => void; onReview: () => void; onSave: () => void; onReviewCamera?: () => void }) {
  const total = count + cameraCount;
  if (total === 0) return null;
  const blocked = errorCount > 0;
  const both = count > 0 && cameraCount > 0;
  return (
    <div className="sticky bottom-0 z-10 flex flex-wrap items-center justify-between gap-2 rounded-m3-xl bg-surface-3 px-5 py-3 shadow-md">
      <span role="status" className="text-sm text-muted">
        {total} {total === 1 ? "cambio" : "cambios"} sin guardar
        {blocked && <span className="text-bad"> · {errorCount} {errorCount === 1 ? "error" : "errores"}</span>}
      </span>
      <div className="flex gap-2">
        <Button variant="text" onClick={onDiscard}>Descartar</Button>
        {both && onReviewCamera && <Button variant="secondary" onClick={onReviewCamera}>Revisar cambios de la cámara</Button>}
        <Button variant="secondary" disabled={blocked && count > 0} onClick={onReview}>Revisar cambios</Button>
        <Button variant="primary" disabled={blocked && count > 0} onClick={onSave}>Guardar</Button>
      </div>
    </div>
  );
}
