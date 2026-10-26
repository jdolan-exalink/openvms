import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { objectCatalogQuery } from "@/api/queries";
import { EditYamlLink } from "@/components/camera/advanced/YamlEntry";
import { useCameraChange } from "@/components/camera/CameraChangeProvider";
import { ProvenanceBadge } from "@/components/camera/profile/ProvenanceBadge";
import { Switch } from "@/components/ui";
import { DetectionSettings, type DetectionSettingsValue } from "./DetectionSettings";
import { ObjectCatalogPicker } from "./ObjectCatalogPicker";

type Rec = Record<string, unknown>;

export type DetectionTabProps = {
  serverId?: string;
  readOnly?: boolean;
  /** Existing Frigate fields behind sensitivity, confidence, size and zones. */
  settings: DetectionSettingsValue;
  onMotion: (next: Rec) => void;
  onObjects: (next: Rec) => void;
  onOpenZones: () => void;
  /** The previous Frigate detect/objects/motion editors, kept under an advanced disclosure. */
  legacy: ReactNode;
};

/** Detection tab: enable it, pick what to detect from the catalog, tune the common settings. */
export function DetectionTab({ serverId, readOnly, settings, onMotion, onObjects, onOpenZones, legacy }: DetectionTabProps) {
  const change = useCameraChange();
  const catalog = useQuery(objectCatalogQuery("frigate", serverId));
  const model = change.config.data?.detection;
  const draft = (change.patch.detection ?? {}) as { enabled?: boolean; selection?: string[] };
  const enabled = draft.enabled ?? model?.enabled;
  const selected = draft.selection ?? model?.selection ?? [];
  const raw = model?.raw ?? [];

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1.5 rounded-m3-lg bg-surface-2 p-3">
        <Switch label="Detección de objetos" checked={enabled === true} disabled={readOnly || enabled === undefined} onChange={(v) => change.setPatch({ detection: { enabled: v } })} />
        <ProvenanceBadge path="detection.enabled" />
        {enabled === undefined && <p className="text-xs text-muted">El estado de la detección no está disponible.</p>}
      </div>

      <section aria-label="Qué detectar" className="flex flex-col gap-2">
        <h3 className="flex flex-wrap items-center gap-2 text-sm font-bold">Qué detectar <ProvenanceBadge path="detection.objects" /></h3>
        {catalog.isLoading ? (
          <p className="text-sm text-muted">Cargando catálogo…</p>
        ) : catalog.isError || !catalog.data ? (
          <p role="alert" className="text-sm text-bad">No se pudo cargar el catálogo de objetos.</p>
        ) : (
          <ObjectCatalogPicker catalog={catalog.data} selected={selected} disabled={readOnly || !model} onChange={(next) => change.setPatch({ detection: { selection: next } })} />
        )}
        {raw.length > 0 && (
          <div className="rounded-m3-lg bg-surface-2 p-3">
            <h4 className="text-xs font-bold">Otros objetos del motor</h4>
            <p className="text-xs text-muted">Etiquetas que el motor ya detecta y el catálogo no conoce; no se pueden cambiar desde acá.</p>
            <ul className="mt-1 flex flex-wrap gap-2">
              {raw.map((l) => (
                <li key={l} className="rounded-m3-lg bg-surface-1 px-3 py-1 text-xs">{l}</li>
              ))}
            </ul>
          </div>
        )}
      </section>

      <section aria-label="Ajustes de detección" className="flex flex-col gap-2">
        <h3 className="text-sm font-bold">Ajustes de detección</h3>
        <DetectionSettings value={settings} readOnly={readOnly} onMotion={onMotion} onObjects={onObjects} onOpenZones={onOpenZones} />
      </section>

      <details className="rounded-m3-lg bg-surface-1">
        <summary className="min-h-11 cursor-pointer px-3 py-2.5 text-xs font-bold">Avanzado de detección (Frigate)</summary>
        <div className="flex flex-col gap-4 border-t border-outline-variant p-3">
          <EditYamlLink section="detect" label="Editar YAML de detección" />
          {legacy}
        </div>
      </details>
    </div>
  );
}
