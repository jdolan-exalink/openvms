import { X } from "lucide-react";
import type { CameraDisplayState, MapFilters } from "@/lib/maps/types";

export interface FiltersPanelProps {
  filters: MapFilters;
  onChange: (filters: MapFilters) => void;
  onClose?: () => void;
}

const STATUSES: { value: CameraDisplayState; label: string }[] = [
  { value: "ONLINE", label: "En línea" },
  { value: "DEGRADED", label: "Degradada" },
  { value: "OFFLINE", label: "Offline" },
  { value: "NO_SIGNAL", label: "Sin señal" },
  { value: "RECORDING_ERROR", label: "Error de grabación" },
  { value: "UNREACHABLE", label: "Servidor caído" },
  { value: "ALARM", label: "Con alarma" },
];

const CAMERA_TYPES: { value: "fixed" | "dome" | "ptz" | "fisheye" | "lpr"; label: string }[] = [
  { value: "fixed", label: "Fija" },
  { value: "dome", label: "Cúpula" },
  { value: "ptz", label: "PTZ" },
  { value: "fisheye", label: "Fisheye" },
  { value: "lpr", label: "LPR" },
];

function toggle<T extends string>(values: T[] | undefined, value: T): T[] | undefined {
  const current = values ?? [];
  const next = current.includes(value) ? current.filter((v) => v !== value) : [...current, value];
  return next.length ? next : undefined;
}

/** FiltersPanel edits the dimensions the entity payload can actually answer. */
export function FiltersPanel({ filters, onChange, onClose }: FiltersPanelProps) {
  return (
    <section aria-label="Filters" className="w-64 rounded border border-border bg-card p-3 shadow-sm">
      <header className="mb-2 flex items-center justify-between">
        <h2 className="text-sm font-semibold">Filtros</h2>
        <div className="flex items-center gap-1">
          <button
            type="button"
            onClick={() => onChange({})}
            className="rounded px-1.5 py-0.5 text-xs text-muted hover:bg-raised hover:text-ink"
          >
            Limpiar
          </button>
          {onClose && (
            <button
              type="button"
              aria-label="Close filters panel"
              onClick={onClose}
              className="rounded p-1 text-muted hover:bg-raised hover:text-ink"
            >
              <X className="size-3.5" aria-hidden />
            </button>
          )}
        </div>
      </header>

      <fieldset className="mb-3">
        <legend className="mb-1 text-xs font-medium text-muted">Estado</legend>
        <div className="space-y-1">
          {STATUSES.map(({ value, label }) => (
            <label key={value} className="flex cursor-pointer items-center gap-2 text-sm text-ink">
              <input
                type="checkbox"
                checked={filters.status?.includes(value) ?? false}
                onChange={() => onChange({ ...filters, status: toggle(filters.status, value) })}
                className="size-3.5 accent-accent"
              />
              {label}
            </label>
          ))}
        </div>
      </fieldset>

      <fieldset className="mb-3">
        <legend className="mb-1 text-xs font-medium text-muted">Tipo de cámara</legend>
        <div className="space-y-1">
          {CAMERA_TYPES.map(({ value, label }) => (
            <label key={value} className="flex cursor-pointer items-center gap-2 text-sm text-ink">
              <input
                type="checkbox"
                checked={filters.camera_types?.includes(value) ?? false}
                onChange={() => onChange({ ...filters, camera_types: toggle(filters.camera_types, value) })}
                className="size-3.5 accent-accent"
              />
              {label}
            </label>
          ))}
        </div>
      </fieldset>

      <p className="border-t border-border pt-2 text-xs text-muted">
        Sin un filtro activo se muestran todas las cámaras autorizadas.
      </p>
    </section>
  );
}
