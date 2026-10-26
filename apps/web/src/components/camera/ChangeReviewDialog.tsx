import { useState } from "react";
import { ApiError } from "@/api/client";
import { Modal } from "@/components/Modal";
import { Button } from "@/components/ui";
import { plainErrorOf } from "@/lib/plainError";
import { RollbackOffer } from "./advanced/rollback";
import type { CameraConfigChange } from "@/lib/useCameraConfigChange";

const fmt = (v: unknown): string => (v === undefined || v === null ? "—" : typeof v === "boolean" ? (v ? "ON" : "OFF") : Array.isArray(v) ? v.join(", ") : typeof v === "object" ? JSON.stringify(v) : String(v));

/** Plain-Spanish failure message plus the engine text kept behind "Ver información técnica". */
function failure(change: CameraConfigChange): { message: string; technical: string } | null {
  const { apply, review } = change;
  if (apply.data?.result === "error") {
    const plain = plainErrorOf(apply.data);
    return { message: plain?.message ?? "No se pudo aplicar la configuración.", technical: plain?.technical || "El motor rechazó el cambio." };
  }
  const err = apply.error ?? review.error;
  if (!err) return null;
  if (err instanceof ApiError && err.status === 409) return null; // shown as the stale-revision notice
  return { message: apply.error ? "No se pudo aplicar la configuración." : "No se pudieron calcular los cambios.", technical: err.message };
}

/** Review before applying: "Cambios" and "Impacto" from the review response, then "Aplicar cambios". */
export function ChangeReviewDialog({ change, onClose }: { change: CameraConfigChange; onClose: () => void }) {
  const { review, apply, staleMessage } = change;
  const [showTech, setShowTech] = useState(false);
  const data = review.data;
  const blocked = (data?.unsupported.length ?? 0) > 0;
  const fail = failure(change);
  const cameraId = change.config.data?.identity?.camera_id;
  const applied = apply.data && apply.data.result !== "error";
  return (
    <Modal title="Revisar cambios" onClose={onClose}>
      {review.isPending && <p role="status" className="text-sm text-muted">Calculando cambios…</p>}
      {data && (
        <>
          <section aria-label="Cambios">
            <h3 className="text-sm font-semibold">Cambios</h3>
            {data.changes.length === 0 ? <p className="text-sm text-muted">No hay cambios para aplicar.</p> : (
              <ul className="list-disc pl-5 text-sm">
                {data.changes.map((c) => <li key={c.path}>{c.label}: {fmt(c.from)} → {fmt(c.to)}</li>)}
              </ul>
            )}
          </section>
          {data.impact.length > 0 && (
            <section aria-label="Impacto">
              <h3 className="text-sm font-semibold">Impacto</h3>
              <ul className="list-disc pl-5 text-sm">{data.impact.map((i) => <li key={i.code}>{i.message}</li>)}</ul>
            </section>
          )}
          {blocked && (
            <div role="alert" className="rounded-m3-lg bg-warn/10 px-4 py-3 text-sm text-warn">
              <p>Este motor no puede aplicar estos cambios:</p>
              <ul className="list-disc pl-5">{data.unsupported.map((u) => <li key={u}>{u}</li>)}</ul>
            </div>
          )}
        </>
      )}
      {staleMessage && <p role="alert" className="rounded-m3-lg bg-warn/10 px-4 py-3 text-sm text-warn">{staleMessage} Tus cambios siguen aquí: revisa de nuevo y aplica.</p>}
      {applied && <p role="status" className="text-sm text-ok">Cambios aplicados.</p>}
      {fail && (
        <div role="alert" className="rounded-m3-lg bg-bad/12 px-4 py-3 text-sm text-bad">
          <p>{fail.message}</p>
          <button type="button" className="mt-1 text-xs underline" aria-expanded={showTech} onClick={() => setShowTech((s) => !s)}>Ver información técnica</button>
          {showTech && <pre className="mt-1 whitespace-pre-wrap break-words text-xs">{fail.technical}</pre>}
        </div>
      )}
      {apply.data?.result === "error" && cameraId && <RollbackOffer cameraId={cameraId} onRestored={onClose} />}
      <div className="flex justify-end gap-2">
        <Button variant="text" onClick={onClose}>{applied ? "Cerrar" : "Cancelar"}</Button>
        {!applied && (
          <Button variant="primary" disabled={!data || blocked || data.changes.length === 0 || apply.isPending} onClick={() => apply.mutate(undefined)}>
            {apply.isPending ? "Aplicando…" : "Aplicar cambios"}
          </Button>
        )}
      </div>
    </Modal>
  );
}
