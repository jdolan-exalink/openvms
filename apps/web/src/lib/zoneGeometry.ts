/**
 * Pure geometry + (de)serialization helpers for the visual zone and mask editor.
 *
 * Frigate stores polygons as a flat "x1,y1,x2,y2,..." string of RELATIVE (0-1) coordinates. Legacy
 * configs may hold absolute pixel values instead. Masks come in several shapes depending on the
 * Frigate version (string, list of strings, 0.18 dicts); see {@link parseMasks}/{@link serializeMasks}.
 */

export interface Point {
  x: number;
  y: number;
}

export interface FrameSize {
  width: number;
  height: number;
}

const clamp01 = (n: number) => Math.min(1, Math.max(0, n));

/** Rounds to 3 decimals like Frigate's own editor. */
export const round3 = (n: number) => Math.round(n * 1000) / 1000;

export const normalizePoint = (p: Point): Point => ({ x: round3(clamp01(p.x)), y: round3(clamp01(p.y)) });

/**
 * Parses a Frigate coordinate string (or array of numbers/strings) into relative points.
 * When any value is > 1 the whole polygon is treated as absolute pixels and divided by `frame`;
 * without a frame size the values are just clamped. A trailing odd value is ignored.
 */
export function parseCoordinates(raw: unknown, frame?: Partial<FrameSize>): Point[] {
  let nums: number[];
  if (typeof raw === "string") {
    nums = raw
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s !== "")
      .map(Number);
  } else if (Array.isArray(raw)) {
    nums = raw.map((v) => Number(v));
  } else {
    return [];
  }
  nums = nums.filter((n) => Number.isFinite(n));
  const absolute = nums.some((n) => n > 1);
  const pts: Point[] = [];
  for (let i = 0; i + 1 < nums.length; i += 2) {
    let x = nums[i]!;
    let y = nums[i + 1]!;
    if (absolute && frame?.width && frame.height) {
      x /= frame.width;
      y /= frame.height;
    }
    pts.push(normalizePoint({ x, y }));
  }
  return pts;
}

/** True when the raw coordinates contain legacy absolute pixel values. */
export function hasAbsoluteCoordinates(raw: unknown): boolean {
  const parts = typeof raw === "string" ? raw.split(",") : Array.isArray(raw) ? raw : [];
  return parts.some((v) => Number(v) > 1);
}

/** Serializes points as "x1,y1,x2,y2,..." with at most 3 decimals and no trailing zeros. */
export function serializeCoordinates(points: Point[]): string {
  return points
    .map(normalizePoint)
    .flatMap((p) => [p.x, p.y])
    .join(",");
}

/** Signed-area based polygon area (always >= 0), in relative units. */
export function polygonArea(points: Point[]): number {
  let sum = 0;
  for (let i = 0; i < points.length; i++) {
    const a = points[i]!;
    const b = points[(i + 1) % points.length]!;
    sum += a.x * b.y - b.x * a.y;
  }
  return Math.abs(sum) / 2;
}

const cross = (o: Point, a: Point, b: Point) => (a.x - o.x) * (b.y - o.y) - (a.y - o.y) * (b.x - o.x);

function segmentsIntersect(a: Point, b: Point, c: Point, d: Point): boolean {
  const d1 = cross(a, b, c);
  const d2 = cross(a, b, d);
  const d3 = cross(c, d, a);
  const d4 = cross(c, d, b);
  return ((d1 > 0 && d2 < 0) || (d1 < 0 && d2 > 0)) && ((d3 > 0 && d4 < 0) || (d3 < 0 && d4 > 0));
}

/** True when two non-adjacent edges of the closed polygon properly cross each other. */
export function hasSelfIntersection(points: Point[]): boolean {
  const n = points.length;
  if (n < 4) return false;
  for (let i = 0; i < n; i++) {
    for (let j = i + 1; j < n; j++) {
      const adjacent = j === i + 1 || (i === 0 && j === n - 1);
      if (adjacent) continue;
      if (segmentsIntersect(points[i]!, points[(i + 1) % n]!, points[j]!, points[(j + 1) % n]!)) return true;
    }
  }
  return false;
}

/** Ray-casting point-in-polygon test. */
export function pointInPolygon(p: Point, points: Point[]): boolean {
  let inside = false;
  for (let i = 0, j = points.length - 1; i < points.length; j = i++) {
    const a = points[i]!;
    const b = points[j]!;
    if (a.y > p.y !== b.y > p.y && p.x < ((b.x - a.x) * (p.y - a.y)) / (b.y - a.y) + a.x) inside = !inside;
  }
  return inside;
}

const toPx = (p: Point, size: FrameSize): Point => ({ x: p.x * size.width, y: p.y * size.height });

/** Index of the vertex within `tolPx` pixels of `p` (closest wins), or -1. `size` is the rendered size. */
export function hitVertex(points: Point[], p: Point, tolPx: number, size: FrameSize): number {
  const q = toPx(p, size);
  let best = -1;
  let bestD = tolPx;
  points.forEach((v, i) => {
    const a = toPx(v, size);
    const d = Math.hypot(a.x - q.x, a.y - q.y);
    if (d <= bestD) {
      bestD = d;
      best = i;
    }
  });
  return best;
}

/**
 * Finds the closed-polygon edge within `tolPx` pixels of `p`. Returns the index of the edge start
 * vertex (the new vertex belongs at index + 1) and the projected point on the edge.
 */
export function hitEdge(
  points: Point[],
  p: Point,
  tolPx: number,
  size: FrameSize,
): { index: number; point: Point } | null {
  if (points.length < 2) return null;
  const q = toPx(p, size);
  let best: { index: number; point: Point } | null = null;
  let bestD = tolPx;
  for (let i = 0; i < points.length; i++) {
    const a = toPx(points[i]!, size);
    const b = toPx(points[(i + 1) % points.length]!, size);
    const dx = b.x - a.x;
    const dy = b.y - a.y;
    const len2 = dx * dx + dy * dy;
    const t = len2 === 0 ? 0 : Math.min(1, Math.max(0, ((q.x - a.x) * dx + (q.y - a.y) * dy) / len2));
    const proj = { x: a.x + t * dx, y: a.y + t * dy };
    const d = Math.hypot(proj.x - q.x, proj.y - q.y);
    if (d <= bestD) {
      bestD = d;
      best = { index: i, point: { x: proj.x / size.width, y: proj.y / size.height } };
    }
  }
  return best;
}

/** Returns a new polygon with `point` inserted after vertex `edgeIndex`. */
export function insertVertex(points: Point[], edgeIndex: number, point: Point): Point[] {
  const next = points.slice();
  next.splice(edgeIndex + 1, 0, point);
  return next;
}

/** Returns a new polygon without vertex `index`; refuses to go below 3 vertices. */
export function removeVertex(points: Point[], index: number, min = 3): Point[] {
  if (points.length <= min || index < 0 || index >= points.length) return points;
  return points.filter((_, i) => i !== index);
}

/** Translates the polygon by (dx, dy), clamped so that it stays inside the frame without deforming. */
export function translatePolygon(points: Point[], dx: number, dy: number): Point[] {
  if (points.length === 0) return points;
  const minX = Math.min(...points.map((p) => p.x));
  const maxX = Math.max(...points.map((p) => p.x));
  const minY = Math.min(...points.map((p) => p.y));
  const maxY = Math.max(...points.map((p) => p.y));
  const cdx = Math.min(1 - maxX, Math.max(-minX, dx));
  const cdy = Math.min(1 - maxY, Math.max(-minY, dy));
  return points.map((p) => ({ x: p.x + cdx, y: p.y + cdy }));
}

// ---------------------------------------------------------------------------------------------
// Mask format handling
// ---------------------------------------------------------------------------------------------

/** "string": one polygon; "list": list of strings; "dict-list": 0.18 array of {id,...}; "dict-map": object keyed by id. */
export type MaskFormat = "string" | "list" | "dict-list" | "dict-map";

export interface MaskItem {
  id: string;
  name: string;
  enabled: boolean;
  points: Point[];
  /** Unknown keys of a dict mask, preserved verbatim on save. */
  extra?: Record<string, unknown>;
}

export interface MaskList {
  format: MaskFormat;
  items: MaskItem[];
}

/** Whether the Frigate version stores masks as dicts (0.18+). Unknown versions default to legacy. */
export function usesDictMasks(frigateVersion: string | null | undefined): boolean {
  const m = /(\d+)\.(\d+)/.exec(frigateVersion ?? "");
  if (!m) return false;
  const major = Number(m[1]);
  const minor = Number(m[2]);
  return major > 0 || minor >= 18;
}

let idCounter = 0;
export const newId = (prefix: string) => `${prefix}_${Date.now().toString(36)}${(idCounter++).toString(36)}`;

function dictEntry(key: string | undefined, v: unknown, frame: Partial<FrameSize> | undefined, i: number): MaskItem {
  const o = (v && typeof v === "object" ? v : {}) as Record<string, unknown>;
  const { id, friendly_name, enabled, coordinates, ...extra } = o;
  const mid = key ?? (typeof id === "string" && id ? id : newId("mask"));
  return {
    id: mid,
    name: typeof friendly_name === "string" && friendly_name ? friendly_name : `Máscara ${i + 1}`,
    enabled: enabled !== false,
    points: parseCoordinates(coordinates, frame),
    extra: Object.keys(extra).length ? extra : undefined,
  };
}

/** Normalizes any supported mask representation into an internal list, remembering the original format. */
export function parseMasks(raw: unknown, frame?: Partial<FrameSize>, frigateVersion?: string | null): MaskList {
  if (Array.isArray(raw)) {
    if (raw.some((v) => v && typeof v === "object")) {
      return { format: "dict-list", items: raw.map((v, i) => dictEntry(undefined, v, frame, i)) };
    }
    const items = raw
      .filter((v): v is string => typeof v === "string" && v.trim() !== "")
      .map((s, i) => ({ id: newId("mask"), name: `Máscara ${i + 1}`, enabled: true, points: parseCoordinates(s, frame) }));
    return { format: "list", items };
  }
  if (raw && typeof raw === "object") {
    return {
      format: "dict-map",
      items: Object.entries(raw as Record<string, unknown>).map(([k, v], i) => dictEntry(k, v, frame, i)),
    };
  }
  if (typeof raw === "string") {
    const items = raw.trim()
      ? [{ id: newId("mask"), name: "Máscara 1", enabled: true, points: parseCoordinates(raw, frame) }]
      : [];
    return { format: "string", items };
  }
  // Missing value: pick the native format of the running Frigate version.
  return { format: usesDictMasks(frigateVersion) ? "dict-map" : "list", items: [] };
}

/**
 * Serializes back to the ORIGINAL format. A "string" mask with more than one polygon is upgraded to a
 * list (the only lossless option); dict formats keep ids, friendly names, enabled flag and extra keys.
 */
export function serializeMasks(list: MaskList): unknown {
  const items = list.items.filter((m) => m.points.length >= 3);
  switch (list.format) {
    case "string":
      if (items.length === 0) return "";
      if (items.length === 1) return serializeCoordinates(items[0]!.points);
      return items.map((m) => serializeCoordinates(m.points));
    case "list":
      return items.map((m) => serializeCoordinates(m.points));
    case "dict-list":
      return items.map((m) => ({
        ...m.extra,
        id: m.id,
        friendly_name: m.name,
        enabled: m.enabled,
        coordinates: serializeCoordinates(m.points),
      }));
    case "dict-map":
      return Object.fromEntries(
        items.map((m) => [m.id, { ...m.extra, friendly_name: m.name, enabled: m.enabled, coordinates: serializeCoordinates(m.points) }]),
      );
  }
}

/** Frigate zone names: lowercase letters, digits and underscores. */
export const isValidZoneName = (name: string) => /^[a-z0-9_]+$/.test(name);
