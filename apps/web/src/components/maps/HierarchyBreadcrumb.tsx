import { ChevronRight, Building2, MapPin } from "lucide-react";
import type { Site } from "@/lib/maps/types";

export interface HierarchyBreadcrumbProps {
  sites?: Site[];
  currentSite?: Site;
  buildingName?: string;
  floorName?: string;
  onSelectSite?: (siteId: string) => void;
  onClearSite?: () => void;
}

export function HierarchyBreadcrumb({
  sites = [],
  currentSite,
  buildingName,
  floorName,
  onSelectSite,
  onClearSite,
}: HierarchyBreadcrumbProps) {
  return (
    <nav
      aria-label="Jerarquía del mapa"
      className="flex items-center gap-1.5 rounded-lg border border-line bg-surface/90 px-3 py-1.5 text-xs text-muted shadow-sm backdrop-blur-xs"
    >
      <button
        type="button"
        onClick={onClearSite}
        className="flex items-center gap-1 font-medium text-ink hover:text-accent transition-colors"
        title="Vista global de sitios"
      >
        <MapPin className="size-3.5 text-accent" aria-hidden />
        <span>Todos los sitios</span>
      </button>

      {currentSite && (
        <>
          <ChevronRight className="size-3.5 text-muted/60 shrink-0" aria-hidden />
          {sites.length > 1 && onSelectSite ? (
            <select
              value={currentSite.id}
              onChange={(e) => onSelectSite(e.target.value)}
              className="bg-transparent font-medium text-ink focus-visible:outline-2 focus-visible:outline-accent cursor-pointer"
              aria-label="Seleccionar sitio"
            >
              {sites.map((s) => (
                <option key={s.id} value={s.id} className="bg-surface text-ink">
                  {s.name}
                </option>
              ))}
            </select>
          ) : (
            <span className="font-medium text-ink">{currentSite.name}</span>
          )}
        </>
      )}

      {buildingName && (
        <>
          <ChevronRight className="size-3.5 text-muted/60 shrink-0" aria-hidden />
          <span className="flex items-center gap-1 text-ink">
            <Building2 className="size-3.5 text-muted" aria-hidden />
            <span>{buildingName}</span>
          </span>
        </>
      )}

      {floorName && (
        <>
          <ChevronRight className="size-3.5 text-muted/60 shrink-0" aria-hidden />
          <span className="font-semibold text-accent">{floorName}</span>
        </>
      )}
    </nav>
  );
}
