import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";
import { meQuery } from "@/api/queries";
import { mapsOverviewQuery } from "@/lib/maps/api";
import { addMapToLiveGrid } from "@/lib/maps/liveGridHelper";
import { can } from "@/lib/perm";
import { MapShell, type MapShellProps } from "./MapShell";
import { FloorMap } from "./FloorMap";
import { MapHierarchyControls } from "./editor/MapHierarchyControls";
import { MapToolbar } from "./MapToolbar";
import { Button, Select } from "@/components/ui";
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
  const [notice, setNotice] = useState<string>();
  const externalBlocked = request !== selection.request && dirty;
  // Adjust state during render only when a new URL arrives; never drop an unsaved draft.
  if (request !== selection.request && !dirty)
    setSelection({ request, site: props.initialSiteId, floor: props.initialFloorId });
  const siteId = selection.site;
  const floorId = selection.floor;
  const onlySite = !siteId && sites.data?.length === 1 ? sites.data[0]?.id : undefined;
  const detail = useQuery(workspaceSiteQuery(siteId ?? onlySite ?? ""));
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
  const mapName = floor?.name ?? (siteId ? "Mapa geográfico" : "Todos los sitios");
  function shareMap() {
    const url = new URL("/maps", window.location.origin);
    if (siteId) url.searchParams.set("site", siteId);
    if (floorId) url.searchParams.set("floor", floorId);
    void navigator.clipboard?.writeText(url.toString());
    setNotice("Enlace copiado. Quien tenga permiso de mapas puede abrir este mapa.");
  }
  function showInLive() {
    if (!me.data || !siteId) return;
    const added = addMapToLiveGrid(me.data.tenant_id ?? null, me.data.id, { site_id: siteId, floor_id: floorId, name: mapName });
    setNotice(added ? `${mapName} quedó en la grilla de En vivo de este navegador.` : "No se pudo agregar el mapa a la grilla.");
  }
  const [modeSlot, setModeSlot] = useState<HTMLDivElement | null>(null);
  const mapCatalog = (siteId ?? onlySite) && can(me.data, "maps.edit") && detail.data ? (
    <MapHierarchyControls embedded site={detail.data} activeFloor={floor} disabled={dirty} onSaved={refresh} onCreated={id => change({ site: siteId ?? onlySite, floor: id })} onRemoved={() => change({ site: siteId ?? onlySite })} onOpenGeographic={() => change({ site: siteId ?? onlySite })} />
  ) : null;
  const visibleSiteId = siteId ?? onlySite ?? "";
  return <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
  <div className="pointer-events-auto absolute left-3 top-3 z-40 flex max-w-[calc(100%-1.5rem)] items-center gap-2 overflow-x-auto rounded-m3-xl bg-surface-1/95 p-1 text-xs shadow-lg backdrop-blur">
   <div ref={setModeSlot} className="contents">
    {floorId && <MapToolbar embedded mode={props.initialMode ?? "live"} onModeChange={props.onModeChange ?? (() => {})} canEdit={can(me.data, "maps.edit") || can(me.data, "maps.edit_device")} />}
   </div>
   <label className="order-2 flex shrink-0 items-center gap-1.5">
    <span className="font-medium text-on-surface-variant">Sitio</span>
    <Select aria-label="Seleccionar sitio" className="h-11 max-w-40 rounded-full text-xs" value={visibleSiteId} onChange={event => change({ site: event.target.value || undefined })}>
     <option value="">Seleccionar sitio</option>{sites.data?.map(site => <option key={site.id} value={site.id}>{site.name}</option>)}
    </Select>
   </label>
   <label className="order-3 flex shrink-0 items-center gap-1.5">
    <span className="font-medium text-on-surface-variant">Mapa</span>
    <Select aria-label="Mapa" title="El mapa geográfico usa coordenadas. Los demás son una imagen de fondo, sin coordenadas, para edificios o planos." className="h-11 max-w-56 rounded-full text-xs" value={floorId ?? ""} disabled={!visibleSiteId || detail.isLoading} onChange={event => change({ site: siteId ?? onlySite, floor: event.target.value || undefined })}>
     <option value="">Mapa geográfico</option>{detail.data?.buildings.map(building => building.floors.map(item => <option key={item.id} value={item.id}>{building.name} / {item.name}</option>))}
    </Select>
   </label>
   <div className="order-4 flex shrink-0 items-center gap-1">
    {siteId && <Button variant="text" size="sm" onClick={shareMap}>Copiar enlace</Button>}
    {siteId && <Button variant="text" size="sm" onClick={showInLive}>Ver en la grilla</Button>}
   </div>
  </div>
  {notice && <p role="status" className="pointer-events-auto absolute right-3 top-3 z-40 max-w-sm rounded-m3-lg bg-surface-3 px-4 py-3 text-xs shadow-lg">{notice}</p>}
  {sites.isError && <div role="alert" className="mx-3 mt-16 flex flex-wrap items-center gap-2 rounded-m3-lg bg-bad/10 px-4 py-3 text-sm text-bad">Los sitios no están disponibles. <Button variant="tonal" size="sm" onClick={() => void sites.refetch()}>Reintentar sitios</Button></div>}
  {siteId && detail.isError && <div role="alert" className="mx-3 mt-16 flex flex-wrap items-center gap-2 rounded-m3-lg bg-bad/10 px-4 py-3 text-sm text-bad">La lista de mapas no está disponible. <Button variant="tonal" size="sm" onClick={() => void detail.refetch()}>Reintentar lista</Button></div>}
  {(switchTo || externalBlocked) && <div role="alert" className="mx-3 mt-16 flex flex-wrap items-center gap-2 rounded-m3-lg bg-warn/15 px-4 py-3 text-sm text-on-surface">
   Este mapa tiene cambios sin guardar. Guardá primero, o descartalos antes de cambiar.
   <Button variant="tonal" size="sm" onClick={() => change(switchTo ?? { site: props.initialSiteId, floor: props.initialFloorId }, true)}>Descartar y cambiar</Button>
   {switchTo && <Button variant="outlined" size="sm" onClick={() => setSwitchTo(undefined)}>Seguir editando</Button>}
  </div>}
  {invalid ? <div role="alert" className="mx-3 mt-16 flex shrink-0 flex-wrap items-center gap-2 rounded-m3-lg bg-bad/10 px-4 py-3 text-sm text-bad">El mapa pedido no está en este sitio. <Button variant="tonal" size="sm" onClick={() => change({ site: siteId })}>Abrir mapa geográfico</Button></div>
      : floorId ? <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">{floor && siteId ? <FloorMap key={`${siteId}/${floor.id}`} siteId={siteId} floor={floor} initialMode={props.initialMode ?? "live"} onModeChange={props.onModeChange} onDirty={setDirty} onPlanSaved={refresh} onSelectCamera={props.onSelectCamera} editorMaps={mapCatalog}/> : <p role="status" className="p-4 text-sm text-on-surface-variant">Loading selected map…</p>}</div>
        : <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden"><MapShell {...props} initialSiteId={siteId} hostedChrome modeSlot={modeSlot} editorMaps={mapCatalog} onSelectSite={id => change({ site: id })}/></div>}
 </div>;
}
export type WorkspaceFloor = Schemas["MapFloor"];
