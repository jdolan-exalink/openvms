import { FisheyeSection } from "@/components/fisheye/FisheyeSection";
import { useFisheyeConfig } from "@/lib/fisheye/useFisheyeConfig";

const MODE_LABEL: Record<string, string> = {
  original: "Original (sin dewarp)",
  "360": "360°",
  panorama: "Panorámica 2×180°",
  virtual_ptz: "PTZ virtual",
  "3_fixed_120": "3 vistas fijas de 120°",
};

export const VIEW_ONLY_NOTE = "El dewarping es solo de visualización: se aplica sobre el stream original en este navegador y nunca modifica el stream ni la grabación de la cámara.";
export const EXPORT_NOTE = "La elección de exportar el original o la vista dewarped se hará al exportar; todavía no hay una preferencia guardada por cámara.";

/**
 * Fisheye tab, shown only for fisheye cameras. It wraps the existing section and states what is
 * original (stream, recording) and what is a view (dewarped), so nobody mistakes one for the other.
 */
export function FisheyeTab({ cameraId, cameraName, canManage }: { cameraId: string; cameraName: string; canManage: boolean }) {
  const config = useFisheyeConfig(cameraId);
  const rows: Array<[string, string]> = [
    ["Stream original", "Sin cambios; es lo que recibe la grabación."],
    ["Visualización dewarped", config.enabled ? `Activa · ${MODE_LABEL[config.defaultMode] ?? config.defaultMode}` : "Desactivada"],
    ["Grabación original", "No cambia con el dewarping: se graba lo que emite la cámara."],
  ];
  return (
    <div className="flex flex-col gap-4">
      <section aria-label="Original y vista" className="flex flex-col gap-2 rounded-m3-lg border border-outline-variant/50 bg-surface-2 p-4">
        <h3 className="text-sm font-semibold">Original y vista dewarped</h3>
        <dl className="flex flex-col gap-1 text-sm">
          {rows.map(([k, v]) => (
            <div key={k} className="flex justify-between gap-3">
              <dt className="text-muted">{k}</dt>
              <dd className="text-right">{v}</dd>
            </div>
          ))}
        </dl>
        <p className="text-xs text-muted">{VIEW_ONLY_NOTE}</p>
        <p className="text-xs text-muted">{EXPORT_NOTE}</p>
      </section>
      <FisheyeSection cameraId={cameraId} cameraName={cameraName} snapshotUrl={`/media/v1/cameras/${cameraId}/snapshot.jpg`} canManage={canManage} />
    </div>
  );
}
