import { X } from "lucide-react";
import { Checkbox, IconButton } from "@/components/ui";
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
    <section aria-label="Layers" className="w-64 rounded-m3-xl bg-surface-1/95 p-4 shadow-lg backdrop-blur">
      <header className="mb-1 flex items-center justify-between">
        <h2 className="text-lg font-bold">Capas</h2>
        {onClose && (
          <IconButton icon={X} aria-label="Close layers panel" onClick={onClose} className="-mr-2" />
        )}
      </header>

      {GROUPS.map((group) => (
        <fieldset key={group.title} className="mb-3 last:mb-0">
          <legend className="mb-1 text-xs font-bold text-on-surface-variant">{group.title}</legend>
          <div className="space-y-0">
            {group.options.map(({ key, label }) => (
              <Checkbox key={key} checked={layers[key]} onChange={() => onChange({ ...layers, [key]: !layers[key] })} label={label} className="text-on-surface" />
            ))}
          </div>
        </fieldset>
      ))}

      <p className="mt-3 rounded-m3-lg bg-surface-2 p-3 text-xs text-on-surface-variant">{PENDING_GROUPS}</p>
    </section>
  );
}
