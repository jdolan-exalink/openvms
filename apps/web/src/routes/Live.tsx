import { ChevronsRight } from "lucide-react";
import { Icon } from "@/components/Icon";
import { DndContext, DragOverlay, type DragEndEvent, useDroppable } from "@dnd-kit/core";
import { rectSortingStrategy, rectSwappingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createPortal } from "react-dom";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Camera, CircleCheck, CircleHelp, CircleX, Compass, Fish, History, Maximize2, Minimize2, SlidersHorizontal, X } from "lucide-react";
import { type CSSProperties, type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { camerasQuery, meQuery, serversQuery, sitesQuery, viewsQuery } from "@/api/queries";
import { LiveMapTile } from "@/components/maps/LiveMapTile";
import { LiveVisionTile } from "@/components/live/LiveVisionTile";
import { DetectionListOverlay } from "@/components/live/DetectionListOverlay";
import { MsePlayer } from "@/components/MsePlayer";
import { CodecBadge } from "@/components/CodecBadge";
import { QualityBadge } from "@/components/QualityBadge";
import { TelemetryOverlay } from "@/components/live/TelemetryOverlay";
import { telemetryRuntime, useAdaptiveVideoPolicy } from "@/lib/telemetry";
import { LAYER_KEYS } from "@/components/clientStation/keys";
import { FisheyeViewer } from "@/components/fisheye/FisheyeViewer";
import { FisheyeSection } from "@/components/fisheye/FisheyeSection";
import { getEffectiveVirtualViews, getFisheyeConfig } from "@/lib/fisheye/storage";
import { detectFisheyeProfile } from "@/lib/fisheye/profiles";
import type { VirtualFisheyeView } from "@/lib/fisheye/types";
import { useFeatures } from "@/lib/features";
import { LivePlaybackPanel } from "@/components/LivePlaybackPanel";
import { LiveRecDock } from "@/components/LiveRecDock";
import { RecTile, type RecTileState } from "@/components/RecTile";
import { LiveModeToggle } from "@/components/LiveModeToggle";
import { setContextSidebarCollapsed, useTopBarActionsPortalTarget } from "@/components/AppShell";
import { streamCodec } from "@/lib/cameraCodec";
import { useT } from "@/i18n";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { type ExplorerActions, LiveExplorer } from "@/components/LiveExplorer";
import { Modal } from "@/components/Modal";
import { ContextMenu, type MenuItem } from "@/components/ContextMenu";
import { FullscreenButton, LayoutMenu, PresentationEditor } from "@/components/Presentations";
import { Button, ErrorNote, IconButton, Switch, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import {
  cameraIdOf, cleanFisheyeVirtualViewTiles, duplicateTileIndexes, feviewFromDragId, fillTiles, fitTiles, growLayout, isMapTile, isVisionTile, LIVE_GRID_DROP_ID, liveSelectionKey, mapFromDragId, parseSelection, placeCameraAt, placeCameraUnique, placeInOpenCell, placeMapAt, placeVisionAt, placeVisionInOpenCell, resizeTiles, resolveDragEnd, reorderTiles, tileIdentityKey, tilesToViewCells, viewCellsToTiles,
  serializeSelection, swapTiles, tileDragId, tilesForFisheyeMultiView, visionFromDragId,
  type Quality, type Tile, type LiveVisionRef,
} from "@/lib/liveGrid";
import { DEFAULT_PRESENTATIONS, loadCatalog, presentationForCount, recallViewPanes, rememberViewPanes, saveCatalog, uniformPanes, type Pane, type Presentation } from "@/lib/presentations";
import { camerasOfServer as serverCameras, camerasOfSite, loadSidebarPinned, saveSidebarPinned } from "@/lib/explorer";
import { can } from "@/lib/perm";
import { LivePhone } from "@/components/LivePhone";
import { useIsPhoneDevice } from "@/lib/useIsPhoneDevice";
import { useLiveDragSensors } from "@/lib/dndSensors";
import { useTouchDoubleTap } from "@/lib/useTouchDoubleTap";
import { useCameraFolders } from "@/lib/useCameraFolders";
import { useRecSession } from "@/lib/useRecSession";
import { usePlayerSessionManager } from "@/lib/live/PlayerSessionProvider";
import { IDENTITY_ZOOM, type DigitalZoom } from "@/lib/live/digitalZoom";

/** copyViewName picks «Nombre (copia)» or «Nombre (copia 2)» so a duplicate stays unique. */
/** Empties a restored tile whose saved fisheye view no longer exists (e.g. removed quadrant views). */
function dropUnresolvedFisheyeTile(tile: Tile): Tile {
  if (!tile || !("camera_id" in tile) || !tile.fisheye_view_id) return tile;
  const config = getFisheyeConfig(tile.camera_id);
  const resolves =
    getEffectiveVirtualViews(config).some((v) => v.id === tile.fisheye_view_id) ||
    config.views.some((v) => v.id === tile.fisheye_view_id);
  return resolves ? tile : null;
}

function copyViewName(name: string, taken: Set<string>): string {
  const base = `${name} (copia)`;
  if (!taken.has(base.toLowerCase())) return base;
  for (let n = 2; n < 100; n++) {
    const candidate = `${name} (copia ${n})`;
    if (!taken.has(candidate.toLowerCase())) return candidate;
  }
  return `${base} ${taken.size + 1}`;
}

/** Explorer panel is w-80 (20rem) from the rail edge; main already pads 2rem, plus an 0.5rem gap. */
const PINNED_STYLE = { "--pinned-offset": "18.5rem" } as CSSProperties;

/**
 * Live is the multi-server live screen (PRD §46-49): a camera tree grouped by site and
 * server, a grid of live tiles from any Frigate, and saved views.
 */
function LiveDesktop() {
  const tr = useT();
  const me = useQuery(meQuery);
  const cameras = useQuery(camerasQuery({}));
  const sites = useQuery(sitesQuery);
  const servers = useQuery(serversQuery);
  const views = useQuery(viewsQuery);
  const qc = useQueryClient();

  const [columns, setColumns] = useState(2);
  const [rows, setRows] = useState(2);
  const [panes, setPanes] = useState<Pane[]>(() => uniformPanes(2, 2));
  const [catalog, setCatalog] = useState<Presentation[]>(loadCatalog);
  const [editorOpen, setEditorOpen] = useState(false);
  const [tiles, setTiles] = useState<Tile[]>(() => Array(4).fill(null));
  const [keepVisionVisible, setKeepVisionVisible] = useState(false);
  const visionTile = useMemo(() => tiles.find(isVisionTile), [tiles]);
  const [selected, setSelected] = useState(0);
  const [focus, setFocus] = useState<number | null>(null);
  const [qualityOverrides, setQualityOverrides] = useState<Record<string, { quality: Quality; identities: string[]; source: "manual" | "maximize" }>>({});
  const [viewId, setViewId] = useState("");
  const [playbackCameraId, setPlaybackCameraId] = useState<string | null>(null);
  const [viewDialog, setViewDialog] = useState<null | { mode: "create" } | { mode: "edit"; id: string }>(null);
  const [dialogName, setDialogName] = useState("");
  const [dialogShared, setDialogShared] = useState(false);
  // Set once the saved grid selection (or the default) has been applied, so the persistence
  // effect below never fires before restoration and overwrites a saved selection with defaults.
  const [restored, setRestored] = useState(false);
  const tilesRef = useRef(tiles);
  const selectedRef = useRef(selected);
  const qualityOverridesRef = useRef(qualityOverrides);
  const qualityTimersRef = useRef(new Map<string, ReturnType<typeof setTimeout>>());
  const maximizedCameraIdsRef = useRef(new Set<string>());
  const [invalidatedOverrideKeys, setInvalidatedOverrideKeys] = useState<Set<string>>(() => new Set());
  const previousFocusedCameraRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    tilesRef.current = tiles;
    selectedRef.current = selected;
  }, [tiles, selected]);
  useEffect(() => {
    qualityOverridesRef.current = qualityOverrides;
  }, [qualityOverrides]);
  const [consumedCamera, setConsumedCamera] = useState<string | undefined>();
  const folderApi = useCameraFolders(cameras.data);
  const [sidebarPinned, setSidebarPinnedState] = useState(loadSidebarPinned);
  const [edgeOpen, setEdgeOpen] = useState(false);
  const edgeHover = useRef(false);
  const dragging = useRef(false);
  const pinnedRef = useRef(sidebarPinned);
  const hideEdge = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const [treeNotice, setTreeNotice] = useState<string | null>(null);
  const [noticeTone, setNoticeTone] = useState<"ok" | "warn">("warn");
  const [deleteViewId, setDeleteViewId] = useState<string | null>(null);
  const [fisheyeModalCameraId, setFisheyeModalCameraId] = useState<string | null>(null);
  const [fisheyeConfigCameraId, setFisheyeConfigCameraId] = useState<string | null>(null);
  const flash = (text: string, tone: "ok" | "warn" = "ok") => {
    setNoticeTone(tone);
    setTreeNotice(text);
    setTimeout(() => setTreeNotice(null), 4000);
  };
  const [activeDrag, setActiveDrag] = useState<string | null>(null);
  const setSidebarPinned = (next: boolean) => {
    pinnedRef.current = next;
    setSidebarPinnedState(next);
    saveSidebarPinned(next);
  };
  const revealEdge = () => {
    edgeHover.current = true;
    clearTimeout(hideEdge.current);
    setEdgeOpen(true);
  };
  const concealEdge = () => {
    edgeHover.current = false;
    clearTimeout(hideEdge.current);
    hideEdge.current = setTimeout(() => {
      if (!edgeHover.current && !pinnedRef.current && !dragging.current) setEdgeOpen(false);
    }, 280);
  };
  useEffect(() => () => clearTimeout(hideEdge.current), []);
  // Touch has no hover to leave: an unpinned edge explorer closes on a tap outside it (menus and dialogs it opened excluded).
  useEffect(() => {
    if (!edgeOpen || sidebarPinned) return;
    const onDown = (event: PointerEvent) => {
      if (event.pointerType !== "touch") return;
      if ((event.target as Element | null)?.closest?.('[data-testid="live-edge"], [data-context-menu], [role="dialog"]')) return;
      edgeHover.current = false;
      setEdgeOpen(false);
    };
    document.addEventListener("pointerdown", onDown, true);
    return () => document.removeEventListener("pointerdown", onDown, true);
  }, [edgeOpen, sidebarPinned]);
  // The explorer is the same edge panel in the page and in fullscreen, so the shell column stays shut.
  useEffect(() => {
    setContextSidebarCollapsed(true);
    return () => setContextSidebarCollapsed(false);
  }, []);
  const topBar = useTopBarActionsPortalTarget();
  const { persistentPlayers, videoSurfaceLayer, adaptiveStreaming } = useFeatures();
  const playerSessionManager = usePlayerSessionManager();
  const [fullscreen, setFullscreen] = useState(false);
  useEffect(() => {
    const sync = () => {
      const on = document.fullscreenElement === document.documentElement;
      setFullscreen(on);
      document.documentElement.toggleAttribute("data-live-fullscreen", on);
    };
    document.addEventListener("fullscreenchange", sync);
    return () => {
      document.removeEventListener("fullscreenchange", sync);
      document.documentElement.removeAttribute("data-live-fullscreen");
    };
  }, []);
  const toggleFullscreen = () => {
    if (document.fullscreenElement) void document.exitFullscreen?.();
    else void document.documentElement.requestFullscreen?.();
  };
  const uniformLayouts = DEFAULT_PRESENTATIONS.filter((item) => item.panes.length === item.columns * item.rows).map((item) => ({ columns: item.columns, rows: item.rows }));

  const camById = useMemo(() => new Map(cameras.data?.map((c) => [c.id, c])), [cameras.data]);

  const [fisheyeVersion, setFisheyeVersion] = useState(0);
  useEffect(() => {
    const handleConfigChanged = (e: Event) => {
      const customEvent = e as CustomEvent<{ cameraId?: string }>;
      const updatedCameraId = customEvent.detail?.cameraId;
      setFisheyeVersion((v) => v + 1);

      if (updatedCameraId) {
        const feConfig = getFisheyeConfig(updatedCameraId);
        const isMultiMode = feConfig.defaultMode === "3_fixed_120";

        // "y quiero que si salimos de grilla 3 o 4 que borre las vistas"
        if (!isMultiMode || feConfig.views.length === 0) {
          const res = cleanFisheyeVirtualViewTiles(tilesRef.current, updatedCameraId, {
            resetIfOnlyThisCamera: true,
            defaultQuality: "main",
          });

          if (res.changed) {
            if (res.shouldResetGridToSingle) {
              setColumns(1);
              setRows(1);
              setPanes(uniformPanes(1, 1));
              setTiles(res.tiles);
              tilesRef.current = res.tiles;
              setSelected(0);
              setFocus(null);
            } else {
              setTiles(res.tiles);
              tilesRef.current = res.tiles;
            }
          }
        }
      }
    };

    window.addEventListener("openvms:fisheye-config-changed", handleConfigChanged);
    return () => {
      window.removeEventListener("openvms:fisheye-config-changed", handleConfigChanged);
    };
  }, [camById]);

  // LIVE/REC (LV-9): the mode and shared time live in the URL (?mode=rec&t=<ISO>) so reload and
  // links work; LIVE clears both. REC needs a recordings grant somewhere, else it never activates.
  const search = useSearch({ strict: false }) as Record<string, unknown>;
  const navigate = useNavigate();
  const canRec = can(me.data, "recordings.view");

  // Restore the last grid selection for this user+tenant once both are known, dropping any
  // camera the user can no longer see. This adjusts state during render (React's documented
  // alternative to an Effect for "state that depends on data becoming available"), not inside
  // an Effect, so it applies before the default grid ever paints and runs at most once.
  // localStorage can be unavailable (private mode) or hold stale/malformed data, so every step
  // is guarded.
  if (!restored && me.data && cameras.data) {
    setRestored(true);
    try {
      const key = liveSelectionKey(me.data.tenant_id, me.data.id);
      const validIds = new Set(cameras.data.map((c) => c.id));
      const saved = parseSelection(localStorage.getItem(key), validIds);
      if (saved) {
        setColumns(saved.columns);
        setRows(saved.rows);
        setPanes(saved.panes ?? uniformPanes(saved.columns, saved.rows));
        setTiles(saved.tiles.map(dropUnresolvedFisheyeTile));
      }
    } catch {
      // Storage unavailable or corrupt: keep the default empty grid.
    }
  }

  // Persist the current grid selection (columns, tile order and camera ids) once restored.
  useEffect(() => {
    if (!restored || !me.data) return;
    try {
      localStorage.setItem(liveSelectionKey(me.data.tenant_id, me.data.id), serializeSelection(columns, tiles, rows, panes));
    } catch {
      // Storage unavailable (private mode, quota): the grid still works for this session.
    }
  }, [restored, me.data, columns, rows, tiles, panes]);

  // A Maps handoff is consumed only after REST has supplied the authorized camera list.
  const requestedCamera = typeof search.camera === "string" ? search.camera : undefined;
  // Like storage restoration above, adjust derived local state during render, not in an effect.
  if (!requestedCamera && consumedCamera) setConsumedCamera(undefined);
  if (restored && me.data && cameras.data && requestedCamera && consumedCamera !== requestedCamera) {
    setConsumedCamera(requestedCamera);
    const allowed = cameras.data.find((camera) => camera.id === requestedCamera);
    if (allowed && can(me.data, "live.view")) {
      const existing = tiles.findIndex((tile) => cameraIdOf(tile) === requestedCamera);
      if (existing >= 0) setSelected(existing);
      else {
        const empty = tiles.findIndex((tile) => tile === null);
        const target = empty >= 0 ? empty : tiles.length;
        const next = [...tiles];
        if (target === next.length) {
          next.push(...Array(columns).fill(null));
          setRows(rows + 1);
        }
        next[target] = { camera_id: requestedCamera, quality: "sub" };
        setTiles(next);
        setSelected(target);
      }
    }
  }
  useEffect(() => {
    if (!requestedCamera || consumedCamera !== requestedCamera) return;
    void navigate({
      to: ".",
      search: ((previous: Record<string, unknown>) => ({ ...previous, camera: undefined })) as never,
      replace: true,
    });
  }, [requestedCamera, consumedCamera, navigate]);

  const setGrid = (nextColumns: number, nextRows: number) => {
    setColumns(nextColumns);
    setRows(nextRows);
    setPanes(uniformPanes(nextColumns, nextRows));
    setTiles((t) => resizeTiles(t, nextColumns, nextRows));
    setSelected((s) => Math.min(s, nextColumns * nextRows - 1));
    setFocus(null);
  };
  const applyPresentation = (presentation: Presentation) => {
    setColumns(presentation.columns);
    setRows(presentation.rows);
    setPanes(presentation.panes);
    setTiles((current) => fitTiles(current, presentation.panes.length));
    setSelected((index) => Math.min(index, Math.max(0, presentation.panes.length - 1)));
    setFocus(null);
  };

  const discardRemovedSessions = (previous: Tile[], next: Tile[]) => {
    if (!persistentPlayers || !playerSessionManager) return;
    const retained = new Set(next.flatMap((tile) => tile && "camera_id" in tile
      ? [`${tile.camera_id}:${"quality" in tile ? tile.quality : "sub"}`]
      : []));
    const removed = new Map<string, "main" | "sub">();
    for (const tile of previous) {
      if (!tile || !("camera_id" in tile)) continue;
      const quality = "quality" in tile ? tile.quality : "sub";
      const key = `${tile.camera_id}:${quality}`;
      if (!retained.has(key)) removed.set(key, quality);
    }
    for (const [key, quality] of removed) {
      const cameraId = key.slice(0, key.lastIndexOf(":"));
      playerSessionManager.discard(cameraId, quality);
    }
  };

  // index defaults to the currently selected tile (click-to-place); dragging a camera from the
  // list onto a specific slot passes that slot's index explicitly instead.
  const place = (cameraId: string, index?: number, viewId?: string) => {
    const at = index ?? selectedRef.current;
    const defaultQuality = cameras.data?.find((c) => c.id === cameraId)?.default_live_quality ?? "sub";
    const quality = columns === 1 ? "main" : defaultQuality;
    // The ref updates immediately so two clicks before the next render fill two cells.
    const source = tilesRef.current;
    const next = persistentPlayers ? placeCameraUnique(source, at, cameraId, quality, viewId) : placeCameraAt(source, at, cameraId, quality, viewId);
    discardRemovedSessions(source, next);
    const empty = next.findIndex((cell, i) => cell === null && i !== at);
    tilesRef.current = next;
    if (empty >= 0) selectedRef.current = empty;
    setTiles(next);
    if (empty >= 0) setSelected(empty);
  };

  const removeTile = (index: number) => {
    const next = tilesRef.current.map((tile, tileIndex) => (tileIndex === index ? null : tile));
    discardRemovedSessions(tilesRef.current, next);
    tilesRef.current = next;
    setTiles(next);
    setSelected((current) => current === index ? 0 : current);
    selectedRef.current = selectedRef.current === index ? 0 : selectedRef.current;
    setFocus(null);
  };

  const closeAllStreams = () => {
    const next = tilesRef.current.map(() => null);
    discardRemovedSessions(tilesRef.current, next);
    tilesRef.current = next;
    setTiles(next);
    setSelected(0);
    selectedRef.current = 0;
    setFocus(null);
    setViewId("");
  };

  const clearQualityTimer = (cameraId: string) => {
    const timer = qualityTimersRef.current.get(cameraId);
    if (timer !== undefined) clearTimeout(timer);
    qualityTimersRef.current.delete(cameraId);
  };

  const setQualityOverride = (cameraId: string, quality: Quality, source: "manual" | "maximize") => {
    const identities = tilesRef.current.flatMap((tile) => {
      if (!tile || !("camera_id" in tile) || tile.camera_id !== cameraId) return [];
      const identity = tileIdentityKey(tile);
      if (!identity) return [];
      setInvalidatedOverrideKeys((current) => {
        const next = new Set(current);
        next.delete(`${cameraId}:${identity}`);
        return next;
      });
      return [identity];
    });
    if (identities.length === 0) return;
    setQualityOverrides((current) => ({ ...current, [cameraId]: { quality, identities: [...new Set(identities)], source } }));
  };

  const requestTemporaryMainQuality = (cameraId: string) => {
    clearQualityTimer(cameraId);
    setQualityOverride(cameraId, "main", "manual");
    const timer = setTimeout(() => {
      qualityTimersRef.current.delete(cameraId);
      setQualityOverrides((current) => {
        if (current[cameraId]?.source !== "manual") return current;
        if (maximizedCameraIdsRef.current.has(cameraId)) {
          return { ...current, [cameraId]: { ...current[cameraId], quality: "main", source: "maximize" } };
        }
        const next = { ...current };
        delete next[cameraId];
        return next;
      });
    }, 30_000);
    qualityTimersRef.current.set(cameraId, timer);
  };

  // Swapping (not shifting) keeps every other tile in its cell, so only the two swapped cameras move.
  const reorder = (from: number, to: number) => setTiles((t) => (persistentPlayers ? swapTiles(t, from, to) : reorderTiles(t, from, to)));

  const sensors = useLiveDragSensors(sortableKeyboardCoordinates);

  const placeMap = (map: { site_id: string; floor_id?: string; name: string }, index?: number) => {
    const at = index ?? selectedRef.current;
    const next = placeMapAt(tilesRef.current, at, map);
    const empty = next.findIndex((cell, i) => cell === null && i !== at);
    tilesRef.current = next;
    if (empty >= 0) selectedRef.current = empty;
    setTiles(next);
    if (empty >= 0) setSelected(empty);
  };

  const placeVision = (vision: LiveVisionRef, index?: number) => {
    if (index === undefined) {
      const res = placeVisionInOpenCell(tilesRef.current, vision);
      tilesRef.current = res.tiles;
      setTiles(res.tiles);
      setSelected(res.index);
      selectedRef.current = res.index;
      return;
    }
    const next = placeVisionAt(tilesRef.current, index, vision);
    const empty = next.findIndex((cell, i) => cell === null && i !== index);
    tilesRef.current = next;
    if (empty >= 0) selectedRef.current = empty;
    setTiles(next);
    if (empty >= 0) setSelected(empty);
  };

  const populateEmptyGridWithFisheyeMultiView = (cameraId: string, virtualViews: VirtualFisheyeView[], defaultQuality: "sub" | "main" = "sub") => {
    const totalCount = 1 + virtualViews.length;
    const pres = presentationForCount(catalog, totalCount);
    const quality = pres.columns === 1 ? "main" : defaultQuality;
    const nextTiles = tilesForFisheyeMultiView(cameraId, virtualViews, quality, pres.panes.length);
    setColumns(pres.columns);
    setRows(pres.rows);
    setPanes(pres.panes);
    setTiles(nextTiles);
    tilesRef.current = nextTiles;
    setSelected(0);
    setFocus(null);
    setViewId("");
  };

  const finishDrag = () => {
    dragging.current = false;
    setActiveDrag(null);
    if (!edgeHover.current && !pinnedRef.current) setEdgeOpen(false);
  };

  const handleDragEnd = (event: DragEndEvent) => {
    finishDrag();
    const resolution = resolveDragEnd(event.active.id, event.over?.id ?? null);
    if (!resolution) {
      // Not a grid drop: it may be a move inside the shared explorer tree.
      if (folderApi.drop(event.active.id, event.over?.id ?? null) === "rejected") {
        flash(tr("live.cannotMoveServers"), "warn");
      }
      return;
    }
    if (resolution.type === "place") {
      const isGridEmpty = tiles.every((t) => t === null);
      const feConfig = getFisheyeConfig(resolution.cameraId);
      const cam = camById.get(resolution.cameraId);
      const isFisheye = Boolean(
        (feConfig.enabled && feConfig.lensType !== "normal") ||
        cam?.tags?.includes("fisheye") ||
        cam?.tags?.includes("ojo_de_pez") ||
        Boolean(cam?.onvif_profile && detectFisheyeProfile({ onvif_profile: cam.onvif_profile }))
      );
      const effectiveViews = isFisheye ? getEffectiveVirtualViews(feConfig) : [];

      if (!resolution.fisheyeViewId && isGridEmpty && isFisheye && effectiveViews.length >= 2) {
        populateEmptyGridWithFisheyeMultiView(resolution.cameraId, effectiveViews, cam?.default_live_quality ?? "sub");
      } else {
        place(resolution.cameraId, resolution.index, resolution.fisheyeViewId);
      }
    }
    else if (resolution.type === "place-map") placeMap(resolution.map, resolution.index);
    else if (resolution.type === "place-vision") placeVision(resolution.vision, resolution.index);
    else if (resolution.type === "reorder") reorder(resolution.from, resolution.to);
    else if (resolution.type === "fill-server") showGroup(camerasOfServer(resolution.serverId), servers.data?.find((server) => server.id === resolution.serverId)?.name ?? tr("live.theServer"));
    else if (resolution.type === "fill-site") showGroup(camerasOfSiteId(resolution.siteId), sites.data?.find((site) => site.id === resolution.siteId)?.name ?? tr("live.theSite"));
    else showGroup(camerasOfFolder(resolution.folderId), folderApi.folders.find((folder) => folder.id === resolution.folderId)?.name ?? tr("live.theFolder"));
  };

  const showView = (v: Schemas["View"]) => {
    setViewId(v.id);
    const n = v.layout.columns;
    const next: Tile[] = viewCellsToTiles(v.layout.cells);
    const remembered = recallViewPanes(v.id, n, next.length);
    const nextRows = remembered?.rows ?? (next.length ? Math.ceil(next.length / n) : n);
    const nextPanes = remembered?.panes ?? uniformPanes(n, nextRows);
    setColumns(n);
    setRows(nextRows);
    setPanes(nextPanes);
    setTiles(fitTiles(next, nextPanes.length));
    setFocus(null);
  };
  const loadView = (id: string) => {
    const v = views.data?.find((x) => x.id === id);
    if (v) showView(v);
  };
  const currentLayout = (): Schemas["ViewLayout"] => ({
    columns,
    cells: tilesToViewCells(tiles),
  });
  const openViewProperties = (id: string) => {
    const view = views.data?.find((item) => item.id === id);
    if (!view?.editable) return;
    setDialogName(view.name);
    setDialogShared(view.shared);
    setViewDialog({ mode: "edit", id });
  };
  const openNewView = () => {
    setDialogName("");
    setDialogShared(false);
    setViewDialog({ mode: "create" });
  };
  const save = useMutation({
    mutationFn: async () => {
      const name = dialogName.trim();
      const tenantId = me.data?.tenant_id ?? cameras.data?.find((c) => tiles.some((t) => cameraIdOf(t) === c.id))?.tenant_id;
      if (viewDialog?.mode === "edit") {
        const view = views.data?.find((item) => item.id === viewDialog.id);
        if (!view) throw new Error(tr("live.viewGone"));
        return unwrap(
          await api.PUT("/api/v1/views/{viewId}", {
            params: { path: { viewId: view.id } },
            body: { name, shared: dialogShared, tenant_id: view.tenant_id, layout: viewId === view.id ? currentLayout() : view.layout },
          }),
        );
      }
      return unwrap(await api.POST("/api/v1/views", { body: { name, shared: dialogShared, tenant_id: tenantId, layout: currentLayout() } }));
    },
    onSuccess: async (v) => {
      await qc.invalidateQueries({ queryKey: ["views"] });
      if (viewDialog?.mode === "create" || viewId === v.id) {
        setViewId(v.id);
        rememberViewPanes(v.id, columns, rows, panes);
      }
      setViewDialog(null);
    },
  });
  const duplicate = useMutation({
    mutationFn: async (id: string) => {
      const view = views.data?.find((item) => item.id === id);
      if (!view) throw new Error(tr("live.viewGone"));
      const taken = new Set((views.data ?? []).map((item) => item.name.toLowerCase()));
      const created = unwrap(
        await api.POST("/api/v1/views", {
          body: { name: copyViewName(view.name, taken), shared: false, tenant_id: view.tenant_id, layout: view.layout },
        }),
      );
      return { created, sourceId: id };
    },
    onSuccess: async ({ created, sourceId }) => {
      const remembered = recallViewPanes(sourceId, created.layout.columns, created.layout.cells.length) ?? (viewId === sourceId ? { columns, rows, panes } : null);
      if (remembered) rememberViewPanes(created.id, remembered.columns, remembered.rows, remembered.panes);
      await qc.invalidateQueries({ queryKey: ["views"] });
      showView(created);
      flash(`Se duplicó «${created.name}».`);
    },
    onError: (err) => flash(err instanceof Error && err.message ? err.message : tr("live.duplicateFailed"), "warn"),
  });
  const remove = useMutation({
    mutationFn: async (id: string) => unwrap(await api.DELETE("/api/v1/views/{viewId}", { params: { path: { viewId: id } } })),
    onSuccess: async () => {
      setViewId("");
      await qc.invalidateQueries({ queryKey: ["views"] });
    },
  });

  const plural = (n: number) => `${n} ${n === 1 ? "cámara" : "cámaras"}`;
  const reportFill = (r: { added: number; already: number; skipped: number }, target: string) => {
    if (r.added > 0 && r.skipped === 0) flash(`Se agregó ${plural(r.added)} ${target}${r.already ? ` (${r.already} ya estaba${r.already === 1 ? "" : "n"})` : ""}.`);
    else if (r.added > 0) flash(`Se agregó ${plural(r.added)} ${target}; ${r.skipped} no entr${r.skipped === 1 ? "ó" : "aron"}: no hay espacio.`, "warn");
    else if (r.skipped > 0) flash(`No hay espacio libre ${target}.`, "warn");
    else flash(r.already > 1 ? `Esas cámaras ya están ${target}.` : `La cámara ya está ${target}.`, "warn");
  };
  const cameraQuality = (id: string, cols: number) => (cols === 1 ? "main" : (camById.get(id)?.default_live_quality ?? "sub")) as "main" | "sub";
  const byCamera = (a: Schemas["Camera"], b: Schemas["Camera"]) => a.sort_order - b.sort_order || a.display_name.localeCompare(b.display_name);
  const camerasOfServer = (serverId: string) => serverCameras(serverId, cameras.data ?? [], folderApi.folders);
  const camerasOfSiteId = (siteId: string) => camerasOfSite(siteId, servers.data ?? [], cameras.data ?? [], folderApi.folders);
  const camerasOfFolder = (folderId: string) => (cameras.data ?? []).filter((camera) => camera.enabled && camera.folder_id === folderId).sort(byCamera);
  const showGroup = (list: Schemas["Camera"][], label: string) => {
    if (list.length === 0) {
      flash(`${label} no tiene cámaras.`, "warn");
      return;
    }
    const expandedTiles: Tile[] = [];
    for (const camera of list) {
      const feConfig = getFisheyeConfig(camera.id);
      const isFisheye = Boolean(
        (feConfig.enabled && feConfig.lensType !== "normal") ||
        camera.tags?.includes("fisheye") ||
        camera.tags?.includes("ojo_de_pez") ||
        Boolean(camera.onvif_profile && detectFisheyeProfile({ onvif_profile: camera.onvif_profile }))
      );
      const effectiveViews = isFisheye ? getEffectiveVirtualViews(feConfig) : [];
      if (isFisheye && (feConfig.defaultMode === "3_fixed_120" || effectiveViews.length >= 2)) {
        // Tile 0: real camera stream (raw / original)
        expandedTiles.push({ camera_id: camera.id, quality: "sub" });
        // Tiles 1..N: the virtual views (e.g. 3 of 120°)
        for (const v of effectiveViews) {
          expandedTiles.push({ camera_id: camera.id, quality: "sub", fisheye_view_id: v.id });
        }
      } else {
        expandedTiles.push({ camera_id: camera.id, quality: "sub" });
      }
    }
    const layout = presentationForCount(DEFAULT_PRESENTATIONS, expandedTiles.length);
    const built = fitTiles(
      expandedTiles.map((t) =>
        t && "camera_id" in t ? { ...t, quality: cameraQuality(t.camera_id, layout.columns) } : t
      ),
      layout.panes.length
    );
    const skipped = Math.max(0, expandedTiles.length - layout.panes.length);
    setColumns(layout.columns);
    setRows(layout.rows);
    setPanes(layout.panes);
    setTiles(built);
    setSelected(0);
    setFocus(null);
    setViewId("");
    if (skipped > 0) flash(`${label}: se muestran ${expandedTiles.length - skipped} de ${expandedTiles.length} vistas. La grilla más grande es ${layout.name}.`, "warn");
    else flash(`${label}: ${layout.name} con ${plural(expandedTiles.length)}.`);
  };
  const placeInOpen = (cameraId: string, viewId?: string) => {
    if (!camById.has(cameraId)) return;
    const isGridEmpty = tiles.every((t) => t === null);
    const feConfig = getFisheyeConfig(cameraId);
    const cam = camById.get(cameraId);
    const isFisheye = Boolean(
      (feConfig.enabled && feConfig.lensType !== "normal") ||
      cam?.tags?.includes("fisheye") ||
      cam?.tags?.includes("ojo_de_pez") ||
      Boolean(cam?.onvif_profile && detectFisheyeProfile({ onvif_profile: cam.onvif_profile }))
    );
    const effectiveViews = isFisheye ? getEffectiveVirtualViews(feConfig) : [];

    if (!viewId && isGridEmpty && isFisheye && effectiveViews.length >= 2) {
      populateEmptyGridWithFisheyeMultiView(cameraId, effectiveViews, cam?.default_live_quality ?? "sub");
      return;
    }

    setTiles((current) => {
      const next = placeInOpenCell(current, cameraId, cameraQuality(cameraId, columns), viewId);
      if (next.index >= 0) setSelected(next.index);
      return next.tiles;
    });
  };

  // Context menu "Agregar a vista > Grilla actual": fills empty cells and, when full, grows the grid to the next layout.
  const addToGrid = (ids: string[]) => {
    const visible = ids.filter((id) => camById.has(id));
    const present = new Set(tiles.flatMap((t) => { const id = cameraIdOf(t); return id ? [id] : []; }));
    const fresh = visible.filter((id) => !present.has(id));
    let cols = columns;
    let base = tiles;
    const free = tiles.filter((t) => t === null).length;
    if (fresh.length > free) {
      const next = growLayout(uniformLayouts, tiles.length, tiles.length - free + fresh.length);
      if (next) {
        cols = next.columns;
        base = resizeTiles(tiles, next.columns, next.rows);
        setGrid(next.columns, next.rows);
      }
    }
    const res = fillTiles(base, visible.map((id) => ({ id, quality: cameraQuality(id, cols) })));
    setTiles(res.tiles);
    reportFill(res, "en la grilla");
  };

  // "Agregar a vista > <vista>": adds to the first free cells of the saved layout, never resizing it.
  const addToView = useMutation({
    mutationFn: async (v: { id: string; ids: string[] }) => {
      const view = views.data?.find((x) => x.id === v.id);
      if (!view) throw new Error(tr("live.viewGone"));
      const n = view.layout.columns;
      const nextRows = view.layout.cells.length ? Math.ceil(view.layout.cells.length / n) : n;
      const existing = resizeTiles(viewCellsToTiles(view.layout.cells), n, nextRows);
      const res = fillTiles(existing, v.ids.filter((id) => camById.has(id)).map((id) => ({ id, quality: cameraQuality(id, n) })));
      if (res.added === 0) return { view, res, saved: false };
      await unwrap(
        await api.PUT("/api/v1/views/{viewId}", {
          params: { path: { viewId: view.id } },
          body: { name: view.name, shared: view.shared, tenant_id: view.tenant_id, layout: { columns: n, cells: tilesToViewCells(res.tiles) } },
        }),
      );
      await qc.invalidateQueries({ queryKey: ["views"] });
      if (viewId === view.id) setTiles(res.tiles);
      return { view, res, saved: true };
    },
    onSuccess: ({ view, res }) => reportFill(res, `a la vista «${view.name}»`),
    onError: (err) => flash(err instanceof Error && err.message ? tr("live.updateFailedDetail", { message: err.message }) : tr("live.updateFailed"), "warn"),
  });
  const explorerActions: ExplorerActions = {
    editableViews: (views.data ?? []).filter((v) => v.editable).map((v) => ({ id: v.id, name: v.name })),
    addToGrid,
    addToView: (id, ids) => addToView.mutate({ id, ids }),
    canConfigureSites: can(me.data, "sites.manage"),
    canConfigureServers: can(me.data, "servers.manage"),
    canConfigureCameras: can(me.data, "servers.config"),
    onConfigureFisheye: (cameraId: string) => setFisheyeConfigCameraId(cameraId),
    onTemporaryMainQuality: requestTemporaryMainQuality,
  };

  // With persistent players the other tiles stay mounted (hidden, sessions WARM) while one is
  // expanded, so leaving the expanded view resumes them instantly instead of reconnecting.
  const shown = focus !== null && !persistentPlayers ? [focus] : tiles.map((_, i) => i);
  const gridCols = focus !== null ? 1 : columns;
  const duplicates = useMemo(() => (persistentPlayers ? duplicateTileIndexes(tiles) : new Set<number>()), [persistentPlayers, tiles]);

  const gridCameraIds = useMemo(() => [...new Set(tiles.flatMap((t) => { const id = cameraIdOf(t); return id && camById.has(id) ? [id] : []; }))], [tiles, camById]);
  const focusedCameraId = focus !== null ? cameraIdOf(tiles[focus] ?? null) : undefined;
  // The telemetry runtime reports how many cameras the operator is looking at (one when a tile is maximized).
  const visibleCameraCount = focus !== null ? Math.min(1, gridCameraIds.length) : gridCameraIds.length;
  useEffect(() => {
    telemetryRuntime.setVisibleCameras(visibleCameraCount);
    return () => telemetryRuntime.setVisibleCameras(0);
  }, [visibleCameraCount]);
  const adaptivePolicy = useAdaptiveVideoPolicy({
    enabled: adaptiveStreaming,
    isMaximized: focus !== null,
    totalCameras: gridCameraIds.length,
  });
  const [dismissedWarningCode, setDismissedWarningCode] = useState<string | null>(null);
  const warningDismissed = Boolean(adaptivePolicy.operatorWarning && dismissedWarningCode === adaptivePolicy.operatorWarning.code);

  useEffect(() => {
    const previous = previousFocusedCameraRef.current;
    if (previous && previous !== focusedCameraId) {
      maximizedCameraIdsRef.current.delete(previous);
      if (qualityOverridesRef.current[previous]?.source === "maximize") {
        clearQualityTimer(previous);
        setQualityOverrides((current) => {
          if (current[previous]?.source !== "maximize") return current;
          const next = { ...current };
          delete next[previous];
          return next;
        });
        playerSessionManager?.discard(previous, "main");
      }
    }
    previousFocusedCameraRef.current = focusedCameraId;
    if (focusedCameraId) {
      const cameraId = focusedCameraId;
      // Promote right away; the quality handoff keeps the old frame until the main stream paints.
      maximizedCameraIdsRef.current.add(cameraId);
      if (qualityOverridesRef.current[cameraId]?.source !== "manual") {
        clearQualityTimer(cameraId);
        setQualityOverride(cameraId, "main", "maximize");
      }
    }
  }, [focusedCameraId, playerSessionManager]);

  useEffect(() => {
    const activeIdentities = new Map<string, Set<string>>();
    for (const tile of tiles) {
      if (!tile || !("camera_id" in tile)) continue;
      const identity = tileIdentityKey(tile);
      if (!identity) continue;
      const active = activeIdentities.get(tile.camera_id) ?? new Set<string>();
      active.add(identity);
      activeIdentities.set(tile.camera_id, active);
    }
    for (const [cameraId, override] of Object.entries(qualityOverridesRef.current)) {
      const active = activeIdentities.get(cameraId);
      let validCount = 0;
      for (const identity of override.identities) {
        if (active?.has(identity)) validCount++;
        else {
          setInvalidatedOverrideKeys((current) => {
            const next = new Set(current);
            next.add(`${cameraId}:${identity}`);
            return next;
          });
        }
      }
      if (validCount === 0) {
        clearQualityTimer(cameraId);
        setQualityOverrides((current) => {
          if (!(cameraId in current)) return current;
          const next = { ...current };
          delete next[cameraId];
          return next;
        });
      }
    }
  }, [tiles]);

  useEffect(() => () => {
    for (const timer of qualityTimersRef.current.values()) clearTimeout(timer);
    qualityTimersRef.current.clear();
  }, []);

  const qualityOverrideFor = (tile: Tile): Quality | undefined => {
    if (!tile || !("camera_id" in tile)) return undefined;
    const identity = tileIdentityKey(tile);
    const override = qualityOverrides[tile.camera_id];
    if (!identity || !override?.identities.includes(identity)) return undefined;
    if (invalidatedOverrideKeys.has(`${tile.camera_id}:${identity}`)) return undefined;
    return override.quality;
  };
  const { rec, now, transport, recData, denied, recLimited, hasCoverage, masterId, timelineCameras, setMode } = useRecSession({
    canRec,
    cameraIds: gridCameraIds,
    focusedId: focusedCameraId,
    selectedId: cameraIdOf(tiles[selected] ?? null),
    cameraById: camById,
  });
  const feview = activeDrag ? feviewFromDragId(activeDrag) : null;
  const dragLabel = feview
    ? `${camById.get(feview.cameraId)?.display_name ?? "Cámara"} · ${
        getEffectiveVirtualViews(getFisheyeConfig(feview.cameraId)).find((v) => v.id === feview.viewId)?.name ?? "Vista Virtual"
      }`
    : activeDrag?.startsWith("camera:")
    ? camById.get(activeDrag.slice("camera:".length))?.display_name
    : activeDrag?.startsWith("tfolder:")
      ? folderApi.folders.find((f) => f.id === activeDrag.slice("tfolder:".length))?.name
        : activeDrag?.startsWith("tserver:")
        ? servers.data?.find((server) => server.id === activeDrag.slice("tserver:".length))?.name
        : activeDrag?.startsWith("tsite:")
        ? sites.data?.find((site) => site.id === activeDrag.slice("tsite:".length))?.name
        : activeDrag?.startsWith("tvision:")
          ? visionFromDragId(activeDrag)?.name
          : activeDrag
            ? mapFromDragId(activeDrag)?.name
            : undefined;
  const modeToggle = <LiveModeToggle rec={rec} onChange={setMode} />;
  // The pinned explorer is a fixed panel that covers the header's left edge, so the portaled
  // toggle starts at the panel's right edge instead of underneath it.
  const topBarActions = canRec && !fullscreen && topBar.available && topBar.target
    ? createPortal(<div data-testid="live-mode-toggle-slot" className={cn(sidebarPinned && "md:ml-(--pinned-offset)")} style={PINNED_STYLE}>{modeToggle}</div>, topBar.target)
    : null;
  const canCreateView = can(me.data, "views.create_private") || can(me.data, "views.create_shared");
  const canShareView = can(me.data, "views.create_shared");
  const sidebarContent = (
    <LiveExplorer
      cameras={cameras.data ?? []}
      gridCameraIds={gridCameraIds}
      sites={sites.data ?? []}
      servers={servers.data ?? []}
      folderApi={folderApi}
      onPick={(id, viewId) => place(id, undefined, viewId)}
      onDoublePlace={placeInOpen}
      onPlayback={setPlaybackCameraId}
      canViewRecordings={can(me.data, "recordings.view")}
      pinned={sidebarPinned}
      onTogglePin={() => setSidebarPinned(!sidebarPinned)}
      canEvents={can(me.data, "events.view")}
      canLpr={can(me.data, "lpr.view")}
      canMaps={can(me.data, "maps.view")}
      onPlaceMap={placeMap}
      onPlaceVision={placeVision}
      views={views.data ?? []}
      activeViewId={viewId}
      onOpenView={loadView}
      onEditView={openViewProperties}
      onDuplicateView={(id) => duplicate.mutate(id)}
      onDeleteView={setDeleteViewId}
      onCreateView={openNewView}
      canCreateView={canCreateView}
      notice={treeNotice}
      noticeTone={noticeTone}
      actions={explorerActions}
      error={cameras.error}
    />
  );
  const edgeVisible = sidebarPinned || edgeOpen;
  const sidebar = createPortal(
    <div
      data-testid="live-edge"
      className={cn("fixed inset-y-0 z-40", fullscreen ? "left-0" : "left-0 md:left-(--rail-w)", edgeVisible ? "w-80 max-w-[85vw]" : "w-3 [@media(pointer:coarse)]:w-11")}
      onPointerEnter={(event) => {
        if (event.pointerType !== "touch") revealEdge();
      }}
      onPointerLeave={(event) => {
        if (event.pointerType !== "touch") concealEdge();
      }}
    >
      <aside hidden={!edgeVisible} aria-label={tr("live.explorer")} className="flex h-full min-h-0 w-80 max-w-[85vw] flex-col rounded-r-m3-xl bg-surface-1 px-3 py-4 shadow-2xl">
        <span aria-hidden className="pointer-events-none absolute inset-y-0 left-0 w-0.5 bg-primary/70" />
        {sidebarContent}
      </aside>
      {!edgeVisible && (
        <button type="button" aria-label={tr("live.showExplorer")} title={tr("live.showExplorer")} className="absolute inset-0 flex items-center justify-center bg-surface-1/95 text-on-surface-variant" onClick={revealEdge}>
          <Icon icon={ChevronsRight} size={12} />
        </button>
      )}
    </div>,
    document.body,
  );

  return (
    <div className={cn("flex min-h-0 flex-1 flex-col gap-2", sidebarPinned && "md:pl-(--pinned-offset)")} style={PINNED_STYLE}>
      <h1 className="sr-only">{tr("nav.live")}</h1>
      {topBarActions}
      <DndContext
        sensors={sensors}
        onDragStart={(e) => {
          dragging.current = true;
          setActiveDrag(String(e.active.id));
        }}
        onDragCancel={finishDrag}
        onDragEnd={handleDragEnd}
      >
        <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
          <section data-live-stage className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
            <div role="group" aria-label={tr("live.gridLayout")} className="flex shrink-0 flex-wrap items-center gap-1">
              <LayoutMenu catalog={catalog} active={{ columns, panes }} onSelect={applyPresentation} onEdit={() => setEditorOpen(true)} />
              <FullscreenButton active={fullscreen} onClick={toggleFullscreen} />
              <IconButton icon={X} title="Cerrar todas las cámaras" aria-label="Cerrar todas las cámaras" className="size-8" onClick={closeAllStreams} />
              {visionTile && (
                <label className="ml-2 flex items-center gap-1.5 text-xs text-on-surface cursor-pointer select-none rounded-m3-sm bg-surface-1 px-2.5 py-1 border border-line">
                  <input
                    type="checkbox"
                    checked={keepVisionVisible}
                    onChange={(e) => setKeepVisionVisible(e.target.checked)}
                    className="size-3.5 rounded border-line text-primary focus:ring-primary"
                  />
                  <span>Mantener Vision AI visible</span>
                </label>
              )}
              {canRec && (fullscreen || !topBar.available) && <div className="ml-auto">{modeToggle}</div>}
            </div>
            <div className="relative flex min-h-0 flex-1 flex-col">
              {keepVisionVisible && visionTile && focus !== null && tiles[focus] != null && !isVisionTile(tiles[focus]!) && (
                <div
                  data-testid="vision-always-on-top-pip"
                  className="fixed bottom-6 right-6 z-30 w-80 max-w-[40vw] aspect-video rounded-m3-lg border-2 border-primary/50 shadow-2xl overflow-hidden bg-black"
                >
                  <div className="absolute top-1 right-1 z-20">
                    <IconButton
                      icon={X}
                      size="sm"
                      title="Cerrar PiP de Vision AI"
                      aria-label="Cerrar PiP de Vision AI"
                      className="size-6 bg-black/60 text-white hover:bg-black/80 rounded-full"
                      onClick={() => setKeepVisionVisible(false)}
                    />
                  </div>
                  <LiveVisionTile vision={visionTile.vision} />
                </div>
              )}
              <TelemetryOverlay />
              {adaptivePolicy.operatorWarning && !warningDismissed && (
                <div role="status" className="mb-1 flex shrink-0 items-center justify-between gap-2 rounded-m3-md border border-warn/30 bg-warn/15 px-3 py-1.5 text-xs text-warn">
                  <span>
                    {tr(adaptivePolicy.operatorWarning.messageKey, {
                      bottleneck: adaptivePolicy.operatorWarning.bottleneck
                        ? tr(LAYER_KEYS[adaptivePolicy.operatorWarning.bottleneck])
                        : tr("clientStation.bottleneckNone"),
                    })}
                  </span>
                  <button
                    type="button"
                    onClick={() => setDismissedWarningCode(adaptivePolicy.operatorWarning?.code ?? "hardware_saturated")}
                    aria-label={tr("clientStation.adaptiveDismiss")}
                    className="font-semibold underline hover:opacity-80"
                  >
                    {tr("clientStation.adaptiveDismiss")}
                  </button>
                </div>
              )}
              <SortableContext items={shown.map((i) => tileDragId(i))} strategy={persistentPlayers ? rectSwappingStrategy : rectSortingStrategy}>
                <LiveGridFrame role="group" label={tr("live.videoGrid")} mode={rec ? "rec" : "live"}
                  className={cn(
                    "grid min-h-0 flex-1 gap-2 overflow-y-auto p-1 ring-2 md:overflow-hidden md:[grid-template-rows:repeat(var(--grid-rows),minmax(0,1fr))]",
                    rec ? "ring-bad/50" : "ring-ok/40",
                  )}
                  style={{ gridTemplateColumns: `repeat(${gridCols}, minmax(0, 1fr))`, "--grid-rows": focus !== null ? 1 : rows } as CSSProperties}
                >
                  {shown.map((i) => {
                    const t = tiles[i] ?? null;
                    const cameraId = cameraIdOf(t);
                    const cam = cameraId ? camById.get(cameraId) : undefined;
                    const isHidden = focus !== null && focus !== i;
                    let recLayer: ReactNode = null;
                    if (rec && cameraId && cam && !isHidden) {
                      const id = cameraId;
                      const state: RecTileState = denied.has(id)
                        ? "denied"
                        : tiles.findIndex((x) => cameraIdOf(x) === id) !== i
                          ? "duplicate"
                          : recLimited.has(id)
                            ? "limited"
                            : recData.loaded.includes(id) && !hasCoverage(id)
                              ? "empty"
                              : "player";
                      recLayer = <RecTile cameraId={id} name={cam.display_name} state={state} transport={transport} isMaster={id === masterId} />;
                    }
                    const identityKey = tileIdentityKey(t);
                    return (
                      <GridTile
                        key={identityKey && !duplicates.has(i) ? `camera:${identityKey}` : `cell:${i}`}
                        index={i}
                        placement={focus === null && panes[i] ? { gridColumn: `${panes[i].col + 1} / span ${panes[i].colSpan}`, gridRow: `${panes[i].row + 1} / span ${panes[i].rowSpan}` } : undefined}
                        tile={t}
                        camera={cam}
                        isSelected={selected === i}
                        isFocused={focus === i}
                        isHidden={isHidden}
                        recLayer={recLayer}
                        suspended={rec || (isHidden && adaptivePolicy.suspendHidden)}
                        trimVisualWork={adaptivePolicy.trimVisualWork}
                        isDuplicate={duplicates.has(i)}
                        persistent={persistentPlayers}
                        surface={persistentPlayers && videoSurfaceLayer}
                        serverId={cam?.server_id}
                        canViewRecordings={can(me.data, "recordings.view")}
                        status={cam?.status}
                        quality={t && "quality" in t ? t.quality : "sub"}
                        qualityOverride={qualityOverrideFor(t)}
                        onSelect={() => setSelected(i)}
                        onToggleFocus={() => t && setFocus(focus === null ? i : null)}
                        onOpenFisheye={(id) => setFisheyeModalCameraId(id)}
                        onConfigureFisheye={(id) => setFisheyeConfigCameraId(id)}
                        fisheyeVersion={fisheyeVersion}
                        onRemove={() => removeTile(i)}
                      />
                    );
                  })}
                </LiveGridFrame>
              </SortableContext>
            </div>
            {rec && <LiveRecDock
                transport={transport}
                cameras={timelineCameras}
                events={recData.events}
                now={now}
                selectedId={cameraIdOf(tiles[selected] ?? null)}
                onSelectCamera={(id) => {
                  const idx = tiles.findIndex((t) => cameraIdOf(t) === id);
                  if (idx >= 0) setSelected(idx);
                }}
              />}
          </section>
        </div>
        {sidebar}
        <DragOverlay dropAnimation={null}>
          {dragLabel ? <div className="pointer-events-none rounded-m3-md bg-surface-2 px-3 py-1.5 text-xs shadow-lg ring-2 ring-primary">{dragLabel}</div> : null}
        </DragOverlay>
      </DndContext>
      {editorOpen && (
        <PresentationEditor
          catalog={catalog}
          active={{ columns, panes }}
          onCancel={() => setEditorOpen(false)}
          onAccept={(next, selectedId) => {
            setCatalog(next);
            saveCatalog(next);
            const selected = next.find((item) => item.id === selectedId);
            if (selected) applyPresentation(selected);
            setEditorOpen(false);
          }}
        />
      )}
      {viewDialog && (
        <Modal title={tr("live.properties")} onClose={() => setViewDialog(null)} className="max-w-md">
          <TextInput aria-label={tr("live.viewName")} placeholder={tr("live.viewName")} value={dialogName} onChange={(event) => setDialogName(event.target.value)} />
          {canShareView ? (
            <Switch checked={dialogShared} onChange={setDialogShared} label={tr("live.sharedOrg")} />
          ) : (
            viewDialog.mode === "edit" && dialogShared && <p className="text-sm text-muted">{tr("live.sharedOrg")}</p>
          )}
          {viewDialog.mode === "edit" && viewId === viewDialog.id && <p className="text-xs text-muted">{tr("live.saveUpdatesGrid")}</p>}
          <ErrorNote error={save.error} />
          <div className="flex justify-end gap-2">
            <Button variant="text" onClick={() => setViewDialog(null)}>{tr("common.cancel")}</Button>
            <Button variant="filled" onClick={() => save.mutate()} disabled={!dialogName.trim() || save.isPending}>
              {tr("live.save")}
            </Button>
          </div>
        </Modal>
      )}
      {deleteViewId && (
        <ConfirmDialog
          title={tr("live.deleteView")}
          message={tr("live.deleteViewMessage", { name: views.data?.find((v) => v.id === deleteViewId)?.name ?? "" })}
          confirmLabel={tr("live.delete")}
          pending={remove.isPending}
          error={remove.error}
          onCancel={() => setDeleteViewId(null)}
          onConfirm={() => remove.mutate(deleteViewId, { onSuccess: () => setDeleteViewId(null) })}
        />
      )}
      {playbackCameraId && camById.has(playbackCameraId) && (
        <LivePlaybackPanel
          cameraId={playbackCameraId}
          cameraName={camById.get(playbackCameraId)!.display_name}
          onClose={() => setPlaybackCameraId(null)}
        />
      )}
      {fisheyeModalCameraId && camById.has(fisheyeModalCameraId) && (
        <Modal
          title={`Fisheye 360° Dewarping · ${camById.get(fisheyeModalCameraId)!.display_name}`}
          onClose={() => setFisheyeModalCameraId(null)}
          className="max-w-5xl"
        >
          <div className="w-full h-[68vh] min-h-[480px]">
            <FisheyeViewer cameraId={fisheyeModalCameraId} />
          </div>
        </Modal>
      )}
      {fisheyeConfigCameraId && camById.has(fisheyeConfigCameraId) && (
        <Modal
          title={`Configuración de vista Fisheye · ${camById.get(fisheyeConfigCameraId)!.display_name}`}
          onClose={() => setFisheyeConfigCameraId(null)}
          className="max-w-4xl"
        >
          <FisheyeSection
            cameraId={fisheyeConfigCameraId}
            cameraName={camById.get(fisheyeConfigCameraId)!.display_name}
            canManage={can(me.data, "cameras.manage") || can(me.data, "servers.config")}
          />
        </Modal>
      )}
    </div>
  );
}

/** Live renders the phone camera list below `md` and the grid otherwise. */
export function Live() {
  return useIsPhoneDevice() ? <LivePhone /> : <LiveDesktop />;
}

/** Overlay action on a camera tile: a 32px pill on the translucent surface so it reads over any video. */
const tileAction = "size-8 [@media(pointer:coarse)]:size-11 rounded-full bg-surface-1/80 text-on-surface backdrop-blur hover:bg-surface-1";

function LiveGridFrame({ className, style, role, label, mode, children }: { className?: string; style?: CSSProperties; role?: string; label: string; mode?: string; children: ReactNode }) {
  const { setNodeRef } = useDroppable({ id: LIVE_GRID_DROP_ID });
  return (
    <div ref={setNodeRef} role={role} aria-label={label} data-mode={mode} className={className} style={style}>
      {children}
    </div>
  );
}

export function useTileOverlayVisibility() {
  const [visible, setVisible] = useState(true);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reveal = () => {
    if (timer.current) clearTimeout(timer.current);
    setVisible(true);
  };
  const scheduleHide = () => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setVisible(false), 3000);
  };
  useEffect(() => {
    timer.current = setTimeout(() => setVisible(false), 3000);
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, []);
  return { visible, reveal, scheduleHide };
}

function FisheyeQualityLayers({
  cameraId,
  config,
  compact,
  initialMode,
  targetViewId,
  className,
  quality,
  qualityOverride,
  identity,
  onTargetReady,
}: {
  cameraId: string;
  config: ReturnType<typeof getFisheyeConfig>;
  compact: boolean;
  initialMode: ReturnType<typeof getFisheyeConfig>["defaultMode"];
  targetViewId?: string;
  className?: string;
  quality: Quality;
  qualityOverride?: Quality;
  identity: string;
  /** The target quality has a drawable frame and is now the visible one. */
  onTargetReady?: (quality: Quality) => void;
}) {
  const [frameReady, setFrameReady] = useState<{ requested: Quality | undefined; quality: Quality | null }>({ requested: undefined, quality: null });
  const readyQuality = frameReady.requested === qualityOverride ? frameReady.quality : null;
  const handoff = qualityOverride !== undefined && qualityOverride !== quality;
  return (
    <div className={cn("relative", className)}>
      <div className="absolute inset-0 z-0">
        <FisheyeViewer
          key={`${identity}:base:${quality}`}
          cameraId={cameraId}
          config={config}
          compact={compact}
          initialMode={initialMode}
          targetViewId={targetViewId}
          quality={quality}
          className="size-full"
        />
      </div>
      {handoff && qualityOverride && (
        <div className={cn("absolute inset-0 z-[1]", readyQuality === qualityOverride ? "opacity-100" : "pointer-events-none opacity-0")}>
          <FisheyeViewer
            key={`${identity}:target:${qualityOverride}`}
            cameraId={cameraId}
            config={config}
            compact={compact}
            initialMode={initialMode}
            targetViewId={targetViewId}
            quality={qualityOverride}
            onFirstFrame={() => {
              setFrameReady({ requested: qualityOverride, quality: qualityOverride });
              onTargetReady?.(qualityOverride);
            }}
            className="size-full"
          />
        </div>
      )}
    </div>
  );
}

function QualityTargetSurface({
  cameraId,
  quality,
  persistent,
  surface,
  serverId,
  active,
  suspended,
  zoom,
  onZoomChange,
  onReady,
}: {
  cameraId: string;
  quality: Quality;
  persistent: boolean;
  surface: boolean;
  serverId?: string;
  active: boolean;
  suspended: boolean;
  zoom: DigitalZoom;
  onZoomChange: (zoom: DigitalZoom) => void;
  /** The target has a drawable frame and is now the visible layer. */
  onReady: (quality: Quality) => void;
}) {
  const [ready, setReady] = useState(false);
  return (
    <div
      data-testid="quality-target-surface"
      data-quality={quality}
      className={cn("absolute inset-0 z-[1]", ready ? "opacity-100" : "pointer-events-none opacity-0")}
    >
      <MsePlayer
        cameraId={cameraId}
        quality={quality}
        persistent={persistent}
        surface={surface}
        serverId={serverId}
        active={active}
        suspended={suspended}
        objectFit="fill"
        className="size-full"
        onFirstFrame={() => {
          setReady(true);
          onReady(quality);
        }}
        hideStatusOverlay
        hideSurfaceUntilFrame
        zoom={zoom}
        onZoomChange={onZoomChange}
      />
    </div>
  );
}

function GridTile({
  index,
  placement,
  tile,
  camera,
  isSelected,
  isFocused,
  isHidden,
  isDuplicate,
  recLayer,
  suspended,
  trimVisualWork,
  persistent,
  surface,
  serverId,
  canViewRecordings,
  status,
  quality,
  qualityOverride,
  onSelect,
  onToggleFocus,
  onOpenFisheye,
  onConfigureFisheye,
  fisheyeVersion,
  onRemove,
}: {
  index: number;
  placement?: CSSProperties;
  tile: Tile;
  camera: Schemas["Camera"] | undefined;
  isSelected: boolean;
  isFocused: boolean;
  /** Another tile is expanded: keep this one mounted (its session WARM) but not displayed. */
  isHidden: boolean;
  /** The camera is already shown in an earlier cell; this one shows a snapshot instead. */
  isDuplicate: boolean;
  /** REC mode: recorded-playback layer covering the live player. */
  recLayer: ReactNode;
  /** REC mode or adaptive policy: the live session is kept but its transport paused. */
  suspended: boolean;
  /** Adaptive policy: reduce GPU work / transitions. */
  trimVisualWork?: boolean;
  persistent: boolean;
  surface: boolean;
  serverId?: string;
  canViewRecordings: boolean;
  status?: string;
  quality: "sub" | "main";
  qualityOverride?: Quality;
  onSelect: () => void;
  onToggleFocus: () => void;
  onOpenFisheye?: (cameraId: string) => void;
  onConfigureFisheye?: (cameraId: string) => void;
  fisheyeVersion?: number;
  onRemove: () => void;
}) {
  const tr = useT();
  const { visible: overlayVisible, reveal: revealOverlay, scheduleHide: scheduleOverlayHide } = useTileOverlayVisibility();
  // Digital zoom/pan belongs to the tile, not to a quality layer, so it survives the sub -> main handoff.
  const [tileZoom, setTileZoom] = useState<DigitalZoom>(IDENTITY_ZOOM);
  const tileCameraId = tile && "camera_id" in tile ? tile.camera_id : null;
  const [zoomCameraId, setZoomCameraId] = useState(tileCameraId);
  if (zoomCameraId !== tileCameraId) {
    setZoomCameraId(tileCameraId);
    setTileZoom(IDENTITY_ZOOM);
  }
  // Quality badge: the target counts as visible only after its first drawable frame.
  const [targetReady, setTargetReady] = useState<Quality | null>(null);
  const handoffRequested = qualityOverride !== undefined && qualityOverride !== quality;
  if (!handoffRequested && targetReady !== null) setTargetReady(null);
  const targetVisible = handoffRequested && targetReady === qualityOverride;
  const visibleQuality: Quality = targetVisible ? qualityOverride : quality;
  const pendingQuality: Quality | null = handoffRequested && !targetVisible ? qualityOverride : null;
  const [fisheyeActionsAt, setFisheyeActionsAt] = useState<{ x: number; y: number; opener: HTMLButtonElement } | null>(null);
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: tileDragId(index), disabled: persistent && (isHidden || isFocused) });
  const style: CSSProperties = { transform: CSS.Transform.toString(transform), transition: trimVisualWork ? undefined : transition, ...placement };
  // Touch: two quick taps expand the tile like a double click. The dblclick some browsers synthesize after it is ignored.
  const touchTap = useTouchDoubleTap(onToggleFocus);
  return (
    <div
      ref={setNodeRef}
      style={style}
      aria-label={tr("live.cell", { index: index + 1 })}
      aria-current={isSelected ? "true" : undefined}
      data-selected={isSelected ? "true" : undefined}
      onClick={(event) => {
        onSelect();
        if (event.detail > 0 && typeof window.matchMedia === "function" && window.matchMedia("(hover: none)").matches) {
          revealOverlay();
          scheduleOverlayHide();
        }
      }}
      onPointerEnter={revealOverlay}
      onPointerLeave={scheduleOverlayHide}
      onFocusCapture={revealOverlay}
      onBlurCapture={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) scheduleOverlayHide();
      }}
      onDoubleClick={() => {
        if (touchTap.justHandled()) return;
        onToggleFocus();
      }}
      onContextMenu={(e) => {
        if (tile && "camera_id" in tile && onConfigureFisheye) {
          const feConfig = getFisheyeConfig(tile.camera_id);
          const isFisheye = Boolean(
            (feConfig.enabled && feConfig.lensType !== "normal") ||
            camera?.tags?.includes("fisheye") ||
            camera?.tags?.includes("ojo_de_pez") ||
            Boolean(camera?.onvif_profile && detectFisheyeProfile({ onvif_profile: camera.onvif_profile }))
          );
          if (isFisheye) {
            e.preventDefault();
            e.stopPropagation();
            onConfigureFisheye(tile.camera_id);
          }
        }
      }}
      {...touchTap.handlers}
      className={cn(
        "group relative select-none touch-manipulation [-webkit-touch-callout:none] aspect-video overflow-hidden bg-video outline-none focus-visible:ring-2 focus-visible:ring-primary md:aspect-auto md:min-h-0",
        isSelected && "outline-2 outline-offset-1 outline-primary",
        isDragging && "opacity-50",
        isHidden && "hidden",
      )}
      {...attributes}
      {...listeners}
    >
      {tile && isMapTile(tile) ? (
        <>
          <LiveMapTile map={tile.map} />
          <IconButton
            icon={X}
            title={tr("live.removeMap")}
            aria-label={tr("live.removeNamed", { name: tile.map.name })}
            className={tileAction + " absolute right-2 top-2 z-[3]"}
            onClick={(event) => {
              event.stopPropagation();
              onRemove();
            }}
          />
        </>
      ) : tile && isVisionTile(tile) ? (
        <>
          <LiveVisionTile vision={tile.vision} isFocused={isFocused} />
          <IconButton
            icon={X}
            title="Quitar Vision AI"
            aria-label={`Quitar ${tile.vision.name}`}
            className={tileAction + " absolute right-2 top-2 z-[3]"}
            onClick={(event) => {
              event.stopPropagation();
              onRemove();
            }}
          />
        </>
      ) : tile && camera && "camera_id" in tile ? (
        <>
          <DetectionListOverlay cameraId={camera.id} lpr={camera.lpr} faces enabled={!isDuplicate && !isHidden} />
          {(() => {
            const feConfig = getFisheyeConfig(tile.camera_id);
            const isFisheyeCamera = Boolean(
              (feConfig.enabled && feConfig.lensType !== "normal") ||
              camera.tags?.includes("fisheye") ||
              camera.tags?.includes("ojo_de_pez") ||
              Boolean(camera.onvif_profile && detectFisheyeProfile({ onvif_profile: camera.onvif_profile }))
            );
            const effectiveViews = isFisheyeCamera ? getEffectiveVirtualViews(feConfig) : [];
            const specificView = tile.fisheye_view_id
              ? effectiveViews.find((v) => v.id === tile.fisheye_view_id) ||
                feConfig.views.find((v) => v.id === tile.fisheye_view_id)
              : undefined;
            const hasMultipleViews = isFisheyeCamera && (feConfig.defaultMode === "3_fixed_120" || effectiveViews.length >= 2);
            const useFisheye =
              !suspended &&
              isFisheyeCamera &&
              (specificView !== undefined || (feConfig.defaultMode !== "original" && !hasMultipleViews));

            if (isDuplicate) {
              return (
                <div className="relative size-full">
                  <img src={`/media/v1/cameras/${tile.camera_id}/snapshot.jpg?h=360`} alt="" draggable={false} className="size-full object-fill opacity-60" />
                  <span className="absolute inset-0 flex items-center justify-center p-2 text-center text-xs"><span className="rounded-full bg-surface-1/80 px-3 py-1 text-on-surface">{tr("live.alreadyVisible")}</span></span>
                </div>
              );
            }

            if (recLayer && !persistent) return null;

            if (useFisheye) {
              return (
                <FisheyeQualityLayers
                  cameraId={tile.camera_id}
                  config={feConfig}
                  compact
                  initialMode={specificView ? "virtual_ptz" : feConfig.defaultMode}
                  targetViewId={specificView?.id}
                  className="size-full"
                  quality={quality}
                  qualityOverride={qualityOverride}
                  identity={`${tile.camera_id}-${tile.fisheye_view_id ?? "root"}-${feConfig.defaultMode}-${fisheyeVersion ?? 0}`}
                  onTargetReady={setTargetReady}
                />
              );
            }

            const basePlayer = <MsePlayer cameraId={tile.camera_id} quality={quality} persistent={persistent} surface={surface} serverId={serverId} active={!isHidden} suspended={suspended} objectFit="fill" className="size-full" zoom={tileZoom} onZoomChange={setTileZoom} />;
            // The wrapper is rendered whether or not a target layer exists, so the base player keeps its
            // position in the tree and is never remounted (its session closed) when the target appears or goes away.
            const showTarget = qualityOverride !== undefined && qualityOverride !== quality;
            return (
              <div className="relative size-full">
                <div className="absolute inset-0 z-0">{basePlayer}</div>
                {showTarget && (
                  <QualityTargetSurface
                    key={`${tileIdentityKey(tile) ?? tile.camera_id}:${qualityOverride}`}
                    cameraId={tile.camera_id}
                    quality={qualityOverride}
                    persistent={persistent}
                    surface={surface}
                    serverId={serverId}
                    active={!isHidden}
                    suspended={suspended}
                    zoom={tileZoom}
                    onZoomChange={setTileZoom}
                    onReady={setTargetReady}
                  />
                )}
              </div>
            );
          })()}
          {recLayer}
          <div className="absolute inset-x-0 top-0 z-[3] flex items-center gap-1.5 p-2 text-xs">
            {(() => {
              const feConfig = getFisheyeConfig(tile.camera_id);
              const isFisheyeCamera = Boolean(
                (feConfig.enabled && feConfig.lensType !== "normal") ||
                camera.tags?.includes("fisheye") ||
                camera.tags?.includes("ojo_de_pez") ||
                Boolean(camera.onvif_profile && detectFisheyeProfile({ onvif_profile: camera.onvif_profile }))
              );
              const effectiveViews = isFisheyeCamera ? getEffectiveVirtualViews(feConfig) : [];
              const specificView = tile.fisheye_view_id
                ? effectiveViews.find((v) => v.id === tile.fisheye_view_id) ||
                  feConfig.views.find((v) => v.id === tile.fisheye_view_id)
                : undefined;
              const hasMultipleViews = isFisheyeCamera && (feConfig.defaultMode === "3_fixed_120" || effectiveViews.length >= 2);
              return (
                <span className="flex min-w-0 items-center gap-1.5 rounded-full bg-surface-1/80 px-2.5 py-1 text-on-surface backdrop-blur">
                  <Icon icon={specificView ? Compass : Camera} size="xs" className={cn("shrink-0", specificView && "text-cyan-400")} />
                  <span className={cn("truncate font-medium transition-opacity", !overlayVisible && status !== "offline" && "opacity-0 group-hover:opacity-100 group-focus-within:opacity-100")}>
                    {camera.display_name}
                    {specificView ? ` · ${specificView.name}` : (isFisheyeCamera && hasMultipleViews ? " (Original)" : "")}
                  </span>
                  {specificView && (
                    <span className="text-[10px] font-mono text-cyan-300 bg-cyan-950/60 px-1 rounded border border-cyan-800/40">
                      {specificView.yaw}°
                    </span>
                  )}
                  <QualityBadge visible={visibleQuality} pending={pendingQuality} />
                  <CodecBadge codec={streamCodec(camera, visibleQuality)} />
                  <span className="inline-flex shrink-0 items-center gap-1" aria-label={`Status: ${status ?? "unknown"}`} role="status">
                    <span className={cn("size-2 rounded-full", status === "online" ? "bg-ok" : status === "offline" ? "bg-bad" : "bg-warn")} />
                    <Icon icon={status === "online" ? CircleCheck : status === "offline" ? CircleX : CircleHelp} size={12} className={status === "offline" ? "text-bad" : undefined} />
                    <span className="sr-only">{status ?? "unknown"}</span>
                  </span>
                </span>
              );
            })()}
            <span className={cn("ml-auto flex shrink-0 gap-1 transition-opacity", !overlayVisible && "pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 group-focus-within:pointer-events-auto group-focus-within:opacity-100")}>
              {canViewRecordings && (
                <Link to="/playback" search={{ camera: tile.camera_id }} title={tr("nav.recordings")} aria-label={tr("live.recordingsOf", { name: camera.display_name })} className={cn(tileAction, "m3-press inline-flex items-center justify-center focus-visible:outline-2 focus-visible:outline-primary")}>
                  <Icon icon={History} size="xs" />
                </Link>
              )}
              {camera && (camera.tags?.includes("fisheye") || camera.tags?.includes("ojo_de_pez") || (getFisheyeConfig(tile.camera_id).enabled && getFisheyeConfig(tile.camera_id).lensType !== "normal") || Boolean(camera.onvif_profile && detectFisheyeProfile({ onvif_profile: camera.onvif_profile }))) && (onOpenFisheye || onConfigureFisheye) && (
                <IconButton
                  icon={Fish}
                  title="Acciones de Fisheye"
                  aria-label="Acciones de Fisheye"
                  aria-haspopup="menu"
                  aria-expanded={Boolean(fisheyeActionsAt)}
                  className={cn(tileAction, "text-cyan-400 hover:text-cyan-300")}
                  onClick={(e) => {
                    e.stopPropagation();
                    const bounds = e.currentTarget.getBoundingClientRect();
                    setFisheyeActionsAt({ x: bounds.left, y: bounds.bottom, opener: e.currentTarget });
                  }}
                />
              )}
              <IconButton
                icon={isFocused ? Minimize2 : Maximize2}
                title={isFocused ? tr("live.backToGrid") : tr("live.expandTile")}
                aria-label={isFocused ? tr("live.backToGrid") : tr("live.expandTile")}
                className={tileAction}
                onClick={(e) => {
                  e.stopPropagation();
                  onToggleFocus();
                }}
              />
              <IconButton
                icon={X}
                title={tr("live.remove")}
                aria-label={tr("live.removeNamed", { name: camera.display_name })}
                className={tileAction}
                onClick={(e) => {
                  e.stopPropagation();
                  onRemove();
                }}
              />
            </span>
          </div>
          {fisheyeActionsAt && camera && (
            <ContextMenu
              x={fisheyeActionsAt.x}
              y={fisheyeActionsAt.y}
              returnFocusTo={fisheyeActionsAt.opener}
              onClose={() => setFisheyeActionsAt(null)}
              items={[
                ...(onOpenFisheye ? [{ id: "dewarp", label: "Navegación y dewarping Fisheye 360°", icon: Fish, onSelect: () => onOpenFisheye(camera.id) } satisfies MenuItem] : []),
                ...(onConfigureFisheye ? [{ id: "configure", label: "Configuración de vista Fisheye", icon: SlidersHorizontal, onSelect: () => onConfigureFisheye(camera.id) } satisfies MenuItem] : []),
              ]}
            />
          )}
        </>
      ) : (
        <div className="flex size-full items-center justify-center text-xs text-on-surface-variant">{tile && !camera ? tr("live.noAccess") : tr("live.emptyCell")}</div>
      )}
    </div>
  );
}
