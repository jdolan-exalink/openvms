import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import { meQuery } from "@/api/queries";
import { mapsOverviewQuery } from "@/lib/maps/api";
import { can } from "@/lib/perm";
import { MapShell, type MapShellProps } from "./MapShell";
import { FloorMap } from "./FloorMap";
import { MapHierarchyControls } from "./editor/MapHierarchyControls";
interface Props extends MapShellProps {
  initialFloorId?: string;
  onSelectFloor?: (floorId: string | undefined) => void;
  onSelectMap?: (siteId: string | undefined, floorId: string | undefined) => void;
}
export const workspaceSiteQuery = (siteId: string) => ({
  queryKey: ["maps", "workspace", siteId], enabled: !!siteId,
  queryFn: async ({ signal }: {
    signal: AbortSignal;
  }) => unwrap(await api.GET("/api/v1/maps/sites/{siteId}", { params: { path: { siteId } }, signal })),
});
export function MapWorkspace(props: Props) {
  const me = useQuery(meQuery);
  const sites = useQuery(mapsOverviewQuery);
  const client = useQueryClient();
  const request = `${props.initialSiteId ?? ""}/${props.initialFloorId ?? ""}`;
  const [selection, setSelection] = useState({ request, site: props.initialSiteId, floor: props.initialFloorId });
  const [dirty, setDirty] = useState(false);
  const [switchTo, setSwitchTo] = useState<{
    site?: string;
    floor?: string;
  }>();
  const externalBlocked = request !== selection.request && dirty;
  // Adjust state during render only when a new URL arrives; never drop an unsaved draft.
  if (request !== selection.request && !dirty)
    setSelection({ request, site: props.initialSiteId, floor: props.initialFloorId });
  const siteId = selection.site;
  const floorId = selection.floor;
  const detail = useQuery(workspaceSiteQuery(siteId ?? ""));
  const floor = detail.data?.buildings.flatMap(building => building.floors).find(item => item.id === floorId);
  const invalid = !!floorId && (!siteId || detail.isSuccess && !detail.isFetching && !floor);
  function change(next: {
    site?: string;
    floor?: string;
  }, discard = false) {
    if (dirty && !discard) {
      setSwitchTo(next);
      return;
    }
    setDirty(false);
    setSwitchTo(undefined);
    setSelection({ request, site: next.site, floor: next.floor });
    if (props.onSelectMap)
      props.onSelectMap(next.site, next.floor);
    else {
      if (next.site !== siteId)
        props.onSelectSite?.(next.site);
      props.onSelectFloor?.(next.floor);
      props.onSelectCamera?.(undefined);
    }
  }
  const refresh = () => { void client.invalidateQueries({ queryKey: ["maps", "workspace", siteId] }); };
  return <div className="flex h-full min-h-0 flex-col gap-2">
  <header className="flex shrink-0 flex-wrap items-center gap-3 rounded-lg border border-line bg-surface p-2 text-xs">
   <label>Site<select aria-label="Map site" value={siteId ?? ""} onChange={event => change({ site: event.target.value || undefined })}>
    <option value="">All sites (geographic)</option>{sites.data?.map(site => <option key={site.id} value={site.id}>{site.name}</option>)}
   </select></label>
   <label>Map<select aria-label="Map" value={floorId ?? ""} disabled={!siteId || detail.isLoading} onChange={event => change({ site: siteId, floor: event.target.value || undefined })}>
    <option value="">Geographic map</option>{detail.data?.buildings.map(building => building.floors.map(item => <option key={item.id} value={item.id}>{building.name} / {item.name}</option>))}
   </select></label>
   {siteId && can(me.data, "maps.edit") && detail.data && <MapHierarchyControls site={detail.data} activeFloor={floor} disabled={dirty} onSaved={refresh} onCreated={id => change({ site: siteId, floor: id })} onRemoved={() => change({ site: siteId })}/>}
  </header>
  {sites.isError && <div role="alert">Sites are unavailable. <button onClick={() => void sites.refetch()}>Retry sites</button></div>}
  {siteId && detail.isError && <div role="alert">Map list is unavailable. <button onClick={() => void detail.refetch()}>Retry map list</button></div>}
  {(switchTo || externalBlocked) && <div role="alert" className="rounded border border-warning p-2 text-sm">
   This map has unsaved changes. Save first, or discard them before switching.
   <button onClick={() => change(switchTo ?? { site: props.initialSiteId, floor: props.initialFloorId }, true)}>Discard changes and switch</button>
   {switchTo && <button onClick={() => setSwitchTo(undefined)}>Keep editing</button>}
  </div>}
  {invalid ? <div role="alert">The requested map is not available in this site. <button onClick={() => change({ site: siteId })}>Open geographic map</button></div>
      : floorId ? <div className="min-h-0 flex-1">{floor && siteId ? <FloorMap key={`${siteId}/${floor.id}`} siteId={siteId} floor={floor} initialMode={props.initialMode ?? "live"} onModeChange={props.onModeChange} onDirty={setDirty} onPlanSaved={refresh} onSelectCamera={props.onSelectCamera}/> : <p role="status">Loading selected map…</p>}</div>
        : <div className="min-h-0 flex-1"><MapShell {...props} initialSiteId={siteId} onSelectSite={id => change({ site: id })}/></div>}
 </div>;
}
export type WorkspaceFloor = Schemas["MapFloor"];
