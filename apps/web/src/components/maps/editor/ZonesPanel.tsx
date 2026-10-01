import { validateZoneDraft, type ZoneDraft } from "@/lib/maps/zoneDraft";
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
    <section aria-label="Zonas" className="rounded border border-line bg-surface p-3 shadow-sm">
      <header className="mb-2 flex items-center justify-between">
        <h2 className="text-sm font-semibold">Zonas ({zones.length})</h2>
        {canEdit && (
          <button
            type="button"
            onClick={onStartCreate}
            className="rounded bg-accent px-2 py-1 text-xs font-medium text-white"
          >
            Nueva zona
          </button>
        )}
      </header>

      {error && (
        <p role="alert" className="mb-2 text-xs text-bad">
          {error}
        </p>
      )}

      <ul className="mb-2 max-h-40 space-y-1 overflow-auto">
        {zones.map((zone) => (
          <li
            key={zone.id}
            className="flex items-center justify-between gap-2 rounded border border-line px-2 py-1"
          >
            <span className="truncate text-sm text-ink">{zone.name}</span>
            <span className="text-xs text-muted">{KIND_LABELS[zone.kind]}</span>
            {canEdit && (
              <span className="flex shrink-0 gap-1">
                <button
                  type="button"
                  onClick={() => onSelectZone(zone.id)}
                  className="rounded border border-line px-2 py-1 text-xs text-ink hover:bg-raised"
                >
                  Editar
                </button>
                <button
                  type="button"
                  onClick={() => onDelete(zone.id)}
                  className="rounded border border-bad px-2 py-1 text-xs text-bad"
                >
                  Eliminar
                </button>
              </span>
            )}
          </li>
        ))}
      </ul>

      {draft && (
        <div className="space-y-2">
          <label htmlFor="zone-name" className="block text-xs text-muted">
            Nombre de la zona
          </label>
          <input
            id="zone-name"
            type="text"
            value={draft.name}
            onChange={(event) => onDraftChange({ name: event.target.value })}
            className="w-full rounded border border-line bg-bg px-2 py-1 text-sm text-ink"
          />

          <label htmlFor="zone-kind" className="block text-xs text-muted">
            Tipo de zona
          </label>
          <select
            id="zone-kind"
            value={draft.kind}
            onChange={(event) => onDraftChange({ kind: event.target.value as ZoneKind })}
            className="w-full rounded border border-line bg-bg px-2 py-1 text-sm text-ink"
          >
            {(Object.keys(KIND_LABELS) as ZoneKind[]).map((kind) => (
              <option key={kind} value={kind}>
                {KIND_LABELS[kind]}
              </option>
            ))}
          </select>

          <p className="text-xs text-muted">Puntos: {draft.points.length}</p>

          {errors.map((message) => (
            <p key={message} className="text-xs text-bad">
              {message}
            </p>
          ))}

          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={onUndoPoint}
              disabled={draft.points.length === 0}
              className="rounded border border-line px-2 py-1 text-xs text-ink disabled:opacity-50"
            >
              Deshacer punto
            </button>
            {draft.closed ? (
              <button
                type="button"
                onClick={onReopenPolygon}
                className="rounded border border-line px-2 py-1 text-xs text-ink"
              >
                Reabrir polígono
              </button>
            ) : (
              <button
                type="button"
                onClick={onClosePolygon}
                disabled={draft.points.length < 3}
                className="rounded border border-line px-2 py-1 text-xs text-ink disabled:opacity-50"
              >
                Cerrar polígono
              </button>
            )}
            <button
              type="button"
              onClick={onSave}
              disabled={errors.length > 0 || saving}
              className="rounded bg-accent px-2 py-1 text-xs font-medium text-white disabled:opacity-50"
            >
              Guardar
            </button>
            <button
              type="button"
              onClick={onCancel}
              className="rounded border border-line px-2 py-1 text-xs text-ink"
            >
              Cancelar
            </button>
          </div>
        </div>
      )}
    </section>
  );
}
