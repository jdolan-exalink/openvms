import { useEffect, useRef, useState, type ReactNode } from "react";
import { Undo2, Redo2 } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { alarmsQuery, cameraFoldersQuery, camerasQuery, meQuery, serversQuery } from "@/api/queries";
import { can } from "@/lib/perm";
import { loadSidebarPinned, saveSidebarPinned } from "@/lib/explorer";
import { mapsOverviewQuery } from "@/lib/maps/api";
import type { CameraEntity, MapMode } from "@/lib/maps/types";
import { floorEntitiesQuery, floorUnplacedQuery, emptyFloorDraft, stageFloor, unstageFloor, undoFloor, redoFloor, saveFloorPlacement, unplaceFloorCamera, type Point } from "@/lib/maps/floorEditor";
import { loadPlanView, savePlanView } from "@/lib/maps/mapView";
import { loadFloorPlan } from "@/lib/maps/plans";
import type { WorkspaceFloor } from "./MapWorkspace";
import { FloorPlanCanvas, type FloorPlanCanvasHandle } from "./canvas/FloorPlanCanvas";
import { MapEditSidebar } from "./editor/MapEditSidebar";
import { PlanUpload } from "./editor/PlanUpload";
import { MapSocSidebar } from "./panel/MapSocSidebar";
import { MapOperationsPanel } from "./panel/MapOperationsPanel";
import { AlarmPanel } from "./panel/AlarmPanel";
import { MapPlateSnapshot, type PlateSnapshotTarget } from "./panel/MapPlateSnapshot";
import { MapMaximizedCamera } from "./panel/MapMaximizedCamera";
import { CameraPanel } from "./panel/CameraPanel";
import { captureGrowOrigin, rectFromElement, type GrowRect } from "./panel/MapGrowFrame";
import { usePinnedMapWindows } from "./panel/usePinnedMapWindows";
import { useFeatures } from "@/lib/features";
import { Button, IconButton, ErrorNote } from "../ui";
interface Props {
  siteId: string;
  floor: WorkspaceFloor;
  initialMode: MapMode;
  onModeChange?: (mode: MapMode) => void;
  onDirty: (dirty: boolean) => void;
  onPlanSaved: () => void;
  onSelectCamera?: (id: string | undefined) => void;
  editorMaps?: ReactNode;
}
export function FloorMap({ siteId, floor, initialMode, onModeChange, onDirty, onPlanSaved, onSelectCamera, editorMaps }: Props) {
  const me = useQuery(meQuery);
  const { persistentPlayers } = useFeatures();
  const client = useQueryClient();
  const navigate = useNavigate();
  const entities = useQuery(floorEntitiesQuery(siteId, floor.id));
  const unplaced = useQuery(floorUnplacedQuery(siteId, floor.id));
  const plan = useQuery({
    queryKey: ["maps", "private-plan", siteId, floor.id, floor.revision], enabled: !!floor.plan_key,
    queryFn: ({ signal }) => loadFloorPlan(siteId, floor.id, signal), retry: false
  });
  const canvasRef = useRef<FloorPlanCanvasHandle>(null);
  const [planDirty, setPlanDirty] = useState(false);
  const [modeState, setModeState] = useState({ initial: initialMode, value: initialMode });
  if (modeState.initial !== initialMode)
    setModeState({ initial: initialMode, value: initialMode });
  const mode = modeState.value;
  const [draft, setDraft] = useState(emptyFloorDraft);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();
  const [planError, setPlanError] = useState<string>();
  const [selected, setSelected] = useState<string>();
  const [armed, setArmed] = useState<string>();
  const [sidebarPinned, setSidebarPinned] = useState(loadSidebarPinned);
  const toggleSidebarPin = () => setSidebarPinned((current) => {
    const next = !current;
    saveSidebarPinned(next);
    return next;
  });
  const [plateSnapshot, setPlateSnapshot] = useState<PlateSnapshotTarget>();
  const [maximizedId, setMaximizedId] = useState<string>();
  const [maximizedOrigin, setMaximizedOrigin] = useState<GrowRect>();
  const pinned = usePinnedMapWindows(me.data?.tenant_id ?? null, me.data?.id, `floor:${floor.id}`);
  const liveChrome = mode !== "edit";
  const overview = useQuery({ ...mapsOverviewQuery, enabled: liveChrome });
  const currentSite = overview.data?.find((site) => site.id === siteId);
  const inventory = useQuery({ ...camerasQuery({ site_id: siteId }), enabled: liveChrome && can(me.data, "cameras.view") });
  const folders = useQuery({ ...cameraFoldersQuery, enabled: liveChrome && can(me.data, "cameras.view") });
  const alarms = useQuery({ ...alarmsQuery({ site_id: siteId, limit: 100 }), enabled: liveChrome && can(me.data, "alarms.view") });
  const servers = useQuery({ ...serversQuery, enabled: liveChrome && can(me.data, "servers.view") });
  const treeFolders = (folders.data?.items ?? []).map((folder) => ({ id: folder.id, name: folder.name, serverId: folder.server_id, sortOrder: folder.sort_order }));
  const treeServers = (servers.data ?? []).map((server) => ({ id: server.id, name: server.name }));
  const pending = Object.entries(draft.entries);
  useEffect(() => { onDirty(pending.length > 0 || planDirty || saving); }, [pending.length, planDirty, saving, onDirty]);
  const editable = mode === "edit" && can(me.data, "maps.edit_device");
  const source = (entities.data ?? []).filter((entity): entity is CameraEntity => entity.type === "camera" && "camera" in entity);
  const cameras: CameraEntity[] = source.map(camera => { const entry = draft.entries[camera.id]; return entry ? { ...camera, position: { kind: "floor", floorId: floor.id, x: entry.x, y: entry.y } } : camera; });
  for (const camera of unplaced.data ?? []) {
    const entry = draft.entries[camera.id];
    if (!entry || cameras.some(placed => placed.id === camera.id))
      continue;
    cameras.push({
      id: camera.id, siteId, name: camera.name, type: "camera", status: camera.status === "online" ? "online" : camera.status === "offline" ? "offline" : "unknown",
      position: { kind: "floor", floorId: floor.id, x: entry.x, y: entry.y }, metadata: {}, activeAlarms: 0,
      camera: { bearingDeg: null, fovDeg: 60, rangeM: 100, cameraType: "fixed", ptz: false, lpr: false }
    });
  }
  function place(id: string, point: Point) {
    if (!editable || saving || !entities.isSuccess || !unplaced.isSuccess)
      return;
    const entity = source.find(camera => camera.id === id);
    if (!entity && !unplaced.data?.some(camera => camera.id === id))
      return;
    onDirty(true);
    setDraft(previous => stageFloor(previous, id, point, entity?.revision));
    setArmed(undefined);
    setError(undefined);
  }
  async function unplaceCamera(id: string) {
    if (!editable || saving) return;
    setError(undefined);
    if (draft.entries[id]) {
      setDraft(previous => unstageFloor(previous, id));
    }
    const saved = source.find(camera => camera.id === id);
    if (saved) {
      try {
        await unplaceFloorCamera(id, saved.revision);
        await Promise.all([
          client.invalidateQueries({ queryKey: floorEntitiesQuery(siteId, floor.id).queryKey }),
          client.invalidateQueries({ queryKey: floorUnplacedQuery(siteId, floor.id).queryKey }),
        ]);
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    }
  }
  async function save() {
    if (!editable || saving)
      return;
    setSaving(true);
    setError(undefined);
    const saved: string[] = [];
    const failures: string[] = [];
    for (const [id, entry] of pending) {
      try {
        await saveFloorPlacement(siteId, floor.id, id, entry, source.find(camera => camera.id === id)?.camera);
        saved.push(id);
      }
      catch (cause) {
        failures.push(`${id}: ${cause instanceof Error ? cause.message : String(cause)}`);
      }
    }
    await Promise.all([client.invalidateQueries({ queryKey: floorEntitiesQuery(siteId, floor.id).queryKey }), client.invalidateQueries({ queryKey: floorUnplacedQuery(siteId, floor.id).queryKey })]);
    setDraft(previous => ({ entries: Object.fromEntries(Object.entries(previous.entries).filter(([id]) => !saved.includes(id))), past: [], future: [] }));
    setSaving(false);
    if (failures.length)
      setError(failures.join("; "));
  }
  const selectedCamera = cameras.find(camera => camera.id === selected);
  const tray = (unplaced.data ?? []).filter(camera => !draft.entries[camera.id] && !source.some(placed => placed.id === camera.id));
  const sidebarCameras = [
    ...cameras.map((camera) => ({ id: camera.id, name: camera.name, status: camera.status, placed: true })),
    ...tray.map((camera) => ({ id: camera.id, name: camera.name, status: camera.status, placed: false })),
  ];
  const pinnedCameras = pinned.windows.flatMap((window) => {
    const camera = cameras.find((item) => item.id === window.id);
    return camera ? [camera] : [];
  });
  const maximized = cameras.find((camera) => camera.id === maximizedId);
  function selectCamera(id: string) {
    setSelected(id);
    onSelectCamera?.(id);
    const camera = cameras.find((item) => item.id === id);
    if (camera?.position.kind === "floor") canvasRef.current?.focus({ x: camera.position.x, y: camera.position.y });
    if (mode === "live" && can(me.data, "live.view")) pinned.pin(id);
  }
  function openCamera(id: string) {
    if (!can(me.data, "live.view")) return;
    setMaximizedOrigin(captureGrowOrigin(id) ?? rectFromElement(document.activeElement));
    setMaximizedId(id);
  }
  function unpinCamera(id: string) {
    pinned.unpin(id);
    if (selected === id) {
      setSelected(undefined);
      onSelectCamera?.(undefined);
    }
  }
  return <section className="relative flex h-full min-h-0 flex-1 flex-col gap-2 overflow-hidden" aria-label={`Mapa: ${floor.name}`}>
 {[{ label: "Camera placements", query: entities }, { label: "Unplaced cameras", query: unplaced }, { label: "Private background", query: plan }].map(({ label, query }) => query.isError &&
      <div key={label} role="alert" className="flex flex-wrap items-center gap-2 rounded-m3-lg bg-bad/10 px-3 py-2 text-sm text-bad">{label}: {query.error.message} <Button variant="text" size="sm" onClick={() => void query.refetch()}>Retry {label.toLowerCase()}</Button></div>)}
 <div data-map-stage className="relative min-h-0 flex-1 overflow-hidden">
  {mode === "edit" && (editorMaps || can(me.data, "maps.edit")) && (
    <div className="absolute left-3 top-16 z-30 flex max-h-[calc(100%-5rem)] w-72 flex-col gap-2 overflow-auto">
     {editorMaps && <div className="rounded-m3-xl bg-surface-1/95 p-2 shadow-2xl backdrop-blur">{editorMaps}</div>}
     {planError && <p role="alert" className="rounded-m3-lg bg-surface-1/95 p-3 text-xs text-bad shadow-2xl">{planError}</p>}
     {can(me.data, "maps.edit") && (floor.revision ? <PlanUpload siteId={siteId} floorId={floor.id} revision={floor.revision} source={plan.data} onDirty={setPlanDirty} onSaved={() => { setPlanError(undefined); onPlanSaved(); }} onConflict={() => { setPlanError("The background changed on the server. Reload and review it before uploading again. Nothing was overwritten."); onPlanSaved(); }}/> : <p role="alert" className="rounded-m3-lg bg-surface-1/95 p-3 text-xs shadow-2xl">La revisión del mapa no está disponible. Recargá la lista antes de subir un plano.</p>)}
    </div>
  )}
  {mode === "edit" && <aside aria-label="Edición del mapa" className="absolute bottom-3 right-3 top-16 z-30 flex w-76 max-w-[calc(100%-1.5rem)] min-h-0 flex-col overflow-hidden rounded-m3-xl border border-outline-variant/30 bg-surface-1/95 shadow-2xl backdrop-blur-md">
   <div className="min-h-0 flex-1 p-2"><MapEditSidebar cameras={sidebarCameras} armedId={armed} onArm={setArmed} onUnplace={unplaceCamera} /></div>
   <footer className="shrink-0 space-y-2 border-t border-outline-variant/20 bg-surface-2/80 p-2.5 text-xs">
    <div className="flex items-center justify-between font-mono text-[11px] text-on-surface-variant">
      <span>{pending.length > 0 ? `${pending.length} cambio(s) sin guardar` : "Sin cambios"}</span>
      <div className="flex gap-1">
        <IconButton
          icon={Undo2}
          size="sm"
          disabled={saving || !draft.past.length}
          aria-label="Deshacer"
          title="Deshacer"
          onClick={() => setDraft(undoFloor(draft))}
        />
        <IconButton
          icon={Redo2}
          size="sm"
          disabled={saving || !draft.future.length}
          aria-label="Rehacer"
          title="Rehacer"
          onClick={() => setDraft(redoFloor(draft))}
        />
      </div>
    </div>
    {armed && editable && (
      <Button variant="tonal" size="sm" className="w-full text-xs" onClick={() => place(armed, { x: .5, y: .5 })}>
        Ubicar seleccionada en el centro
      </Button>
    )}
    {error && <p role="alert" className="text-bad text-[11px] leading-tight">{error}</p>}
    <div className="flex gap-1.5 pt-0.5">
     <Button variant="outlined" size="sm" className="flex-1 text-xs" onClick={() => { setDraft(emptyFloorDraft()); setError(undefined); setModeState({ initial: initialMode, value: "live" }); onModeChange?.("live"); }}>Cancelar</Button>
     <Button variant="filled" size="sm" className="flex-1 text-xs" disabled={saving || !editable || pending.length === 0} onClick={() => void save()}>{pending.length > 0 ? `Guardar (${pending.length})` : "Guardar"}</Button>
    </div>
   </footer>
  </aside>}
  {liveChrome && <MapSocSidebar
    pinned={sidebarPinned}
    onTogglePin={toggleSidebarPin}
    alarmCount={alarms.data?.length ?? 0}
    showAlarms={can(me.data, "alarms.view")}
    showLpr={can(me.data, "lpr.view")}
    siteId={siteId}
    onSelectCamera={selectCamera}
    onOpenRead={setPlateSnapshot}
    cameras={<>
      <MapOperationsPanel
        mode={mode}
        sites={overview.data ?? []}
        currentSite={currentSite}
        requestedSiteId={siteId}
        cameras={cameras}
        inventory={inventory.data}
        folders={treeFolders}
        servers={treeServers}
        visibleCount={cameras.length}
        camerasVisible
        canEdit={can(me.data, "maps.edit") || can(me.data, "maps.edit_device")}
        canLive={can(me.data, "live.view")}
        canEvents={can(me.data, "events.view")}
        canPlayback={can(me.data, "recordings.view")}
        canInventory={can(me.data, "cameras.view")}
        selectedCameraId={selected}
        loading={overview.isLoading || entities.isLoading}
        errors={[
          { label: "Site overview", error: overview.error, retry: () => void overview.refetch() },
          { label: "Camera placements", error: entities.error, retry: () => void entities.refetch() },
          { label: "Camera inventory", error: inventory.error, retry: () => void inventory.refetch() },
        ]}
        onSelectSite={() => undefined}
        onSelectCamera={selectCamera}
        onEdit={() => { setModeState({ initial: initialMode, value: "edit" }); onModeChange?.("edit"); }}
        onOpenLive={(id) => { if (can(me.data, "live.view")) pinned.pin(id); }}
        onCloseLive={pinned.unpin}
        openCameraIds={pinned.windows.map((window) => window.id)}
        onEvents={(id) => void navigate({ to: "/events", search: { camera: id } })}
        onPlayback={(id) => void navigate({ to: "/playback", search: { camera: id } })}
        onResetVisibility={() => undefined}
      />
    </>}
    alarms={<>
      {alarms.isError && <ErrorNote error={alarms.error} />}
      <AlarmPanel alarms={alarms.data ?? []} canManage={can(me.data, "alarms.manage")} />
    </>}
  />}
  {plateSnapshot && <MapPlateSnapshot target={plateSnapshot} onClose={() => setPlateSnapshot(undefined)} />}
  {mode === "live" && <CameraPanel
    pinnedCameras={pinnedCameras.filter((camera) => camera.id !== maximizedId)}
    windows={pinned.windows}
    sites={overview.data}
    onUnpin={unpinCamera}
    onOpenLive={openCamera}
    onMove={pinned.move}
    onArrange={pinned.arrange}
    persistent={persistentPlayers}
    canPreview={can(me.data, "live.view")}
  />}
  {maximized && <MapMaximizedCamera camera={maximized} origin={maximizedOrigin} persistent={persistentPlayers} closeOnEscape={!plateSnapshot} onClose={() => { setMaximizedId(undefined); setMaximizedOrigin(undefined); }} />}
  {mode === "investigate" && selectedCamera && <section aria-label="Selected floor camera" className="absolute right-3 top-16 z-20 flex max-w-xs flex-col gap-2 rounded-m3-xl bg-surface-1/95 p-3 text-xs shadow-2xl backdrop-blur">
  <strong className="text-sm">{selectedCamera.name}</strong>
  <div className="flex flex-wrap gap-2">
  {can(me.data, "events.view") && <Button variant="tonal" size="sm" onClick={() => void navigate({ to: "/events", search: { camera: selectedCamera.id } })}>Events</Button>}
  {can(me.data, "recordings.view") && <Button variant="tonal" size="sm" onClick={() => void navigate({ to: "/playback", search: { camera: selectedCamera.id } })}>Playback</Button>}
  </div>
  </section>}
  <div className="absolute inset-0"><FloorPlanCanvas ref={canvasRef} key={`${siteId}/${floor.id}/${me.data?.id ?? "pending"}`} imageBlob={plan.data} width={floor.plan_width_px || 1000} height={floor.plan_height_px || 1000} cameras={cameras} editable={editable && !saving} onPlace={place} onSelect={id => { if (mode === "edit") { setSelected(id); onSelectCamera?.(id); } else selectCamera(id); }} onOpen={mode === "live" ? openCamera : undefined} tenantId={me.data?.tenant_id} siteId={siteId} floorId={floor.id} canEvents={can(me.data, "events.view")} canSnapshots={can(me.data, "snapshots.view")} initialView={me.data ? loadPlanView(me.data.tenant_id, me.data.id, floor.id) ?? undefined : undefined} onViewChange={view => { if (me.data) savePlanView(me.data.tenant_id, me.data.id, floor.id, view); }}/></div>
 </div>
</section>;
}
