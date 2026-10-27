import { useState } from "react";
import type { Schemas } from "@/api/client";
import { Button } from "@/components/ui";
import { patchFromDrift } from "@/lib/useCameraConfigChange";
import { SyncStatusBadge } from "./SyncStatusBadge";

const fmt = (v: unknown): string => (v === undefined || v === null ? "—" : typeof v === "boolean" ? (v ? "ON" : "OFF") : typeof v === "object" ? JSON.stringify(v) : String(v));

/** Shown for any non-synced state; on drift it offers the three ways out. */
export function SyncStatusPanel({ state, busy = false, applyError, onUseDesired }: { state: Schemas["CameraConfigState"]; busy?: boolean; applyError?: string; onUseDesired: (patch: Record<string, unknown>) => void }) {
  const [showDiff, setShowDiff] = useState(false);
  if (state.sync_state === "synced") return null;
  return (
    <section aria-label="Estado de sincronización" className="flex flex-col gap-2 rounded-m3-lg bg-surface-2 px-4 py-3 text-sm">
      <div className="flex items-center gap-2"><SyncStatusBadge state={state.sync_state} /></div>
      {state.sync_state === "error" && (
        <>
          <p className="text-bad">No se pudo aplicar la configuración en el motor.</p>
          {state.last_error && (
            <details>
              <summary className="cursor-pointer text-xs">Ver información técnica</summary>
              <pre className="whitespace-pre-wrap break-words text-xs">{state.last_error}</pre>
            </details>
          )}
        </>
      )}
      {state.sync_state === "drift" && (
        <>
          <p>La cámara fue modificada externamente.</p>
          <div className="flex flex-wrap gap-2">
            <Button variant="primary" disabled={busy} onClick={() => onUseDesired(patchFromDrift(state.drift))}>Usar configuración de OpenVMS</Button>
            <Button variant="secondary" disabled title="Disponible próximamente">Importar configuración actual</Button>
            <Button variant="text" aria-expanded={showDiff} onClick={() => setShowDiff((s) => !s)}>Ver diferencias</Button>
          </div>
          {showDiff && (
            <ul aria-label="Diferencias" className="list-disc pl-5">
              {state.drift.map((d) => <li key={d.path}>{d.path}: OpenVMS {fmt(d.desired)} · Cámara {fmt(d.effective)}</li>)}
            </ul>
          )}
        </>
      )}
      {applyError && (
        <div role="alert" className="text-bad">
          <p>No se pudo aplicar la configuración.</p>
          <details>
            <summary className="cursor-pointer text-xs">Ver información técnica</summary>
            <pre className="whitespace-pre-wrap break-words text-xs">{applyError}</pre>
          </details>
        </div>
      )}
    </section>
  );
}
