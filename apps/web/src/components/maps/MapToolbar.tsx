import { Video, Search, BarChart3, Edit3, SlidersHorizontal, Layers, Radar } from "lucide-react";
import { useT } from "@/i18n";
import type { MapMode } from "@/lib/maps/types";
import { cn } from "@/lib/cn";

export interface MapToolbarProps {
  mode: MapMode;
  onModeChange: (mode: MapMode) => void;
  canEdit?: boolean;
  coverage?: boolean;
  onToggleCoverage?: () => void;
  onToggleLayers?: () => void;
  onToggleFilters?: () => void;
  layersActive?: boolean;
  filtersActive?: boolean;
  /** Sit inside a parent bar instead of drawing a second box. */
  embedded?: boolean;
}

const MODES: { id: MapMode; label: string; icon: typeof Video; requiresEdit?: boolean }[] = [
  { id: "live", label: "En vivo", icon: Video },
  { id: "investigate", label: "Investigar", icon: Search },
  { id: "analytics", label: "Analítica", icon: BarChart3 },
  { id: "edit", label: "Editor", icon: Edit3, requiresEdit: true },
];

export function MapToolbar({
  mode,
  onModeChange,
  canEdit = false,
  coverage = true,
  onToggleCoverage,
  onToggleLayers,
  onToggleFilters,
  layersActive = false,
  filtersActive = false,
  embedded = false,
}: MapToolbarProps) {
  const t = useT();
  return (
    <div className={embedded ? "contents" : "flex items-center gap-2 rounded-lg border border-line bg-surface/90 p-1 shadow-sm backdrop-blur-xs"}>
      <div className="order-1 flex items-center gap-0.5 rounded-md bg-bg/50 p-0.5" role="tablist" aria-label={t("Modo de mapa")}>
        {MODES.filter((m) => !m.requiresEdit || canEdit).map((m) => {
          const Icon = m.icon;
          const active = mode === m.id;
          return (
            <button
              key={m.id}
              type="button"
              role="tab"
              aria-selected={active}
              onClick={() => onModeChange(m.id)}
              className={cn(
                "flex items-center gap-1.5 rounded px-2.5 py-1 text-xs font-medium transition-colors",
                active
                  ? "bg-accent text-white shadow-xs"
                  : "text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent",
              )}
            >
              {m.id === "live" && (
                <span className="relative flex size-2.5 items-center justify-center" data-live-led={active ? "on" : "off"} aria-hidden>
                  {active && <span className="absolute size-2.5 animate-ping rounded-full bg-ok" />}
                  <span className={cn("relative size-2 rounded-full", active ? "bg-ok shadow-[0_0_8px] shadow-ok" : "bg-ok/40")} />
                </span>
              )}
              <Icon className="size-3.5" aria-hidden />
              <span>{t(m.label)}</span>
            </button>
          );
        })}
      </div>

      <div className="order-5 h-4 w-px bg-line" aria-hidden />

      <div className="order-5 flex items-center gap-1">
        {onToggleCoverage && (
          <button
            type="button"
            onClick={onToggleCoverage}
            aria-pressed={coverage}
            title={coverage ? t("Ocultar conos FOV (Cobertura)") : t("Mostrar conos FOV (Cobertura)")}
            className={cn(
              "flex size-7 items-center justify-center rounded text-muted hover:bg-raised hover:text-ink transition-colors",
              coverage && "bg-accent/15 text-accent ring-1 ring-inset ring-accent/30",
            )}
          >
            <Radar className="size-3.5" aria-hidden />
          </button>
        )}

        {onToggleLayers && (
          <button
            type="button"
            onClick={onToggleLayers}
            aria-pressed={layersActive}
            title={t("Capas del mapa")}
            className={cn(
              "flex size-7 items-center justify-center rounded text-muted hover:bg-raised hover:text-ink transition-colors",
              layersActive && "bg-accent/15 text-accent ring-1 ring-inset ring-accent/30",
            )}
          >
            <Layers className="size-3.5" aria-hidden />
          </button>
        )}

        {onToggleFilters && (
          <button
            type="button"
            onClick={onToggleFilters}
            aria-pressed={filtersActive}
            title={t("Filtros")}
            className={cn(
              "flex size-7 items-center justify-center rounded text-muted hover:bg-raised hover:text-ink transition-colors",
              filtersActive && "bg-accent/15 text-accent ring-1 ring-inset ring-accent/30",
            )}
          >
            <SlidersHorizontal className="size-3.5" aria-hidden />
          </button>
        )}
      </div>
    </div>
  );
}
