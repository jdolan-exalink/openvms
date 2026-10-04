import { Check, Copy } from "lucide-react";
import { type ReactNode, useState } from "react";
import { Icon } from "@/components/Icon";
import { Button, Field, Switch, Textarea, TextInput } from "@/components/ui";
import { labelName } from "@/lib/format";
import { serializeCoordinates } from "@/lib/zoneGeometry";
import { type EditorItem, type ItemIssues } from "./zoneDraft";

export interface LabelPickerProps {
  value: string[];
  onChange: (next: string[]) => void;
  ariaLabel: string;
}

/** Plain multi-select fallback used when the parent does not provide `renderLabelPicker`. */
function FallbackLabelPicker({ value, onChange, ariaLabel, labels }: LabelPickerProps & { labels: string[] }) {
  const all = [...new Set([...labels, ...value])];
  return (
    <div role="group" aria-label={ariaLabel} className="flex flex-wrap gap-x-4 gap-y-1">
      {all.map((l) => (
        <label key={l} className="inline-flex min-h-11 items-center gap-2 text-sm">
          <input
            type="checkbox"
            className="size-5 accent-primary"
            checked={value.includes(l)}
            onChange={(e) => onChange(e.target.checked ? [...value, l] : value.filter((v) => v !== l))}
          />
          {labelName(l)}
        </label>
      ))}
    </div>
  );
}

function NumberField({
  label,
  hint,
  value,
  min,
  step,
  onChange,
}: {
  label: string;
  hint?: string;
  value: unknown;
  min: number;
  step?: number;
  onChange: (v: number | undefined) => void;
}) {
  return (
    <Field label={label} hint={hint}>
      <TextInput
        type="number"
        min={min}
        step={step ?? 1}
        value={typeof value === "number" ? value : ""}
        onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))}
      />
    </Field>
  );
}

export function ZoneProperties({
  item,
  issues,
  dictFormat,
  labels,
  renderLabelPicker,
  onPatch,
  onConfig,
}: {
  item: EditorItem;
  issues: ItemIssues;
  /** Dict mask formats persist friendly name and enabled flag; legacy formats do not. */
  dictFormat: boolean;
  labels: string[];
  renderLabelPicker?: (props: LabelPickerProps) => ReactNode;
  onPatch: (patch: Partial<Pick<EditorItem, "name" | "enabled">>) => void;
  /** Sets (or removes, when undefined) a key of the zone config. */
  onConfig: (key: string, value: unknown) => void;
}) {
  const [copied, setCopied] = useState(false);
  const coords = serializeCoordinates(item.points);
  const objects = Array.isArray(item.config?.objects) ? (item.config!.objects as string[]) : [];
  const pickerProps: LabelPickerProps = {
    value: objects,
    onChange: (next) => onConfig("objects", next.length ? next : undefined),
    ariaLabel: "Objetos que activan la zona",
  };
  const isZone = item.kind === "zone";

  async function copy() {
    try {
      await navigator.clipboard.writeText(coords);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard unavailable: the textarea stays selectable */
    }
  }

  return (
    <section aria-label="Propiedades" className="flex flex-col gap-3 rounded-m3-lg bg-surface-2 p-4">
      <Field
        label={isZone ? "Nombre de la zona" : "Nombre de la máscara"}
        hint={isZone ? "Minúsculas, números y guion bajo; único en la cámara." : dictFormat ? undefined : "El nombre solo se guarda con el formato de máscaras de Frigate 0.18."}
      >
        <TextInput
          value={item.name}
          disabled={!isZone && !dictFormat}
          aria-invalid={issues.errors.length > 0}
          onChange={(e) => onPatch({ name: isZone ? e.target.value.toLowerCase() : e.target.value })}
        />
      </Field>
      {!isZone && dictFormat && (
        <Switch label="Máscara activa" checked={item.enabled} onChange={(next) => onPatch({ enabled: next })} />
      )}
      {item.kind === "object" && <p className="text-xs text-muted">Ámbito: {item.scope ? labelName(item.scope) : "todos los objetos"}</p>}
      {isZone && (
        <>
          <div className="flex flex-col gap-1 text-sm">
            <span>Objetos permitidos</span>
            <span className="text-xs text-muted">Sin selección, la zona reacciona a todos los objetos rastreados.</span>
            {renderLabelPicker ? renderLabelPicker(pickerProps) : <FallbackLabelPicker {...pickerProps} labels={labels} />}
          </div>
          <div className="grid grid-cols-2 gap-2">
            <NumberField label="Inercia (cuadros)" min={1} value={item.config?.inertia} onChange={(v) => onConfig("inertia", v)} />
            <NumberField label="Permanencia (s)" hint="loitering_time" min={0} value={item.config?.loitering_time} onChange={(v) => onConfig("loitering_time", v)} />
          </div>
          <NumberField
            label="Umbral de velocidad (opcional)"
            hint="Solo se notifica por encima de esta velocidad; requiere distancias calibradas."
            min={0}
            step={0.1}
            value={item.config?.speed_threshold}
            onChange={(v) => onConfig("speed_threshold", v)}
          />
        </>
      )}
      <div className="flex flex-col gap-1 text-sm">
        <div className="flex items-center justify-between">
          <label htmlFor="zone-coords">Coordenadas ({item.points.length} puntos)</label>
          <Button size="sm" variant="text" onClick={copy}>
            <Icon icon={copied ? Check : Copy} size="xs" />
            {copied ? "Copiado" : "Copiar"}
          </Button>
        </div>
        <Textarea
          id="zone-coords"
          readOnly
          rows={3}
          value={coords}
          onFocus={(e) => e.currentTarget.select()}
          className="min-h-0 bg-surface-3 font-mono text-xs"
        />
      </div>
      {issues.errors.map((m) => (
        <p key={m} role="alert" className="text-xs text-bad">
          {m}
        </p>
      ))}
      {issues.warnings.map((m) => (
        <p key={m} className="text-xs text-warn">
          {m}
        </p>
      ))}
    </section>
  );
}
