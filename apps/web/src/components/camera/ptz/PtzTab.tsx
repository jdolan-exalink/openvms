import { ErrorNote } from "@/components/ui";
import { LIMITS_UNAVAILABLE, PresetsPanel } from "./PresetsPanel";
import { PtzPad } from "./PtzPad";
import { Slider } from "./Slider";
import { ToursPanel } from "./ToursPanel";
import { usePtzControl } from "./usePtz";

export const FOCUS_UNAVAILABLE = "El foco no se controla por el servicio PTZ de ONVIF (requiere el servicio Imaging); no está disponible.";
export const PREVIEW_NOTE = "Con esta sección abierta, la vista previa muestra controles PTZ para probar la cámara directamente.";

/**
 * PTZ tab. Shown only when the camera has PTZ. Controls exist only for what the ONVIF PTZ path
 * really does today: move, zoom, speed, go to preset, and stored tours.
 */
export function PtzTab({ cameraId, canManage }: { cameraId: string; canManage: boolean }) {
  const ptz = usePtzControl(cameraId);
  return (
    <div className="flex flex-col gap-5">
      <p className="text-sm text-muted">Orden de control: 1. ONVIF PTZ; 2. API del fabricante; 3. adaptador específico. Hoy solo está disponible ONVIF PTZ.</p>
      <section aria-label="Movimiento" className="flex flex-col gap-3 rounded-m3-lg border border-outline-variant/50 bg-surface-2 p-4">
        <h3 className="text-sm font-semibold">Movimiento</h3>
        {!canManage && <p className="text-xs text-muted">Se necesita el permiso de gestión de cámaras para mover la cámara.</p>}
        <PtzPad onMove={ptz.move} onStop={ptz.stop} disabled={!canManage} />
        <Slider label="Velocidad" value={ptz.speed} onChange={ptz.setSpeed} disabled={!canManage} />
        <p className="text-xs text-muted">Foco: {FOCUS_UNAVAILABLE}</p>
        <p className="text-xs text-muted">Límites: {LIMITS_UNAVAILABLE}</p>
        <p className="text-xs text-muted">{PREVIEW_NOTE}</p>
        {ptz.status && <p role="status" className="text-xs text-ok">{ptz.status}</p>}
        <ErrorNote error={ptz.error} />
      </section>
      <PresetsPanel cameraId={cameraId} canManage={canManage} onGo={ptz.gotoPreset} busy={ptz.pending} />
      <ToursPanel cameraId={cameraId} canManage={canManage} />
    </div>
  );
}
