import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import type { Schemas } from "@/api/client";
import { cameraConfigHistoryQuery, cameraConfigQuery, rollbackCameraConfig } from "@/api/queries";
import { Modal } from "@/components/Modal";
import { Button } from "@/components/ui";
import { plainErrorOf } from "@/lib/plainError";
import { PlainErrorNote } from "./TechnicalInfo";

export type HistoryEntry = Schemas["CameraConfigHistoryEntry"];

export const fmtValue = (v: unknown): string =>
  v === undefined || v === null ? "—" : typeof v === "boolean" ? (v ? "ON" : "OFF") : Array.isArray(v) ? v.join(", ") : typeof v === "object" ? JSON.stringify(v) : String(v);

/** History of a camera plus the rollback action ("Restaurar configuración anterior"). */
export function useCameraRollback(cameraId: string) {
  const qc = useQueryClient();
  const history = useQuery(cameraConfigHistoryQuery(cameraId));
  const config = useQuery(cameraConfigQuery(cameraId));
  const rollback = useMutation({
    mutationFn: (auditId?: number) => rollbackCameraConfig(cameraId, { expected_revision: config.data?.state.desired_revision ?? 0, ...(auditId ? { audit_id: auditId } : {}) }),
    onSettled: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["cameras", cameraId, "config-model"] }),
        qc.invalidateQueries({ queryKey: ["cameras", cameraId, "config-history"] }),
        qc.invalidateQueries({ queryKey: ["cameras", cameraId, "advanced"] }),
      ]);
    },
  });
  /** What a rollback without audit_id would undo: the newest entry that can be undone. */
  const latest = history.data?.find((e) => e.rollbackable);
  return { history, rollback, latest };
}

/** Confirmation that spells out what will be restored (old values of the change being undone). */
export function RollbackConfirm({ entry, pending, error, onConfirm, onClose }: { entry: HistoryEntry; pending: boolean; error: unknown; onConfirm: () => void; onClose: () => void }) {
  return (
    <Modal title="Restaurar configuración anterior" onClose={onClose}>
      <p className="text-sm">Se van a restaurar estos valores:</p>
      <ul aria-label="Valores a restaurar" className="list-disc pl-5 text-sm">
        {entry.changes.map((c) => <li key={c.path}>{c.path}: {fmtValue(c.to)} → {fmtValue(c.from)}</li>)}
      </ul>
      <p className="text-xs text-muted">Se aplica como un cambio nuevo, con revisión y registro de auditoría.</p>
      {error ? <PlainErrorNote error={{ message: "No se pudo restaurar la configuración.", technical: error instanceof Error ? error.message : String(error) }} /> : null}
      <div className="flex justify-end gap-2">
        <Button variant="text" onClick={onClose}>Cancelar</Button>
        <Button variant="primary" disabled={pending} onClick={onConfirm}>{pending ? "Restaurando…" : "Restaurar"}</Button>
      </div>
    </Modal>
  );
}

/**
 * "Restaurar configuración anterior" for a failed apply: offered only when the history holds a change
 * that can be undone. Mounts its own queries, so render it only when the offer is relevant.
 */
export function RollbackOffer({ cameraId, onRestored }: { cameraId: string; onRestored?: () => void }) {
  const { latest, rollback } = useCameraRollback(cameraId);
  const [confirming, setConfirming] = useState(false);
  const [done, setDone] = useState<string | null>(null);
  if (done) return <p role="status" className="text-sm text-ok">{done}</p>;
  if (!latest) return null;
  const outcome = rollback.data?.result;
  const failed = outcome ? plainErrorOf(outcome) : null;
  return (
    <div className="flex flex-col gap-2">
      <p className="text-sm">Podés volver a la configuración anterior.</p>
      <div><Button variant="secondary" onClick={() => { rollback.reset(); setConfirming(true); }}>Restaurar configuración anterior</Button></div>
      {confirming && (
        <RollbackConfirm
          entry={latest}
          pending={rollback.isPending}
          error={rollback.error ?? (failed ? new Error(failed.technical || failed.message) : null)}
          onClose={() => setConfirming(false)}
          onConfirm={() => rollback.mutate(undefined, {
            onSuccess: (r) => {
              if (r.result.result === "error") return;
              setConfirming(false);
              setDone("Configuración restaurada.");
              onRestored?.();
            },
          })}
        />
      )}
    </div>
  );
}
