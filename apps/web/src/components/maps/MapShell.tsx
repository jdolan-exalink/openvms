import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { mapsConfigQuery, mapsOverviewQuery, siteEntitiesQuery } from "@/lib/maps/api";
import type { CameraEntity, MapMode } from "@/lib/maps/types";
import { MapCanvas } from "./canvas/MapCanvas";
import { MapToolbar } from "./MapToolbar";
import { HierarchyBreadcrumb } from "./HierarchyBreadcrumb";
import { ErrorNote } from "../ui";
import { Loader2 } from "lucide-react";

export interface MapShellProps {
  initialSiteId?: string;
  initialCameraId?: string;
  initialMode?: MapMode;
  canEdit?: boolean;
}

export function MapShell({
  initialSiteId,
  initialCameraId,
  initialMode = "live",
  canEdit = false,
}: MapShellProps) {
  const [mode, setMode] = useState<MapMode>(initialMode);
  const [selectedSiteId, setSelectedSiteId] = useState<string | undefined>(initialSiteId);
  const [selectedCameraId, setSelectedCameraId] = useState<string | undefined>(initialCameraId);
  const [hoveredCameraId, setHoveredCameraId] = useState<string | null>(null);
  const [layersOpen, setLayersOpen] = useState(false);
  const [filtersOpen, setFiltersOpen] = useState(false);

  const configQuery = useQuery(mapsConfigQuery);
  const overviewQuery = useQuery(mapsOverviewQuery);

  const sites = overviewQuery.data ?? [];
  const currentSite = sites.find((s) => s.id === selectedSiteId) ?? (sites.length === 1 ? sites[0] : undefined);

  const entitiesQuery = useQuery(siteEntitiesQuery(currentSite?.id ?? ""));

  const cameras: CameraEntity[] = useMemo(() => {
    if (!entitiesQuery.data) return [];
    return entitiesQuery.data.entities.filter((e): e is CameraEntity => e.type === "camera");
  }, [entitiesQuery.data]);

  if (configQuery.isLoading) {
    return (
      <div className="flex h-full w-full items-center justify-center bg-bg" data-testid="map-loading">
        <div className="flex flex-col items-center gap-2 text-muted">
          <Loader2 className="size-6 animate-spin text-accent" aria-hidden />
          <span className="text-sm">Cargando mapa operativo...</span>
        </div>
      </div>
    );
  }

  if (configQuery.isError || !configQuery.data) {
    return (
      <div className="flex h-full w-full items-center justify-center p-6 bg-bg">
        <div className="max-w-md">
          <ErrorNote error={configQuery.error || new Error("No se pudo obtener la configuración del proveedor de mapas")} />
        </div>
      </div>
    );
  }

  const { provider, defaultCenter, defaultZoom } = configQuery.data;

  const center: [number, number] = currentSite?.center
    ? [currentSite.center.lng, currentSite.center.lat]
    : [defaultCenter.lng, defaultCenter.lat];

  const zoom = currentSite?.defaultZoom ?? defaultZoom ?? 12;

  return (
    <div className="relative flex h-full w-full flex-col overflow-hidden bg-bg">
      {/* Floating Top Controls Bar */}
      <div className="pointer-events-none absolute inset-x-0 top-3 z-10 flex items-center justify-between px-4">
        <div className="pointer-events-auto">
          <HierarchyBreadcrumb
            sites={sites}
            currentSite={currentSite}
            onSelectSite={(id) => {
              setSelectedSiteId(id);
              setSelectedCameraId(undefined);
            }}
            onClearSite={() => {
              setSelectedSiteId(undefined);
              setSelectedCameraId(undefined);
            }}
          />
        </div>

        <div className="pointer-events-auto">
          <MapToolbar
            mode={mode}
            onModeChange={setMode}
            canEdit={canEdit}
            onToggleLayers={() => setLayersOpen(!layersOpen)}
            onToggleFilters={() => setFiltersOpen(!filtersOpen)}
            layersActive={layersOpen}
            filtersActive={filtersOpen}
          />
        </div>
      </div>

      {/* Map Canvas */}
      <div className="relative h-full w-full flex-1">
        <MapCanvas
          provider={provider}
          center={center}
          zoom={zoom}
          cameras={cameras}
          sites={sites}
          selectedCameraId={selectedCameraId}
          hoveredCameraId={hoveredCameraId ?? undefined}
          onSelectCamera={(id) => setSelectedCameraId(id)}
          onHoverCamera={(id) => setHoveredCameraId(id)}
          onSelectSite={(id) => {
            setSelectedSiteId(id);
            setSelectedCameraId(undefined);
          }}
        />
      </div>
    </div>
  );
}
