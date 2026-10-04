import type { Polygon } from "geojson";
import { hasSelfIntersection } from "@/lib/zoneGeometry";
import type { Zone, ZoneKind } from "./types";

export const ZONE_KIND_COLOR: Record<ZoneKind, string> = {
  security: "#1683f8",
  perimeter: "#f59e0b",
  warning: "#ef3f46",
  custom: "#a855f7",
};

/** Swatches offered in the zone editor; zone colors are persisted as data, not theme colors. */
export const ZONE_COLOR_CHOICES = ["#1683f8", "#22c55e", "#f59e0b", "#ef3f46", "#a855f7", "#f8fafc"] as const;

/**
 * A zone under construction: vertices accumulate on map clicks until the operator closes
 * the polygon, mirroring exactly what the backend validates (closed GeoJSON ring, at least
 * three points, no self-intersection). Every action returns a new draft, so React renders
 * straight from the value like the placement editor does.
 */
export interface ZoneDraft {
  /** Set when the draft edits a stored zone; absent means the write is a create. */
  zoneId?: string;
  name: string;
  kind: ZoneKind;
  points: Array<{ lng: number; lat: number }>;
  closed: boolean;
  /** CSS color stored in the zone style. Empty means the kind colour. */
  color?: string;
}

export function emptyZoneDraft(): ZoneDraft {
  return { name: "", kind: "security", points: [], closed: false };
}

/** addZonePoint refuses vertices once the polygon is closed: reopening comes first. */
export function addZonePoint(draft: ZoneDraft, point: { lng: number; lat: number }): ZoneDraft {
  if (draft.closed) return draft;
  return { ...draft, points: [...draft.points, point] };
}

/** undoZonePoint drops the last vertex and reopens the polygon so drawing can continue. */
export function undoZonePoint(draft: ZoneDraft): ZoneDraft {
  if (draft.points.length === 0) return draft;
  return { ...draft, points: draft.points.slice(0, -1), closed: false };
}

export function closeZonePolygon(draft: ZoneDraft): ZoneDraft {
  return draft.points.length >= 3 ? { ...draft, closed: true } : draft;
}

export function reopenZonePolygon(draft: ZoneDraft): ZoneDraft {
  return { ...draft, closed: false };
}

/**
 * validateZoneDraft reports every rule the backend's geometry validation would reject, so
 * the panel can block the save with the same messages instead of discovering them later.
 */
export function validateZoneDraft(draft: ZoneDraft): string[] {
  const errors: string[] = [];
  if (!draft.name.trim()) errors.push("El nombre es obligatorio.");
  if (draft.points.length < 3) {
    errors.push("Se necesitan al menos 3 puntos.");
    return errors;
  }
  if (!draft.closed) errors.push("Cerrá el polígono antes de guardar.");
  if (hasSelfIntersection(draft.points.map((point) => ({ x: point.lng, y: point.lat })))) {
    errors.push("El polígono se cruza consigo mismo.");
  }
  return errors;
}

/** zoneDraftToPolygon closes the ring the way GeoJSON and the backend require. */
export function zoneDraftToPolygon(draft: ZoneDraft): Polygon {
  const ring = draft.points.map((point) => [point.lng, point.lat]);
  if (ring.length === 0) return { type: "Polygon", coordinates: [] };
  const first = ring[0]!;
  const last = ring[ring.length - 1]!;
  if (first[0] !== last[0] || first[1] !== last[1]) ring.push([...first]);
  return { type: "Polygon", coordinates: [ring] };
}

/** zoneToDraft seeds the editor from a stored zone: the closing vertex does not count. */
export function zoneToDraft(zone: Zone): ZoneDraft {
  const ring = zone.geometry.coordinates[0] ?? [];
  return {
    zoneId: zone.id,
    name: zone.name,
    kind: zone.kind,
    color: zone.style.color,
    points: ring.slice(0, -1).map((pos) => ({ lng: pos[0]!, lat: pos[1]! })),
    closed: true,
  };
}
