type GeoPoint = { lng: number; lat: number };
type Pixel = [number, number];

/** A provisional working layout, never a claim about physical camera locations. */
export function spreadCameraPositions(ids: readonly string[], center: GeoPoint) {
  const sorted = [...ids].sort();
  const columns = Math.ceil(Math.sqrt(sorted.length));
  const rows = Math.ceil(sorted.length / columns);
  const latStep = 45 / 111320;
  const lngStep = latStep / Math.max(0.1, Math.cos(center.lat * Math.PI / 180));
  return sorted.map((id, i) => ({ id,
    lat: Math.max(-85, Math.min(85, center.lat + ((rows - 1) / 2 - Math.floor(i / columns)) * latStep)),
    lng: ((center.lng + (i % columns - (columns - 1) / 2) * lngStep + 540) % 360) - 180,
  }));
}

export function canvasDropPoint(canvas: HTMLElement,
  rect: Pick<DOMRect, "left" | "top" | "width" | "height">,
  target: EventTarget | null, clientX: number, clientY: number): Pixel | undefined {
  const x = clientX - rect.left, y = clientY - rect.top;
  if (!(target instanceof Node) || !canvas.contains(target) || x < 0 || y < 0 || x > rect.width || y > rect.height) return;
  return [x, y];
}

interface DragOptions {
  enabled: () => boolean;
  pick: (point: Pixel) => string | undefined;
  unproject: (point: Pixel) => GeoPoint;
  pan: { isEnabled: () => boolean; disable: () => void; enable: () => void };
  start: (id: string) => void;
  move: (id: string, point: GeoPoint) => void;
  end: (id: string, point: GeoPoint) => void;
}

/** Pointer capture handles mouse, pen and touch, including releases outside the map. */
export function bindCameraPointerDrag(canvas: HTMLElement, options: DragOptions) {
  let active: { id: string; pointer: number; origin: Pixel; last: Pixel; moved: boolean; pan: boolean } | undefined;
  let suppressClickUntil = 0;
  const point = (event: PointerEvent): Pixel => {
    const rect = canvas.getBoundingClientRect();
    return [event.clientX - rect.left, event.clientY - rect.top];
  };
  const inside = ([x, y]: Pixel) => {
    const rect = canvas.getBoundingClientRect();
    return x >= 0 && y >= 0 && x <= rect.width && y <= rect.height;
  };
  const finish = () => {
    if (!active) return;
    const ended = active; active = undefined;
    if (ended.pan) options.pan.enable();
    canvas.style.cursor = "";
    try { canvas.releasePointerCapture(ended.pointer); } catch { /* Capture may already be lost. */ }
    if (ended.moved) {
      suppressClickUntil = Date.now() + 250;
      options.end(ended.id, options.unproject(ended.last));
    }
  };
  const down = (event: PointerEvent) => {
    if (active || event.button !== 0 || !options.enabled()) return;
    const origin = point(event), id = options.pick(origin);
    if (!id) return;
    active = { id, pointer: event.pointerId, origin, last: origin, moved: false, pan: options.pan.isEnabled() };
    options.pan.disable();
    try { canvas.setPointerCapture(event.pointerId); } catch { finish(); }
  };
  const move = (event: PointerEvent) => {
    if (!active || event.pointerId !== active.pointer) return;
    if (!options.enabled()) { finish(); return; }
    const next = point(event);
    if (!inside(next) || Math.hypot(next[0] - active.origin[0], next[1] - active.origin[1]) < 4 && !active.moved) return;
    event.preventDefault();
    if (!active.moved) { active.moved = true; options.start(active.id); }
    active.last = next;
    canvas.style.cursor = "grabbing";
    options.move(active.id, options.unproject(next));
  };
  const up = (event: PointerEvent) => {
    if (!active || event.pointerId !== active.pointer) return;
    if (event.type === "pointerup" && active.moved && inside(point(event))) active.last = point(event);
    finish();
  };
  canvas.addEventListener("pointerdown", down, true);
  canvas.addEventListener("pointermove", move, true);
  for (const type of ["pointerup", "pointercancel", "lostpointercapture"]) canvas.addEventListener(type, up as EventListener, true);
  window.addEventListener("blur", finish);
  return {
    consumeClick: () => { const suppress = Date.now() < suppressClickUntil; suppressClickUntil = 0; return suppress; },
    dispose: () => {
      finish(); canvas.removeEventListener("pointerdown", down, true); canvas.removeEventListener("pointermove", move, true);
      for (const type of ["pointerup", "pointercancel", "lostpointercapture"]) canvas.removeEventListener(type, up as EventListener, true);
      window.removeEventListener("blur", finish);
    },
  };
}
