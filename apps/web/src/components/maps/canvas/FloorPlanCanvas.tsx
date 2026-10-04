import { useEffect, useImperativeHandle, useRef, useState, type PointerEvent, type Ref } from "react";
import { Video, X } from "lucide-react";
import { Icon } from "@/components/Icon";
import type { CameraEntity } from "@/lib/maps/types";
import { computeDisplayState } from "@/lib/maps/entityIndex";
import { floorPoint, type Point, type PlanView } from "@/lib/maps/floorEditor";
import { DRAG_MIME } from "../editor/UnplacedTray";
import { CameraEventPopups } from "../events/CameraEventPopups";
export interface FloorPlanCanvasHandle {
  dropCamera: (id: string, x: number, y: number) => void;
  focus: (point: Point) => void;
}
interface Props {
  ref?: Ref<FloorPlanCanvasHandle>;
  imageUrl?: string;
  imageBlob?: Blob;
  width: number;
  height: number;
  cameras: CameraEntity[];
  editable: boolean;
  onPlace: (id: string, point: Point) => void;
  onSelect: (id: string) => void;
  onOpen?: (id: string) => void;
  tenantId?: string | null;
  siteId?: string;
  floorId?: string;
  canEvents?: boolean;
  canSnapshots?: boolean;
  /** Last pan and zoom for this plan. Maps and Live share it. */
  initialView?: PlanView;
  onViewChange?: (view: PlanView) => void;
}
type Gesture = {
  pointer: number;
  id?: string;
  start: Point;
  view: PlanView;
  moved: boolean;
};
/** Image and fixed-size markers share one projection, independent of the geographic engine. */
export function FloorPlanCanvas({ ref, imageUrl, imageBlob, width, height, cameras, editable, onPlace, onSelect, onOpen, tenantId, siteId, floorId, canEvents = false, canSnapshots = false, initialView, onViewChange }: Props) {
  const viewport = useRef<HTMLDivElement>(null);
  const image = useRef<HTMLImageElement>(null);
  useEffect(() => {
    if (!imageBlob || !image.current)
      return;
    const element = image.current;
    const url = URL.createObjectURL(imageBlob);
    element.src = url;
    return () => { element.removeAttribute("src"); URL.revokeObjectURL(url); };
  }, [imageBlob]);
  const gesture = useRef<Gesture | undefined>(undefined);
  const [size, setSize] = useState({ width: 400, height: 200 });
  const [view, setView] = useState<PlanView>(initialView ?? { scale: 1, x: 0, y: 0 });
  const onViewChangeRef = useRef(onViewChange);
  useEffect(() => { onViewChangeRef.current = onViewChange; });
  useEffect(() => { onViewChangeRef.current?.(view); }, [view]);
  const [preview, setPreview] = useState<{
    id: string;
    point: Point;
  }>();
  useEffect(() => {
    const element = viewport.current;
    if (!element)
      return;
    const measure = () => { const rect = element.getBoundingClientRect(); if (rect.width && rect.height)
      setSize({ width: rect.width, height: rect.height }); };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  const factor = Math.min(size.width / width, size.height / height);
  const fitted = { width: width * factor, height: height * factor, left: (size.width - width * factor) / 2, top: (size.height - height * factor) / 2 };
  function point(clientX: number, clientY: number) {
    const rect = viewport.current?.getBoundingClientRect();
    if (!rect)
      return;
    if (clientX < rect.left || clientY < rect.top || clientX > rect.left + rect.width || clientY > rect.top + rect.height)
      return;
    return floorPoint({ ...fitted, left: rect.left + fitted.left, top: rect.top + fitted.top }, view, clientX, clientY);
  }
  useImperativeHandle(ref, () => ({
    dropCamera: (id, x, y) => { const pos = point(x, y); if (editable && pos) onPlace(id, pos); },
    focus: (target) => setView((current) => {
      const scale = Math.max(current.scale, 1.6);
      return {
        scale,
        x: size.width / 2 - fitted.left - target.x * fitted.width * scale,
        y: size.height / 2 - fitted.top - target.y * fitted.height * scale,
      };
    }),
  }));
  function project(camera: CameraEntity) {
    if (camera.position.kind !== "floor")
      return;
    const pos = preview?.id === camera.id ? preview.point : camera.position;
    return { x: fitted.left + view.x + pos.x * fitted.width * view.scale, y: fitted.top + view.y + pos.y * fitted.height * view.scale };
  }
  function start(event: PointerEvent<HTMLDivElement>, id?: string) {
    if (event.button !== 0 || !event.isPrimary)
      return;
    if (id && !editable) {
      onSelect(id);
      return;
    }
    gesture.current = { pointer: event.pointerId, id, start: { x: event.clientX, y: event.clientY }, view, moved: false };
    event.currentTarget.setPointerCapture(event.pointerId);
  }
  function move(event: PointerEvent<HTMLDivElement>) {
    const current = gesture.current;
    if (!current || current.pointer !== event.pointerId)
      return;
    const dx = event.clientX - current.start.x, dy = event.clientY - current.start.y;
    if (Math.hypot(dx, dy) < 5 && !current.moved)
      return;
    current.moved = true;
    if (current.id) {
      const pos = point(event.clientX, event.clientY);
      setPreview(pos ? { id: current.id, point: pos } : undefined);
    }
    else
      setView({ ...current.view, x: current.view.x + dx, y: current.view.y + dy });
  }
  function finish(event: PointerEvent<HTMLDivElement>, cancel = false) {
    const current = gesture.current;
    if (!current || current.pointer !== event.pointerId)
      return;
    gesture.current = undefined;
    setPreview(undefined);
    if (cancel) {
      if (!current.id)
        setView(current.view);
      return;
    }
    if (current.id) {
      const pos = point(event.clientX, event.clientY);
      if (current.moved && pos)
        onPlace(current.id, pos);
      else if (!current.moved)
        onSelect(current.id);
    }
  }
  return <div className="relative h-full min-h-0 w-full overflow-hidden rounded-xl border border-line bg-raised">
  <div ref={viewport} data-testid="floor-viewport" className="absolute inset-0 touch-none overflow-hidden" onPointerDown={event => { const marker = (event.target as Element).closest<HTMLElement>("[data-camera-id]"); start(event, marker?.dataset.cameraId); }} onPointerMove={move} onPointerUp={event => finish(event)} onPointerCancel={event => finish(event, true)} onLostPointerCapture={event => finish(event, true)} onDragOver={event => { if (editable && event.dataTransfer.types.includes(DRAG_MIME) && point(event.clientX, event.clientY)) {
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
  } }} onDrop={event => { if (!editable)
    return; event.preventDefault(); const id = event.dataTransfer.getData(DRAG_MIME); const pos = point(event.clientX, event.clientY); if (id && pos)
    onPlace(id, pos); }} onWheel={event => {
      if (gesture.current)
        return;
      const rect = viewport.current?.getBoundingClientRect();
      if (!rect)
        return;
      const scale = Math.max(.5, Math.min(8, view.scale * (event.deltaY < 0 ? 1.2 : 1 / 1.2)));
      const x = event.clientX - rect.left - fitted.left, y = event.clientY - rect.top - fitted.top;
      setView({ scale, x: x - (x - view.x) * scale / view.scale, y: y - (y - view.y) * scale / view.scale });
    }}>
   {imageUrl || imageBlob ? <img ref={image} src={imageUrl} alt="Map background" draggable={false} className="pointer-events-none absolute select-none" style={{ left: fitted.left + view.x, top: fitted.top + view.y, width: fitted.width, height: fitted.height, transform: `scale(${view.scale})`, transformOrigin: "top left" }}/>
      : <p className="pointer-events-none absolute left-1/2 top-1/2 -translate-x-1/2 px-4 text-center text-sm text-muted">Elegí una imagen de fondo, o colocá las cámaras sobre este plano.</p>}
   {cameras.map(camera => {
      const pos = project(camera);
      if (!pos)
        return null;
      // Alarm counts do not change connection color; server outages override stale camera status.
      const connectivity = computeDisplayState(camera.status, 0, !!camera.metadata.serverOffline);
      const unavailable = connectivity === "OFFLINE" || connectivity === "NO_SIGNAL" || connectivity === "UNREACHABLE";
      return <button key={camera.id} type="button" aria-label={camera.name} title={camera.name} data-camera-id={camera.id} data-map-source={camera.id} data-map-source-rank="0" data-connection={connectivity.toLowerCase()} onClick={event => { if (event.detail === 0) onSelect(camera.id); }} onDoubleClick={() => { if (!editable) onOpen?.(camera.id); }} className={`absolute z-10 flex size-10 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full border-2 bg-surface shadow-lg ${unavailable ? "border-muted text-muted" : connectivity === "ONLINE" ? "border-ok text-ok" : "border-warning text-warning"}`} style={{ left: pos.x, top: pos.y, touchAction: "none", cursor: editable ? "grab" : "pointer" }}>
     <Icon icon={Video} size="xs" />
     {unavailable && <Icon icon={X} size={18} label="Unavailable" className="absolute -bottom-1 -right-1 rounded-full bg-surface p-1" />}
     {camera.activeAlarms > 0 && <span className="absolute -right-1 -top-2 rounded-full bg-bad px-1 text-xs text-white">{camera.activeAlarms}</span>}
     <span className="pointer-events-none absolute top-full mt-1 max-w-32 truncate rounded bg-surface/90 px-1 text-[10px] text-ink">{camera.name}</span>
    </button>;
    })}
   <CameraEventPopups map={null} cameras={cameras} projectCamera={project} projectionKey={floorId} tenantId={tenantId} siteId={siteId} canEvents={canEvents} canSnapshots={canSnapshots}/>
  </div>
  <div className="absolute bottom-2 right-2 z-30 flex gap-1 rounded bg-surface p-1">
   <button type="button" aria-label="Acercar" onClick={() => setView({ ...view, scale: Math.min(8, view.scale * 1.2) })}>+</button>
   <button type="button" aria-label="Alejar" onClick={() => setView({ ...view, scale: Math.max(.5, view.scale / 1.2) })}>−</button>
   <button type="button" onClick={() => setView({ scale: 1, x: 0, y: 0 })}>Ajustar</button>
  </div>
 </div>;
}
