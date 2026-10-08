import { pointInPolygon, type Point } from "@/lib/zoneGeometry";

/** One observed position: normalized 0..1 bottom-center of the box and unix seconds. */
export interface TrackPoint {
  x: number;
  y: number;
  t: number;
}

/** The part of an API ObjectTrack these helpers need. */
export interface TrackPath {
  path: TrackPoint[];
}

export interface NamedZone {
  name: string;
  points: Point[];
}

export interface ZoneEntry {
  zone: string;
  t: number;
  x: number;
  y: number;
}

/** Seconds a track stays visible after its last observed point. */
export const TRACK_GRACE_SECONDS = 1.5;

/** Points observed up to and including the instant. */
export function trailUntil(track: TrackPath, tUnix: number): TrackPoint[] {
  return track.path.filter((p) => p.t <= tUnix);
}

/** Visible time span of a track: first point to last point plus the grace. Null without points. */
export function trackWindow(track: TrackPath): { from: number; to: number } | null {
  const first = track.path[0];
  const last = track.path[track.path.length - 1];
  if (!first || !last) return null;
  return { from: first.t, to: last.t + TRACK_GRACE_SECONDS };
}

/**
 * Position of the object at the instant, linearly interpolated between the surrounding points.
 * Null before the first point and after the last point plus the grace; inside the grace the last
 * point is held.
 */
export function positionAt(track: TrackPath, tUnix: number): Point | null {
  const w = trackWindow(track);
  if (!w || tUnix < w.from || tUnix > w.to) return null;
  const pts = track.path;
  const last = pts[pts.length - 1]!;
  if (tUnix >= last.t) return { x: last.x, y: last.y };
  for (let i = 1; i < pts.length; i++) {
    const b = pts[i]!;
    if (tUnix > b.t) continue;
    const a = pts[i - 1]!;
    const span = b.t - a.t;
    const f = span > 0 ? (tUnix - a.t) / span : 1;
    return { x: a.x + (b.x - a.x) * f, y: a.y + (b.y - a.y) * f };
  }
  return { x: last.x, y: last.y };
}

/**
 * Every moment the path goes from outside to inside a zone, ordered by time. A first point that
 * is already inside counts as an entry at its own time. Zones with fewer than 3 vertices are ignored.
 */
export function zoneEntries(track: TrackPath, zones: NamedZone[]): ZoneEntry[] {
  const out: ZoneEntry[] = [];
  for (const zone of zones) {
    if (zone.points.length < 3) continue;
    let inside = false;
    for (const p of track.path) {
      const now = pointInPolygon(p, zone.points);
      if (now && !inside) out.push({ zone: zone.name, t: p.t, x: p.x, y: p.y });
      inside = now;
    }
  }
  return out.sort((a, b) => a.t - b.t);
}
