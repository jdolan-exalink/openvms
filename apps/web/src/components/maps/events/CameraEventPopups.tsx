import { useEffect, useState } from "react";
import type { Map } from "maplibre-gl";
import { api, unwrap, type Schemas } from "@/api/client";
import { subscribeFrames } from "@/lib/realtime";
import { VehicleFacts } from "@/components/VehicleMark";
import { cn } from "@/lib/cn";
import { ArPlate } from "@/components/plates/ArPlate";
import type { CameraEntity } from "@/lib/maps/types";

interface Props {
  map: Map | null;
  /** Explicit floor projection avoids interpreting normalized coordinates as longitude. */
  projectCamera?: (camera: CameraEntity) => {x:number;y:number} | undefined;
  projectionKey?: string;
  cameras: CameraEntity[];
  tenantId?: string | null;
  siteId?: string;
  canEvents: boolean;
  canSnapshots: boolean;
  /** Plate balloons open the detection still. The id stays mounted so the grow can hide it. */
  quietNoticeId?: string;
  onOpenPlate?: (target: { id: string; plate: string; cameraName: string; imageUrl: string; origin: { left: number; top: number; width: number; height: number } }) => void;
}
type Notice = { id: string; cameraId: string; detail: Schemas["Event"] };
const LIFETIME = 5000;
// Push timestamps are event start times, not publication times; delayed indexing is bounded.
const EVENT_START_RECENCY_MS = 120000;

/** REST remains authoritative; push transport IDs are deliberately not event IDs. */
export function CameraEventPopups({ map, cameras, tenantId, siteId, canEvents, canSnapshots, projectCamera, projectionKey, quietNoticeId, onOpenPlate }: Props) {
  const [notices, setNotices] = useState<Notice[]>([]);
  const [, moved] = useState(0);
  useEffect(() => {
    if (!map || !canEvents) return;
    const update = () => moved(value => value + 1);
    map.on("move", update);
    return () => { map.off("move", update); };
  }, [map, canEvents]);
  const floorProjection=!!projectCamera;
  const eligibleKey = JSON.stringify(cameras.filter(camera => floorProjection ? camera.position.kind === "floor" : camera.position.kind === "geo")
    .map(camera => [camera.id, camera.siteId]).sort());
  // Status/name/coordinate updates keep notices alive; eligibility and identity changes cancel them.
  useEffect(() => {
    if (!canEvents || !tenantId || (!map && !floorProjection)) return;
    const eligible = new globalThis.Map<string, string>(JSON.parse(eligibleKey) as [string, string][]);
    let active = true;
    const seen = new Set<string>();
    const pending = new globalThis.Map<string, { id: string; abort: AbortController; timer: ReturnType<typeof setTimeout>; retry?: ReturnType<typeof setTimeout> }>();
    const unsubscribe = subscribeFrames(raw => {
      if (!raw || typeof raw !== "object") return;
      const frame = raw as { type?: string; camera_id?: string; site_id?: string; tenant_id?: string; ts?: string; data?: { id?: string } };
      const id = frame.data?.id;
      const cameraId = frame.camera_id;
      const cameraSite = cameraId ? eligible.get(cameraId) : undefined;
      const timestamp = Date.parse(frame.ts ?? "");
      if (frame.type !== "event.created" || typeof id !== "string" || !cameraId || !cameraSite || seen.has(id)
        || (frame.tenant_id && frame.tenant_id !== tenantId)
        || (frame.site_id && frame.site_id !== cameraSite)
        || (siteId && cameraSite !== siteId)
        || !Number.isFinite(timestamp) || Date.now() - timestamp > EVENT_START_RECENCY_MS || timestamp > Date.now() + EVENT_START_RECENCY_MS) return;
      seen.add(id);
      if (seen.size > 512) seen.delete(seen.values().next().value!);
      const previous = pending.get(cameraId);
      if (previous) { previous.abort.abort(); clearTimeout(previous.timer); clearTimeout(previous.retry); }
      setNotices(items => items.filter(item => item.cameraId !== cameraId));
      const request = { id, abort: new AbortController(), timer: setTimeout(() => {
        request.abort.abort(); clearTimeout(request.retry); pending.delete(cameraId);
        if (active) setNotices(items => items.filter(item => item.cameraId !== cameraId));
      }, LIFETIME), retry: undefined as ReturnType<typeof setTimeout> | undefined };
      pending.set(cameraId, request);
      const enrich = async (attempt: "first" | "error" | "color" = "first") => {
        try {
          const detail = unwrap(await api.GET("/api/v1/events/{eventId}", { params: { path: { eventId: id } }, signal: request.abort.signal }));
          if (!active || request.abort.signal.aborted || pending.get(cameraId) !== request || detail.id !== id || detail.camera_id !== cameraId) return;
          if (attempt !== "color") {
            clearTimeout(request.timer);
            request.timer = setTimeout(() => {
              pending.delete(cameraId);
              if (active) setNotices(items => items.filter(item => item.cameraId !== cameraId));
            }, LIFETIME);
          }
          setNotices(items => [...items.filter(item => item.cameraId !== cameraId), { id, cameraId: cameraId, detail }]);
          const wantsVehicle = detail.labels.some((label) => label === "car" || label === "truck" || label === "bus" || label === "motorcycle");
          const waiting = attempt !== "color" && ((wantsVehicle && !detail.attributes?.vehicle) || (detail.labels.includes("person") && !detail.attributes?.person));
          if (waiting) request.retry = setTimeout(() => { void enrich("color"); }, 1500);
        } catch {
          if (attempt === "first" && active && !request.abort.signal.aborted && pending.get(cameraId) === request) {
            request.retry = setTimeout(() => { void enrich("error"); }, 500);
          }
        }
      };
      void enrich();
    });
    return () => {
      active = false; unsubscribe();
      for (const request of pending.values()) { request.abort.abort(); clearTimeout(request.timer); clearTimeout(request.retry); }
      setNotices([]);
    };
  }, [map, eligibleKey, tenantId, siteId, canEvents, floorProjection, projectionKey]);
  if (!canEvents || (!map && !projectCamera)) return null;
  const balloons = notices.filter((notice) => notice.detail.plates.length > 0 || notice.detail.severity === "alert");
  return <div className="pointer-events-none absolute inset-0 overflow-hidden z-20" aria-live="polite">
    {balloons.map(notice => {
      const camera = cameras.find(item => item.id === notice.cameraId);
      if (!camera) return null;
      const point = projectCamera ? projectCamera(camera) : camera.position.kind === "geo" ? map?.project([camera.position.lng, camera.position.lat]) : undefined;
      if (!point) return null;
      return <EventCard key={notice.id} notice={notice} name={camera.name} x={point.x} y={point.y} canSnapshots={canSnapshots} quiet={notice.id === quietNoticeId} onOpenPlate={onOpenPlate} />;
    })}
  </div>;
}
function EventCard({ notice, name, x, y, canSnapshots, quiet, onOpenPlate }: { notice: Notice; name: string; x: number; y: number; canSnapshots: boolean; quiet?: boolean; onOpenPlate?: Props["onOpenPlate"] }) {
  const [failed, setFailed] = useState(false);
  const event = notice.detail;
  const critical = event.severity === "alert";
  const image = canSnapshots && event.has_snapshot ? `/media/v1/events/${encodeURIComponent(event.id)}/snapshot.jpg` : `/api/v1/events/${encodeURIComponent(event.id)}/thumbnail`;
  const plate = event.plates[0];
  const openPlate = plate && onOpenPlate ? (source: HTMLElement) => {
    const rect = source.getBoundingClientRect();
    onOpenPlate({ id: notice.id, plate, cameraName: name, imageUrl: image, origin: { left: rect.left, top: rect.top, width: rect.width, height: rect.height } });
  } : undefined;
  return <section data-map-source={notice.cameraId} data-map-source-rank="1" tabIndex={openPlate ? 0 : undefined} onClick={openPlate ? (click) => openPlate(click.currentTarget) : undefined} onKeyDown={openPlate ? (key) => { if (key.key === "Enter" || key.key === " ") { key.preventDefault(); openPlate(key.currentTarget); } } : undefined} className={cn("soc-popup absolute w-56 -translate-x-1/2 -translate-y-full rounded-2xl border bg-surface/95 p-2 shadow-2xl backdrop-blur-md", openPlate && "pointer-events-auto cursor-pointer", quiet && "invisible", critical ? "border-bad/70" : "border-accent/60")} style={{ left: x, top: y - 16 }} aria-label={`Camera event: ${name}`}>
    <strong className="block truncate text-xs">{name}</strong>
    <span className={critical ? "text-bad text-xs" : "text-muted text-xs"}>{critical ? "Alerta crítica" : "Lectura LPR"}</span>
    <VehicleFacts labels={event.labels} vehicle={event.attributes?.vehicle} person={event.attributes?.person} serverName={event.server_name} vehicleJob={event.attributes?.vehicle_job} personJob={event.attributes?.person_job} />
    {event.plates.length > 0 && <p className="mt-1 flex flex-wrap gap-1">{event.plates.map((plate) => <ArPlate key={plate} plate={plate} />)}</p>}
    {failed ? <p className="text-xs text-muted">Image unavailable</p> : <img src={image} alt={`Event at ${name}`} className="mt-1 h-24 w-full rounded-lg object-cover" onError={() => setFailed(true)} />}
  </section>;
}
