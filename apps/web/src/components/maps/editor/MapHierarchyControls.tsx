import { useState } from "react";
import { api, unwrap, type Schemas } from "@/api/client";
interface Props {
  site: Schemas["MapSiteDetails"];
  activeFloor?: Schemas["MapFloor"];
  disabled: boolean;
  onSaved: () => void;
  onCreated: (id: string) => void;
  onRemoved: () => void;
}
/** Ordinals are explicit: deleted ordinals remain reserved by the backend. */
export function MapHierarchyControls({ site, activeFloor, disabled, onSaved, onCreated, onRemoved }: Props) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [buildingId, setBuildingId] = useState("");
  const [buildingName, setBuildingName] = useState("");
  const [ordinal, setOrdinal] = useState(0);
  const [error, setError] = useState<string>();
  const [busy, setBusy] = useState(false);
  async function run(action: () => Promise<void>) { setBusy(true); setError(undefined); try {
    await action();
    onSaved();
  }
  catch (cause) {
    setError(cause instanceof Error ? cause.message : String(cause));
    onSaved();
  }
  finally {
    setBusy(false);
  } }
  const building = site.buildings.find(item => item.id === (buildingId || activeFloor?.building_id));
  return <div className="relative">
  <button type="button" disabled={disabled} onClick={() => { if (!open && activeFloor) {
    setName(activeFloor.name);
    setOrdinal(activeFloor.ordinal);
    setBuildingId(activeFloor.building_id);
  } setOpen(!open); }}>Manage maps</button>
  {open && <section aria-label="Manage maps" className="absolute right-0 top-full z-50 mt-2 w-72 space-y-2 rounded border border-line bg-surface p-3 shadow-lg">
   {error && <p role="alert">{error}</p>}
   <label className="block">Plant/building<select aria-label="Plant/building" value={buildingId} onChange={event => setBuildingId(event.target.value)}>
    <option value="">Select a plant/building</option>{site.buildings.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
   </select></label>
   <label className="block">Building name<input aria-label="Building name" value={buildingName} onChange={event => setBuildingName(event.target.value)}/></label>
   <button disabled={busy || disabled || !buildingName.trim()} onClick={() => void run(async () => {
        const result = unwrap(await api.POST("/api/v1/maps/sites/{siteId}/buildings", { params: { path: { siteId: site.id } }, body: { name: buildingName.trim() } }));
        setBuildingId(result.id);
        setBuildingName("");
      })}>Create building</button>
   {building && <><button disabled={busy || disabled || !buildingName.trim() || !building.revision} onClick={() => void run(async () => {
          unwrap(await api.PATCH("/api/v1/maps/sites/{siteId}/buildings/{buildingId}", { params: { path: { siteId: site.id, buildingId: building.id }, header: { "If-Match": String(building.revision) } }, body: { name: buildingName.trim() } }));
        })}>Rename building</button>
    <button disabled={busy || disabled || building.floors.length > 0 || !building.revision} onClick={() => void run(async () => {
          unwrap(await api.DELETE("/api/v1/maps/sites/{siteId}/buildings/{buildingId}", { params: { path: { siteId: site.id, buildingId: building.id }, header: { "If-Match": String(building.revision) } } }));
          setBuildingId("");
        })}>Delete empty building</button></>}
   <label className="block">Map name<input aria-label="Map name" value={name} onChange={event => setName(event.target.value)}/></label>
   <label className="block">Map ordinal<input aria-label="Map ordinal" type="number" value={ordinal} onChange={event => setOrdinal(Number(event.target.value))}/></label>
   <p className="text-muted">Use a unique ordinal. Deleted ordinals cannot be reused.</p>
   <button disabled={busy || disabled || !buildingId || !name.trim() || !Number.isInteger(ordinal)} onClick={() => void run(async () => {
        const result = unwrap(await api.POST("/api/v1/maps/sites/{siteId}/buildings/{buildingId}/floors", { params: { path: { siteId: site.id, buildingId } }, body: { name: name.trim(), ordinal } }));
        onCreated(result.id);
        setName("");
        setOpen(false);
      })}>Create map</button>
   {activeFloor && <><button disabled={busy || disabled || !name.trim() || !activeFloor.revision} onClick={() => void run(async () => {
          unwrap(await api.PATCH("/api/v1/maps/sites/{siteId}/floors/{floorId}", { params: { path: { siteId: site.id, floorId: activeFloor.id }, header: { "If-Match": String(activeFloor.revision) } }, body: { name: name.trim(), ordinal } }));
        })}>Rename selected map</button>
    <button disabled={busy || disabled || !activeFloor.revision} onClick={() => void run(async () => {
          // The server refuses occupied maps. No placement or image cleanup is inferred here.
          unwrap(await api.DELETE("/api/v1/maps/sites/{siteId}/floors/{floorId}", { params: { path: { siteId: site.id, floorId: activeFloor.id }, header: { "If-Match": String(activeFloor.revision) } } }));
          onRemoved();
          setOpen(false);
        })}>Delete selected empty map</button></>}
  </section>}
 </div>;
}
