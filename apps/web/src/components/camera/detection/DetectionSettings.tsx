import { TextInput } from "@/components/ui";

type Rec = Record<string, unknown>;
const isRec = (v: unknown): v is Rec => typeof v === "object" && v !== null && !Array.isArray(v);
const num = (v: unknown) => (typeof v === "number" ? v : undefined);

/**
 * Existing Frigate fields behind the detection settings: sensitivity is motion.threshold (a lower
 * threshold reacts to smaller changes) and minimum confidence / size are the min_score and min_area
 * object filters of every tracked label. The zones are the camera's existing Frigate zones.
 */
export type DetectionSettingsValue = { motion: unknown; objects: unknown; zones: string[] };

/** The value every tracked label shares for a filter field, or undefined when they differ or none is set. */
export function sharedFilter(objects: unknown, field: "min_score" | "min_area"): number | undefined {
  if (!isRec(objects)) return undefined;
  const track = Array.isArray(objects.track) ? (objects.track as string[]) : [];
  const filters = isRec(objects.filters) ? objects.filters : {};
  const values = track.map((l) => (isRec(filters[l]) ? num(filters[l][field]) : undefined));
  return values.length > 0 && values.every((v) => v !== undefined && v === values[0]) ? values[0] : undefined;
}

/** Sets a filter field on every tracked label, keeping the other filter keys. */
export function withFilter(objects: unknown, field: "min_score" | "min_area", value: number | undefined): Rec {
  const o: Rec = isRec(objects) ? objects : {};
  const track = Array.isArray(o.track) ? (o.track as string[]) : [];
  const filters: Rec = isRec(o.filters) ? { ...o.filters } : {};
  for (const l of track) {
    const f: Rec = isRec(filters[l]) ? { ...filters[l] } : {};
    if (value === undefined) delete f[field];
    else f[field] = value;
    filters[l] = f;
  }
  return { ...o, filters };
}

const parse = (s: string) => (s.trim() === "" || Number.isNaN(Number(s)) ? undefined : Number(s));

export function DetectionSettings({ value, onMotion, onObjects, onOpenZones, readOnly }: { value: DetectionSettingsValue; onMotion: (next: Rec) => void; onObjects: (next: Rec) => void; onOpenZones: () => void; readOnly?: boolean }) {
  const motion: Rec = isRec(value.motion) ? value.motion : {};
  const noTracked = !(isRec(value.objects) && Array.isArray(value.objects.track) && value.objects.track.length > 0);
  return (
    <div className="grid gap-3 md:grid-cols-2">
      <label className="flex flex-col gap-1 text-xs font-semibold">
        Sensibilidad de movimiento
        <TextInput type="number" min={1} max={255} disabled={readOnly} value={num(motion.threshold) ?? ""} onChange={(e) => onMotion({ ...motion, threshold: parse(e.target.value) })} />
        <span className="font-normal text-muted">Umbral de Frigate: un valor menor detecta cambios más chicos.</span>
      </label>
      <label className="flex flex-col gap-1 text-xs font-semibold">
        Confianza mínima
        <TextInput type="number" min={0} max={1} step={0.05} disabled={readOnly || noTracked} value={sharedFilter(value.objects, "min_score") ?? ""} onChange={(e) => onObjects(withFilter(value.objects, "min_score", parse(e.target.value)))} />
        <span className="font-normal text-muted">{noTracked ? "Elegí objetos para ajustarla." : "Entre 0 y 1; se aplica a todos los objetos elegidos."}</span>
      </label>
      <label className="flex flex-col gap-1 text-xs font-semibold">
        Tamaño mínimo (px²)
        <TextInput type="number" min={0} disabled={readOnly || noTracked} value={sharedFilter(value.objects, "min_area") ?? ""} onChange={(e) => onObjects(withFilter(value.objects, "min_area", parse(e.target.value)))} />
        <span className="font-normal text-muted">{noTracked ? "Elegí objetos para ajustarlo." : "Área mínima del recuadro; se aplica a todos los objetos elegidos."}</span>
      </label>
      <div className="flex flex-col gap-1 text-xs font-semibold">
        Zonas aplicables
        <p className="font-normal text-muted">{value.zones.length > 0 ? value.zones.join(", ") : "Sin zonas: se detecta en toda la imagen."}</p>
        <button type="button" onClick={onOpenZones} className="min-h-9 self-start rounded-m3-lg bg-surface-3 px-3 text-xs font-bold">
          Editar zonas
        </button>
      </div>
    </div>
  );
}
