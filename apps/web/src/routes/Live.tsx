import { DndContext, type DragEndEvent, KeyboardSensor, PointerSensor, useDraggable, useSensor, useSensors } from "@dnd-kit/core";
import { rectSortingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createPortal } from "react-dom";
import { Link } from "@tanstack/react-router";
import { ArrowUpRight, Camera, CircleCheck, CircleHelp, CircleX, ChevronDown, ChevronRight, History, Maximize2, Minimize2, Save, Trash2, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { camerasQuery, meQuery, serversQuery, sitesQuery, viewsQuery } from "@/api/queries";
import { MsePlayer } from "@/components/MsePlayer";
import { LivePlaybackPanel } from "@/components/LivePlaybackPanel";
import { useContextSidebarPortalTarget } from "@/components/AppShell";
import { Button, ErrorNote, PageHeader, Select, StatusBadge, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import {
  cameraDragId, liveSelectionKey, parseSelection, placeCameraAt, resizeTiles, resolveDragEnd, reorderTiles, serializeSelection, tileDragId,
  type Tile,
} from "@/lib/liveGrid";
import { can } from "@/lib/perm";

const GRID_LAYOUTS = [
  { columns: 1, rows: 1 }, { columns: 2, rows: 1 }, { columns: 2, rows: 2 },
  { columns: 3, rows: 2 }, { columns: 3, rows: 3 }, { columns: 4, rows: 3 }, { columns: 4, rows: 4 },
];

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

  const camById = useMemo(() => new Map(cameras.data?.map((c) => [c.id, c])), [cameras.data]);

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
      const next = placeCameraAt(t, index, cameraId, columns === 1 ? "main" : "sub");
      // After a click (not a drag), move the selection to the next empty tile.
      const empty = next.findIndex((x, i) => x === null && i !== index);
      if (empty >= 0) setSelected(empty);
      return next;
    });
  };

  const reorder = (from: number, to: number) => setTiles((t) => reorderTiles(t, from, to));

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

  const shown = focus !== null ? [focus] : tiles.map((_, i) => i);
  const gridCols = focus !== null ? 1 : columns;
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
    <div className="flex flex-col gap-4">
      <PageHeader
        title="En vivo"
        description="Cámaras de cualquier servidor Frigate en una misma grilla. Arrastrá una cámara a un cuadro, o arrastrá cuadros entre sí para reordenarlos. Doble clic en un cuadro para ampliarlo."
      />
      <DndContext sensors={sensors} onDragEnd={handleDragEnd}>
        <div className="flex min-w-0 flex-col gap-2">
          <section className="flex min-w-0 flex-1 flex-col gap-2">
            <div role="group" aria-label="Layout de la grilla" className="flex items-center gap-1">
              {GRID_LAYOUTS.map(({ columns: layoutColumns, rows: layoutRows }) => (
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
              <span className="ml-2 text-xs text-muted">Elegí un cuadro y después una cámara del árbol, o arrastrala.</span>
            </div>
            <SortableContext items={shown.map((i) => tileDragId(i))} strategy={rectSortingStrategy}>
              <div role="group" aria-label="Grilla de video" className="grid gap-1" style={{ gridTemplateColumns: `repeat(${gridCols}, minmax(0, 1fr))` }}>
                {shown.map((i) => {
                  const t = tiles[i] ?? null;
                  const cam = t ? camById.get(t.camera_id) : undefined;
                  return (
                    <GridTile
                      key={i}
                      index={i}
                      tile={t}
                      camera={cam}
                      isSelected={selected === i}
                      isFocused={focus === i}
                      canViewRecordings={can(me.data, "recordings.view")}
                      status={cam?.status}
                      quality={focus === i || columns === 1 ? "main" : (t?.quality ?? "sub")}
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
  canViewRecordings: boolean;
  status?: string;
  quality: "sub" | "main";
  onSelect: () => void;
  onToggleFocus: () => void;
  onRemove: () => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: tileDragId(index) });
  const style = { transform: CSS.Transform.toString(transform), transition };
  return (
    <div
      ref={setNodeRef}
      style={style}
      aria-label={`Cuadro ${index + 1}`}
      onClick={onSelect}
      onDoubleClick={onToggleFocus}
      className={cn(
        "group relative aspect-video overflow-hidden rounded border bg-black outline-none focus-visible:ring-2 focus-visible:ring-accent",
        isSelected ? "border-accent" : "border-line",
        isDragging && "opacity-50",
      )}
      {...attributes}
      {...listeners}
    >
      {tile && camera ? (
        <>
          <MsePlayer cameraId={tile.camera_id} quality={quality} className="size-full" />
          <div className="absolute inset-x-0 top-0 flex items-center gap-1.5 bg-gradient-to-b from-black/80 via-black/45 to-transparent px-2 py-1.5 text-xs text-white">
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
      <nav aria-label="Cámaras" className="flex max-h-[60vh] flex-col overflow-y-auto text-sm">
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
