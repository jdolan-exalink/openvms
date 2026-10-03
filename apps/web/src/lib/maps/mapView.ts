import type { PlanView } from "./floorEditor";

/** The geographic camera the operator last left on a site map. */
export type GeoView = { center: [number, number]; zoom: number };

export type ViewPadding = { left: number; top: number; right: number; bottom: number };

/** Room for the maps sidebar so the opening frame does not hide cameras under it. */
export const MAP_SIDEBAR_PADDING: ViewPadding = { left: 360, top: 80, right: 28, bottom: 28 };

/** A live cell has no sidebar; keep a small margin around the markers. */
export const MAP_CELL_PADDING: ViewPadding = { left: 28, top: 36, right: 28, bottom: 28 };

const TILE = 512;
const STORAGE_KEY = "openvms.maps.view.v1";

type Store = { geo: Record<string, GeoView>; plan: Record<string, PlanView> };

function emptyStore(): Store {
  return { geo: {}, plan: {} };
}

function viewKey(tenantId: string | null, userId: string): string {
  return `${STORAGE_KEY}:${tenantId ?? "platform"}:${userId}`;
}

function readStore(tenantId: string | null, userId: string): Store {
  try {
    const raw = localStorage.getItem(viewKey(tenantId, userId));
    if (!raw) return emptyStore();
    const parsed = JSON.parse(raw) as Partial<Store>;
    return { geo: parsed.geo ?? {}, plan: parsed.plan ?? {} };
  } catch {
    return emptyStore();
  }
}

function writeStore(tenantId: string | null, userId: string, store: Store) {
  try {
    localStorage.setItem(viewKey(tenantId, userId), JSON.stringify(store));
  } catch {
    // Private mode can reject the write; the current view still stands.
  }
}

function isGeoView(value: unknown): value is GeoView {
  if (!value || typeof value !== "object") return false;
  const view = value as Partial<GeoView>;
  return Array.isArray(view.center)
    && view.center.length === 2
    && view.center.every((n) => typeof n === "number" && Number.isFinite(n))
    && typeof view.zoom === "number"
    && Number.isFinite(view.zoom);
}

function isPlanView(value: unknown): value is PlanView {
  if (!value || typeof value !== "object") return false;
  const view = value as Partial<PlanView>;
  return typeof view.scale === "number" && typeof view.x === "number" && typeof view.y === "number"
    && [view.scale, view.x, view.y].every((n) => Number.isFinite(n));
}

export function loadGeoView(tenantId: string | null, userId: string, siteId: string): GeoView | null {
  const view = readStore(tenantId, userId).geo[siteId];
  return isGeoView(view) ? view : null;
}

export function saveGeoView(tenantId: string | null, userId: string, siteId: string, view: GeoView) {
  if (!isGeoView(view)) return;
  const store = readStore(tenantId, userId);
  store.geo[siteId] = { center: [view.center[0], view.center[1]], zoom: view.zoom };
  writeStore(tenantId, userId, store);
}

export function loadPlanView(tenantId: string | null, userId: string, floorId: string): PlanView | null {
  const view = readStore(tenantId, userId).plan[floorId];
  return isPlanView(view) ? view : null;
}

export function savePlanView(tenantId: string | null, userId: string, floorId: string, view: PlanView) {
  if (!isPlanView(view)) return;
  const store = readStore(tenantId, userId);
  store.plan[floorId] = { scale: view.scale, x: view.x, y: view.y };
  writeStore(tenantId, userId, store);
}

function project(lng: number, lat: number): { x: number; y: number } {
  const clamped = Math.max(-85, Math.min(85, lat));
  const sin = Math.sin((clamped * Math.PI) / 180);
  return {
    x: (lng + 180) / 360,
    y: 0.5 - Math.log((1 + sin) / (1 - sin)) / (4 * Math.PI),
  };
}

function unproject(x: number, y: number): [number, number] {
  const lng = x * 360 - 180;
  const n = Math.PI - 2 * Math.PI * y;
  const lat = (180 / Math.PI) * Math.atan(Math.sinh(n));
  return [lng, lat];
}

/**
 * frameCameras picks the closest zoom that still shows every camera, then shifts the
 * center so the left padding (the sidebar) does not cover them.
 */
export function frameCameras(
  points: { lng: number; lat: number }[],
  viewport: { width: number; height: number },
  padding: ViewPadding,
  limits?: { minZoom?: number; maxZoom?: number },
): GeoView | null {
  const usable = points.filter((point) => Number.isFinite(point.lng) && Number.isFinite(point.lat));
  if (!usable.length) return null;
  const minZoom = limits?.minZoom ?? 2;
  const maxZoom = limits?.maxZoom ?? 16;
  const innerW = Math.max(32, viewport.width - padding.left - padding.right);
  const innerH = Math.max(32, viewport.height - padding.top - padding.bottom);
  const projected = usable.map((point) => project(point.lng, point.lat));
  let minX = Infinity;
  let maxX = -Infinity;
  let minY = Infinity;
  let maxY = -Infinity;
  for (const point of projected) {
    minX = Math.min(minX, point.x);
    maxX = Math.max(maxX, point.x);
    minY = Math.min(minY, point.y);
    maxY = Math.max(maxY, point.y);
  }
  const spanX = Math.max(maxX - minX, 1e-9);
  const spanY = Math.max(maxY - minY, 1e-9);
  const zoom = Math.min(maxZoom, Math.max(minZoom, Math.min(Math.log2(innerW / (spanX * TILE)), Math.log2(innerH / (spanY * TILE)))));
  const scale = TILE * 2 ** zoom;
  const shiftX = (padding.left - padding.right) / 2 / scale;
  const shiftY = (padding.top - padding.bottom) / 2 / scale;
  return {
    center: unproject((minX + maxX) / 2 - shiftX, (minY + maxY) / 2 - shiftY),
    zoom,
  };
}
