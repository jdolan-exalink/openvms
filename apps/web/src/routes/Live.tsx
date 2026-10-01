import { DndContext, DragOverlay, type DragEndEvent, KeyboardSensor, PointerSensor, useSensor, useSensors } from "@dnd-kit/core";
import { rectSortingStrategy, rectSwappingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createPortal } from "react-dom";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { faAnglesRight } from "@fortawesome/free-solid-svg-icons";
import { faFolderOpen, faPen, faTrash } from "@fortawesome/free-solid-svg-icons";
import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import { Camera, CircleCheck, CircleHelp, CircleX, History, Maximize2, Minimize2, Save, Trash2, X } from "lucide-react";
import { type CSSProperties, type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { camerasQuery, meQuery, serversQuery, sitesQuery, viewsQuery } from "@/api/queries";
import { MsePlayer } from "@/components/MsePlayer";
import { useFeatures } from "@/lib/features";
import { LivePlaybackPanel } from "@/components/LivePlaybackPanel";
import { LiveRecDock } from "@/components/LiveRecDock";
import { RecTile, type RecTileState } from "@/components/RecTile";
import { LiveModeToggle } from "@/components/LiveModeToggle";
import { setContextSidebarCollapsed, useContextSidebarPortalTarget, useTopBarActionsPortalTarget } from "@/components/AppShell";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { type ExplorerActions, LiveExplorer } from "@/components/LiveExplorer";
import type { MenuItem } from "@/components/ContextMenu";
import { Button, ErrorNote, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import {
  duplicateTileIndexes, fillTiles, growLayout, liveSelectionKey, parseSelection, placeCameraAt, placeCameraUnique, resizeTiles, resolveDragEnd, reorderTiles,
  serializeSelection, swapTiles, tileDragId,
  type Tile,
} from "@/lib/liveGrid";
import { assignRecPlayers, parseRecSearch, pickMaster, REC_ENTRY_OFFSET_S, recSearch } from "@/lib/liveRec";
import { loadSidebarCollapsed, saveSidebarCollapsed } from "@/lib/explorer";
import { can } from "@/lib/perm";
import { useCameraFolders } from "@/lib/useCameraFolders";
import { useRecData } from "@/lib/useRecData";
import { useRecPlayback } from "@/lib/useRecPlayback";
import { useSyncedPlayback } from "@/lib/useSyncedPlayback";

const unixNow = () => Math.floor(Date.now() / 1000);
/** How often the shared REC time is written to the URL while playing. */
const URL_SYNC_MS = 15_000;

const GRID_LAYOUTS = [
  { columns: 1, rows: 1 }, { columns: 2, rows: 1 }, { columns: 2, rows: 2 },
  { columns: 3, rows: 2 }, { columns: 3, rows: 3 }, { columns: 4, rows: 3 }, { columns: 4, rows: 4 },
];
// Large walls (25 and 32 cameras); offered with persistent players, which pause off-screen tiles.
const LARGE_GRID_LAYOUTS = [{ columns: 5, rows: 5 }, { columns: 8, rows: 4 }];

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
  const [tiles, setTiles] = useState<Tile[]>(() => Array(4).fill(null));
  const [selected, setSelected] = useState(0);
  const [focus, setFocus] = useState<number | null>(null);
  const [viewId, setViewId] = useState("");
  const [saveName, setSaveName] = useState("");
  const [playbackCameraId, setPlaybackCameraId] = useState<string | null>(null);
  const [shared, setShared] = useState(false);
  // Set once the saved grid selection (or the default) has been applied, so the persistence
  // effect below never fires before restoration and overwrites a saved selection with defaults.
  const [restored, setRestored] = useState(false);
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
  const layouts = persistentPlayers ? [...GRID_LAYOUTS, ...LARGE_GRID_LAYOUTS] : GRID_LAYOUTS;

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
      localStorage.setItem(liveSelectionKey(me.data.tenant_id, me.data.id), serializeSelection(columns, tiles, rows));
    } catch {
      // Storage unavailable (private mode, quota): the grid still works for this session.
    }
  }, [restored, me.data, columns, rows, tiles]);

  // A Maps handoff is consumed only after REST has supplied the authorized camera list.
  const requestedCamera = typeof search.camera === "string" ? search.camera : undefined;
  // Like storage restoration above, adjust derived local state during render, not in an effect.
  if (!requestedCamera && consumedCamera) setConsumedCamera(undefined);
  if (restored && me.data && cameras.data && requestedCamera && consumedCamera !== requestedCamera) {
    setConsumedCamera(requestedCamera);
    const allowed = cameras.data.find((camera) => camera.id === requestedCamera);
    if (allowed && can(me.data, "live.view")) {
      const existing = tiles.findIndex((tile) => tile?.camera_id === requestedCamera);
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
    setTiles((t) => resizeTiles(t, nextColumns, nextRows));
    setSelected((s) => Math.min(s, nextColumns * nextRows - 1));
    setFocus(null);
  };

  // index defaults to the currently selected tile (click-to-place); dragging a camera from the
  // list onto a specific slot passes that slot's index explicitly instead.
  const place = (cameraId: string, index: number = selected) => {
    setTiles((t) => {
      const defaultQuality = cameras.data?.find((c) => c.id === cameraId)?.default_live_quality ?? "sub";
      const quality = columns === 1 ? "main" : defaultQuality;
      // With persistent players a camera is never shown twice: placing one that is already on
      // the grid moves it here (swapping cells), keeping its running session.
      const next = persistentPlayers ? placeCameraUnique(t, index, cameraId, quality) : placeCameraAt(t, index, cameraId, quality);
      // After a click (not a drag), move the selection to the next empty tile.
      const empty = next.findIndex((x, i) => x === null && i !== index);
      if (empty >= 0) setSelected(empty);
      return next;
    });
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
    else reorder(resolution.from, resolution.to);
  };

  const loadView = (id: string) => {
    setViewId(id);
    const v = views.data?.find((x) => x.id === id);
    if (!v) return;
    const n = v.layout.columns;
    const nextRows = v.layout.cells.length ? Math.ceil(v.layout.cells.length / n) : n;
    setColumns(n);
    setRows(nextRows);
    const next: Tile[] = v.layout.cells.map((c) => (c.camera_id ? { camera_id: c.camera_id, quality: c.quality ?? "sub" } : null));
    setTiles(resizeTiles(next, n, nextRows));
    setSaveName(v.name);
    setShared(v.shared);
    setFocus(null);
  };

  const current = views.data?.find((v) => v.id === viewId);
  const privateViews = views.data?.filter((v) => !v.shared) ?? [];
  const sharedViews = views.data?.filter((v) => v.shared) ?? [];
  const body = (): Schemas["ViewInput"] => ({
    name: saveName.trim(),
    shared,
    tenant_id: me.data?.tenant_id ?? cameras.data?.find((c) => tiles.some((t) => t?.camera_id === c.id))?.tenant_id,
    layout: { columns, cells: tiles.map((t) => (t ? { camera_id: t.camera_id, quality: t.quality } : { quality: "sub" })) },
  });
  const save = useMutation({
    mutationFn: async (asNew: boolean) =>
      asNew || !current?.editable
        ? unwrap(await api.POST("/api/v1/views", { body: body() }))
        : unwrap(await api.PUT("/api/v1/views/{viewId}", { params: { path: { viewId: current.id } }, body: body() })),
    onSuccess: async (v) => {
      await qc.invalidateQueries({ queryKey: ["views"] });
      setViewId(v.id);
    },
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

  // Context menu "Agregar a vista > Grilla actual": fills empty cells and, when full, grows the grid to the next layout.
  const addToGrid = (ids: string[]) => {
    const visible = ids.filter((id) => camById.has(id));
    const present = new Set(tiles.flatMap((t) => (t ? [t.camera_id] : [])));
    const fresh = visible.filter((id) => !present.has(id));
    let cols = columns;
    let base = tiles;
    const free = tiles.filter((t) => t === null).length;
    if (fresh.length > free) {
      const next = growLayout(layouts, tiles.length, tiles.length - free + fresh.length);
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
          body: { name: view.name, shared: view.shared, tenant_id: view.tenant_id, layout: { columns: n, cells: res.tiles.map((t) => (t ? { camera_id: t.camera_id, quality: t.quality } : { quality: "sub" as const })) } },
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
  const gridCameraIds = useMemo(() => [...new Set(tiles.flatMap((t) => (t && camById.has(t.camera_id) ? [t.camera_id] : [])))], [tiles, camById]);
  const recData = useRecData(gridCameraIds, transport.day, rec);
  const denied = useMemo(() => new Set(recData.denied), [recData.denied]);
  const focusedCameraId = focus !== null ? tiles[focus]?.camera_id : undefined;
  const { players: recPlayers, limited: recLimited } = useMemo(
    () => assignRecPlayers(focusedCameraId ? [focusedCameraId] : gridCameraIds, (id) => !denied.has(id)),
    [focusedCameraId, gridCameraIds, denied],
  );
  const hasCoverage = (id: string) => (recData.spans[id]?.length ?? 0) > 0;
  const syncIds = useMemo(() => recPlayers.filter((id) => !recData.loaded.includes(id) || (recData.spans[id]?.length ?? 0) > 0), [recPlayers, recData.loaded, recData.spans]);
  const masterId = pickMaster(syncIds, tiles[selected]?.camera_id, hasCoverage);
  useSyncedPlayback(rec ? masterId : "", syncIds, (id) => transport.players.current.get(id)?.video);
  const timelineCameras = useMemo(
    () => gridCameraIds.filter((id) => !denied.has(id)).map((id) => ({ id, name: camById.get(id)?.display_name ?? id, spans: recData.spans[id] ?? [], live: camById.get(id)?.status === "online" })),
    [gridCameraIds, denied, camById, recData.spans],
  );

  // Entering REC via the toggle starts at now - 30 s, or at the latest recording when the cameras stopped earlier.
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
      : undefined;
  const modeToggle = <LiveModeToggle rec={rec} onChange={setMode} />;
  const topBarActions = canRec && topBar.available && topBar.target ? createPortal(modeToggle, topBar.target) : null;
  const renderViews = (query: string, bindViewMenu: (build: () => MenuItem[]) => object) => {
    const match = (v: Schemas["View"]) => !query || v.name.toLowerCase().includes(query);
    const groups = [
      { label: "Privadas", items: privateViews.filter(match) },
      { label: "Compartidas", items: sharedViews.filter(match) },
    ];
    const viewButton = "flex w-full min-w-0 items-center gap-1 rounded px-1.5 py-0.5 text-left text-sm hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent";
    return (
      <div className="flex flex-col gap-2">
        <ul aria-label="Vistas" className="flex flex-col">
          <li>
            <button type="button" aria-current={viewId === "" ? "true" : undefined} onClick={() => setViewId("")} className={cn(viewButton, viewId === "" && "bg-raised font-medium")}>
              Vista sin guardar
            </button>
          </li>
          {groups.map(
            (group) =>
              group.items.length > 0 && (
                <li key={group.label} aria-label={group.label}>
                  <p className="px-1.5 pt-1 text-[11px] font-semibold uppercase tracking-wide text-muted">{group.label}</p>
                  <ul className="flex flex-col">
                    {group.items.map((v) => (
                      <li key={v.id}>
                        <button
                          type="button"
                          aria-current={viewId === v.id ? "true" : undefined}
                          onClick={() => loadView(v.id)}
                          className={cn(viewButton, viewId === v.id && "bg-raised font-medium")}
                          {...bindViewMenu(() => [
                            { id: "open", label: "Abrir", icon: faFolderOpen, onSelect: () => loadView(v.id) },
                            {
                              id: "edit",
                              label: "Editar",
                              icon: faPen,
                              disabled: !v.editable,
                              hint: v.editable ? undefined : "Solo lectura",
                              // Edit = load the view and focus its name so the existing Guardar flow applies.
                              onSelect: () => {
                                loadView(v.id);
                                setTimeout(() => document.querySelector<HTMLInputElement>('input[aria-label="Nombre de la vista"]')?.focus(), 0);
                              },
                            },
                            ...(v.editable ? [{ id: "delete", label: "Eliminar", icon: faTrash, danger: true, onSelect: () => setDeleteViewId(v.id) } satisfies MenuItem] : []),
                          ])}
                        >
                          <span className="truncate">{v.shared && v.owner_name ? `${v.name} · ${v.owner_name}` : v.name}</span>
                        </button>
                      </li>
                    ))}
                  </ul>
                </li>
              ),
          )}
        </ul>
        <p role="status" aria-label="Vista activa" className="px-1 text-xs text-muted">
          {current
            ? `${current.name} · ${current.shared ? (current.owner_name ? `Compartida por ${current.owner_name}` : "Compartida") : "Privada"}${current.editable ? "" : " · Solo lectura: guardar crea una copia propia"}`
            : "Vista sin guardar"}
        </p>
        <TextInput aria-label="Nombre de la vista" placeholder="Nombre de la vista" value={saveName} onChange={(e) => setSaveName(e.target.value)} />
        {can(me.data, "views.create_shared") && (
          <label className="flex items-center gap-2 text-xs">
            <input type="checkbox" checked={shared} onChange={(e) => setShared(e.target.checked)} /> Compartida con mi organización
          </label>
        )}
        <div className="flex flex-wrap gap-1.5">
          {/* Creating a view needs views.create_private (or views.create_shared once
              Compartida is checked); editing an existing view the caller owns or can
              manage does not, so the button stays available for that case regardless. */}
          {(current?.editable || can(me.data, "views.create_private") || can(me.data, "views.create_shared")) && (
            <Button onClick={() => save.mutate(false)} disabled={!saveName.trim() || save.isPending}>
              <Save className="size-4" aria-hidden /> {current?.editable ? "Guardar" : "Guardar vista actual"}
            </Button>
          )}
          {current?.editable && (
            <>
              <Button onClick={() => save.mutate(true)} disabled={!saveName.trim() || save.isPending}>
                Guardar como nueva
              </Button>
              <Button onClick={() => remove.mutate(current.id)} aria-label="Borrar vista" title="Borrar vista">
                <Trash2 className="size-4" aria-hidden />
              </Button>
            </>
          )}
        </div>
        <ErrorNote error={save.error ?? remove.error} />
      </div>
    );
  };
  const sidebarContent = (
    <LiveExplorer
      cameras={cameras.data ?? []}
      sites={sites.data ?? []}
      servers={servers.data ?? []}
      folderApi={folderApi}
      onPick={place}
      onPlayback={setPlaybackCameraId}
      canViewRecordings={can(me.data, "recordings.view")}
      onCollapse={() => setSidebarCollapsed(true)}
      renderViews={renderViews}
      viewCount={views.data?.length ?? 0}
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
    <div className="flex flex-col gap-2 md:h-full md:min-h-0">
      <h1 className="sr-only">En vivo</h1>
      {topBarActions}
      <DndContext sensors={sensors} onDragStart={(e) => setActiveDrag(String(e.active.id))} onDragCancel={() => setActiveDrag(null)} onDragEnd={handleDragEnd}>
        <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
          <section className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
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
              {layouts.map(({ columns: layoutColumns, rows: layoutRows }) => (
                <button
                  key={`${layoutColumns}x${layoutRows}`}
                  type="button"
                  onClick={() => setGrid(layoutColumns, layoutRows)}
                  aria-pressed={layoutColumns === columns && layoutRows === rows && focus === null}
                  aria-label={`Layout ${layoutColumns} by ${layoutRows}`}
                  className={cn(
                    "rounded border border-line px-2 py-1 font-mono text-xs",
                    layoutColumns === columns && layoutRows === rows && focus === null ? "bg-accent text-bg" : "bg-surface hover:bg-raised",
                  )}
                >
                  {layoutColumns}×{layoutRows}
                </button>
              ))}
              <span className="ml-2 hidden truncate text-xs text-muted 2xl:inline">Elegí un cuadro y después una cámara del árbol, o arrastrala.</span>
              {canRec && !topBar.available && <div className="ml-auto">{modeToggle}</div>}
            </div>
            <SortableContext items={shown.map((i) => tileDragId(i))} strategy={persistentPlayers ? rectSwappingStrategy : rectSortingStrategy}>
              <div role="group" aria-label="Grilla de video" data-mode={rec ? "rec" : "live"}
                className={cn(
                  "grid gap-1 rounded-md ring-2 md:min-h-0 md:flex-1 md:[grid-template-rows:repeat(var(--grid-rows),minmax(0,1fr))]",
                  rec ? "ring-bad/50" : "ring-ok/40",
                )}
                style={{ gridTemplateColumns: `repeat(${gridCols}, minmax(0, 1fr))`, "--grid-rows": focus !== null ? 1 : rows } as CSSProperties}
              >
                {shown.map((i) => {
                  const t = tiles[i] ?? null;
                  const cam = t ? camById.get(t.camera_id) : undefined;
                  const isHidden = focus !== null && focus !== i;
                  let recLayer: ReactNode = null;
                  if (rec && t && cam && !isHidden) {
                    const id = t.camera_id;
                    const state: RecTileState = denied.has(id)
                      ? "denied"
                      : tiles.findIndex((x) => x?.camera_id === id) !== i
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
                      key={persistentPlayers ? (t && !duplicates.has(i) ? `camera:${t.camera_id}` : `cell:${i}`) : i}
                      index={i}
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
                      quality={persistentPlayers ? (t?.quality ?? "sub") : focus === i || columns === 1 ? "main" : (t?.quality ?? "sub")}
                      onSelect={() => setSelected(i)}
                      onToggleFocus={() => t && setFocus(focus === null ? i : null)}
                      onRemove={() => {
                        setTiles((x) => x.map((v, j) => (j === i ? null : v)));
                        setFocus(null);
                      }}
                    />
                  );
                })}
              </div>
            </SortableContext>
            {rec && <LiveRecDock
                transport={transport}
                cameras={timelineCameras}
                events={recData.events}
                now={now}
                selectedId={tiles[selected]?.camera_id}
                onSelectCamera={(id) => {
                  const idx = tiles.findIndex((t) => t?.camera_id === id);
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

function GridTile({
  index,
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
  const style = { transform: CSS.Transform.toString(transform), transition };
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
      {tile && camera ? (
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
