import { X } from "lucide-react";
import type { LayerPreference } from "@/lib/maps/types";

export interface LayersPanelProps {
  layers: LayerPreference;
  onChange: (layers: LayerPreference) => void;
  onClose?: () => void;
}

interface LayerOption {
  key: keyof LayerPreference;
  label: string;
}

/**
 * Only the groups MapLibre actually renders are offered here. The remaining contract keys
 * belong to Phase 2/3 layers and are listed below instead of shipped as dead switches.
 */
const GROUPS: { title: string; options: LayerOption[] }[] = [
  { title: "Mapa", options: [{ key: "sites", label: "Sitios" }, { key: "coverage", label: "Conos FOV" }] },
  { title: "Cámaras", options: [{ key: "cameras", label: "Cámaras" }] },
  {
    title: "Eventos",
    options: [
      { key: "events_alarm", label: "Alarmas" },
      { key: "events_motion", label: "Detecciones" },
    ],
  },
];

const PENDING_GROUPS = "IA por etiqueta, LPR, caras, infraestructura, heatmap y tráfico: Fase 2.";

export function LayersPanel({ layers, onChange, onClose }: LayersPanelProps) {
  return (
    <section aria-label="Layers" className="w-64 rounded border border-border bg-card p-3 shadow-sm">
      <header className="mb-2 flex items-center justify-between">
        <h2 className="text-sm font-semibold">Capas</h2>
        {onClose && (
          <button
            type="button"
            aria-label="Close layers panel"
            onClick={onClose}
            className="rounded p-1 text-muted hover:bg-raised hover:text-ink"
          >
            <X className="size-3.5" aria-hidden />
          </button>
        )}
      </header>

      {GROUPS.map((group) => (
        <fieldset key={group.title} className="mb-3 last:mb-0">
          <legend className="mb-1 text-xs font-medium text-muted">{group.title}</legend>
          <div className="space-y-1">
            {group.options.map(({ key, label }) => (
              <label key={key} className="flex cursor-pointer items-center gap-2 text-sm text-ink">
                <input
                  type="checkbox"
                  checked={layers[key]}
                  onChange={() => onChange({ ...layers, [key]: !layers[key] })}
                  className="size-3.5 accent-accent"
                />
                {label}
              </label>
            ))}
          </div>
        </fieldset>
      ))}

      <p className="mt-3 border-t border-border pt-2 text-xs text-muted">{PENDING_GROUPS}</p>
    </section>
  );
}
