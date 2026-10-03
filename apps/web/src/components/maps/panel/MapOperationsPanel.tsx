import { Search } from "lucide-react";
import { useState } from "react";
import type { Schemas } from "@/api/client";
import type { CameraEntity, MapMode, Site } from "@/lib/maps/types";
import { MapCameraTree, type MapTreeFolder, type MapTreeServer } from "./MapCameraTree";

interface Props {
  mode: MapMode;
  sites: Site[];
  currentSite?: Site;
  requestedSiteId?: string;
  cameras: CameraEntity[];
  inventory?: Schemas["Camera"][];
  folders?: MapTreeFolder[];
  servers?: MapTreeServer[];
  visibleCount: number;
  camerasVisible: boolean;
  canEdit: boolean;
  canLive: boolean;
  canEvents: boolean;
  canPlayback: boolean;
  canInventory: boolean;
  selectedCameraId?: string;
  loading: boolean;
  errors: { label: string; error: unknown; retry: () => void }[];
  onSelectSite: (id: string) => void;
  onSelectCamera: (id: string) => void;
  onEdit: () => void;
  onOpenLive: (id: string) => void;
  onEvents: (id: string) => void;
  onPlayback: (id: string) => void;
  onResetVisibility: () => void;
}

const actionClass = "rounded border border-line px-2 py-1 text-xs hover:bg-raised disabled:opacity-50";

/** Inventory is not placement: missing geographic data never receives invented coordinates. */
export function MapOperationsPanel(props: Props) {
  const [search, setSearch] = useState("");
  const { currentSite: site, cameras, inventory, mode } = props;
  const placedIds = new Set(cameras.map(camera => camera.id));
  const serverName = new Map((props.servers ?? []).map((server) => [server.id, server.name]));
  const rows = inventory?.map(camera => ({
    id: camera.id, name: camera.display_name, status: camera.status, placed: placedIds.has(camera.id),
    serverId: camera.server_id, serverName: serverName.get(camera.server_id), folderId: camera.folder_id,
  })) ?? cameras.map(camera => ({ id: camera.id, name: camera.name, status: camera.status, placed: true, serverId: camera.serverId, serverName: serverName.get(camera.serverId ?? ""), folderId: undefined as string | null | undefined }));
  const matching = rows.filter(camera => camera.name.toLowerCase().includes(search.toLowerCase()));
  const unplaced = inventory ? inventory.filter(camera => !placedIds.has(camera.id)).length : undefined;
  const hidden = cameras.length > 0 && (!props.camerasVisible || props.visibleCount < cameras.length);
  const failed = props.errors.filter(item => item.error);
  const placementFailed = failed.some(item => item.label === "Camera placements");
  const placementUnavailable = placementFailed || props.loading;
  const selected = rows.find(camera => camera.id === props.selectedCameraId);

  return <section aria-label="Map operations" className="space-y-2 text-xs">
    {failed.map(item => <div key={item.label} role="alert" className="space-y-1 text-bad">
      <p>{item.label}: {item.error instanceof Error ? item.error.message : "Request failed"}</p>
      <button type="button" className={actionClass} onClick={item.retry}>Retry {item.label.toLowerCase()}</button>
    </div>)}
    {props.loading && <p role="status">Loading authorized map data…</p>}
    {!site ? <>
      {props.requestedSiteId && !props.loading && <p role="alert">The requested site is unavailable or not authorized. Select an accessible site.</p>}
      {!props.loading && props.sites.length === 0 && failed.length === 0 && <p>No authorized sites. Ask an administrator to check site inventory and access.</p>}
      <ul className="space-y-1">{props.sites.map(item => <li key={item.id}>
        <button type="button" className={`${actionClass} w-full text-left`} onClick={() => props.onSelectSite(item.id)}>
          {item.name} · {item.cameraCount ?? "Unknown"} cameras{item.center ? "" : " · No geographic center"}
        </button>
      </li>)}</ul>
    </> : <>
      {mode === "analytics" ? <>
        <p>Current authorized inventory and placement summary, not historical analytics or a heatmap. Overview refreshes every 30 seconds.</p>
        <dl className="grid grid-cols-2 gap-2">
          <dt>Inventory (overview)</dt><dd>{site.cameraCount ?? "Unavailable"}</dd>
          <dt>Loaded placements</dt><dd>{placementUnavailable ? "Unavailable" : cameras.length}</dd>
          <dt>Unplaced inventory</dt><dd>{placementUnavailable ? "Unavailable" : unplaced ?? "Inventory access required"}</dd>
          <dt>Online (overview)</dt><dd>{site.health?.online ?? "Unavailable"}</dd>
          <dt>Offline (overview)</dt><dd>{site.health?.offline ?? "Unavailable"}</dd>
          <dt>Degraded (overview)</dt><dd>{site.health?.degraded ?? "Unavailable"}</dd>
          <dt>Active alarms (overview)</dt><dd>{site.health?.activeAlarms ?? "Unavailable"}</dd>
        </dl>
      </> : <>
        {mode === "investigate" && <p>Elegí una cámara para ver sus eventos o la grabación.</p>}
        {!props.loading && !placementFailed && cameras.length === 0 && <p>
          {site.cameraCount === 0 ? "Este sitio no tiene cámaras."
            : "Las cámaras aparecen en el mapa cuando tienen una ubicación guardada."}
        </p>}
        {!props.canInventory && <p>Solo se listan las cámaras ya ubicadas.</p>}
        <label className="relative block">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted" aria-hidden />
          <span className="sr-only">Find camera</span>
          <input type="search" value={search} onChange={event => setSearch(event.target.value)} placeholder="Buscar cámara" aria-label="Find camera"
            className="w-full rounded-lg border border-line bg-bg py-1.5 pl-8 pr-2" />
        </label>
        <div>
          <MapCameraTree
            cameras={matching.slice(0, 100)}
            folders={props.folders ?? []}
            servers={props.servers ?? []}
            selectedId={props.selectedCameraId}
            onSelect={props.onSelectCamera}
            onOpen={mode === "live" && props.canLive ? props.onOpenLive : undefined}
            keepEmptyFolders={!search}
          />
        </div>
        {rows.length > 0 && matching.length === 0 && <p>No camera matches this search.</p>}
        {mode === "live" && !props.canLive && <p>Live preview requires live.view.</p>}
        {mode === "investigate" && <div className="space-y-2">
          <p>{selected ? `Selected: ${selected.name}` : "Select a camera from the list or map."}</p>
          {props.canEvents && <button type="button" className={actionClass} disabled={!selected} onClick={() => selected && props.onEvents(selected.id)}>Open camera events</button>}
          {props.canPlayback && <button type="button" className={actionClass} disabled={!selected} onClick={() => selected && props.onPlayback(selected.id)}>Open camera playback</button>}
          {!props.canEvents && <p>Event access requires events.view.</p>}
          {!props.canPlayback && <p>Playback requires recordings.view.</p>}
        </div>}
      </>}
      {hidden && <div><p>Some markers are hidden by saved filters or the camera layer.</p>
        <button type="button" className={actionClass} onClick={props.onResetVisibility}>Show all authorized markers</button></div>}
      {!props.canEdit && mode !== "live" && <p>Placement tools require maps.edit or maps.edit_device. Ask an administrator for site-scoped access; this page never grants permissions.</p>}
      {mode === "edit" && !props.canEdit && <p role="alert">Editor is unavailable for this account.</p>}
    </>}
  </section>;
}
