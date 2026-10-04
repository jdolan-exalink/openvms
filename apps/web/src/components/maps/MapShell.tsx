import { useEffect, useMemo, useRef, useState, useSyncExternalStore, type DragEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { alarmsQuery, cameraFoldersQuery, serversQuery, meQuery, camerasQuery } from "@/api/queries";
import { can } from "@/lib/perm";
import { mapsConfigQuery, mapsOverviewQuery, siteEntitiesQuery, siteZonesQuery, unplacedCamerasQuery } from "@/lib/maps/api";
import { applyFilters, mapUserPrefsQuery, mergeLayers, saveMapUserPrefs } from "@/lib/maps/prefs";
import {
  DEFAULT_PLACEMENT,
  commitPlacement,
  emptyDraft,
  markConflict,
  pendingPlacements,
  rebasePlacement,
  updateStagedPosition,
  stagePlacement,
  type DraftPlacement,
  type DraftState,
} from "@/lib/maps/placementDraft";
import { canvasDropPoint } from "@/lib/maps/editorInteractions";
import { frameCameras, loadGeoView, MAP_SIDEBAR_PADDING, saveGeoView } from "@/lib/maps/mapView";
import { savePlacements } from "@/lib/maps/placements";
import {
  addZonePoint,
  closeZonePolygon,
  emptyZoneDraft,
  reopenZonePolygon,
  undoZonePoint,
  validateZoneDraft,
  ZONE_KIND_COLOR,
  zoneToDraft,
  type ZoneDraft,
} from "@/lib/maps/zoneDraft";
import { deleteZone, saveZone } from "@/lib/maps/zones";
import { EMPTY_FILTERS, type CameraEntity, type LayerPreference, type MapFilters, type MapMode } from "@/lib/maps/types";
import { MapRealtimeStore } from "@/lib/maps/mapRealtimeStore";
import { IncidentFocus } from "@/lib/maps/incidentPolicy";
import { MapOperationsPanel } from "./panel/MapOperationsPanel";
import { MapSocSidebar } from "./panel/MapSocSidebar";
import { loadSidebarPinned, saveSidebarPinned } from "@/lib/explorer";
import { MapMaximizedCamera } from "./panel/MapMaximizedCamera";
import { MapPlateSnapshot, type PlateSnapshotTarget } from "./panel/MapPlateSnapshot";
import { captureGrowOrigin, rectFromElement, type GrowRect } from "./panel/MapGrowFrame";
import { AlarmPanel } from "./panel/AlarmPanel";
import type { Map as MapLibreMap } from "maplibre-gl";
import { CameraEventPopups } from "./events/CameraEventPopups";
import { MapCanvas } from "./canvas/MapCanvas";
import { MapToolbar } from "./MapToolbar";
import { CameraPreview } from "./panel/CameraPreview";
import { CameraPanel } from "./panel/CameraPanel";
import { CameraContextMenu } from "./panel/CameraContextMenu";
import { LayersPanel } from "./panel/LayersPanel";
import { FiltersPanel } from "./panel/FiltersPanel";
import { DRAG_MIME } from "./editor/UnplacedTray";
import { MapEditSidebar } from "./editor/MapEditSidebar";
import { PlacementPropsForm } from "./editor/PlacementPropsForm";
import { ZonesPanel } from "./editor/ZonesPanel";
import { HoverIntentManager, type HoverIntentState } from "@/lib/maps/hoverIntent";
import { installPerfMetrics, perfMetricsEnabled, type PerfMetrics } from "@/lib/maps/perfMetrics";
import { subscribeFrames } from "@/lib/realtime";
import { saveSiteMonitoringCenter } from "@/lib/maps/sites";
import { addCameraToLiveGrid } from "@/lib/maps/liveGridHelper";
import { usePinnedMapWindows } from "./panel/usePinnedMapWindows";
import { useFeatures } from "@/lib/features";
import { Button, Checkbox, ErrorNote } from "../ui";
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
  /** Host element for the mode tabs when the workspace draws one shared bar. */
  modeSlot?: HTMLElement | null;
  /** The workspace owns the bar, so this shell does not draw a second one. */
  hostedChrome?: boolean;
  /** Map catalog shown at the top of the edit sidebar. */
  editorMaps?: ReactNode;
}

export function MapShell(props: MapShellProps) {
  const me = useQuery(meQuery);
  if (me.isError) return <ErrorNote error={me.error} />;
  if (!me.data) return <div data-testid="map-loading">Loading map identity...</div>;
  return <MapShellContent key={`${me.data.tenant_id}:${me.data.id}`} {...props} />;
}

function MapShellContent({
  initialSiteId,
  initialCameraId,
  initialMode = "live",
  canEdit = false,
  liveOnHover = false,
  onSelectSite,
  onSelectCamera,
  onModeChange,
  modeSlot,
  hostedChrome = false,
  editorMaps,
}: MapShellProps) {
  const navigate = useNavigate();
  const me = useQuery(meQuery);
  const { persistentPlayers } = useFeatures();
  const prefsQuery = useQuery(mapUserPrefsQuery);
  const storedPrefs = prefsQuery.data;
  const prefsSettled = prefsQuery.isSuccess || prefsQuery.isError;
  const [realtimeStore] = useState(() => new MapRealtimeStore(false, me.data?.tenant_id ?? undefined));
  const snapshot = useSyncExternalStore(realtimeStore.subscribe, realtimeStore.getSnapshot);
  const [focusPolicy] = useState(() => new IncidentFocus());
  // Preferences are restored at render time from the query result: the overrides only record
  // what the user changed during this visit, so a background refetch can never move a map the
  // operator is already working with, and no effect ever has to seed state.
  const focusMode = storedPrefs?.focus_mode ?? "none";
  const mapRef = useRef<MapLibreMap | null>(null);
  const [readyMap, setReadyMap] = useState<MapLibreMap | null>(null);
  useEffect(() => { realtimeStore.connect(); return () => realtimeStore.destroy(); }, [realtimeStore]);
  const [hoverLiveOverride, setHoverLiveOverride] = useState<boolean>();
  const hoverLiveEnabled = hoverLiveOverride ?? storedPrefs?.hover_live ?? liveOnHover;
  const [mode, setMode] = useState<MapMode>(initialMode);
  const [renderedMode, setRenderedMode] = useState(initialMode);
  if (renderedMode !== initialMode) {
    setRenderedMode(initialMode);
    setMode(initialMode);
  }
  const [selectedSiteId, setSelectedSiteId] = useState<string | undefined>(initialSiteId);
  // The URL is the source of truth for the selection: browser back/forward and pasted
  // deep links select sites exactly like the breadcrumb does. Adjusting during render
  // (not in an effect) keeps this a same-commit update, as react.dev recommends.
  const [renderedSiteId, setRenderedSiteId] = useState(initialSiteId);
  if (renderedSiteId !== initialSiteId) {
    setRenderedSiteId(initialSiteId);
    setSelectedSiteId(initialSiteId);
  }
  const [selectedCameraId, setSelectedCameraId] = useState<string | undefined>(initialCameraId);
  const [renderedCameraId, setRenderedCameraId] = useState(initialCameraId);
  if (renderedCameraId !== initialCameraId) {
    setRenderedCameraId(initialCameraId);
    setSelectedCameraId(initialCameraId);
  }
  const pinned = usePinnedMapWindows(me.data?.tenant_id ?? null, me.data?.id, `geo:${selectedSiteId ?? "overview"}`);
  const seededWindow = useRef("");
  useEffect(() => {
    if (!pinned.ready || !initialCameraId) return;
    const token = `${selectedSiteId ?? ""}:${initialCameraId}`;
    if (seededWindow.current === token) return;
    seededWindow.current = token;
    pinned.pin(initialCameraId);
  }, [pinned.ready, pinned.pin, initialCameraId, selectedSiteId]);
  const [contextMenu, setContextMenu] = useState<{ cameraId: string; x: number; y: number } | null>(null);
  const [hoverState, setHoverState] = useState<HoverIntentState>({
    cameraId: null,
    stage: "none",
    x: 0,
    y: 0,
  });
  const [layersOverride, setLayersOverride] = useState<LayerPreference>();
  const layers = useMemo(() => layersOverride ?? mergeLayers(storedPrefs?.layers), [layersOverride, storedPrefs]);
  const [filtersOverride, setFiltersOverride] = useState<MapFilters>();
  const filters = filtersOverride ?? storedPrefs?.filters ?? EMPTY_FILTERS;
  const [layersOpen, setLayersOpen] = useState(false);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const coverage = layers.coverage;
  const [hoverManager] = useState(() => new HoverIntentManager({ liveOnHover: hoverLiveEnabled }));
  useEffect(() => {
    const unsubscribe = hoverManager.subscribe(setHoverState);
    return () => {
      unsubscribe();
      hoverManager.destroy();
    };
  }, [hoverManager]);

  useEffect(() => {
    hoverManager.setLiveOnHover(hoverLiveEnabled && mode === "live");
    if (mode !== "live") hoverManager.leave();
  }, [hoverManager, hoverLiveEnabled, mode]);

  const configQuery = useQuery(mapsConfigQuery);
  const overviewQuery = useQuery(mapsOverviewQuery);

  // The focus policy is an external object that the alarm subscription reads, so it is kept
  // in sync from render state instead of being mutated by the control that changes it.
  useEffect(() => { focusPolicy.setMode(focusMode); }, [focusPolicy, focusMode]);

  // The first run after the query settles carries the blob that was just restored; writing
  // it straight back would race any other tab of the same user that saved in the meantime.
  const skipNextSaveRef = useRef(true);
  useEffect(() => {
    if (!prefsSettled) return;
    if (skipNextSaveRef.current) {
      skipNextSaveRef.current = false;
      return;
    }
    void saveMapUserPrefs({ layers, filters, focus_mode: focusMode, hover_live: hoverLiveEnabled });
  }, [prefsSettled, layers, filters, focusMode, hoverLiveEnabled]);

  const sites = overviewQuery.data ?? [];
  const currentSite = sites.find((s) => s.id === selectedSiteId) ?? (selectedSiteId === undefined && sites.length === 1 ? sites[0] : undefined);

  const entitiesQuery = useQuery(siteEntitiesQuery(currentSite?.id ?? ""));
  const inventoryQuery = useQuery({
    ...camerasQuery({ site_id: currentSite?.id }),
    enabled: !!currentSite && can(me.data, "cameras.view"),
  });
  const foldersQuery = useQuery({
    ...cameraFoldersQuery,
    enabled: !!currentSite && can(me.data, "cameras.view"),
  });

  // --- Placement editor (M-W8) -------------------------------------------------------
  // Everything the operator changes stays in the draft; nothing is written until Save,
  // and each save carries the revision it was based on as If-Match.
  const queryClient = useQueryClient();
  const editActive = mode === "edit" && canEdit && !!currentSite;
  const unplacedQuery = useQuery({ ...unplacedCamerasQuery(currentSite?.id ?? ""), enabled: editActive && can(me.data, "maps.edit") });
  const placedEntities = entitiesQuery.data?.entities;
  const unplacedCameras = entitiesQuery.isSuccess && placedEntities
    ? (unplacedQuery.data ?? inventoryQuery.data?.filter(camera =>
      !placedEntities.some(entity => entity.type === "camera" && entity.id === camera.id))
      .map(camera => ({ id: camera.id, name: camera.display_name, status: camera.status })))
    : undefined;
  const [draft, setDraft] = useState<DraftState>(emptyDraft);
  const [armedCameraId, setArmedCameraId] = useState<string>();
  const [activeDraftId, setActiveDraftId] = useState<string>();
  const cameraDragIdRef = useRef<string | undefined>(undefined);
  const cameraDragMovedRef = useRef(false);
  const [saving, setSaving] = useState(false);
  const [dropReady, setDropReady] = useState(false);
  const [sidebarPinned, setSidebarPinned] = useState(loadSidebarPinned);
  const toggleSidebarPin = () => setSidebarPinned((current) => {
    const next = !current;
    saveSidebarPinned(next);
    return next;
  });
  const [maximizedCameraId, setMaximizedCameraId] = useState<string>();
  const [maximizedOrigin, setMaximizedOrigin] = useState<GrowRect>();
  const [plateSnapshot, setPlateSnapshot] = useState<PlateSnapshotTarget>();
  const [saveError, setSaveError] = useState<string>();
  const pending = pendingPlacements(draft);
  const knownRevision = (entityId: string): number | undefined =>
    draft.revisions[entityId] ?? entitiesQuery.data?.entities.find((e) => e.id === entityId)?.revision;

  const placeCameraAt = (cameraId: string, point: { lng: number; lat: number }) => {
    if (!currentSite) return;
    const unplaced = (unplacedCameras ?? []).find((camera) => camera.id === cameraId);
    if (unplaced) {
      setActiveDraftId(cameraId);
      setArmedCameraId(undefined);
      setDraft(stagePlacement(draft, {
        entityId: cameraId,
        entityType: "camera",
        siteId: currentSite.id,
        lat: point.lat,
        lng: point.lng,
        ...DEFAULT_PLACEMENT,
      }));
      return;
    }
    const camera = cameras.find((item) => item.id === cameraId);
    if (!camera || camera.position.kind !== "geo") return;
    setActiveDraftId(cameraId);
    setDraft(stagePlacement(draft, {
      entityId: cameraId,
      entityType: "camera",
      siteId: currentSite.id,
      lat: point.lat,
      lng: point.lng,
      bearingDeg: camera.camera.bearingDeg ?? 0,
      fovDeg: camera.camera.fovDeg,
      rangeM: camera.camera.rangeM,
      cameraType: camera.camera.cameraType,
      ptz: camera.camera.ptz,
      lpr: camera.camera.lpr,
    }, knownRevision(cameraId)));
  };

  const handleMapClick = (point: { lng: number; lat: number }) => {
    if (!editActive) return;
    if (zoneDraft) {
      // Drawing wins: every click grows the polygon until the operator closes it.
      setZoneDraft(addZonePoint(zoneDraft, point));
      return;
    }
    if (armedCameraId) placeCameraAt(armedCameraId, point);
    else if (selectedCameraId) placeCameraAt(selectedCameraId, point);
  };

  const handleCameraDragStart = (cameraId: string) => {
    if (!editActive) return;
    cameraDragIdRef.current = cameraId;
    cameraDragMovedRef.current = false;
    setActiveDraftId(cameraId);
  };

  const handleCameraDragMove = (cameraId: string, point: { lng: number; lat: number }) => {
    if (!editActive || cameraDragIdRef.current !== cameraId) return;
    const camera = cameras.find((item) => item.id === cameraId);
    if (!camera || camera.position.kind !== "geo") return;
    const originalPosition = camera.position;
    const firstMove = !cameraDragMovedRef.current;
    cameraDragMovedRef.current = true;
    setDraft((previous) => {
      let next = previous;
      if (firstMove) {
        const current = previous.entries[cameraId] ?? {
          entityId: cameraId,
          entityType: "camera" as const,
          siteId: currentSite?.id ?? camera.siteId,
          lat: originalPosition.lat,
          lng: originalPosition.lng,
          bearingDeg: camera.camera.bearingDeg ?? 0,
          fovDeg: camera.camera.fovDeg,
          rangeM: camera.camera.rangeM,
          cameraType: camera.camera.cameraType,
          ptz: camera.camera.ptz,
          lpr: camera.camera.lpr,
        };
        next = stagePlacement(previous, current, knownRevision(cameraId));
      }
      return updateStagedPosition(next, cameraId, point.lat, point.lng);
    });
  };

  const handleCameraDragEnd = (cameraId: string, point: { lng: number; lat: number }) => {
    handleCameraDragMove(cameraId, point);
    cameraDragIdRef.current = undefined;
    cameraDragMovedRef.current = false;
  };

  const handleMapDrop = (event: DragEvent) => {
    setDropReady(false);
    if (!editActive) return;
    const cameraId = event.dataTransfer.getData(DRAG_MIME);
    if (!cameraId) return;
    event.preventDefault();
    const map = mapRef.current;
    if (!map) return;
    const rect = map.getContainer().getBoundingClientRect();
    const pixel = canvasDropPoint(map.getCanvas(), rect, event.target, event.clientX, event.clientY);
    if (!pixel) return;
    const point = map.unproject(pixel);
    placeCameraAt(cameraId, { lng: point.lng, lat: point.lat });
  };

  const handleDraftChange = (patch: Partial<DraftPlacement>) => {
    if (!activeDraftId) return;
    const current = draft.entries[activeDraftId];
    if (!current) return;
    setDraft(stagePlacement(draft, { ...current, ...patch }));
  };

  const handleSave = async () => {
    if (saving || pending.length === 0) return;
    setSaving(true);
    try {
      const outcome = await savePlacements(pending);
      setDraft((prev) => {
        let next = prev;
        for (const saved of outcome.saved) next = commitPlacement(next, saved.entityId, saved.revision);
        for (const conflict of outcome.conflicts) next = markConflict(next, conflict.entityId);
        return next;
      });
      const notices = [...outcome.conflicts, ...outcome.failed].map((item) => item.message);
      setSaveError(notices.length ? notices.join(" · ") : undefined);
      if (outcome.saved.length && currentSite) {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: ["maps", "sites", currentSite.id, "entities"] }),
          queryClient.invalidateQueries({ queryKey: ["maps", "unplaced", currentSite.id] }),
        ]);
      }
      if (outcome.saved.length > 0 && outcome.conflicts.length === 0 && outcome.failed.length === 0) {
        setArmedCameraId(undefined);
        setActiveDraftId(undefined);
        setMode("live");
        onModeChange?.("live");
      }
    } finally {
      setSaving(false);
    }
  };

  // A 409 means someone else moved the placement: re-read the site entities and rebase the
  // draft on the revision the server is actually at, keeping the operator's values.
  const handleRebase = async () => {
    if (!currentSite) return;
    const fresh = await queryClient.fetchQuery({ ...siteEntitiesQuery(currentSite.id), staleTime: 0 });
    setDraft((prev) => prev.conflicts.reduce<DraftState>(
      (next, entityId) => rebasePlacement(next, entityId, fresh.entities.find((e) => e.id === entityId)?.revision),
      prev,
    ));
  };

  // --- Zone editor (M-W9) --------------------------------------------------------------
  // The zone list is read as soon as a site is selected (the map draws the polygons), while
  // the panel and its draft only exist in edit mode; nothing is written until Guardar.
  const zonesQuery = useQuery({ ...siteZonesQuery(currentSite?.id ?? ""), enabled: !!currentSite });
  const canCreateZone = can(me.data, "maps.create_zone");
  const [zoneDraft, setZoneDraft] = useState<ZoneDraft>();
  const [zoneError, setZoneError] = useState<string>();
  const [zoneSaving, setZoneSaving] = useState(false);

  const [draftSiteId, setDraftSiteId] = useState(currentSite?.id);
  if (draftSiteId !== currentSite?.id) {
    setDraftSiteId(currentSite?.id);
    setDraft(emptyDraft());
    setArmedCameraId(undefined);
    setActiveDraftId(undefined);
    setZoneDraft(undefined);
    setZoneError(undefined);
    setSaveError(undefined);
    setMaximizedCameraId(undefined);
    setMaximizedOrigin(undefined);
    setPlateSnapshot(undefined);
    setContextMenu(null);
  }

  const handleZoneStart = () => {
    setZoneError(undefined);
    setZoneDraft(emptyZoneDraft());
  };

  const handleZoneSelect = (zoneId: string) => {
    const zone = (zonesQuery.data ?? []).find((item) => item.id === zoneId);
    if (!zone) return;
    setZoneError(undefined);
    setZoneDraft(zoneToDraft(zone));
  };

  const handleZoneDraftPatch = (patch: Partial<ZoneDraft>) => {
    setZoneDraft((prev) => (prev ? { ...prev, ...patch } : prev));
  };

  const handleZoneSave = async () => {
    if (!zoneDraft || !currentSite || zoneSaving) return;
    // The panel already blocks an invalid draft with the same messages; this is the backstop.
    if (validateZoneDraft(zoneDraft).length > 0) return;
    setZoneSaving(true);
    try {
      await saveZone(currentSite.id, zoneDraft);
      setZoneDraft(undefined);
      setZoneError(undefined);
      await queryClient.invalidateQueries({ queryKey: ["maps", "sites", currentSite.id, "zones"] });
    } catch (error) {
      setZoneError(error instanceof Error ? error.message : String(error));
    } finally {
      setZoneSaving(false);
    }
  };

  const handleZoneDelete = async (zoneId: string) => {
    if (!currentSite || zoneSaving) return;
    setZoneSaving(true);
    try {
      await deleteZone(zoneId);
      setZoneDraft((prev) => (prev?.zoneId === zoneId ? undefined : prev));
      setZoneError(undefined);
      await queryClient.invalidateQueries({ queryKey: ["maps", "sites", currentSite.id, "zones"] });
    } catch (error) {
      setZoneError(error instanceof Error ? error.message : String(error));
    } finally {
      setZoneSaving(false);
    }
  };

  const baseCameras: CameraEntity[] = useMemo(() => {
    if (!entitiesQuery.data) return [];
    return entitiesQuery.data.entities.filter((e): e is CameraEntity => e.type === "camera");
  }, [entitiesQuery.data]);

  const alarms = useQuery({ ...alarmsQuery({ site_id: currentSite?.id, limit: 100 }),
    enabled: !!currentSite && can(me.data, "alarms.view") });
  const servers = useQuery({ ...serversQuery, enabled: can(me.data, "servers.view") });
  useEffect(() => { realtimeStore.seedCameras(baseCameras); }, [baseCameras, entitiesQuery.dataUpdatedAt, realtimeStore]);
  useEffect(() => { if (alarms.data) realtimeStore.seedAlarms(alarms.data); }, [alarms.data, alarms.dataUpdatedAt, realtimeStore]);
  useEffect(() => { if (servers.data) realtimeStore.seedServers(servers.data); }, [servers.data, servers.dataUpdatedAt, realtimeStore]);
  const cameras = useMemo(() => {
    void snapshot.revision;
    return realtimeStore.patchCameras(baseCameras).cameras;
  }, [baseCameras, realtimeStore, snapshot.revision]);
  const editCameras = useMemo(() => {
    const placed = new Set(cameras.map((camera) => camera.id));
    for (const id of Object.keys(draft.entries)) placed.add(id);
    const rows = new Map<string, { id: string; name: string; status: string; serverId?: string; serverName?: string; folderId?: string | null; placed: boolean }>();
    for (const camera of inventoryQuery.data ?? []) {
      rows.set(camera.id, {
        id: camera.id,
        name: camera.display_name,
        status: camera.status,
        serverId: camera.server_id,
        serverName: servers.data?.find((server) => server.id === camera.server_id)?.name,
        folderId: camera.folder_id,
        placed: placed.has(camera.id),
      });
    }
    for (const camera of cameras) {
      if (!rows.has(camera.id)) rows.set(camera.id, { id: camera.id, name: camera.name, status: camera.status, serverId: camera.serverId, placed: true });
    }
    for (const camera of unplacedCameras ?? []) {
      const current = rows.get(camera.id);
      rows.set(camera.id, {
        id: camera.id,
        name: camera.name,
        status: camera.status,
        serverId: current?.serverId,
        serverName: current?.serverName,
        folderId: current?.folderId,
        placed: placed.has(camera.id),
      });
    }
    return [...rows.values()];
  }, [cameras, draft.entries, inventoryQuery.data, servers, unplacedCameras]);
  const treeFolders = (foldersQuery.data?.items ?? []).map((folder) => ({ id: folder.id, name: folder.name, serverId: folder.server_id, sortOrder: folder.sort_order }));
  const treeServers = (servers.data ?? []).map((server) => ({ id: server.id, name: server.name }));

  // --- Performance overlay (M-W10) --------------------------------------------------
  // Installed only in dev builds or with `?perf` in the URL: production users never pay
  // for the rAF loop, and the Playwright perf smoke passes the param to profile a
  // production build through window.__openvmsMapMetrics.
  const metricsRef = useRef<PerfMetrics | undefined>(undefined);
  useEffect(() => {
    if (!perfMetricsEnabled(window.location.search, import.meta.env.DEV)) return;
    const installed = installPerfMetrics(window);
    metricsRef.current = installed.metrics;
    return () => {
      metricsRef.current = undefined;
      installed.stop();
    };
  }, []);

  useEffect(() => {
    const metrics = metricsRef.current;
    if (!metrics) return;
    metrics.setEntitiesVisible(cameras.length);
    if (cameras.length === 0) return;
    // "First render" is the first paint after the data is on screen: the mark waits two
    // frames so the initial GeoJSON/cluster build is cold-start work, not a budget hit.
    let second = 0;
    const first = requestAnimationFrame(() => {
      second = requestAnimationFrame(() => metrics.markFirstRender());
    });
    return () => {
      cancelAnimationFrame(first);
      cancelAnimationFrame(second);
    };
  }, [cameras.length]);

  useEffect(() => {
    const metrics = metricsRef.current;
    if (!metrics) return;
    return subscribeFrames((frame) => {
      metrics.recordEventsApplied(1);
      const ts = (frame as { ts?: number | string }).ts;
      if (typeof ts === "number") metrics.recordWsLag(Date.now() - ts);
      else if (typeof ts === "string") metrics.recordWsLag(Date.now() - new Date(ts).getTime());
    });
  }, []);

  // Panels read `cameras` (everything authorized); the canvas only draws what survives the
  // active filters, so a filtered-out camera is still pinnable from an existing deep link.
  const visibleCameras = useMemo(() => applyFilters(cameras.map((camera) => {
    const staged = draft.entries[camera.id];
    if (!staged) return camera;
    return {
      ...camera,
      position: { kind: "geo" as const, lat: staged.lat, lng: staged.lng },
      camera: {
        ...camera.camera,
        bearingDeg: staged.bearingDeg,
        fovDeg: staged.fovDeg,
        rangeM: staged.rangeM,
        cameraType: staged.cameraType ?? camera.camera.cameraType,
        ptz: staged.ptz ?? camera.camera.ptz,
        lpr: staged.lpr ?? camera.camera.lpr,
      },
    };
  }), filters), [cameras, filters, draft.entries]);
  const layerVisibility = useMemo(() => ({
    cameras: layers.cameras,
    sites: layers.sites,
    coverage: layers.coverage,
    alarmFx: layers.events_alarm,
    detectionFx: layers.events_motion,
  }), [layers]);
  useEffect(() => {
    let lastEvent = realtimeStore.getRecentEvents(1)[0]?.id;
    return realtimeStore.subscribe(() => {
      const event = realtimeStore.getRecentEvents(1)[0];
      if (!event || event.id === lastEvent) return;
      lastEvent = event.id;
      if (!event.type.startsWith("alarm.") || !mapRef.current || (event.siteId && event.siteId !== currentSite?.id)) return;
      const data = event.data as { status?: string; camera_id?: string } | undefined;
      if (["resolved", "closed"].includes(data?.status ?? "")) return;
      const camera = baseCameras.find(item => item.id === (event.cameraId ?? data?.camera_id));
      if (!camera || !focusPolicy.accept(camera, currentSite?.id) || camera.position.kind !== "geo") return;
      const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
      mapRef.current.easeTo({ center: [camera.position.lng, camera.position.lat], duration: reduced ? 0 : 500 });
    });
  }, [baseCameras, currentSite?.id, focusPolicy, realtimeStore]);

  const pinnedCameras = useMemo(() => {
    return pinned.windows
      .map((window) => cameras.find((camera) => camera.id === window.id))
      .filter((camera): camera is CameraEntity => camera !== undefined);
  }, [pinned.windows, cameras]);

  const hoveredCamera = useMemo(() => {
    if (!hoverState.cameraId || hoverState.stage === "none") return null;
    if (pinned.windows.some((window) => window.id === hoverState.cameraId)) return null;
    return cameras.find((c) => c.id === hoverState.cameraId) ?? null;
  }, [hoverState, pinned.windows, cameras]);

  const contextMenuCamera = useMemo(() => {
    if (!contextMenu) return null;
    return cameras.find((c) => c.id === contextMenu.cameraId) ?? null;
  }, [contextMenu, cameras]);


  // --- Monitoring center (M-W11) -------------------------------------------------------
  // Default view: the selected site's monitoring center, else the platform default. All
  // hooks in this block live above the config early-returns (hook order is fixed).
  const defaultCenter = configQuery.data?.defaultCenter;
  const siteCenter = currentSite?.center;
  const center = useMemo<[number, number]>(() => (
    siteCenter ? [siteCenter.lng, siteCenter.lat] : [defaultCenter?.lng ?? 0, defaultCenter?.lat ?? 0]
  ), [siteCenter, defaultCenter]);
  const zoom = currentSite?.defaultZoom ?? configQuery.data?.defaultZoom ?? 12;

  // The first visit frames every camera beside the sidebar. After that, the operator's
  // last center and zoom for this site win, including when the same map opens in Live.
  const followViewRef = useRef<{ center?: [number, number]; zoom?: number } | undefined>(undefined);
  const framedSite = useRef<string | undefined>(undefined);
  const geoPoints = useMemo(() => cameras.flatMap((camera) => (
    camera.siteId === currentSite?.id && camera.position.kind === "geo"
      ? [{ lng: camera.position.lng, lat: camera.position.lat }]
      : []
  )), [cameras, currentSite?.id]);
  useEffect(() => {
    const map = mapRef.current;
    const siteId = currentSite?.id;
    if (!map || !siteId || !me.data || typeof map.jumpTo !== "function") return;
    if (framedSite.current === siteId) return;
    const saved = loadGeoView(me.data.tenant_id, me.data.id, siteId);
    const container = typeof map.getContainer === "function" ? map.getContainer() : undefined;
    const viewport = {
      width: container && container.clientWidth > 64 ? container.clientWidth : 1280,
      height: container && container.clientHeight > 64 ? container.clientHeight : 720,
    };
    const view = saved ?? (geoPoints.length ? frameCameras(geoPoints, viewport, MAP_SIDEBAR_PADDING) : null);
    if (!view) return;
    framedSite.current = siteId;
    if (!saved) saveGeoView(me.data.tenant_id, me.data.id, siteId, view);
    map.jumpTo({ center: view.center, zoom: view.zoom });
  }, [center, zoom, currentSite?.id, geoPoints, me.data, readyMap]);
  useEffect(() => {
    const map = mapRef.current;
    const prev = followViewRef.current;
    followViewRef.current = { center, zoom };
    if (!map || !prev) return;
    if (prev.center?.[0] === center[0] && prev.center?.[1] === center[1] && prev.zoom === zoom) return;
    if (typeof map.jumpTo === "function" && currentSite && (loadGeoView(me.data?.tenant_id ?? null, me.data?.id ?? "", currentSite.id) || geoPoints.length)) return;
    const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
    map.easeTo({ center, zoom, duration: reduced ? 0 : 500 });
  }, [center, zoom, currentSite, geoPoints.length, me.data]);

  const [centerSaving, setCenterSaving] = useState(false);
  const [centerError, setCenterError] = useState<string>();
  const handleSetMonitoringCenter = async () => {
    const map = mapRef.current;
    if (!map || !currentSite || centerSaving) return;
    setCenterSaving(true);
    setCenterError(undefined);
    try {
      const view = map.getCenter();
      await saveSiteMonitoringCenter(currentSite.id, view.lat, view.lng, Math.round(map.getZoom()));
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["maps", "overview"] }),
        queryClient.invalidateQueries({ queryKey: ["sites"] }),
      ]);
    } catch (cause) {
      setCenterError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setCenterSaving(false);
    }
  };

  if (configQuery.isLoading) {
    return (
      <div className="flex h-full w-full items-center justify-center bg-bg" data-testid="map-loading">
        <div className="flex flex-col items-center gap-2 text-on-surface-variant">
          <Loader2 className="size-6 animate-spin text-primary" aria-hidden />
          <span className="text-sm">Cargando mapa operativo...</span>
        </div>
      </div>
    );
  }

  if (configQuery.isError || !configQuery.data) {
    return (
      <div className="flex h-full w-full items-center justify-center p-6 bg-bg">
        <div className="flex max-w-md flex-col items-start gap-3 rounded-m3-xl bg-surface-1 p-5">
          <ErrorNote error={configQuery.error || new Error("No se pudo obtener la configuración del proveedor de mapas")} />
          <Button variant="tonal" onClick={() => void configQuery.refetch()}>Reintentar</Button>
        </div>
      </div>
    );
  }

  const { provider } = configQuery.data;

  const handleModeChange = (nextMode: MapMode) => {
    setMode(nextMode);
    onModeChange?.(nextMode);
  };

  const modeChrome = (
    <>
      <MapToolbar
        embedded={!!modeSlot}
        mode={mode}
        onModeChange={handleModeChange}
        canEdit={canEdit}
        coverage={coverage}
        onToggleCoverage={() => setLayersOverride({ ...layers, coverage: !layers.coverage })}
        onToggleLayers={() => setLayersOpen(!layersOpen)}
        onToggleFilters={() => setFiltersOpen(!filtersOpen)}
        layersActive={layersOpen}
        filtersActive={filtersOpen}
      />
      {can(me.data, "live.view") && (
        <Checkbox className="order-6 px-2 text-xs font-medium text-on-surface-variant" checked={hoverLiveEnabled} onChange={setHoverLiveOverride} label="Live on hover" />
      )}
    </>
  );


  const handleSelectSite = (siteId: string | undefined) => {
    focusPolicy.interact();
    setSelectedSiteId(siteId);
    setSelectedCameraId(undefined);
    onSelectSite?.(siteId);
    onSelectCamera?.(undefined);
  };

  const handlePinCamera = (cameraId: string) => {
    pinned.pin(cameraId);
  };

  const handleUnpinCamera = (cameraId: string) => {
    pinned.unpin(cameraId);
    if (selectedCameraId === cameraId) {
      setSelectedCameraId(undefined);
      onSelectCamera?.(undefined);
    }
  };

  const handleOpenLive = (cameraId: string) => {
    if (!can(me.data, "live.view")) return;
    setMaximizedOrigin(captureGrowOrigin(cameraId) ?? rectFromElement(document.activeElement));
    setMaximizedCameraId(cameraId);
    setContextMenu(null);
  };

  const handleAddToLive = (cameraId: string) => {
    if (can(me.data, "live.view") && me.data?.tenant_id) {
      addCameraToLiveGrid(me.data.tenant_id, me.data.id, cameraId);
    }
  };

  const handleHoverCamera = (id: string | null, point?: { x: number; y: number }) => {
    if (mode !== "live" || !can(me.data, "live.view")) return;
    if (id && point) {
      hoverManager.enter(id, point.x, point.y);
    } else {
      hoverManager.leave(150);
    }
  };

  const handleSelectCamera = (cameraId: string) => {
    focusPolicy.interact();
    setSelectedCameraId(cameraId);
    const camera = cameras.find(item => item.id === cameraId);
    if (camera?.position.kind === "geo") {
      const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
      const map = mapRef.current;
      if (map) map.easeTo({ center: [camera.position.lng, camera.position.lat], zoom: Math.max(map.getZoom(), 15), duration: reduced ? 0 : 500 });
    }
    if (mode === "live") handlePinCamera(cameraId);
    onSelectCamera?.(cameraId);
  };

  return (
    <div className="relative flex h-full min-h-0 w-full flex-1 flex-col overflow-hidden bg-bg"
      onPointerDownCapture={() => focusPolicy.interact()} onWheelCapture={() => focusPolicy.interact()}
      onKeyDownCapture={() => focusPolicy.interact()}
      onDragOver={(event) => {
        if (!editActive || !Array.from(event.dataTransfer.types ?? []).includes(DRAG_MIME)) return;
        const map = mapRef.current;
        const valid = !!map && !!canvasDropPoint(map.getCanvas(), map.getContainer().getBoundingClientRect(), event.target, event.clientX, event.clientY);
        setDropReady(valid);
        if (valid) { event.preventDefault(); event.dataTransfer.dropEffect = "copy"; }
      }}
      onDragLeave={() => setDropReady(false)}
      onDragEnd={() => setDropReady(false)}
      onDrop={handleMapDrop}>
      {dropReady && <div role="status" className="pointer-events-none absolute inset-2 z-40 flex items-center justify-center rounded-m3-xl border-2 border-dashed border-primary bg-primary/10">
        <span className="rounded-full bg-primary-container px-4 py-2 text-sm font-bold text-on-primary-container shadow-lg">Suelta la cámara para colocarla</span>
      </div>}
      {modeSlot ? createPortal(modeChrome, modeSlot) : hostedChrome ? null : (
        <div className="pointer-events-none absolute left-3 top-3 z-30">
          <div className="pointer-events-auto flex flex-wrap items-center gap-2 rounded-m3-xl bg-surface-1/95 p-1 shadow-lg backdrop-blur">{modeChrome}</div>
        </div>
      )}

      {/* Map Canvas */}
      <div data-map-stage className="relative min-h-0 w-full flex-1 overflow-hidden">
        <MapCanvas
          realtimeStore={realtimeStore}
          onMapReady={map => {
            mapRef.current = map;
            setReadyMap(map);
            // The long-task budget starts when MapLibre first reports a full render:
            // tasks before idle are init work, tasks after idle are the steady state
            // the design budgets at 50 ms.
            if (metricsRef.current) {
              map.once("idle", () => metricsRef.current?.markRenderSettled());
            }
          }}
          provider={provider}
          center={center}
          zoom={zoom}
          cameras={visibleCameras}
          sites={sites}
          zones={zonesQuery.data ?? []}
          coverage={coverage}
          layerVisibility={layerVisibility}
          selectedCameraId={selectedCameraId}
          hoveredCameraId={hoverState.cameraId ?? undefined}
          onSelectCamera={handleSelectCamera}
          onHoverCamera={handleHoverCamera}
          onDoubleClickCamera={handleOpenLive}
          onContextMenuCamera={(id, point) => setContextMenu({ cameraId: id, ...point })}
          onSelectSite={(id) => handleSelectSite(id)}
          onMapClick={handleMapClick}
          onMoveEnd={(view) => {
            if (!me.data || !currentSite || framedSite.current !== currentSite.id) return;
            saveGeoView(me.data.tenant_id, me.data.id, currentSite.id, { center: view.center, zoom: view.zoom });
          }}
          onCameraDragStart={editActive && !(zoneDraft && !zoneDraft.closed) ? handleCameraDragStart : undefined}
          onCameraDragMove={editActive && !(zoneDraft && !zoneDraft.closed) ? handleCameraDragMove : undefined}
          onCameraDragEnd={editActive && !(zoneDraft && !zoneDraft.closed) ? handleCameraDragEnd : undefined}
          drawingZone={!!zoneDraft && !zoneDraft.closed}
          zoneSketch={zoneDraft ? { points: zoneDraft.points, color: zoneDraft.color || ZONE_KIND_COLOR[zoneDraft.kind], closed: zoneDraft.closed } : undefined}
          clusterCameras={!editActive}
        />

        <CameraEventPopups map={readyMap} cameras={visibleCameras} tenantId={me.data?.tenant_id}
          siteId={currentSite?.id} canEvents={can(me.data, "events.view")} canSnapshots={can(me.data, "snapshots.view")}
          quietNoticeId={plateSnapshot?.id}
          onOpenPlate={(target) => setPlateSnapshot(target)} />

        {!editActive && (
          <MapSocSidebar
            pinned={sidebarPinned}
            onTogglePin={toggleSidebarPin}
            alarmCount={alarms.data?.length ?? 0}
            showAlarms={!!currentSite && can(me.data, "alarms.view")}
            showLpr={can(me.data, "lpr.view")}
            siteId={currentSite?.id}
            onSelectCamera={handleSelectCamera}
            onOpenRead={setPlateSnapshot}
            cameras={<>
              <MapOperationsPanel
                mode={mode} sites={sites} currentSite={currentSite} requestedSiteId={selectedSiteId}
                cameras={cameras} inventory={inventoryQuery.data} folders={treeFolders} servers={treeServers} visibleCount={visibleCameras.length}
                camerasVisible={layers.cameras} canEdit={canEdit}
                canLive={can(me.data, "live.view")} canEvents={can(me.data, "events.view")}
                canPlayback={can(me.data, "recordings.view")} canInventory={can(me.data, "cameras.view")}
                selectedCameraId={selectedCameraId}
                loading={overviewQuery.isLoading || (!!currentSite && entitiesQuery.isLoading)}
                errors={[
                  { label: "Site overview", error: overviewQuery.error, retry: () => void overviewQuery.refetch() },
                  { label: "Camera placements", error: entitiesQuery.error, retry: () => void entitiesQuery.refetch() },
                  { label: "Camera inventory", error: inventoryQuery.error, retry: () => void inventoryQuery.refetch() },
                  { label: "Zones", error: zonesQuery.error, retry: () => void zonesQuery.refetch() },
                  { label: "Unplaced cameras", error: unplacedQuery.error, retry: () => void unplacedQuery.refetch() },
                  { label: "Saved preferences", error: prefsQuery.error, retry: () => void prefsQuery.refetch() },
                ]}
                onSelectSite={handleSelectSite} onSelectCamera={handleSelectCamera}
                onEdit={() => handleModeChange("edit")} onOpenLive={handleOpenLive}
                onEvents={(cameraId) => void navigate({ to: "/events", search: { camera: cameraId } })}
                onPlayback={(cameraId) => void navigate({ to: "/playback", search: { camera: cameraId } })}
                onResetVisibility={() => { setFiltersOverride(EMPTY_FILTERS); setLayersOverride({ ...layers, cameras: true, sites: true }); }}
              />
            </>}
            alarms={<>
              {alarms.isError && <ErrorNote error={alarms.error} />}
              <AlarmPanel alarms={alarms.data ?? []} canManage={can(me.data, "alarms.manage")} />
            </>}
          />
        )}

        {/* Layer and filter preference panels */}
        {(layersOpen || filtersOpen) && (
          <div className="absolute right-3 top-16 z-20 flex flex-col items-end gap-2">
            {layersOpen && (
              <LayersPanel layers={layers} onChange={(next) => setLayersOverride(next)} onClose={() => setLayersOpen(false)} />
            )}
            {filtersOpen && (
              <FiltersPanel filters={filters} onChange={(next) => setFiltersOverride(next)} onClose={() => setFiltersOpen(false)} />
            )}
          </div>
        )}

        {editActive && editorMaps && (
          <div className="absolute left-3 top-16 z-30 w-72 max-w-[calc(100%-1.5rem)] rounded-m3-xl bg-surface-1/95 p-2 text-on-surface shadow-2xl backdrop-blur">
            {editorMaps}
          </div>
        )}

        {editActive && (
          <aside aria-label="Edición del mapa" className="absolute bottom-3 right-3 top-16 z-30 flex w-80 max-w-[calc(100%-1.5rem)] min-h-0 flex-col overflow-hidden rounded-m3-xl bg-surface-1/95 text-on-surface shadow-2xl backdrop-blur">
            <header className="px-4 pb-2 pt-3">
              <h2 className="text-lg font-bold">Edición</h2>
              <p className="font-mono text-[11px] text-on-surface-variant">{pending.length} cambio(s) sin guardar</p>
            </header>
            <div className="shrink-0 space-y-2 px-2 pb-2">
              {unplacedCameras === undefined && (
                <p role="status" className="text-xs text-on-surface-variant">El inventario de cámaras sin ubicar no está disponible.</p>
              )}
              {saveError && <p role="alert" className="text-xs text-bad">{saveError}</p>}
              {draft.conflicts.length > 0 && (
                <Button variant="danger" size="sm" onClick={() => void handleRebase()}>
                  Rebase
                </Button>
              )}
              {can(me.data, "maps.edit") && (
                <div>
                  {centerError && <p role="alert" className="mb-1 text-xs text-bad">{centerError}</p>}
                  <Button
                    variant="tonal"
                    size="sm"
                    disabled={centerSaving}
                    onClick={() => void handleSetMonitoringCenter()}
                    className="w-full"
                    title="Guarda la vista actual como centro del sitio: al entrar al mapa se abre aquí"
                  >
                    Fijar centro de monitoreo aquí
                  </Button>
                </div>
              )}
              {activeDraftId && draft.entries[activeDraftId] && (
                <PlacementPropsForm
                  name={cameras.find((camera) => camera.id === activeDraftId)?.name
                    ?? (unplacedCameras ?? []).find((camera) => camera.id === activeDraftId)?.name
                    ?? "Cámara"}
                  draft={draft.entries[activeDraftId]}
                  onChange={handleDraftChange}
                />
              )}
            </div>
            <div className="flex min-h-0 flex-1 flex-col">
              <div className="min-h-0 flex-1 p-2">
                <MapEditSidebar cameras={editCameras} folders={treeFolders} servers={treeServers} armedId={armedCameraId} onArm={setArmedCameraId} />
              </div>
              <div className="max-h-[48%] shrink-0 overflow-auto bg-surface-2">
                <ZonesPanel
                  zones={zonesQuery.data ?? []}
                  draft={zoneDraft}
                  canEdit={canCreateZone}
                  error={zoneError}
                  saving={zoneSaving}
                  onStartCreate={handleZoneStart}
                  onSelectZone={handleZoneSelect}
                  onDraftChange={handleZoneDraftPatch}
                  onClosePolygon={() => setZoneDraft((prev) => (prev ? closeZonePolygon(prev) : prev))}
                  onReopenPolygon={() => setZoneDraft((prev) => (prev ? reopenZonePolygon(prev) : prev))}
                  onUndoPoint={() => setZoneDraft((prev) => (prev ? undoZonePoint(prev) : prev))}
                  onSave={() => void handleZoneSave()}
                  onDelete={(zoneId) => void handleZoneDelete(zoneId)}
                  onCancel={() => {
                    setZoneDraft(undefined);
                    setZoneError(undefined);
                  }}
                />
              </div>
            </div>
            <footer className="flex shrink-0 gap-2 bg-surface-2 p-3">
              <Button
                variant="outlined"
                className="flex-1"
                onClick={() => {
                  setDraft(emptyDraft());
                  setArmedCameraId(undefined);
                  setActiveDraftId(undefined);
                  setSaveError(undefined);
                  handleModeChange("live");
                }}
              >
                Cancelar
              </Button>
              <Button variant="filled" className="flex-1" onClick={() => void handleSave()} disabled={saving || pending.length === 0}>
                {pending.length > 0 ? `Guardar (${pending.length})` : "Guardar ubicaciones"}
              </Button>
            </footer>
          </aside>
        )}
        {/* Hover preview floating card */}
        {mode === "live" && can(me.data, "live.view") && hoveredCamera && hoveredCamera.id !== maximizedCameraId && (
          <CameraPreview
            camera={hoveredCamera}
            onHoverEnter={() => hoverManager.cancelLeave()}
            onHoverLeave={() => hoverManager.leave(150)}
            siteName={currentSite?.name}
            stage={hoverState.stage}
            position={{ x: hoverState.x, y: hoverState.y }}
            liveOnHover={hoverLiveEnabled}
            persistent={persistentPlayers}
            canPreview={can(me.data, "live.view")}
            onPin={handlePinCamera}
            onOpenLive={handleOpenLive}
          />
        )}

        {/* Pinned Camera Panel (max 4 pinned previews) */}
        <CameraPanel
          pinnedCameras={mode === "live" && can(me.data, "live.view") ? pinnedCameras.filter((camera) => camera.id !== maximizedCameraId) : []}
          windows={pinned.windows}
          sites={sites}
          onUnpin={handleUnpinCamera}
          onOpenLive={handleOpenLive}
          onMove={pinned.move}
          onArrange={pinned.arrange}
          persistent={persistentPlayers}
          canPreview={can(me.data, "live.view")}
        />

        {/* Right-click Context Menu */}
        {maximizedCameraId && cameras.find((camera) => camera.id === maximizedCameraId) && (
          <MapMaximizedCamera
            camera={cameras.find((camera) => camera.id === maximizedCameraId)!}
            origin={maximizedOrigin}
            persistent={persistentPlayers}
            closeOnEscape={!plateSnapshot}
            onClose={() => { setMaximizedCameraId(undefined); setMaximizedOrigin(undefined); }}
          />
        )}

        {plateSnapshot && <MapPlateSnapshot target={plateSnapshot} onClose={() => setPlateSnapshot(undefined)} />}

        {mode === "live" && can(me.data, "live.view") && contextMenu && contextMenuCamera && (
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
