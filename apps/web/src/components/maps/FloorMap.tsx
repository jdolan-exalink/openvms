import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { meQuery } from "@/api/queries";
import { can } from "@/lib/perm";
import type { CameraEntity, MapMode } from "@/lib/maps/types";
import { floorEntitiesQuery, floorUnplacedQuery, emptyFloorDraft, stageFloor, undoFloor, redoFloor, saveFloorPlacement, type Point } from "@/lib/maps/floorEditor";
import { loadFloorPlan } from "@/lib/maps/plans";
import type { WorkspaceFloor } from "./MapWorkspace";
import { FloorPlanCanvas, type FloorPlanCanvasHandle } from "./canvas/FloorPlanCanvas";
import { UnplacedTray } from "./editor/UnplacedTray";
import { PlanUpload } from "./editor/PlanUpload";
interface Props {
  siteId: string;
  floor: WorkspaceFloor;
  initialMode: MapMode;
  onModeChange?: (mode: MapMode) => void;
  onDirty: (dirty: boolean) => void;
  onPlanSaved: () => void;
  onSelectCamera?: (id: string | undefined) => void;
}
export function FloorMap({ siteId, floor, initialMode, onModeChange, onDirty, onPlanSaved, onSelectCamera }: Props) {
  const me = useQuery(meQuery);
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
  return <section className="flex h-full min-h-0 flex-col gap-2" aria-label={`Map: ${floor.name}`}>
 <div className="flex flex-wrap items-center gap-2 text-xs">
 <strong>{floor.name}</strong><label>Mode<select aria-label="Floor map mode" value={mode} disabled={planDirty} onChange={event => { const next = event.target.value as MapMode; setModeState({ initial: initialMode, value: next }); onModeChange?.(next); }}>
  <option value="live">Live</option><option value="investigate">Investigate</option><option value="analytics">Analytics</option>
  {(can(me.data, "maps.edit_device") || can(me.data, "maps.edit")) && <option value="edit">Edit</option>}
 </select></label><span className="text-muted">All connection states remain visible. Alarms are shown separately.</span>
 </div>
 {[{ label: "Camera placements", query: entities }, { label: "Unplaced cameras", query: unplaced }, { label: "Private background", query: plan }].map(({ label, query }) => query.isError &&
      <div key={label} role="alert">{label}: {query.error.message} <button onClick={() => void query.refetch()}>Retry {label.toLowerCase()}</button></div>)}
 <div className="flex min-h-0 flex-1 gap-2">
 <aside className="w-64 shrink-0 space-y-2 overflow-auto">
  {editable && <UnplacedTray siteName={floor.name} cameras={tray} armedId={armed} onArm={id => { setArmed(id); }} onPointerDrop={(id, x, y) => canvasRef.current?.dropCamera(id, x, y)}/>}
  {armed && editable && <button onClick={() => place(armed, { x: .5, y: .5 })}>Place selected camera at center</button>}
  {planError && <p role="alert">{planError}</p>}
  {mode === "edit" && can(me.data, "maps.edit") && (floor.revision ? <PlanUpload siteId={siteId} floorId={floor.id} revision={floor.revision} onDirty={setPlanDirty} onSaved={() => { setPlanError(undefined); onPlanSaved(); }} onConflict={() => { setPlanError("The background changed on the server. Reload and review it before uploading again. Nothing was overwritten."); onPlanSaved(); }}/> : <p role="alert">Current map revision is unavailable. Reload the map list before uploading.</p>)}
  {pending.length > 0 && <div className="rounded border border-line bg-surface p-2 text-xs">
  <p>{pending.length} unsaved changes</p>{error && <p role="alert">{error}</p>}
  <button disabled={saving || !draft.past.length} onClick={() => setDraft(undoFloor(draft))}>Undo</button>
  <button disabled={saving || !draft.future.length} onClick={() => setDraft(redoFloor(draft))}>Redo</button>
  <button disabled={saving || !editable} onClick={() => void save()}>Save placements ({pending.length})</button>
  <button disabled={saving} onClick={() => { setDraft(emptyFloorDraft()); setError(undefined); }}>Discard floor changes</button>
  {error && <p>Changes were not retried. Discard and reload before placing again if the revision changed.</p>}
  </div>}
  {selectedCamera && <section aria-label="Selected floor camera" className="space-y-2 rounded border border-line bg-surface p-2 text-xs">
  <strong>{selectedCamera.name}</strong><p>Connection: {selectedCamera.status}</p><p>Active alarms: {selectedCamera.activeAlarms}</p>
  {can(me.data, "live.view") && <button onClick={() => void navigate({ to: "/live", search: { camera: selectedCamera.id } })}>Open Live</button>}
  {can(me.data, "events.view") && <button onClick={() => void navigate({ to: "/events", search: { camera: selectedCamera.id, site: siteId } })}>Events</button>}
  {can(me.data, "recordings.view") && <button onClick={() => void navigate({ to: "/playback", search: { camera: selectedCamera.id } })}>Playback</button>}
  </section>}
  {mode === "edit" && <p className="text-xs text-muted">Drag a camera directly onto the plan, or drag an existing marker. Save explicitly. Floor coverage and zones are not supported here; geographic positions stay unchanged.</p>}
 </aside>
 <div className="min-w-0 flex-1"><FloorPlanCanvas ref={canvasRef} key={`${siteId}/${floor.id}`} imageBlob={plan.data} width={floor.plan_width_px || 1000} height={floor.plan_height_px || 1000} cameras={cameras} editable={editable && !saving} onPlace={place} onSelect={id => { setSelected(id); onSelectCamera?.(id); }} tenantId={me.data?.tenant_id} siteId={siteId} floorId={floor.id} canEvents={can(me.data, "events.view")} canSnapshots={can(me.data, "snapshots.view")}/></div>
 </div>
</section>;
}
