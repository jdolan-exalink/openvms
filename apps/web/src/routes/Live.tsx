import { DndContext, DragOverlay, type DragEndEvent, KeyboardSensor, PointerSensor, useDroppable, useSensor, useSensors } from "@dnd-kit/core";
import { rectSortingStrategy, rectSwappingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createPortal } from "react-dom";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { faAnglesRight } from "@fortawesome/free-solid-svg-icons";
import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import { Camera, CircleCheck, CircleHelp, CircleX, History, Maximize2, Minimize2, X } from "lucide-react";
import { type CSSProperties, type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { camerasQuery, meQuery, serversQuery, sitesQuery, viewsQuery } from "@/api/queries";
import { LiveMapTile } from "@/components/maps/LiveMapTile";
import { MsePlayer } from "@/components/MsePlayer";
import { useFeatures } from "@/lib/features";
import { LivePlaybackPanel } from "@/components/LivePlaybackPanel";
import { LiveRecDock } from "@/components/LiveRecDock";
import { RecTile, type RecTileState } from "@/components/RecTile";
import { LiveModeToggle } from "@/components/LiveModeToggle";
import { setContextSidebarCollapsed, useContextSidebarPortalTarget, useTopBarActionsPortalTarget } from "@/components/AppShell";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { type ExplorerActions, LiveExplorer } from "@/components/LiveExplorer";
import { Modal } from "@/components/Modal";
import { FullscreenButton, LayoutMenu, PresentationEditor } from "@/components/Presentations";
import { Button, ErrorNote, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import {
  cameraIdOf, duplicateTileIndexes, fillTiles, fitTiles, growLayout, isMapTile, LIVE_GRID_DROP_ID, liveSelectionKey, parseSelection, placeCameraAt, placeCameraUnique, placeInOpenCell, resizeTiles, resolveDragEnd, reorderTiles,
  serializeSelection, swapTiles, tileDragId,
  type Tile,
} from "@/lib/liveGrid";
import { DEFAULT_PRESENTATIONS, loadCatalog, presentationForCount, recallViewPanes, rememberViewPanes, saveCatalog, uniformPanes, type Pane, type Presentation } from "@/lib/presentations";
import { assignRecPlayers, parseRecSearch, pickMaster, REC_ENTRY_OFFSET_S, recSearch } from "@/lib/liveRec";
import { loadSidebarCollapsed, saveSidebarCollapsed } from "@/lib/explorer";
import { can } from "@/lib/perm";
import { useCameraFolders } from "@/lib/useCameraFolders";
import { useRecData } from "@/lib/useRecData";
import { useRecPlayback } from "@/lib/useRecPlayback";
import { useSyncedPlayback } from "@/lib/useSyncedPlayback";

/** copyViewName picks «Nombre (copia)» or «Nombre (copia 2)» so a duplicate stays unique. */
function copyViewName(name: string, taken: Set<string>): string {
  const base = `${name} (copia)`;
  if (!taken.has(base.toLowerCase())) return base;
  for (let n = 2; n < 100; n++) {
    const candidate = `${name} (copia ${n})`;
    if (!taken.has(candidate.toLowerCase())) return candidate;
  }
  return `${base} ${taken.size + 1}`;
}

const unixNow = () => Math.floor(Date.now() / 1000);
/** How often the shared REC time is written to the URL while playing. */
const URL_SYNC_MS = 15_000;

/**
 * Live is the multi-server live screen (PRD §46-49): a camera tree grouped by site and
 * server, a grid of live tiles from any Frigate, and saved views.
 */
export function Live() {
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
  const [selected, setSelected] = useState(0);
  const [focus, setFocus] = useState<number | null>(null);
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
  useEffect(() => {
    tilesRef.current = tiles;
    selectedRef.current = selected;
  }, [tiles, selected]);
  const [consumedCamera, setConsumedCamera] = useState<string | undefined>();
  const folderApi = useCameraFolders(cameras.data);
  const [sidebarCollapsed, setSidebarCollapsedState] = useState(loadSidebarCollapsed);
  const [treeNotice, setTreeNotice] = useState<string | null>(null);
  const [noticeTone, setNoticeTone] = useState<"ok" | "warn">("warn");
  const [deleteViewId, setDeleteViewId] = useState<string | null>(null);
  const flash = (text: string, tone: "ok" | "warn" = "ok") => {
    setNoticeTone(tone);
    setTreeNotice(text);
    setTimeout(() => setTreeNotice(null), 4000);
  };
  const [activeDrag, setActiveDrag] = useState<string | null>(null);
  const setSidebarCollapsed = (next: boolean) => {
    setSidebarCollapsedState(next);
    saveSidebarCollapsed(next);
  };
  // The shell owns the sidebar's width; the route only says whether it is collapsed. Reset on
  // leaving Live so other screens keep their context sidebar.
  useEffect(() => {
    setContextSidebarCollapsed(sidebarCollapsed);
    return () => setContextSidebarCollapsed(false);
  }, [sidebarCollapsed]);
  const sidebarMount = useContextSidebarPortalTarget();
  const topBar = useTopBarActionsPortalTarget();
  const { persistentPlayers, videoSurfaceLayer } = useFeatures();
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

  // LIVE/REC (LV-9): the mode and shared time live in the URL (?mode=rec&t=<ISO>) so reload and
  // links work; LIVE clears both. REC needs a recordings grant somewhere, else it never activates.
  const search = useSearch({ strict: false }) as Record<string, unknown>;
  const navigate = useNavigate();
  const canRec = can(me.data, "recordings.view");
  const { rec: urlRec, t: urlT } = parseRecSearch(search);
  const rec = urlRec && canRec;
  const [now, setNow] = useState(unixNow);
  useEffect(() => {
    if (!rec) return;
    const id = setInterval(() => setNow(unixNow()), 30_000);
    return () => clearInterval(id);
  }, [rec]);

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
        setTiles(saved.tiles);
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

  // index defaults to the currently selected tile (click-to-place); dragging a camera from the
  // list onto a specific slot passes that slot's index explicitly instead.
  const place = (cameraId: string, index?: number) => {
    const at = index ?? selectedRef.current;
    const defaultQuality = cameras.data?.find((c) => c.id === cameraId)?.default_live_quality ?? "sub";
    const quality = columns === 1 ? "main" : defaultQuality;
    // The ref updates immediately so two clicks before the next render fill two cells.
    const source = tilesRef.current;
    const next = persistentPlayers ? placeCameraUnique(source, at, cameraId, quality) : placeCameraAt(source, at, cameraId, quality);
    const empty = next.findIndex((cell, i) => cell === null && i !== at);
    tilesRef.current = next;
    if (empty >= 0) selectedRef.current = empty;
    setTiles(next);
    if (empty >= 0) setSelected(empty);
  };

  // Swapping (not shifting) keeps every other tile in its cell, so only the two swapped cameras move.
  const reorder = (from: number, to: number) => setTiles((t) => (persistentPlayers ? swapTiles(t, from, to) : reorderTiles(t, from, to)));

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const handleDragEnd = (event: DragEndEvent) => {
    setActiveDrag(null);
    const resolution = resolveDragEnd(event.active.id, event.over?.id ?? null);
    if (!resolution) {
      // Not a grid drop: it may be a move inside the shared explorer tree.
      if (folderApi.drop(event.active.id, event.over?.id ?? null) === "rejected") {
        flash("No se puede mover entre servidores.", "warn");
      }
      return;
    }
    if (resolution.type === "place") place(resolution.cameraId, resolution.index);
    else if (resolution.type === "reorder") reorder(resolution.from, resolution.to);
    else if (resolution.type === "fill-server") showGroup(camerasOfServer(resolution.serverId), servers.data?.find((server) => server.id === resolution.serverId)?.name ?? "El servidor");
    else showGroup(camerasOfFolder(resolution.folderId), folderApi.folders.find((folder) => folder.id === resolution.folderId)?.name ?? "La carpeta");
  };

  const showView = (v: Schemas["View"]) => {
    setViewId(v.id);
    const n = v.layout.columns;
    const next: Tile[] = v.layout.cells.map((c) => (c.camera_id ? { camera_id: c.camera_id, quality: c.quality ?? "sub" } : null));
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
    cells: tiles.map((t) => (t && "camera_id" in t ? { camera_id: t.camera_id, quality: t.quality } : { quality: "sub" })),
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
        if (!view) throw new Error("La vista ya no existe.");
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
      if (!view) throw new Error("La vista ya no existe.");
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
    onError: (err) => flash(err instanceof Error && err.message ? err.message : "No se pudo duplicar la vista.", "warn"),
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
  const camerasOfServer = (serverId: string) => {
    const all = (cameras.data ?? []).filter((camera) => camera.enabled && camera.server_id === serverId);
    const folders = folderApi.folders.filter((folder) => folder.server_id === serverId).sort((a, b) => a.sort_order - b.sort_order || a.name.localeCompare(b.name));
    const folderIds = new Set(folders.map((folder) => folder.id));
    return [
      ...folders.flatMap((folder) => all.filter((camera) => camera.folder_id === folder.id).sort(byCamera)),
      ...all.filter((camera) => camera.folder_id == null || !folderIds.has(camera.folder_id)).sort(byCamera),
    ];
  };
  const camerasOfFolder = (folderId: string) => (cameras.data ?? []).filter((camera) => camera.enabled && camera.folder_id === folderId).sort(byCamera);
  const showGroup = (list: Schemas["Camera"][], label: string) => {
    if (list.length === 0) {
      flash(`${label} no tiene cámaras.`, "warn");
      return;
    }
    const layout = presentationForCount(DEFAULT_PRESENTATIONS, list.length);
    const built = fitTiles(list.map((camera): Tile => ({ camera_id: camera.id, quality: cameraQuality(camera.id, layout.columns) })), layout.panes.length);
    const skipped = Math.max(0, list.length - layout.panes.length);
    setColumns(layout.columns);
    setRows(layout.rows);
    setPanes(layout.panes);
    setTiles(built);
    setSelected(0);
    setFocus(null);
    setViewId("");
    if (skipped > 0) flash(`${label}: se muestran ${list.length - skipped} de ${list.length} cámaras. La grilla más grande es ${layout.name}.`, "warn");
    else flash(`${label}: ${layout.name} con ${plural(list.length)}.`);
  };
  const placeInOpen = (cameraId: string) => {
    if (!camById.has(cameraId)) return;
    setTiles((current) => {
      const next = placeInOpenCell(current, cameraId, cameraQuality(cameraId, columns));
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
      if (!view) throw new Error("La vista ya no existe.");
      const n = view.layout.columns;
      const nextRows = view.layout.cells.length ? Math.ceil(view.layout.cells.length / n) : n;
      const existing = resizeTiles(view.layout.cells.map((c): Tile => (c.camera_id ? { camera_id: c.camera_id, quality: c.quality ?? "sub" } : null)), n, nextRows);
      const res = fillTiles(existing, v.ids.filter((id) => camById.has(id)).map((id) => ({ id, quality: cameraQuality(id, n) })));
      if (res.added === 0) return { view, res, saved: false };
      await unwrap(
        await api.PUT("/api/v1/views/{viewId}", {
          params: { path: { viewId: view.id } },
          body: { name: view.name, shared: view.shared, tenant_id: view.tenant_id, layout: { columns: n, cells: res.tiles.map((t) => (t && "camera_id" in t ? { camera_id: t.camera_id, quality: t.quality } : { quality: "sub" as const })) } },
        }),
      );
      await qc.invalidateQueries({ queryKey: ["views"] });
      if (viewId === view.id) setTiles(res.tiles);
      return { view, res, saved: true };
    },
    onSuccess: ({ view, res }) => reportFill(res, `a la vista «${view.name}»`),
    onError: (err) => flash(err instanceof Error && err.message ? `No se pudo actualizar la vista: ${err.message}` : "No se pudo actualizar la vista.", "warn"),
  });
  const explorerActions: ExplorerActions = {
    editableViews: (views.data ?? []).filter((v) => v.editable).map((v) => ({ id: v.id, name: v.name })),
    addToGrid,
    addToView: (id, ids) => addToView.mutate({ id, ids }),
    canConfigureSites: can(me.data, "sites.manage"),
    canConfigureServers: can(me.data, "servers.manage"),
    canConfigureCameras: can(me.data, "servers.config"),
  };

  // With persistent players the other tiles stay mounted (hidden, sessions WARM) while one is
  // expanded, so leaving the expanded view resumes them instantly instead of reconnecting.
  const shown = focus !== null && !persistentPlayers ? [focus] : tiles.map((_, i) => i);
  const gridCols = focus !== null ? 1 : columns;
  const duplicates = useMemo(() => (persistentPlayers ? duplicateTileIndexes(tiles) : new Set<number>()), [persistentPlayers, tiles]);

  const transport = useRecPlayback({ active: rec, seedT: urlT ?? now - REC_ENTRY_OFFSET_S, now });
  const gridCameraIds = useMemo(() => [...new Set(tiles.flatMap((t) => { const id = cameraIdOf(t); return id && camById.has(id) ? [id] : []; }))], [tiles, camById]);
  const recData = useRecData(gridCameraIds, transport.day, rec);
  const denied = useMemo(() => new Set(recData.denied), [recData.denied]);
  const focusedCameraId = focus !== null ? cameraIdOf(tiles[focus] ?? null) : undefined;
  const { players: recPlayers, limited: recLimited } = useMemo(
    () => assignRecPlayers(focusedCameraId ? [focusedCameraId] : gridCameraIds, (id) => !denied.has(id)),
    [focusedCameraId, gridCameraIds, denied],
  );
  const hasCoverage = (id: string) => (recData.spans[id]?.length ?? 0) > 0;
  const syncIds = useMemo(() => recPlayers.filter((id) => !recData.loaded.includes(id) || (recData.spans[id]?.length ?? 0) > 0), [recPlayers, recData.loaded, recData.spans]);
  const masterId = pickMaster(syncIds, cameraIdOf(tiles[selected] ?? null), hasCoverage);
  useSyncedPlayback(rec ? masterId : "", syncIds, (id) => transport.players.current.get(id)?.video);
  const timelineCameras = useMemo(
    () => gridCameraIds.filter((id) => !denied.has(id)).map((id) => ({ id, name: camById.get(id)?.display_name ?? id, spans: recData.spans[id] ?? [], live: camById.get(id)?.status === "online" })),
    [gridCameraIds, denied, camById, recData.spans],
  );

  // Entering GRABADO starts five minutes before now and plays. If the cameras stopped earlier, it starts at the end of the last recording.
  const entryPending = useRef(false);
  const setMode = (next: "live" | "rec") => {
    entryPending.current = next === "rec";
    void navigate({ to: ".", search: ((prev: Record<string, unknown>) => ({ ...prev, ...recSearch(next === "rec", unixNow() - REC_ENTRY_OFFSET_S) })) as never });
  };
  const { seek } = transport;
  useEffect(() => {
    if (!rec) entryPending.current = false;
  }, [rec]);
  useEffect(() => {
    if (!rec || !entryPending.current || recData.loaded.length < gridCameraIds.length - recData.denied.length || recData.loaded.length === 0) return;
    entryPending.current = false;
    const ends = recData.loaded.flatMap((id) => (recData.spans[id] ?? []).map((s) => s.end));
    const latest = ends.length ? Math.max(...ends) : undefined;
    if (latest !== undefined && latest < now - REC_ENTRY_OFFSET_S - 60) seek(latest - 5);
  }, [rec, recData, gridCameraIds.length, now, seek]);
  // Keep ?t= in step with the shared clock so reload and copied links land where the user is:
  // every URL_SYNC_MS while playing, and shortly after the last seek or pause.
  const { win, playing, getPosition, subscribePosition } = transport;
  useEffect(() => {
    if (!rec) return;
    const write = () =>
      void navigate({ to: ".", replace: true, search: ((prev: Record<string, unknown>) => ({ ...prev, ...recSearch(true, Math.floor(getPosition())) })) as never });
    let debounce: ReturnType<typeof setTimeout> | undefined;
    const unsubscribe = subscribePosition(() => {
      clearTimeout(debounce);
      debounce = setTimeout(write, 1500);
    });
    const interval = playing ? setInterval(write, URL_SYNC_MS) : undefined;
    return () => {
      unsubscribe();
      clearTimeout(debounce);
      clearInterval(interval);
    };
  }, [rec, win, playing, getPosition, subscribePosition, navigate]);
  const dragLabel = activeDrag?.startsWith("camera:")
    ? camById.get(activeDrag.slice("camera:".length))?.display_name
    : activeDrag?.startsWith("tfolder:")
      ? folderApi.folders.find((f) => f.id === activeDrag.slice("tfolder:".length))?.name
      : activeDrag?.startsWith("tserver:")
        ? servers.data?.find((server) => server.id === activeDrag.slice("tserver:".length))?.name
        : undefined;
  const modeToggle = <LiveModeToggle rec={rec} onChange={setMode} />;
  const topBarActions = canRec && !fullscreen && topBar.available && topBar.target ? createPortal(modeToggle, topBar.target) : null;
  const canCreateView = can(me.data, "views.create_private") || can(me.data, "views.create_shared");
  const canShareView = can(me.data, "views.create_shared");
  const sidebarContent = (
    <LiveExplorer
      cameras={cameras.data ?? []}
      sites={sites.data ?? []}
      servers={servers.data ?? []}
      folderApi={folderApi}
      onPick={place}
      onDoublePlace={placeInOpen}
      onPlayback={setPlaybackCameraId}
      canViewRecordings={can(me.data, "recordings.view")}
      onCollapse={() => setSidebarCollapsed(true)}
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
  const sidebar = sidebarMount.available
    ? sidebarMount.target ? createPortal(sidebarContent, sidebarMount.target) : null
    : sidebarCollapsed ? null : <aside className="flex w-full shrink-0 flex-col gap-3 lg:w-64">{sidebarContent}</aside>;

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <h1 className="sr-only">En vivo</h1>
      {topBarActions}
      <DndContext sensors={sensors} onDragStart={(e) => setActiveDrag(String(e.active.id))} onDragCancel={() => setActiveDrag(null)} onDragEnd={handleDragEnd}>
        <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
          <section data-live-stage className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
            <div role="group" aria-label="Layout de la grilla" className="flex shrink-0 flex-wrap items-center gap-1">
              {sidebarCollapsed && (
                <button
                  type="button"
                  onClick={() => setSidebarCollapsed(false)}
                  aria-label="Mostrar explorador"
                  title="Mostrar explorador"
                  className="mr-1 rounded border border-line bg-surface p-1 hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent"
                >
                  <FontAwesomeIcon icon={faAnglesRight} fixedWidth className="text-sm" aria-hidden />
                </button>
              )}
              <LayoutMenu catalog={catalog} active={{ columns, panes }} onSelect={applyPresentation} onEdit={() => setEditorOpen(true)} />
              <FullscreenButton active={fullscreen} onClick={toggleFullscreen} />
              {canRec && (fullscreen || !topBar.available) && <div className="ml-auto">{modeToggle}</div>}
            </div>
            <SortableContext items={shown.map((i) => tileDragId(i))} strategy={persistentPlayers ? rectSwappingStrategy : rectSortingStrategy}>
              <LiveGridFrame role="group" label="Grilla de video" mode={rec ? "rec" : "live"}
                className={cn(
                  "grid min-h-0 flex-1 gap-1 overflow-y-auto rounded-md ring-2 md:overflow-hidden md:[grid-template-rows:repeat(var(--grid-rows),minmax(0,1fr))]",
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
                  return (
                    <GridTile
                      key={persistentPlayers ? (cameraId && !duplicates.has(i) ? `camera:${cameraId}` : `cell:${i}`) : i}
                      index={i}
                      placement={focus === null && panes[i] ? { gridColumn: `${panes[i].col + 1} / span ${panes[i].colSpan}`, gridRow: `${panes[i].row + 1} / span ${panes[i].rowSpan}` } : undefined}
                      tile={t}
                      camera={cam}
                      isSelected={selected === i}
                      isFocused={focus === i}
                      isHidden={isHidden}
                      recLayer={recLayer}
                      suspended={rec}
                      isDuplicate={duplicates.has(i)}
                      persistent={persistentPlayers}
                      surface={persistentPlayers && videoSurfaceLayer}
                      serverId={cam?.server_id}
                      canViewRecordings={can(me.data, "recordings.view")}
                      status={cam?.status}
                      quality={persistentPlayers ? (t && "quality" in t ? t.quality : "sub") : focus === i || columns === 1 ? "main" : (t && "quality" in t ? t.quality : "sub")}
                      onSelect={() => setSelected(i)}
                      onToggleFocus={() => t && setFocus(focus === null ? i : null)}
                      onRemove={() => {
                        setTiles((x) => x.map((v, j) => (j === i ? null : v)));
                        setFocus(null);
                      }}
                    />
                  );
                })}
              </LiveGridFrame>
            </SortableContext>
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
          {dragLabel ? <div className="pointer-events-none rounded border border-accent bg-surface px-2 py-1 text-xs shadow-lg">{dragLabel}</div> : null}
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
        <Modal title="Propiedades" onClose={() => setViewDialog(null)} className="max-w-md">
          <TextInput aria-label="Nombre de la vista" placeholder="Nombre de la vista" value={dialogName} onChange={(event) => setDialogName(event.target.value)} />
          {canShareView ? (
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" checked={dialogShared} onChange={(event) => setDialogShared(event.target.checked)} /> Compartida con mi organización
            </label>
          ) : (
            viewDialog.mode === "edit" && dialogShared && <p className="text-sm text-muted">Compartida con mi organización</p>
          )}
          {viewDialog.mode === "edit" && viewId === viewDialog.id && <p className="text-xs text-muted">Al guardar se actualiza el diseño que está en la grilla.</p>}
          <ErrorNote error={save.error} />
          <div className="flex justify-end gap-2">
            <Button onClick={() => setViewDialog(null)}>Cancelar</Button>
            <Button variant="primary" onClick={() => save.mutate()} disabled={!dialogName.trim() || save.isPending}>
              Guardar
            </Button>
          </div>
        </Modal>
      )}
      {deleteViewId && (
        <ConfirmDialog
          title="Eliminar vista"
          message={`Se eliminará la vista «${views.data?.find((v) => v.id === deleteViewId)?.name ?? ""}». Esta acción no se puede deshacer.`}
          confirmLabel="Eliminar"
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
    </div>
  );
}

function LiveGridFrame({ className, style, role, label, mode, children }: { className?: string; style?: CSSProperties; role?: string; label: string; mode?: string; children: ReactNode }) {
  const { setNodeRef } = useDroppable({ id: LIVE_GRID_DROP_ID });
  return (
    <div ref={setNodeRef} role={role} aria-label={label} data-mode={mode} className={className} style={style}>
      {children}
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
  persistent,
  surface,
  serverId,
  canViewRecordings,
  status,
  quality,
  onSelect,
  onToggleFocus,
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
  /** REC mode: the live session is kept but its transport paused. */
  suspended: boolean;
  persistent: boolean;
  surface: boolean;
  serverId?: string;
  canViewRecordings: boolean;
  status?: string;
  quality: "sub" | "main";
  onSelect: () => void;
  onToggleFocus: () => void;
  onRemove: () => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: tileDragId(index), disabled: persistent && (isHidden || isFocused) });
  const style: CSSProperties = { transform: CSS.Transform.toString(transform), transition, ...placement };
  return (
    <div
      ref={setNodeRef}
      style={style}
      aria-label={`Cuadro ${index + 1}`}
      onClick={onSelect}
      onDoubleClick={onToggleFocus}
      className={cn(
        "group relative aspect-video overflow-hidden md:aspect-auto md:min-h-0 rounded border bg-black outline-none focus-visible:ring-2 focus-visible:ring-accent",
        isSelected ? "border-accent" : "border-line",
        isDragging && "opacity-50",
        isHidden && "hidden",
      )}
      {...attributes}
      {...listeners}
    >
      {tile && isMapTile(tile) ? (
        <>
          <LiveMapTile map={tile.map} />
          <button
            type="button"
            title="Quitar mapa"
            aria-label={`Quitar ${tile.map.name}`}
            className="absolute right-2 top-2 z-[3] rounded bg-black/70 p-1 text-white hover:bg-white/20"
            onClick={(event) => {
              event.stopPropagation();
              onRemove();
            }}
          >
            <X className="size-3.5" aria-hidden />
          </button>
        </>
      ) : tile && camera && "camera_id" in tile ? (
        <>
          {isDuplicate ? (
            <div className="relative size-full">
              <img src={`/media/v1/cameras/${tile.camera_id}/snapshot.jpg?h=360`} alt="" draggable={false} className="size-full object-contain opacity-60" />
              <span className="absolute inset-0 flex items-center justify-center p-2 text-center text-xs text-white">Ya visible en otra celda</span>
            </div>
          ) : recLayer && !persistent ? null : (
            // Without persistent players a live socket would keep streaming under REC, so it is
            // unmounted there; persistent sessions stay mounted and suspended (last frame kept).
            <MsePlayer cameraId={tile.camera_id} quality={quality} persistent={persistent} surface={surface} serverId={serverId} active={!isHidden} suspended={suspended} className="size-full" />
          )}
          {recLayer}
          <div className="absolute inset-x-0 top-0 z-[3] flex items-center gap-1.5 bg-gradient-to-b from-black/80 via-black/45 to-transparent px-2 py-1.5 text-xs text-white">
            <Camera className="size-3.5 shrink-0" aria-hidden />
            <span className="truncate font-medium">{camera.display_name}</span>
            <span className="inline-flex shrink-0 items-center gap-1" aria-label={`Status: ${status ?? "unknown"}`} role="status">
              <span className={cn("size-1.5 rounded-full ring-1 ring-white/80", status === "online" ? "bg-emerald-400" : status === "offline" ? "bg-red-400" : "bg-amber-300")} />
              {status === "online" ? <CircleCheck className="size-3" aria-hidden /> : status === "offline" ? <CircleX className="size-3" aria-hidden /> : <CircleHelp className="size-3" aria-hidden />}
              <span className="sr-only">{status ?? "unknown"}</span>
            </span>
            <span className="ml-auto flex shrink-0 gap-1">
              {canViewRecordings && (
                <Link to="/playback" search={{ camera: tile.camera_id }} title="Grabaciones" aria-label={`Grabaciones de ${camera.display_name}`} className="rounded p-1 hover:bg-white/20 focus-visible:outline focus-visible:outline-2 focus-visible:outline-white">
                  <History className="size-3.5" aria-hidden />
                </Link>
              )}
              <button
                type="button"
                title={isFocused ? "Volver a la grilla" : "Ampliar"}
                aria-label={isFocused ? "Volver a la grilla" : "Ampliar"}
                className="rounded p-1 hover:bg-white/20 focus-visible:outline focus-visible:outline-2 focus-visible:outline-white"
                onClick={(e) => {
                  e.stopPropagation();
                  onToggleFocus();
                }}
              >
                {isFocused ? <Minimize2 className="size-3.5" aria-hidden /> : <Maximize2 className="size-3.5" aria-hidden />}
              </button>
              <button
                type="button"
                title="Quitar"
                aria-label={`Quitar ${camera.display_name}`}
                className="rounded p-1 hover:bg-white/20 focus-visible:outline focus-visible:outline-2 focus-visible:outline-white"
                onClick={(e) => {
                  e.stopPropagation();
                  onRemove();
                }}
              >
                <X className="size-3.5" aria-hidden />
              </button>
            </span>
          </div>
        </>
      ) : (
        <div className="flex size-full items-center justify-center text-xs text-muted">{tile && !camera ? "Cámara sin acceso" : "Vacío"}</div>
      )}
    </div>
  );
}
