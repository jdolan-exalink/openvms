import { DndContext, type DragEndEvent, KeyboardSensor, PointerSensor, useDraggable, useSensor, useSensors } from "@dnd-kit/core";
import { rectSortingStrategy, rectSwappingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createPortal } from "react-dom";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { ArrowUpRight, Camera, CircleCheck, CircleHelp, CircleX, ChevronDown, ChevronRight, History, Maximize2, Minimize2, Save, Trash2, X } from "lucide-react";
import { type CSSProperties, type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { camerasQuery, meQuery, serversQuery, sitesQuery, viewsQuery } from "@/api/queries";
import { MsePlayer } from "@/components/MsePlayer";
import { useFeatures } from "@/lib/features";
import { LivePlaybackPanel } from "@/components/LivePlaybackPanel";
import { LiveRecDock } from "@/components/LiveRecDock";
import { RecTile, type RecTileState } from "@/components/RecTile";
import { LiveModeToggle } from "@/components/LiveModeToggle";
import { useContextSidebarPortalTarget, useTopBarActionsPortalTarget } from "@/components/AppShell";
import { Button, ErrorNote, Select, StatusBadge, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import {
  cameraDragId, duplicateTileIndexes, liveSelectionKey, parseSelection, placeCameraAt, placeCameraUnique, resizeTiles, resolveDragEnd, reorderTiles,
  serializeSelection, swapTiles, tileDragId,
  type Tile,
} from "@/lib/liveGrid";
import { assignRecPlayers, parseRecSearch, pickMaster, REC_ENTRY_OFFSET_S, recSearch } from "@/lib/liveRec";
import { can } from "@/lib/perm";
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
    const resolution = resolveDragEnd(event.active.id, event.over?.id ?? null);
    if (!resolution) return;
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
    () => gridCameraIds.filter((id) => !denied.has(id)).map((id) => ({ id, name: camById.get(id)?.display_name ?? id, spans: recData.spans[id] ?? [] })),
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
  const modeToggle = <LiveModeToggle rec={rec} onChange={setMode} />;
  const topBarActions = canRec && topBar.available && topBar.target ? createPortal(modeToggle, topBar.target) : null;
  const sidebarContent = (
    <div className="flex min-h-0 flex-col gap-3" data-live-sidebar="true">
      <section aria-label="Vistas guardadas" className="flex flex-col gap-2 rounded-xl border border-line bg-bg p-2.5">
        <h2 className="px-1 text-xs font-semibold uppercase tracking-wide text-muted">Vista</h2>
        <Select aria-label="Vista guardada" value={viewId} onChange={(e) => (e.target.value ? loadView(e.target.value) : setViewId(""))}>
          <option value="">Vista sin guardar</option>
          {[
            { label: "Privadas", items: privateViews },
            { label: "Compartidas", items: sharedViews },
          ].map(
            (group) =>
              group.items.length > 0 && (
                <optgroup key={group.label} label={group.label}>
                  {group.items.map((v) => (
                    <option key={v.id} value={v.id}>
                      {v.shared && v.owner_name ? `${v.name} · ${v.owner_name}` : v.name}
                    </option>
                  ))}
                </optgroup>
              ),
          )}
        </Select>
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
              <Save className="size-4" aria-hidden /> {current?.editable ? "Guardar" : "Guardar vista"}
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
      </section>
      <div className="flex min-h-0 flex-col gap-2 rounded-xl border border-line bg-bg p-2.5">
        <h2 className="px-1 text-xs font-semibold uppercase tracking-wide text-muted">Cámaras</h2>
        <CameraTree
          cameras={cameras.data ?? []}
          sites={sites.data ?? []}
          servers={servers.data ?? []}
          onPick={place}
          onPlayback={setPlaybackCameraId}
          canViewRecordings={can(me.data, "recordings.view")}
        />
        <ErrorNote error={cameras.error} />
      </div>
    </div>
  );
  const sidebar = sidebarMount.available
    ? sidebarMount.target ? createPortal(sidebarContent, sidebarMount.target) : null
    : <aside className="flex w-full shrink-0 flex-col gap-3 lg:w-64">{sidebarContent}</aside>;

  return (
    <div className="flex flex-col gap-2 md:h-full md:min-h-0">
      <h1 className="sr-only">En vivo</h1>
      {topBarActions}
      <DndContext sensors={sensors} onDragEnd={handleDragEnd}>
        <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
          <section className="flex min-h-0 min-w-0 flex-1 flex-col gap-2">
            <div role="group" aria-label="Layout de la grilla" className="flex shrink-0 flex-wrap items-center gap-1">
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
                  rec ? "ring-warn/50" : "ring-bad/40",
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
            {rec && <LiveRecDock transport={transport} cameras={timelineCameras} events={recData.events} now={now} />}
          </section>
        </div>
        {sidebar}
      </DndContext>
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

function CameraTree({
  cameras,
  sites,
  servers,
  onPick,
  onPlayback,
  canViewRecordings,
}: {
  cameras: Schemas["Camera"][];
  sites: Schemas["Site"][];
  servers: Schemas["Server"][];
  onPick: (id: string) => void;
  onPlayback: (id: string) => void;
  canViewRecordings: boolean;
}) {
  const [q, setQ] = useState("");
  const [closed, setClosed] = useState<Record<string, boolean>>({});
  const filtered = cameras.filter((c) => c.enabled && (!q || c.display_name.toLowerCase().includes(q.toLowerCase())));
  const bySite = new Map<string, Map<string, Schemas["Camera"][]>>();
  for (const c of filtered) {
    const s = bySite.get(c.site_id) ?? new Map<string, Schemas["Camera"][]>();
    s.set(c.server_id, [...(s.get(c.server_id) ?? []), c]);
    bySite.set(c.site_id, s);
  }
  const siteName = new Map(sites.map((s) => [s.id, s.name]));
  const server = new Map(servers.map((s) => [s.id, s]));
  const toggle = (k: string) => setClosed((c) => ({ ...c, [k]: !c[k] }));

  return (
    <div className="flex flex-col gap-2">
      <TextInput aria-label="Buscar cámara" placeholder="Buscar cámara" value={q} onChange={(e) => setQ(e.target.value)} />
      <nav aria-label="Cámaras" className="flex max-h-[60vh] flex-col md:max-h-none overflow-y-auto text-sm">
        {[...bySite.entries()].map(([siteId, srvs]) => (
          <div key={siteId}>
            <button type="button" onClick={() => toggle(siteId)} className="flex w-full items-center gap-1 py-1 font-medium">
              {closed[siteId] ? <ChevronRight className="size-3.5" /> : <ChevronDown className="size-3.5" />}
              {siteName.get(siteId) ?? "Sitio"}
            </button>
            {!closed[siteId] &&
              [...srvs.entries()].map(([srvId, cams]) => (
                <div key={srvId} className="ml-3">
                  <button type="button" onClick={() => toggle(srvId)} className="flex w-full items-center gap-1 py-0.5 text-muted">
                    {closed[srvId] ? <ChevronRight className="size-3.5" /> : <ChevronDown className="size-3.5" />}
                    <span className="truncate">{server.get(srvId)?.name ?? "Servidor"}</span>
                    {server.get(srvId) && <span className="ml-auto"><StatusBadge status={server.get(srvId)!.status} /></span>}
                  </button>
                  {!closed[srvId] &&
                    cams.map((c) => (
                      <DraggableCamera key={c.id} camera={c} onPick={onPick} onPlayback={onPlayback} canViewRecordings={canViewRecordings} />
                    ))}
                </div>
              ))}
          </div>
        ))}
        {filtered.length === 0 && <p className="py-2 text-xs text-muted">No hay cámaras visibles.</p>}
      </nav>
    </div>
  );
}

/** DraggableCamera keeps the existing click-to-place behavior and adds drag-to-place. */
function DraggableCamera({
  camera,
  onPick,
  onPlayback,
  canViewRecordings,
}: {
  camera: Schemas["Camera"];
  onPick: (id: string) => void;
  onPlayback: (id: string) => void;
  canViewRecordings: boolean;
}) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({ id: cameraDragId(camera.id) });
  return (
    <div className="ml-4 flex min-w-0 items-center gap-1">
      <button
        ref={setNodeRef}
        type="button"
        onClick={() => onPick(camera.id)}
        className={cn("flex min-w-0 flex-1 items-center gap-2 rounded px-1.5 py-0.5 text-left hover:bg-raised", isDragging && "opacity-50")}
        {...attributes}
        {...listeners}
      >
        <span className={cn("size-1.5 shrink-0 rounded-full", camera.status === "online" ? "bg-ok" : camera.status === "offline" ? "bg-bad" : "bg-muted")} />
        <span className="truncate">{camera.display_name}</span>
      </button>
      {canViewRecordings && (
        <>
          <button
            type="button"
            title="Ver grabaciones en vivo"
            aria-label={`Ver grabaciones de ${camera.display_name}`}
            onClick={() => onPlayback(camera.id)}
            className="shrink-0 rounded p-1 text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
          >
            <History className="size-3.5" aria-hidden />
          </button>
          <Link
            to="/playback"
            search={{ camera: camera.id }}
            title="Abrir página de grabaciones"
            aria-label={`Grabaciones de ${camera.display_name}`}
            className="shrink-0 rounded p-1 text-muted hover:bg-raised hover:text-ink focus-visible:outline-2 focus-visible:outline-accent"
          >
            <ArrowUpRight className="size-3.5" aria-hidden />
          </Link>
        </>
      )}
    </div>
  );
}
