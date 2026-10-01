import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { meQuery } from "@/api/queries";
import { can } from "@/lib/perm";
import { mapsConfigQuery, mapsOverviewQuery, siteEntitiesQuery } from "@/lib/maps/api";
import type { CameraEntity, MapMode } from "@/lib/maps/types";
import { MapCanvas } from "./canvas/MapCanvas";
import { MapToolbar } from "./MapToolbar";
import { HierarchyBreadcrumb } from "./HierarchyBreadcrumb";
import { CameraPreview } from "./panel/CameraPreview";
import { CameraPanel } from "./panel/CameraPanel";
import { CameraContextMenu } from "./panel/CameraContextMenu";
import { HoverIntentManager, type HoverIntentState } from "@/lib/maps/hoverIntent";
import { addCameraToLiveGrid } from "@/lib/maps/liveGridHelper";
import { ErrorNote } from "../ui";
import { Loader2 } from "lucide-react";

export interface MapShellProps {
  initialSiteId?: string;
  initialCameraId?: string;
  initialMode?: MapMode;
  canEdit?: boolean;
  onSelectSite?: (siteId: string | undefined) => void;
  onSelectCamera?: (cameraId: string | undefined) => void;
  liveOnHover?: boolean;
  onModeChange?: (mode: MapMode) => void;
}

export function MapShell({
  initialSiteId,
  initialCameraId,
  initialMode = "live",
  canEdit = false,
  liveOnHover = false,
  onSelectSite,
  onSelectCamera,
  onModeChange,
}: MapShellProps) {
  const navigate = useNavigate();
  const me = useQuery(meQuery);
  const [hoverLiveEnabled, setHoverLiveEnabled] = useState(liveOnHover);
  const [mode, setMode] = useState<MapMode>(initialMode);
  const [selectedSiteId, setSelectedSiteId] = useState<string | undefined>(initialSiteId);
  const [selectedCameraId, setSelectedCameraId] = useState<string | undefined>(initialCameraId);
  const [pinnedCameraIds, setPinnedCameraIds] = useState<string[]>(initialCameraId ? [initialCameraId] : []);
  const [contextMenu, setContextMenu] = useState<{ cameraId: string; x: number; y: number } | null>(null);
  const [hoverState, setHoverState] = useState<HoverIntentState>({
    cameraId: null,
    stage: "none",
    x: 0,
    y: 0,
  });
  const [coverage, setCoverage] = useState(true);
  const [layersOpen, setLayersOpen] = useState(false);
  const [filtersOpen, setFiltersOpen] = useState(false);

  const [hoverManager] = useState(() => new HoverIntentManager({ liveOnHover: hoverLiveEnabled }));
  useEffect(() => {
    const unsubscribe = hoverManager.subscribe(setHoverState);
    return () => {
      unsubscribe();
      hoverManager.destroy();
    };
  }, [hoverManager]);

  useEffect(() => hoverManager.setLiveOnHover(hoverLiveEnabled), [hoverManager, hoverLiveEnabled]);

  const configQuery = useQuery(mapsConfigQuery);
  const overviewQuery = useQuery(mapsOverviewQuery);

  const sites = overviewQuery.data ?? [];
  const currentSite = sites.find((s) => s.id === selectedSiteId) ?? (sites.length === 1 ? sites[0] : undefined);

  const entitiesQuery = useQuery(siteEntitiesQuery(currentSite?.id ?? ""));

  const cameras: CameraEntity[] = useMemo(() => {
    if (!entitiesQuery.data) return [];
    return entitiesQuery.data.entities.filter((e): e is CameraEntity => e.type === "camera");
  }, [entitiesQuery.data]);

  const pinnedCameras = useMemo(() => {
    return pinnedCameraIds
      .map((id) => cameras.find((c) => c.id === id))
      .filter((c): c is CameraEntity => c !== undefined);
  }, [pinnedCameraIds, cameras]);

  const hoveredCamera = useMemo(() => {
    if (!hoverState.cameraId || hoverState.stage === "none") return null;
    if (pinnedCameraIds.includes(hoverState.cameraId)) return null;
    return cameras.find((c) => c.id === hoverState.cameraId) ?? null;
  }, [hoverState, pinnedCameraIds, cameras]);

  const contextMenuCamera = useMemo(() => {
    if (!contextMenu) return null;
    return cameras.find((c) => c.id === contextMenu.cameraId) ?? null;
  }, [contextMenu, cameras]);


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

  const handleModeChange = (nextMode: MapMode) => {
    setMode(nextMode);
    onModeChange?.(nextMode);
  };

  const handleSelectSite = (siteId: string | undefined) => {
    setSelectedSiteId(siteId);
    setSelectedCameraId(undefined);
    onSelectSite?.(siteId);
    onSelectCamera?.(undefined);
  };

  const handlePinCamera = (cameraId: string) => {
    setPinnedCameraIds((prev) => {
      if (prev.includes(cameraId)) return prev;
      if (prev.length >= 4) {
        return [...prev.slice(1), cameraId]; // FIFO drop oldest
      }
      return [...prev, cameraId];
    });
  };

  const handleUnpinCamera = (cameraId: string) => {
    setPinnedCameraIds((prev) => prev.filter((id) => id !== cameraId));
  };

  const handleOpenLive = (cameraId: string) => {
    void navigate({ to: "/live", search: { camera: cameraId } });
  };

  const handleAddToLive = (cameraId: string) => {
    if (me.data?.tenant_id) {
      addCameraToLiveGrid(me.data.tenant_id, me.data.id, cameraId);
    }
  };

  const handleHoverCamera = (id: string | null, point?: { x: number; y: number }) => {
    if (id && point) {
      hoverManager.enter(id, point.x, point.y);
    } else {
      hoverManager.leave(150);
    }
  };

  const handleSelectCamera = (cameraId: string) => {
    setSelectedCameraId(cameraId);
    handlePinCamera(cameraId);
    onSelectCamera?.(cameraId);
  };

  return (
    <div className="relative flex h-full w-full flex-col overflow-hidden bg-bg">
      {/* Floating Top Controls Bar */}
      <div className="pointer-events-none absolute inset-x-0 top-3 z-10 flex items-center justify-between px-4">
        <div className="pointer-events-auto">
          <HierarchyBreadcrumb
            sites={sites}
            currentSite={currentSite}
            onSelectSite={(id) => handleSelectSite(id)}
            onClearSite={() => handleSelectSite(undefined)}
          />
        </div>

        <div className="pointer-events-auto flex items-center gap-2">
          {can(me.data, "live.view") && (
            <label className="flex items-center gap-2 rounded border border-border bg-card px-2 py-1 text-xs">
              <input type="checkbox" checked={hoverLiveEnabled}
                onChange={(event) => setHoverLiveEnabled(event.target.checked)} />
              Live on hover
            </label>
          )}
          <MapToolbar
            mode={mode}
            onModeChange={handleModeChange}
            canEdit={canEdit}
            coverage={coverage}
            onToggleCoverage={() => setCoverage(!coverage)}
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
          coverage={coverage}
          selectedCameraId={selectedCameraId}
          hoveredCameraId={hoverState.cameraId ?? undefined}
          onSelectCamera={handleSelectCamera}
          onHoverCamera={handleHoverCamera}
          onDoubleClickCamera={handleOpenLive}
          onContextMenuCamera={(id, point) => setContextMenu({ cameraId: id, ...point })}
          onSelectSite={(id) => handleSelectSite(id)}
        />

        {/* Hover preview floating card */}
        {hoveredCamera && (
          <CameraPreview
            camera={hoveredCamera}
            onHoverEnter={() => hoverManager.cancelLeave()}
            onHoverLeave={() => hoverManager.leave(150)}
            siteName={currentSite?.name}
            stage={hoverState.stage}
            position={{ x: hoverState.x, y: hoverState.y }}
            liveOnHover={hoverLiveEnabled}
            canPreview={can(me.data, "live.view")}
            onPin={handlePinCamera}
            onOpenLive={handleOpenLive}
          />
        )}

        {/* Pinned Camera Panel (max 4 pinned previews) */}
        <CameraPanel
          pinnedCameras={pinnedCameras}
          allCameras={cameras}
          sites={sites}
          onUnpin={handleUnpinCamera}
          onSelectCamera={handleSelectCamera}
          onOpenLive={handleOpenLive}
          onAddToLive={handleAddToLive}
          canPreview={can(me.data, "live.view")}
        />

        {/* Right-click Context Menu */}
        {contextMenu && contextMenuCamera && (
          <CameraContextMenu
            camera={contextMenuCamera}
            position={{ x: contextMenu.x, y: contextMenu.y }}
            onClose={() => setContextMenu(null)}
            onOpenLive={handleOpenLive}
            onAddToLive={handleAddToLive}
            onPin={handlePinCamera}
          />
        )}
      </div>
    </div>
  );
}
