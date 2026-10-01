import { useState } from "react";
import type { Schemas } from "@/api/client";
import type { CameraEntity, MapMode, Site } from "@/lib/maps/types";

interface Props {
  mode: MapMode;
  sites: Site[];
  currentSite?: Site;
  requestedSiteId?: string;
  cameras: CameraEntity[];
  inventory?: Schemas["Camera"][];
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

const TITLES: Record<MapMode, string> = {
  live: "Live operations", investigate: "Camera investigation", analytics: "Current site summary", edit: "Map commissioning",
};
const actionClass = "rounded border border-line px-2 py-1 text-xs hover:bg-raised disabled:opacity-50";

/** Inventory is not placement: missing geographic data never receives invented coordinates. */
export function MapOperationsPanel(props: Props) {
  const [search, setSearch] = useState("");
  const { currentSite: site, cameras, inventory, mode } = props;
  const placedIds = new Set(cameras.map(camera => camera.id));
  const rows = inventory?.map(camera => ({
    id: camera.id, name: camera.display_name, status: camera.status, placed: placedIds.has(camera.id),
  })) ?? cameras.map(camera => ({ id: camera.id, name: camera.name, status: camera.status, placed: true }));
  const matching = rows.filter(camera => camera.name.toLowerCase().includes(search.toLowerCase()));
  const unplaced = inventory ? inventory.filter(camera => !placedIds.has(camera.id)).length : undefined;
  const hidden = cameras.length > 0 && (!props.camerasVisible || props.visibleCount < cameras.length);
  const failed = props.errors.filter(item => item.error);
  const placementFailed = failed.some(item => item.label === "Camera placements");
  const placementUnavailable = placementFailed || props.loading;
  const selected = rows.find(camera => camera.id === props.selectedCameraId);

  return <section aria-label="Map operations" className="space-y-3 rounded-lg border border-line bg-surface/95 p-3 text-xs shadow-sm">
    <h2 className="font-semibold text-ink">{TITLES[mode]}</h2>
    {failed.map(item => <div key={item.label} role="alert" className="space-y-1 text-bad">
      <p>{item.label}: {item.error instanceof Error ? item.error.message : "Request failed"}</p>
      <button type="button" className={actionClass} onClick={item.retry}>Retry {item.label.toLowerCase()}</button>
    </div>)}
    {props.loading && <p role="status">Loading authorized map data…</p>}
    {!site ? <>
      {props.requestedSiteId && !props.loading && <p role="alert">The requested site is unavailable or not authorized. Select an accessible site.</p>}
      <p>Select a site to load its cameras and tools. Sites without a geographic center are available here too.</p>
      {!props.loading && props.sites.length === 0 && failed.length === 0 && <p>No authorized sites. Ask an administrator to check site inventory and access.</p>}
      <ul className="space-y-1">{props.sites.map(item => <li key={item.id}>
        <button type="button" className={`${actionClass} w-full text-left`} onClick={() => props.onSelectSite(item.id)}>
          {item.name} · {item.cameraCount ?? "Unknown"} cameras{item.center ? "" : " · No geographic center"}
        </button>
      </li>)}</ul>
    </> : <>
      <p className="font-medium">{site.name}</p>
      {!site.center && <p>No monitoring center is configured. Select Editor to locate cameras and save the current view as this site's center.</p>}
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
        <p>{mode === "investigate" ? "Select a camera, then open its events or recording timeline. No live stream starts in this mode."
          : mode === "edit" ? "Select an unplaced camera in the tray, click its real location, then Save. Drag placed markers to stage a move."
          : "Select a placed camera to preview it, or open any listed camera in Live. Availability is not proof of playable media."}</p>
        {!props.loading && !placementFailed && <p>{cameras.length} placed cameras{unplaced === undefined ? "" : ` · ${unplaced} unplaced cameras`}.</p>}
        {!props.loading && !placementFailed && cameras.length === 0 && <p>
          {site.cameraCount === 0 ? "This site has no cameras in the authorized inventory. Register cameras before commissioning the map."
            : "Camera inventory exists separately from map markers. Cameras need saved geographic placements before they appear on the map."}
        </p>}
        {!props.canInventory && <p>Only placed cameras are listed. Camera inventory access requires cameras.view.</p>}
        <label className="block">Find camera<input type="search" value={search} onChange={event => setSearch(event.target.value)}
          className="mt-1 w-full rounded border border-line bg-bg px-2 py-1" /></label>
        <ul className="max-h-44 space-y-1 overflow-auto">{matching.slice(0, 100).map(camera => <li key={camera.id} className="rounded border border-line p-2">
          <button type="button" aria-pressed={camera.id === props.selectedCameraId}
            className="w-full text-left font-medium" onClick={() => props.onSelectCamera(camera.id)}>{camera.name}</button>
          <p className="text-muted">{camera.status} · {placementFailed ? "Placement unavailable" : props.loading ? "Loading placement" : camera.placed ? "Placed" : "Unplaced"}</p>
          {mode === "live" && props.canLive && <button type="button" className={actionClass} onClick={() => props.onOpenLive(camera.id)}>Live: {camera.name}</button>}
        </li>)}</ul>
        {matching.length > 100 && <p>Showing the first 100 matches. Refine your search.</p>}
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
      {mode !== "edit" && props.canEdit && <button type="button" className={actionClass} onClick={props.onEdit}>Open placement editor</button>}
      {!props.canEdit && <p>Placement tools require maps.edit or maps.edit_device. Ask an administrator for site-scoped access; this page never grants permissions.</p>}
      {mode === "edit" && !props.canEdit && <p role="alert">Editor is unavailable for this account.</p>}
    </>}
  </section>;
}
