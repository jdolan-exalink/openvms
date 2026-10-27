import { Field, TextInput } from "@/components/ui";
import { LprTestCard } from "@/components/provision/LprTestCard";
import { type AnalyticsModule, draftEnhancement, draftMinArea } from "./draft";

type Props = {
  cameraId: string;
  module: AnalyticsModule;
  patch: Record<string, unknown>;
  onMinArea: (value: number) => void;
  onEnhancement?: (value: number) => void;
  readOnly?: boolean;
};

/**
 * Detail of the LPR module, the same whatever reads the plates (Frigate, Provision ISR...).
 * Only values with a real backing are editable; engine-specific parameters are listed read-only.
 */
export function LprSettings({ cameraId, module: m, patch, onMinArea, onEnhancement, readOnly }: Props) {
  const minArea = draftMinArea(patch, m);
  const enhancement = draftEnhancement(patch, m);
  const canTest = m.engine === "frigate" || m.engine === "provision";
  return (
    <div className="flex flex-col gap-3">
      {m.writable && (
        <Field label="Área mínima de la matrícula (px²)" hint="Objetos más pequeños no se analizan.">
          <TextInput
            type="number"
            min={0}
            disabled={readOnly}
            value={minArea ?? ""}
            onChange={(e) => e.target.value !== "" && onMinArea(Math.max(0, Math.round(Number(e.target.value))))}
          />
        </Field>
      )}
      {m.writable && onEnhancement && (
        <Field label="Mejora de imagen (0 a 10)" hint="Mejora el recorte de la matrícula antes de leerla; 0 la desactiva.">
          <TextInput
            type="number"
            min={0}
            max={10}
            disabled={readOnly}
            value={enhancement ?? ""}
            onChange={(e) => e.target.value !== "" && onEnhancement(Math.min(10, Math.max(0, Math.round(Number(e.target.value)))))}
          />
        </Field>
      )}
      <Advanced items={m.advanced} />
      {canTest && <LprTestCard cameraId={cameraId} engine={m.engine as "frigate" | "provision"} />}
    </div>
  );
}

export function Advanced({ items }: { items: AnalyticsModule["advanced"] }) {
  if (items.length === 0) return null;
  return (
    <dl className="grid gap-x-4 gap-y-1 text-xs sm:grid-cols-[max-content_1fr]" aria-label="Parámetros del motor">
      {items.map((d) => (
        <div key={d.label} className="contents">
          <dt className="text-muted">{d.label}</dt>
          <dd className="font-medium text-on-surface">{d.value}</dd>
        </div>
      ))}
    </dl>
  );
}
