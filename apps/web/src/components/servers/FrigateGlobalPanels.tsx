import type { ReactNode } from "react";
import type { Schemas } from "@/api/client";
import { HWACCEL_PRESETS, groupCoverage, topLevelBlock, yamlScalar } from "@/lib/frigateGroups";

type Catalog = Schemas["ObjectCatalog"];

function Block({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="flex flex-col gap-2 rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-3.5">
      <h4 className="text-xs font-bold uppercase tracking-wider text-on-surface">{title}</h4>
      {children}
    </section>
  );
}

function Yaml({ text, empty }: { text: string | null; empty: string }) {
  return <pre className="overflow-x-auto rounded-m3-lg bg-surface-3 p-3 font-mono text-xs text-ink">{text ?? empty}</pre>;
}

/** Supported labels (true), unsupported (false) and unverified (null) of the model, from the catalog. */
export function modelSupportCounts(catalog: Catalog | undefined) {
  const objects = (catalog?.categories ?? []).flatMap((c) => c.objects);
  return {
    supported: objects.filter((o) => o.supported === true).length,
    unsupported: objects.filter((o) => o.supported === false).length,
    unknown: objects.filter((o) => o.supported === null).length,
  };
}

const supportLabel = (s: boolean | null) => (s === true ? "Soportado" : s === false ? "No soportado por el modelo" : "Sin verificar");

/**
 * Wide catalog the engine could detect. Cameras pick a simplified subset, so activating a
 * detection on one camera never edits the model or this list.
 */
export function ObjectCatalogView({ catalog, loading, failed }: { catalog: Catalog | undefined; loading: boolean; failed: boolean }) {
  return (
    <section aria-label="Catálogo soportado" className="flex flex-col gap-3 rounded-m3-xl border border-primary/20 bg-primary/5 p-4">
      <h3 className="text-sm font-bold">Catálogo soportado por el servidor</h3>
      <p className="text-xs text-muted">
        El servidor puede soportar más objetos de los que usa cada cámara. Acá se ve el catálogo amplio; cada cámara elige solo la selección simplificada que necesita, sin modificar el modelo.
      </p>
      {loading && <p className="text-xs text-muted">Cargando catálogo…</p>}
      {failed && <p role="alert" className="text-xs text-bad">No se pudo leer el catálogo de objetos.</p>}
      {catalog && (
        <div className="grid gap-3 md:grid-cols-2">
          {catalog.categories.map((c) => (
            <div key={c.id} role="group" aria-label={`Catálogo ${c.label}`} className="flex flex-col gap-1.5">
              <span className="text-xs font-bold">{c.label}</span>
              <ul className="flex flex-wrap gap-1.5">
                {c.objects.map((o) => (
                  <li key={o.id} className={`rounded-full px-2.5 py-1 text-xs ${o.supported === false ? "bg-surface-3 text-muted line-through" : "bg-surface-1"}`}>
                    {o.label} <span className="sr-only">{supportLabel(o.supported)}</span>
                    {o.supported === null && <span aria-hidden className="text-muted"> ?</span>}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

export function ModelPanel({ yaml, catalog }: { yaml: string; catalog: Catalog | undefined }) {
  const counts = modelSupportCounts(catalog);
  const fields: [string, string | undefined][] = [
    ["Ruta del modelo", yamlScalar(yaml, ["model", "path"])],
    ["Tipo", yamlScalar(yaml, ["model", "model_type"])],
    ["Ancho", yamlScalar(yaml, ["model", "width"])],
    ["Alto", yamlScalar(yaml, ["model", "height"])],
    ["Mapa de etiquetas", yamlScalar(yaml, ["model", "labelmap_path"])],
  ];
  const custom = topLevelBlock(yaml, "model") !== null;
  return (
    <div className="flex flex-col gap-4">
      <Block title="Modelo de detección">
        <p className="text-xs text-muted">
          {custom ? "Modelo definido en config.yml." : "Sin sección model: Frigate usa el modelo por defecto (etiquetas COCO)."}
        </p>
        <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-3">
          {fields.map(([label, value]) => (
            <div key={label}>
              <dt className="text-xs text-muted">{label}</dt>
              <dd className="font-mono text-xs">{value ?? "—"}</dd>
            </div>
          ))}
        </dl>
        <Yaml text={topLevelBlock(yaml, "model")} empty="# Sin sección model" />
      </Block>
      <Block title="Etiquetas que soporta el modelo">
        {catalog ? (
          <p className="text-xs">
            {counts.supported} soportadas · {counts.unsupported} no soportadas · {counts.unknown} sin verificar. El detalle está en Objetos disponibles.
          </p>
        ) : (
          <p className="text-xs text-muted">Catálogo no disponible.</p>
        )}
      </Block>
    </div>
  );
}

export function AccelerationPanel({ yaml, canEdit, onChange }: { yaml: string; canEdit: boolean; onChange: (preset: string) => void }) {
  const current = yamlScalar(yaml, ["ffmpeg", "hwaccel_args"]) ?? "";
  const known = HWACCEL_PRESETS.some((p) => p.value === current);
  return (
    <div className="flex flex-col gap-4">
      <Block title="Aceleración de video">
        <label className="flex max-w-sm flex-col gap-1 text-xs font-semibold">
          Decodificación por hardware (ffmpeg.hwaccel_args)
          <select
            className="min-h-11 rounded-m3-md border border-outline-variant bg-surface-1 px-3 text-sm font-normal"
            disabled={!canEdit}
            value={current}
            onChange={(e) => onChange(e.target.value)}
          >
            {!known && <option value={current}>{current} (personalizado)</option>}
            {HWACCEL_PRESETS.map((p) => (
              <option key={p.value} value={p.value}>{p.label}</option>
            ))}
          </select>
        </label>
        {!canEdit && <p role="note" className="text-xs text-muted">Necesitás permiso de gestión del servidor para cambiarla.</p>}
        <Yaml text={topLevelBlock(yaml, "ffmpeg")} empty="# Sin sección ffmpeg" />
      </Block>
    </div>
  );
}

const DEFAULT_BLOCKS: { key: string; title: string }[] = [
  { key: "detect", title: "Detección (detect)" },
  { key: "motion", title: "Movimiento (motion)" },
  { key: "record", title: "Grabación (record)" },
  { key: "snapshots", title: "Snapshots (snapshots)" },
  { key: "objects", title: "Objetos (objects)" },
];

export function DefaultsPanel({ yaml, onOpenYaml }: { yaml: string; onOpenYaml: () => void }) {
  return (
    <div className="flex flex-col gap-4">
      <div className="rounded-m3-xl border border-primary/20 bg-primary/5 p-4 text-xs text-muted">
        Estos valores son el nivel 1 de herencia: toda cámara que no los redefine los hereda del servidor. Se editan en las pestañas de cada grupo o en el YAML avanzado.
        <div className="mt-2">
          <button type="button" className="font-semibold text-primary underline" onClick={onOpenYaml}>Abrir YAML avanzado</button>
        </div>
      </div>
      {DEFAULT_BLOCKS.map((b) => (
        <Block key={b.key} title={b.title}>
          <Yaml text={topLevelBlock(yaml, b.key)} empty={`# Sin sección ${b.key}: Frigate usa sus valores por defecto`} />
        </Block>
      ))}
    </div>
  );
}

export function OtherPanel({ yaml }: { yaml: string }) {
  const { other } = groupCoverage(yaml);
  return (
    <div className="flex flex-col gap-4">
      <p className="text-xs text-muted">
        Secciones del config.yml que ningún grupo reclama. Siguen siendo editables en el YAML avanzado; las cámaras se editan desde cada cámara.
      </p>
      {other.length === 0 && <p className="py-6 text-center text-xs text-muted">No hay otros parámetros.</p>}
      {other.map((k) => (
        <Block key={k} title={k}>
          <Yaml text={k === "cameras" ? "# Se editan desde la configuración de cada cámara" : topLevelBlock(yaml, k)} empty="" />
        </Block>
      ))}
    </div>
  );
}
