import { Button, Select, TextInput } from "@/components/ui";
import { validateZoneDraft, ZONE_COLOR_CHOICES, ZONE_KIND_COLOR, type ZoneDraft } from "@/lib/maps/zoneDraft";
import type { Zone, ZoneKind } from "@/lib/maps/types";

const KIND_LABELS: Record<ZoneKind, string> = {
  security: "seguridad",
  perimeter: "perímetro",
  warning: "alerta",
  custom: "personalizada",
};

export interface ZonesPanelProps {
  zones: Zone[];
  draft?: ZoneDraft;
  /** Gated on maps.create_zone: without it the panel is read-only. */
  canEdit: boolean;
  /** Server refusal from the last write, kept apart from the client-side rules. */
  error?: string;
  saving?: boolean;
  onStartCreate: () => void;
  onSelectZone: (zoneId: string) => void;
  onDraftChange: (patch: Partial<ZoneDraft>) => void;
  onClosePolygon: () => void;
  onReopenPolygon: () => void;
  onUndoPoint: () => void;
  onSave: () => void;
  onDelete: (zoneId: string) => void;
  onCancel: () => void;
}

/**
 * ZonesPanel drives the whole zone editing flow: pick a stored zone or start a new one,
 * grow the polygon with map clicks, and save only when the draft already satisfies the
 * backend's geometry validation — the same messages appear here before any request goes out.
 */
export function ZonesPanel({
  zones,
  draft,
  canEdit,
  error,
  saving,
  onStartCreate,
  onSelectZone,
  onDraftChange,
  onClosePolygon,
  onReopenPolygon,
  onUndoPoint,
  onSave,
  onDelete,
  onCancel,
}: ZonesPanelProps) {
  const errors = draft ? validateZoneDraft(draft) : [];

  return (
    <section aria-label="Zonas" className="p-3">
      <header className="mb-2 flex items-center justify-between gap-2">
        <h2 className="text-lg font-bold">Zonas ({zones.length})</h2>
        {canEdit && (
          <Button variant="filled" size="sm" onClick={onStartCreate}>
            Nueva zona
          </Button>
        )}
      </header>

      {error && (
        <p role="alert" className="mb-2 text-xs text-bad">
          {error}
        </p>
      )}

      <ul className="mb-2 max-h-40 space-y-2 overflow-auto">
        {zones.map((zone) => (
          <li
            key={zone.id}
            className="flex flex-wrap items-center justify-between gap-2 rounded-m3-lg bg-surface-1 px-3 py-2"
          >
            <span className="min-w-0 truncate text-sm font-medium text-on-surface">{zone.name}</span>
            <span className="text-xs text-on-surface-variant">{KIND_LABELS[zone.kind]}</span>
            {canEdit && (
              <span className="flex shrink-0 gap-1">
                <Button variant="tonal" size="sm" onClick={() => onSelectZone(zone.id)}>
                  Editar
                </Button>
                <Button variant="outlined" size="sm" className="border-bad text-bad" onClick={() => onDelete(zone.id)}>
                  Eliminar
                </Button>
              </span>
            )}
          </li>
        ))}
      </ul>

      {draft && (
        <div className="space-y-2 rounded-m3-lg bg-surface-1 p-3">
          <label htmlFor="zone-name" className="block text-xs text-on-surface-variant">
            Nombre de la zona
          </label>
          <TextInput
            id="zone-name"
            type="text"
            value={draft.name}
            onChange={(event) => onDraftChange({ name: event.target.value })}
          />

          <label htmlFor="zone-kind" className="block text-xs text-on-surface-variant">
            Tipo de zona
          </label>
          <Select
            id="zone-kind"
            value={draft.kind}
            onChange={(event) => onDraftChange({ kind: event.target.value as ZoneKind })}
          >
            {(Object.keys(KIND_LABELS) as ZoneKind[]).map((kind) => (
              <option key={kind} value={kind}>
                {KIND_LABELS[kind]}
              </option>
            ))}
          </Select>

          <p className="text-xs text-on-surface-variant">
            {draft.closed ? "Polígono cerrado." : "Hacé clic en el mapa para marcar cada punto."}
          </p>
          <p className="font-mono text-xs text-on-surface-variant">Puntos: {draft.points.length}</p>
          <div>
            <p className="mb-1 text-xs text-on-surface-variant">Color</p>
            <div className="flex flex-wrap items-center gap-2">
              {ZONE_COLOR_CHOICES.map((color) => (
                <button
                  key={color}
                  type="button"
                  aria-label={`Color ${color}`}
                  aria-pressed={(draft.color ?? ZONE_KIND_COLOR[draft.kind]) === color}
                  onClick={() => onDraftChange({ color })}
                  className="size-8 rounded-full border-2 border-transparent aria-pressed:border-on-surface focus-visible:outline-2 focus-visible:outline-primary"
                  style={{ backgroundColor: color }}
                />
              ))}
              <input
                type="color"
                aria-label="Color de la zona"
                value={draft.color ?? ZONE_KIND_COLOR[draft.kind]}
                onChange={(event) => onDraftChange({ color: event.target.value })}
                className="size-8 cursor-pointer rounded-full border border-outline-variant bg-transparent"
              />
            </div>
          </div>

          {errors.map((message) => (
            <p key={message} className="text-xs text-bad">
              {message}
            </p>
          ))}

          <div className="flex flex-wrap gap-2">
            <Button variant="tonal" size="sm" onClick={onUndoPoint} disabled={draft.points.length === 0}>
              Deshacer punto
            </Button>
            {draft.closed ? (
              <Button variant="tonal" size="sm" onClick={onReopenPolygon}>
                Reabrir polígono
              </Button>
            ) : (
              <Button variant="tonal" size="sm" onClick={onClosePolygon} disabled={draft.points.length < 3}>
                Cerrar polígono
              </Button>
            )}
            <Button variant="filled" size="sm" onClick={onSave} disabled={errors.length > 0 || saving}>
              Guardar
            </Button>
            <Button variant="outlined" size="sm" onClick={onCancel}>
              Cancelar
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}
