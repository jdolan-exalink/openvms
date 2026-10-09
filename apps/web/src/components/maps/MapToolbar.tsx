import { Video, Search, BarChart3, Edit3, SlidersHorizontal, Layers, Radar } from "lucide-react";
import { useT, type MessageKey } from "@/i18n";
import type { MapMode } from "@/lib/maps/types";
import { cn } from "@/lib/cn";
import { IconButton } from "@/components/ui";

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
  analyticsMetric?: "object" | "person" | "vehicle" | "motion" | "alarm" | "lpr";
  onAnalyticsMetricChange?: (metric: "object" | "person" | "vehicle" | "motion" | "alarm" | "lpr") => void;
  analyticsTimeframe?: "1h" | "24h" | "7d" | "30d";
  onAnalyticsTimeframeChange?: (tf: "1h" | "24h" | "7d" | "30d") => void;
  /** Sit inside a parent bar instead of drawing a second box. */
  embedded?: boolean;
}

const MODES: { id: MapMode; label: MessageKey; icon: typeof Video; requiresEdit?: boolean }[] = [
  { id: "live", label: "nav.live", icon: Video },
  { id: "investigate", label: "maps.investigate", icon: Search },
  { id: "analytics", label: "maps.analytics", icon: BarChart3 },
  { id: "edit", label: "maps.editor", icon: Edit3, requiresEdit: true },
];

const ANALYTICS_METRICS: { id: "person" | "vehicle" | "motion" | "alarm" | "lpr" | "object"; label: string }[] = [
  { id: "person", label: "Personas" },
  { id: "vehicle", label: "Vehículos" },
  { id: "motion", label: "Movimiento" },
  { id: "alarm", label: "Alarmas" },
  { id: "lpr", label: "LPR" },
  { id: "object", label: "Todo" },
];

const ANALYTICS_TIMEFRAMES: { id: "1h" | "24h" | "7d" | "30d"; label: string }[] = [
  { id: "1h", label: "1h" },
  { id: "24h", label: "24h" },
  { id: "7d", label: "7d" },
  { id: "30d", label: "30d" },
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
  analyticsMetric,
  onAnalyticsMetricChange,
  analyticsTimeframe,
  onAnalyticsTimeframeChange,
  embedded = false,
}: MapToolbarProps) {
  const t = useT();
  return (
    <div className={embedded ? "contents" : "flex flex-wrap items-center gap-2 rounded-m3-xl bg-surface-1/95 p-1 shadow-sm backdrop-blur-xs"}>
      <div className="order-1 flex items-center gap-0.5 rounded-full bg-surface-2 p-0.5" role="tablist" aria-label={t("maps.mode")}>
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
                "m3-press flex h-11 items-center gap-1.5 rounded-full px-3 text-xs font-bold focus-visible:outline-2 focus-visible:outline-primary",
                active
                  ? "bg-primary-container text-on-primary-container"
                  : "text-on-surface-variant hover:bg-on-surface/8",
              )}
            >
              {m.id === "live" && (
                <span className="relative flex size-2.5 items-center justify-center" data-live-led={active ? "on" : "off"} aria-hidden>
                  {active && <span className="absolute size-2.5 animate-ping rounded-full bg-ok" />}
                  <span className={cn("relative size-2 rounded-full", active ? "bg-ok shadow-[0_0_8px] shadow-ok" : "bg-ok/40")} />
                </span>
              )}
              <Icon className="size-4" aria-hidden />
              <span>{t(m.label)}</span>
            </button>
          );
        })}
      </div>

      {mode === "analytics" && onAnalyticsMetricChange && (
        <>
          <div className="order-2 hidden h-5 w-px bg-outline-variant sm:block" aria-hidden />
          <div className="order-2 flex items-center gap-0.5 rounded-full bg-surface-2 p-0.5" role="radiogroup" aria-label="Métrica de analítica">
            {ANALYTICS_METRICS.map((met) => {
              const active = (analyticsMetric ?? "person") === met.id;
              return (
                <button
                  key={met.id}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  onClick={() => onAnalyticsMetricChange(met.id)}
                  className={cn(
                    "m3-press flex h-8 items-center rounded-full px-2.5 text-xs font-semibold focus-visible:outline-2 focus-visible:outline-primary",
                    active
                      ? "bg-secondary-container text-on-secondary-container"
                      : "text-on-surface-variant hover:bg-on-surface/8",
                  )}
                >
                  {met.label}
                </button>
              );
            })}
          </div>
        </>
      )}

      {mode === "analytics" && onAnalyticsTimeframeChange && (
        <>
          <div className="order-3 hidden h-5 w-px bg-outline-variant sm:block" aria-hidden />
          <div className="order-3 flex items-center gap-0.5 rounded-full bg-surface-2 p-0.5" role="radiogroup" aria-label="Rango de tiempo">
            {ANALYTICS_TIMEFRAMES.map((tf) => {
              const active = (analyticsTimeframe ?? "24h") === tf.id;
              return (
                <button
                  key={tf.id}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  onClick={() => onAnalyticsTimeframeChange(tf.id)}
                  className={cn(
                    "m3-press flex h-8 items-center rounded-full px-2.5 text-xs font-semibold focus-visible:outline-2 focus-visible:outline-primary",
                    active
                      ? "bg-secondary-container text-on-secondary-container"
                      : "text-on-surface-variant hover:bg-on-surface/8",
                  )}
                >
                  {tf.label}
                </button>
              );
            })}
          </div>
        </>
      )}

      <div className="order-5 hidden h-5 w-px bg-outline-variant sm:block" aria-hidden />

      <div className="order-5 flex items-center gap-1">
        {onToggleCoverage && (
          <IconButton
            icon={Radar}
            variant={coverage ? "tonal" : "standard"}
            onClick={onToggleCoverage}
            aria-pressed={coverage}
            title={coverage ? t("maps.hideFov") : t("maps.showFov")}
            aria-label={coverage ? t("maps.hideFov") : t("maps.showFov")}
          />
        )}

        {onToggleLayers && (
          <IconButton
            icon={Layers}
            variant={layersActive ? "tonal" : "standard"}
            onClick={onToggleLayers}
            aria-pressed={layersActive}
            title={t("maps.layers")}
            aria-label={t("maps.layers")}
          />
        )}

        {onToggleFilters && (
          <IconButton
            icon={SlidersHorizontal}
            variant={filtersActive ? "tonal" : "standard"}
            onClick={onToggleFilters}
            aria-pressed={filtersActive}
            title={t("common.filters")}
            aria-label={t("common.filters")}
          />
        )}
      </div>
    </div>
  );
}
