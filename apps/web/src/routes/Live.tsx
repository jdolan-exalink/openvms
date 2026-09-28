import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, History, Maximize2, Minimize2, Save, Trash2, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { camerasQuery, meQuery, serversQuery, sitesQuery, viewsQuery } from "@/api/queries";
import { MsePlayer } from "@/components/MsePlayer";
import { Button, ErrorNote, PageHeader, Select, StatusBadge, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import { liveSelectionKey, parseSelection, placeCameraAt, resizeTiles, serializeSelection, type Tile } from "@/lib/liveGrid";
import { can } from "@/lib/perm";

const GRIDS = [1, 2, 3, 4];

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
  const [tiles, setTiles] = useState<Tile[]>(() => Array(4).fill(null));
  const [selected, setSelected] = useState(0);
  const [focus, setFocus] = useState<number | null>(null);
  const [viewId, setViewId] = useState("");
  const [saveName, setSaveName] = useState("");
  const [shared, setShared] = useState(false);
  // Set once the saved grid selection (or the default) has been applied, so the persistence
  // effect below never fires before restoration and overwrites a saved selection with defaults.
  const [restored, setRestored] = useState(false);

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
      localStorage.setItem(liveSelectionKey(me.data.tenant_id, me.data.id), serializeSelection(columns, tiles));
    } catch {
      // Storage unavailable (private mode, quota): the grid still works for this session.
    }
  }, [restored, me.data, columns, tiles]);

  const setGrid = (n: number) => {
    setColumns(n);
    setTiles((t) => resizeTiles(t, n));
    setSelected((s) => Math.min(s, n * n - 1));
    setFocus(null);
  };

  const place = (cameraId: string) => {
    setTiles((t) => {
      // Fill the selected tile, then move the selection to the next empty one.
      const next = placeCameraAt(t, selected, cameraId, columns === 1 ? "main" : "sub");
      const empty = next.findIndex((x, i) => x === null && i !== selected);
      if (empty >= 0) setSelected(empty);
      return next;
    });
  };

  const loadView = (id: string) => {
    setViewId(id);
    const v = views.data?.find((x) => x.id === id);
    if (!v) return;
    const n = v.layout.columns;
    setColumns(n);
    const next: Tile[] = v.layout.cells.map((c) => (c.camera_id ? { camera_id: c.camera_id, quality: c.quality ?? "sub" } : null));
    while (next.length < n * n) next.push(null);
    setTiles(next.slice(0, n * n));
    setSaveName(v.name);
    setShared(v.shared);
    setFocus(null);
  };

  const current = views.data?.find((v) => v.id === viewId);
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

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="En vivo" description="Cámaras de cualquier servidor Frigate en una misma grilla. Doble clic en un cuadro para ampliarlo." />
      <div className="flex flex-col gap-4 lg:flex-row">
        <aside className="flex w-full shrink-0 flex-col gap-3 lg:w-64">
          <div className="flex flex-col gap-2 rounded border border-line bg-surface p-3">
            <Select aria-label="Vista guardada" value={viewId} onChange={(e) => (e.target.value ? loadView(e.target.value) : setViewId(""))}>
              <option value="">Vista sin guardar</option>
              {views.data?.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.name}
                  {v.shared ? " (compartida)" : ""}
                </option>
              ))}
            </Select>
            <TextInput aria-label="Nombre de la vista" placeholder="Nombre de la vista" value={saveName} onChange={(e) => setSaveName(e.target.value)} />
            {can(me.data, "views.create_shared") && (
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={shared} onChange={(e) => setShared(e.target.checked)} /> Compartida con mi organización
              </label>
            )}
            <div className="flex flex-wrap gap-2">
              <Button onClick={() => save.mutate(false)} disabled={!saveName.trim() || save.isPending}>
                <Save className="size-4" aria-hidden /> {current?.editable ? "Guardar" : "Guardar vista"}
              </Button>
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
          <CameraTree
            cameras={cameras.data ?? []}
            sites={sites.data ?? []}
            servers={servers.data ?? []}
            onPick={place}
          />
          <ErrorNote error={cameras.error} />
        </aside>

        <section className="flex min-w-0 flex-1 flex-col gap-2">
          <div className="flex items-center gap-1">
            {GRIDS.map((n) => (
              <button
                key={n}
                type="button"
                onClick={() => setGrid(n)}
                className={cn(
                  "rounded border border-line px-2 py-1 font-mono text-xs",
                  n === columns && focus === null ? "bg-accent text-bg" : "bg-surface hover:bg-raised",
                )}
              >
                {n}×{n}
              </button>
            ))}
            <span className="ml-2 text-xs text-muted">Elegí un cuadro y después una cámara del árbol.</span>
          </div>
          <div className="grid gap-1" style={{ gridTemplateColumns: `repeat(${gridCols}, minmax(0, 1fr))` }}>
            {shown.map((i) => {
              const t = tiles[i];
              const cam = t ? camById.get(t.camera_id) : undefined;
              return (
                <div
                  key={i}
                  onClick={() => setSelected(i)}
                  onDoubleClick={() => t && setFocus(focus === null ? i : null)}
                  className={cn(
                    "group relative aspect-video overflow-hidden rounded border bg-black",
                    selected === i ? "border-accent" : "border-line",
                  )}
                >
                  {t && cam ? (
                    <>
                      <MsePlayer cameraId={t.camera_id} quality={focus === i || columns === 1 ? "main" : t.quality} className="size-full" />
                      <div className="absolute inset-x-0 top-0 flex items-center gap-2 bg-gradient-to-b from-black/70 to-transparent px-2 py-1 text-xs text-white">
                        <span className="truncate font-medium">{cam.display_name}</span>
                        <span className="ml-auto flex gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                          {can(me.data, "recordings.view") && (
                            <Link to="/playback" search={{ camera: t.camera_id }} title="Grabaciones" className="rounded p-0.5 hover:bg-white/20">
                              <History className="size-3.5" aria-hidden />
                            </Link>
                          )}
                          <button type="button" title={focus === i ? "Volver a la grilla" : "Ampliar"} className="rounded p-0.5 hover:bg-white/20" onClick={() => setFocus(focus === null ? i : null)}>
                            {focus === i ? <Minimize2 className="size-3.5" aria-hidden /> : <Maximize2 className="size-3.5" aria-hidden />}
                          </button>
                          <button
                            type="button"
                            title="Quitar"
                            className="rounded p-0.5 hover:bg-white/20"
                            onClick={(e) => {
                              e.stopPropagation();
                              setTiles((x) => x.map((v, j) => (j === i ? null : v)));
                              setFocus(null);
                            }}
                          >
                            <X className="size-3.5" aria-hidden />
                          </button>
                        </span>
                      </div>
                    </>
                  ) : (
                    <div className="flex size-full items-center justify-center text-xs text-muted">
                      {t && !cam ? "Cámara sin acceso" : "Vacío"}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </section>
      </div>
    </div>
  );
}

function CameraTree({
  cameras,
  sites,
  servers,
  onPick,
}: {
  cameras: Schemas["Camera"][];
  sites: Schemas["Site"][];
  servers: Schemas["Server"][];
  onPick: (id: string) => void;
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
    <div className="flex flex-col gap-2 rounded border border-line bg-surface p-3">
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
                      <button
                        key={c.id}
                        type="button"
                        onClick={() => onPick(c.id)}
                        className="ml-4 flex w-[calc(100%-1rem)] items-center gap-2 rounded px-1.5 py-0.5 text-left hover:bg-raised"
                      >
                        <span className={cn("size-1.5 shrink-0 rounded-full", c.status === "online" ? "bg-ok" : c.status === "offline" ? "bg-bad" : "bg-muted")} />
                        <span className="truncate">{c.display_name}</span>
                      </button>
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
