import { X } from "lucide-react";
import { Button, IconButton } from "@/components/ui";
import { STATE_DOT } from "../stateTone";
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
    <section aria-label="Filters" className="w-64 rounded-m3-xl bg-surface-1/95 p-4 shadow-lg backdrop-blur">
      <header className="mb-1 flex items-center justify-between">
        <h2 className="text-lg font-bold">Filtros</h2>
        <div className="flex items-center gap-1">
          <Button variant="text" size="sm" onClick={() => onChange({})}>
            Limpiar
          </Button>
          {onClose && (
            <IconButton icon={X} aria-label="Close filters panel" onClick={onClose} className="-mr-2" />
          )}
        </div>
      </header>

      <fieldset className="mb-3">
        <legend className="mb-1 text-xs font-bold text-on-surface-variant">Estado (leyenda)</legend>
        <div>
          {STATUSES.map(({ value, label }) => (
            <div key={value} className="flex min-h-9 items-center gap-3 text-sm text-on-surface">
              <span aria-hidden className={`size-3 rounded-full ${STATE_DOT[value]}`} />
              {label}
            </div>
          ))}
        </div>
      </fieldset>

      <fieldset className="mb-3">
        <legend className="mb-1 text-xs font-bold text-on-surface-variant">Tipo de cámara</legend>
        <div>
          {CAMERA_TYPES.map(({ value, label }) => (
            <label key={value} className="flex min-h-11 cursor-pointer items-center gap-3 text-sm text-on-surface">
              <input
                type="checkbox"
                checked={filters.camera_types?.includes(value) ?? false}
                onChange={() => onChange({ ...filters, camera_types: toggle(filters.camera_types, value) })}
                className="size-5 accent-primary"
              />
              {label}
            </label>
          ))}
        </div>
      </fieldset>

      <p className="rounded-m3-lg bg-surface-2 p-3 text-xs text-on-surface-variant">
        Todas las cámaras se muestran independientemente de su estado o alarmas.
      </p>
    </section>
  );
}
