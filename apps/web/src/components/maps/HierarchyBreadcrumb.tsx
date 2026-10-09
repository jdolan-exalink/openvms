import { ChevronRight, Building2, MapPin } from "lucide-react";
import { Select } from "@/components/ui";
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
      className="flex flex-wrap items-center gap-1.5 rounded-m3-xl bg-surface-1/95 px-3 py-1.5 text-xs text-on-surface-variant shadow-sm backdrop-blur-xs"
    >
      <button
        type="button"
        onClick={onClearSite}
        className="flex min-h-11 items-center gap-1 rounded-full px-2 font-bold text-on-surface hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-primary"
        title="Vista global de sitios"
      >
        <MapPin className="size-4 text-primary" aria-hidden />
        <span>Todos los sitios</span>
      </button>

      {sites.length > 0 && onSelectSite ? (
        <>
          <ChevronRight className="size-4 text-on-surface-variant/60 shrink-0" aria-hidden />
          <Select
            value={currentSite?.id ?? ""}
            onChange={(event) => event.target.value ? onSelectSite(event.target.value) : onClearSite?.()}
            className="h-11 max-w-48 rounded-full text-xs font-medium"
            aria-label="Seleccionar sitio"
          >
            <option value="" className="bg-surface-1 text-on-surface">Seleccionar sitio</option>
            {sites.map((site) => (
              <option key={site.id} value={site.id} className="bg-surface-1 text-on-surface">
                {site.name}{site.center ? "" : " · Sin centro geográfico"}
              </option>
            ))}
          </Select>
        </>
      ) : currentSite ? <span className="font-bold text-on-surface">{currentSite.name}</span> : null}

      {buildingName && (
        <>
          <ChevronRight className="size-4 text-on-surface-variant/60 shrink-0" aria-hidden />
          <span className="flex items-center gap-1 text-on-surface">
            <Building2 className="size-4 text-on-surface-variant" aria-hidden />
            <span>{buildingName}</span>
          </span>
        </>
      )}

      {floorName && (
        <>
          <ChevronRight className="size-4 text-on-surface-variant/60 shrink-0" aria-hidden />
          <span className="font-bold text-primary">{floorName}</span>
        </>
      )}
    </nav>
  );
}
