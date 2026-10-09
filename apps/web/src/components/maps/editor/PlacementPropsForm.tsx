import { Chip, IconButton, TextInput } from "@/components/ui";
import { RotateCcw, RotateCw } from "lucide-react";
import { stepBearing, type DraftPlacement } from "@/lib/maps/placementDraft";

export interface PlacementPropsFormProps {
  name: string;
  draft: DraftPlacement;
  onChange: (patch: Partial<DraftPlacement>) => void;
}

const clamp = (value: number, min: number, max: number) => Math.min(max, Math.max(min, value));

/**
 * React owns only the properties form of an unsaved placement: rotation advances in the
 * 15° step the design asks for, and FOV/range stay inside the ranges the backend accepts,
 * so a draft can never fail validation for a typo.
 */
export function PlacementPropsForm({ name, draft, onChange }: PlacementPropsFormProps) {
  const number = (raw: string, apply: (value: number) => Partial<DraftPlacement>) => {
    const parsed = Number(raw);
    if (!Number.isFinite(parsed)) return;
    onChange(apply(parsed));
  };

  return (
    <section aria-label="Propiedades de la cámara" className="rounded-m3-xl bg-surface-2 p-3">
      <h2 className="mb-1 text-base font-bold">{name}</h2>
      <p className="mb-2 font-mono text-xs text-on-surface-variant">
        {draft.lat.toFixed(5)}, {draft.lng.toFixed(5)}
      </p>

      <fieldset className="mb-3 flex flex-wrap items-center gap-2" aria-label="Tipo de cámara">
        <legend className="text-xs text-on-surface-variant">Tipo</legend>
        {([ ["fixed", "Bullet"], ["dome", "Domo"], ["ptz", "PTZ"] ] as const).map(([value, label]) => (
          <Chip key={value} selected={(draft.cameraType ?? "fixed") === value} onChange={() => onChange({ cameraType: value })}>{label}</Chip>
        ))}
      </fieldset>

      <div className="mb-2 flex items-center gap-2">
        <label htmlFor="placement-bearing" className="text-xs text-on-surface-variant">Rumbo</label>
        <TextInput
          id="placement-bearing"
          type="number"
          min={0}
          max={360}
          value={draft.bearingDeg}
          onChange={(event) => number(event.target.value, (value) => ({ bearingDeg: clamp(value, 0, 360) }))}
          className="h-11 w-20 px-2 font-mono text-xs"
        />
        <IconButton icon={RotateCcw} variant="tonal" aria-label="Girar 15° a la izquierda" onClick={() => onChange({ bearingDeg: stepBearing(draft.bearingDeg, -15) })} />
        <IconButton icon={RotateCw} variant="tonal" aria-label="Girar 15° a la derecha" onClick={() => onChange({ bearingDeg: stepBearing(draft.bearingDeg, 15) })} />
      </div>

      <label className="mb-2 flex items-center justify-between gap-2 text-xs text-on-surface-variant">
        Ángulo FOV
        <TextInput
          type="number"
          min={1}
          max={360}
          value={draft.fovDeg}
          onChange={(event) => number(event.target.value, (value) => ({ fovDeg: clamp(value, 1, 360) }))}
          className="h-11 w-20 px-2 font-mono text-xs"
        />
      </label>

      <label className="flex items-center justify-between gap-2 text-xs text-on-surface-variant">
        Alcance (m)
        <TextInput
          type="number"
          min={0}
          value={draft.rangeM}
          onChange={(event) => number(event.target.value, (value) => ({ rangeM: Math.max(0, value) }))}
          className="h-11 w-20 px-2 font-mono text-xs"
        />
      </label>
    </section>
  );
}
