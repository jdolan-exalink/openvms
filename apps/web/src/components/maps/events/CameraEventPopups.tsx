import { useEffect, useState } from "react";
import type { Map } from "maplibre-gl";
import { api, unwrap, type Schemas } from "@/api/client";
import { subscribeFrames } from "@/lib/realtime";
import { labelName } from "@/lib/format";
import type { CameraEntity } from "@/lib/maps/types";

interface Props {
  map: Map | null;
  cameras: CameraEntity[];
  tenantId?: string | null;
  siteId?: string;
  canEvents: boolean;
  canSnapshots: boolean;
}
type Notice = { id: string; cameraId: string; detail: Schemas["Event"] };
const LIFETIME = 5000;
// Push timestamps are event start times, not publication times; delayed indexing is bounded.
const EVENT_START_RECENCY_MS = 120000;

/** REST remains authoritative; push transport IDs are deliberately not event IDs. */
export function CameraEventPopups({ map, cameras, tenantId, siteId, canEvents, canSnapshots }: Props) {
  const [notices, setNotices] = useState<Notice[]>([]);
  const [, moved] = useState(0);
  useEffect(() => {
    if (!map || !canEvents) return;
    const update = () => moved(value => value + 1);
    map.on("move", update);
    return () => { map.off("move", update); };
  }, [map, canEvents]);
  const eligibleKey = JSON.stringify(cameras.filter(camera => camera.position.kind === "geo")
    .map(camera => [camera.id, camera.siteId]).sort());
  // Status/name/coordinate updates keep notices alive; eligibility and identity changes cancel them.
  useEffect(() => {
    if (!canEvents || !tenantId || !map) return;
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
      const enrich = async (retried = false) => {
        try {
          const detail = unwrap(await api.GET("/api/v1/events/{eventId}", { params: { path: { eventId: id } }, signal: request.abort.signal }));
          if (!active || request.abort.signal.aborted || pending.get(cameraId) !== request || detail.id !== id || detail.camera_id !== cameraId) return;
          clearTimeout(request.timer);
          request.timer = setTimeout(() => {
            pending.delete(cameraId);
            if (active) setNotices(items => items.filter(item => item.cameraId !== cameraId));
          }, LIFETIME);
          setNotices(items => [...items.filter(item => item.cameraId !== cameraId), { id, cameraId: cameraId, detail }]);
        } catch {
          if (!retried && active && !request.abort.signal.aborted && pending.get(cameraId) === request) {
            request.retry = setTimeout(() => { void enrich(true); }, 500);
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
  }, [map, eligibleKey, tenantId, siteId, canEvents]);
  if (!canEvents || !map) return null;
  return <div className="pointer-events-none absolute inset-0 overflow-hidden z-20" aria-live="polite">
    {notices.map(notice => {
      const camera = cameras.find(item => item.id === notice.cameraId);
      if (!camera || camera.position.kind !== "geo") return null;
      const point = map.project([camera.position.lng, camera.position.lat]);
      return <EventCard key={notice.id} notice={notice} name={camera.name} x={point.x} y={point.y} canSnapshots={canSnapshots} />;
    })}
  </div>;
}
function EventCard({ notice, name, x, y, canSnapshots }: { notice: Notice; name: string; x: number; y: number; canSnapshots: boolean }) {
  const [failed, setFailed] = useState(false);
  const event = notice.detail;
  const image = canSnapshots && event.has_snapshot ? `/media/v1/events/${encodeURIComponent(event.id)}/snapshot.jpg` : `/api/v1/events/${encodeURIComponent(event.id)}/thumbnail`;
  return <section className="absolute w-44 -translate-x-1/2 -translate-y-full rounded-lg border border-line bg-surface p-2 shadow-xl" style={{ left: x, top: y - 12 }} aria-label={`Camera event: ${name}`}>
    <strong className="block truncate text-xs">{name}</strong>
    <span className={event.severity === "alert" ? "text-bad text-xs" : "text-muted text-xs"}>{event.severity}</span>
    <p className="text-xs">{event.labels.map(labelName).join(", ") || "Event"}</p>
    {event.plates.length > 0 && <p className="font-mono text-accent">{event.plates.join(" · ")}</p>}
    {failed ? <p className="text-xs text-muted">Image unavailable</p> : <img src={image} alt={`Event at ${name}`} className="mt-1 h-20 w-full rounded object-cover" onError={() => setFailed(true)} />}
  </section>;
}
