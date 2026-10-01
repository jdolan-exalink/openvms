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

      {sites.length > 0 && onSelectSite ? (
        <>
          <ChevronRight className="size-3.5 text-muted/60 shrink-0" aria-hidden />
          <select
            value={currentSite?.id ?? ""}
            onChange={(event) => event.target.value ? onSelectSite(event.target.value) : onClearSite?.()}
            className="max-w-48 bg-transparent font-medium text-ink focus-visible:outline-2 focus-visible:outline-accent"
            aria-label="Seleccionar sitio"
          >
            <option value="" className="bg-surface text-ink">Seleccionar sitio</option>
            {sites.map((site) => (
              <option key={site.id} value={site.id} className="bg-surface text-ink">
                {site.name}{site.center ? "" : " · Sin centro geográfico"}
              </option>
            ))}
          </select>
        </>
      ) : currentSite ? <span className="font-medium text-ink">{currentSite.name}</span> : null}

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
