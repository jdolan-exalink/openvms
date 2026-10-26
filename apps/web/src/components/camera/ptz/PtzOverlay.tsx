import { PtzPad } from "./PtzPad";
import { usePtzControl } from "./usePtz";

/**
 * Contextual PTZ controls drawn over the preview while the PTZ tab is open, to test the camera
 * directly. They use the same ONVIF path as the tab.
 */
export function PtzOverlay({ cameraId }: { cameraId: string }) {
  const ptz = usePtzControl(cameraId);
  return (
    <div role="group" aria-label="Controles PTZ de la vista previa" className="rounded-m3-lg bg-black/55 p-2 text-white backdrop-blur">
      <PtzPad compact onMove={ptz.move} onStop={ptz.stop} />
      {ptz.error && <p role="alert" className="mt-1 max-w-40 text-[10px] text-red-300">No se pudo enviar el comando PTZ.</p>}
    </div>
  );
}
