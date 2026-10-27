import { useState } from "react";
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
export const EXPORT_NOTE = "Las exportaciones siguen usando el original hasta que el proceso de exportación soporte la vista dewarp; la preferencia queda guardada para cuando esté disponible.";

export type ExportChoice = "original" | "dewarp";
/** Same browser storage family as the other fisheye settings (openvms.fisheye.*). */
export const exportPreferenceKey = (cameraId: string) => `openvms.fisheye.export.${cameraId}`;

function readExportChoice(cameraId: string): ExportChoice {
  try {
    return localStorage.getItem(exportPreferenceKey(cameraId)) === "dewarp" ? "dewarp" : "original";
  } catch {
    return "original";
  }
}

/**
 * Fisheye tab, shown only for fisheye cameras. It wraps the existing section and states what is
 * original (stream, recording) and what is a view (dewarped), so nobody mistakes one for the other.
 */
export function FisheyeTab({ cameraId, cameraName, canManage }: { cameraId: string; cameraName: string; canManage: boolean }) {
  const config = useFisheyeConfig(cameraId);
  const [exportChoice, setExportChoice] = useState<ExportChoice>(() => readExportChoice(cameraId));
  const chooseExport = (next: ExportChoice) => {
    setExportChoice(next);
    try {
      localStorage.setItem(exportPreferenceKey(cameraId), next);
    } catch {
      // Storage unavailable: the choice only lasts for this view.
    }
  };
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
        <label className="flex flex-col gap-1 text-xs font-semibold">
          Exportar
          <select aria-label="Exportar" value={exportChoice} disabled={!canManage} onChange={(e) => chooseExport(e.target.value as ExportChoice)} className="min-h-9 rounded-m3-lg bg-surface-1 px-2 text-xs">
            <option value="original">Original</option>
            <option value="dewarp">Vista dewarp</option>
          </select>
        </label>
        <p className="text-xs text-muted">{EXPORT_NOTE}</p>
      </section>
      <FisheyeSection cameraId={cameraId} cameraName={cameraName} snapshotUrl={`/media/v1/cameras/${cameraId}/snapshot.jpg`} canManage={canManage} />
    </div>
  );
}
