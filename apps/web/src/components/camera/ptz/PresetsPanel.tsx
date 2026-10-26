import { useQuery } from "@tanstack/react-query";
import { cameraPtzPresetsQuery } from "@/api/queries";
import { Button, ErrorNote } from "@/components/ui";

/** The agent that talks to the camera only exposes list and go-to for presets today. */
export const PRESET_EDIT_UNAVAILABLE = "Crear, renombrar y eliminar presets requiere un agente con soporte de edición PTZ; todavía no está disponible.";
export const HOME_UNAVAILABLE = "La posición home no se puede leer ni guardar con el agente actual.";
export const LIMITS_UNAVAILABLE = "Los límites de movimiento no se exponen por ONVIF PTZ en esta versión.";

type Props = { cameraId: string; canManage: boolean; onGo: (token: string) => void; busy: boolean };

/** Presets reported by the camera: run them; editing is shown disabled with its reason. */
export function PresetsPanel({ cameraId, canManage, onGo, busy }: Props) {
  const presets = useQuery({ ...cameraPtzPresetsQuery(cameraId), enabled: canManage });
  const list = presets.data?.presets ?? [];
  return (
    <section aria-label="Presets" className="flex flex-col gap-3 rounded-m3-lg border border-outline-variant/50 bg-surface-2 p-4">
      <h3 className="text-sm font-semibold">Presets</h3>
      {!canManage && <p className="text-xs text-muted">Se necesita el permiso de gestión de cámaras para ver y ejecutar presets.</p>}
      {canManage && presets.isLoading && <p className="text-xs text-muted">Leyendo presets de la cámara…</p>}
      {canManage && presets.isError && <ErrorNote error={presets.error} />}
      {canManage && presets.isSuccess && list.length === 0 && <p className="text-xs text-muted">La cámara no tiene presets guardados.</p>}
      <ul className="flex flex-col gap-1.5">
        {list.map((p) => (
          <li key={p.token} className="flex items-center justify-between gap-2 text-sm">
            <span className="truncate">{p.name || `Preset ${p.token}`} <span className="font-mono text-[10px] text-muted">#{p.token}</span></span>
            <Button size="sm" disabled={busy || !p.token} onClick={() => p.token && onGo(p.token)} aria-label={`Ir a ${p.name || p.token}`}>
              Ir
            </Button>
          </li>
        ))}
      </ul>
      <div className="flex flex-wrap gap-2">
        {["Crear preset", "Renombrar", "Eliminar"].map((label) => (
          <Button key={label} size="sm" disabled title={PRESET_EDIT_UNAVAILABLE}>{label}</Button>
        ))}
        <Button size="sm" disabled title={HOME_UNAVAILABLE}>Ir a home</Button>
        <Button size="sm" disabled title={HOME_UNAVAILABLE}>Guardar home</Button>
      </div>
      <p className="text-xs text-muted">{PRESET_EDIT_UNAVAILABLE} {HOME_UNAVAILABLE}</p>
    </section>
  );
}
