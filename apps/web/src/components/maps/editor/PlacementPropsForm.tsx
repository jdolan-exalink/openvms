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
    <section aria-label="Propiedades de la cámara" className="rounded border border-line bg-surface p-3 shadow-sm">
      <h2 className="mb-1 text-sm font-semibold">{name}</h2>
      <p className="mb-2 text-xs text-muted">
        {draft.lat.toFixed(5)}, {draft.lng.toFixed(5)}
      </p>

      <fieldset className="mb-2 flex gap-1" aria-label="Tipo de cámara">
        <legend className="text-xs text-muted">Tipo</legend>
        {([ ["fixed", "Bullet"], ["dome", "Domo"], ["ptz", "PTZ"] ] as const).map(([value, label]) => (
          <button key={value} type="button" aria-pressed={(draft.cameraType ?? "fixed") === value}
            onClick={() => onChange({ cameraType: value })}
            className="rounded border border-line px-2 py-1 text-xs">{label}</button>
        ))}
      </fieldset>

      <div className="mb-2 flex items-center gap-1">
        <label htmlFor="placement-bearing" className="text-xs text-muted">Rumbo</label>
        <input
          id="placement-bearing"
          type="number"
          min={0}
          max={360}
          value={draft.bearingDeg}
          onChange={(event) => number(event.target.value, (value) => ({ bearingDeg: clamp(value, 0, 360) }))}
          className="w-16 rounded border border-line bg-bg px-1 text-xs text-ink"
        />
        <button
          type="button"
          aria-label="Girar 15° a la izquierda"
          onClick={() => onChange({ bearingDeg: stepBearing(draft.bearingDeg, -15) })}
          className="rounded border border-line px-2 py-1 text-xs"
        >
          ⟲
        </button>
        <button
          type="button"
          aria-label="Girar 15° a la derecha"
          onClick={() => onChange({ bearingDeg: stepBearing(draft.bearingDeg, 15) })}
          className="rounded border border-line px-2 py-1 text-xs"
        >
          ⟳
        </button>
      </div>

      <label className="mb-2 flex items-center justify-between gap-2 text-xs text-muted">
        Ángulo FOV
        <input
          type="number"
          min={1}
          max={360}
          value={draft.fovDeg}
          onChange={(event) => number(event.target.value, (value) => ({ fovDeg: clamp(value, 1, 360) }))}
          className="w-16 rounded border border-line bg-bg px-1 text-xs text-ink"
        />
      </label>

      <label className="flex items-center justify-between gap-2 text-xs text-muted">
        Alcance (m)
        <input
          type="number"
          min={0}
          value={draft.rangeM}
          onChange={(event) => number(event.target.value, (value) => ({ rangeM: Math.max(0, value) }))}
          className="w-16 rounded border border-line bg-bg px-1 text-xs text-ink"
        />
      </label>
    </section>
  );
}
