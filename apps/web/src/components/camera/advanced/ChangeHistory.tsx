import { useState } from "react";
import { Button } from "@/components/ui";
import { fmtValue, type HistoryEntry, RollbackConfirm, useCameraRollback } from "./rollback";
import { PlainErrorNote } from "./TechnicalInfo";

const ORIGIN: Record<string, string> = { ui: "Interfaz", yaml: "YAML", profile: "Perfil", group: "Grupo", api: "API", sync: "Sincronización", external_device: "Dispositivo externo" };
const RESULT: Record<HistoryEntry["result"], string> = { ok: "Aplicado", unverified: "Sin verificar", error: "Falló" };

/** "Historial de cambios": who changed what, with "Restaurar configuración anterior" where it is possible. */
export function ChangeHistory({ cameraId, canRollback }: { cameraId: string; canRollback: boolean }) {
  const { history, rollback } = useCameraRollback(cameraId);
  const [target, setTarget] = useState<HistoryEntry | null>(null);
  const rolledBack = rollback.data && rollback.data.result.result !== "error";
  return (
    <section aria-label="Historial de cambios" className="flex flex-col gap-3">
      <h3 className="text-sm font-bold">Historial de cambios</h3>
      {history.isLoading && <p role="status" className="text-sm text-muted">Cargando historial…</p>}
      {history.isError && <p role="alert" className="text-sm text-bad">No se pudo cargar el historial de cambios.</p>}
      {history.data && history.data.length === 0 && <p className="text-sm text-muted">Todavía no hay cambios registrados.</p>}
      {rolledBack && <p role="status" className="text-sm text-ok">Configuración restaurada.</p>}
      <ul className="flex flex-col gap-2">
        {history.data?.map((e) => (
          <li key={e.id} className="flex flex-col gap-1 rounded-m3-lg bg-surface-2 p-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <time dateTime={e.time} className="font-medium">{new Date(e.time).toLocaleString()}</time>
              <span className="text-muted">{e.user || "—"}</span>
              <span className="rounded-m3-lg bg-surface-1 px-2 text-xs">{ORIGIN[e.origin] ?? e.origin}{e.section ? ` · ${e.section}` : ""}</span>
              <span className={e.result === "error" ? "text-bad" : e.result === "unverified" ? "text-warn" : "text-ok"}>{RESULT[e.result]}</span>
              {e.rollback_of ? <span className="text-xs text-muted">Restauración del cambio #{e.rollback_of}</span> : null}
              {e.rolled_back_by ? <span className="text-xs text-muted">Restaurado</span> : null}
            </div>
            {e.changes.length > 0 && (
              <ul className="list-disc pl-5 text-xs">
                {e.changes.map((c) => <li key={c.path}>{c.path}: {fmtValue(c.from)} → {fmtValue(c.to)}</li>)}
              </ul>
            )}
            {e.error && <PlainErrorNote error={e.error} />}
            {e.rollbackable && canRollback && (
              <div><Button variant="secondary" onClick={() => { rollback.reset(); setTarget(e); }}>Restaurar configuración anterior</Button></div>
            )}
            {!e.rollbackable && e.rollback_reason && <p className="text-xs text-muted">No se puede restaurar: {e.rollback_reason}</p>}
          </li>
        ))}
      </ul>
      {target && (
        <RollbackConfirm
          entry={target}
          pending={rollback.isPending}
          error={rollback.error ?? (rollback.data?.result.result === "error" ? new Error(rollback.data.result.plain_error?.technical || rollback.data.result.state.last_error || "El motor rechazó el cambio.") : null)}
          onClose={() => setTarget(null)}
          onConfirm={() => rollback.mutate(target.id, { onSuccess: (r) => { if (r.result.result !== "error") setTarget(null); } })}
        />
      )}
    </section>
  );
}
