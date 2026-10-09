/** Digital picture zoom. The transform belongs on a wrapper, never on the `<video>` element. */

export type DigitalZoom = { scale: number; x: number; y: number };

export const ZOOM_MIN = 1;
export const ZOOM_MAX = 8;
/** One mouse notch (~100px) changes the scale by about 20%. */
export const WHEEL_GAIN = 0.0018;

export const IDENTITY_ZOOM: DigitalZoom = Object.freeze({ scale: 1, x: 0, y: 0 });

/** clampZoom keeps the scaled picture covering the box. At 1× the pan is cleared. */
export function clampZoom(zoom: DigitalZoom, width: number, height: number): DigitalZoom {
  const scale = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, zoom.scale));
  if (scale <= ZOOM_MIN || width < 1 || height < 1) return IDENTITY_ZOOM;
  const minX = width * (1 - scale);
  const minY = height * (1 - scale);
  return {
    scale,
    x: Math.min(0, Math.max(minX, zoom.x)),
    y: Math.min(0, Math.max(minY, zoom.y)),
  };
}

/** wheelPixels normalises WheelEvent deltas to pixels. */
export function wheelPixels(deltaY: number, deltaMode = 0): number {
  if (deltaMode === 1) return deltaY * 16;
  if (deltaMode === 2) return deltaY * 800;
  return deltaY;
}

/**
 * zoomAtPoint changes scale toward the cursor. localX/localY are inside the box.
 * A negative deltaY zooms in.
 */
export function zoomAtPoint(current: DigitalZoom, width: number, height: number, localX: number, localY: number, deltaY: number): DigitalZoom {
  const factor = Math.exp(-deltaY * WHEEL_GAIN);
  const scale = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, current.scale * factor));
  const contentX = (localX - current.x) / current.scale;
  const contentY = (localY - current.y) / current.scale;
  return clampZoom({ scale, x: localX - contentX * scale, y: localY - contentY * scale }, width, height);
}

/**
 * pinchZoom applies one step of a two-finger gesture: the scale follows the finger distance
 * and the picture point that was under the previous midpoint stays under the new one (so the
 * pinch also pans). Midpoints are inside the box.
 */
export function pinchZoom(
  current: DigitalZoom,
  width: number,
  height: number,
  prevMid: { x: number; y: number },
  mid: { x: number; y: number },
  prevDistance: number,
  distance: number,
): DigitalZoom {
  if (prevDistance < 1) return current;
  const scale = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, current.scale * (distance / prevDistance)));
  const contentX = (prevMid.x - current.x) / current.scale;
  const contentY = (prevMid.y - current.y) / current.scale;
  return clampZoom({ scale, x: mid.x - contentX * scale, y: mid.y - contentY * scale }, width, height);
}

/** panBy shifts the picture and clamps it so the box stays covered. */
export function panBy(current: DigitalZoom, width: number, height: number, dx: number, dy: number): DigitalZoom {
  if (current.scale <= ZOOM_MIN) return IDENTITY_ZOOM;
  return clampZoom({ scale: current.scale, x: current.x + dx, y: current.y + dy }, width, height);
}

/** zoomTransform is the CSS transform for a wrapper. Empty at 1×. */
export function zoomTransform(zoom: DigitalZoom): string {
  if (zoom.scale <= ZOOM_MIN) return "";
  return `translate(${zoom.x}px, ${zoom.y}px) scale(${zoom.scale})`;
}
