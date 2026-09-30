/**
 * timeScale holds the pure math of the zoomable day timeline (LV-9): the visible time
 * window ("view"), wheel zoom around the cursor, panning, time <-> pixel mapping, tick
 * spacing, span merging and marker clustering. No React or DOM, so it is unit-testable.
 * All times are unix seconds.
 */

export const DAY_S = 24 * 3600;
/** Narrowest window the timeline can zoom to. */
export const MIN_SPAN_S = 120;

export type View = { start: number; end: number };
export type Span = { start: number; end: number };

/** clampView limits the span to [minSpan, bounds span] and shifts the window inside bounds. */
export function clampView(view: View, bounds: View, minSpan = MIN_SPAN_S): View {
  const max = bounds.end - bounds.start;
  const span = Math.min(Math.max(view.end - view.start, Math.min(minSpan, max)), max);
  const start = Math.min(Math.max(view.start, bounds.start), bounds.end - span);
  return { start, end: start + span };
}

/** zoomView scales the window by `factor` (> 1 zooms in) keeping the time under `focus` fixed. */
export function zoomView(view: View, focus: number, factor: number, bounds: View, minSpan = MIN_SPAN_S): View {
  const span = view.end - view.start;
  const next = span / factor;
  const ratio = span > 0 ? (focus - view.start) / span : 0.5;
  const start = focus - ratio * next;
  return clampView({ start, end: start + next }, bounds, minSpan);
}

/** panView shifts the window by deltaS seconds, staying inside bounds. */
export function panView(view: View, deltaS: number, bounds: View): View {
  return clampView({ start: view.start + deltaS, end: view.end + deltaS }, bounds);
}

/** centerView moves the window so `time` is at its center (used to follow the playhead). */
export function centerView(view: View, time: number, bounds: View): View {
  const half = (view.end - view.start) / 2;
  return clampView({ start: time - half, end: time + half }, bounds);
}

export function timeToX(time: number, view: View, width: number): number {
  return ((time - view.start) / (view.end - view.start)) * width;
}

export function xToTime(x: number, view: View, width: number): number {
  return view.start + (x / width) * (view.end - view.start);
}

const TICK_STEPS = [1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200];

/** tickStep picks the smallest tick interval (seconds) that keeps ticks at least minPx apart. */
export function tickStep(span: number, width: number, minPx = 64): number {
  const perPx = span / Math.max(width, 1);
  return TICK_STEPS.find((step) => step / perPx >= minPx) ?? TICK_STEPS[TICK_STEPS.length - 1]!;
}

/** ticks lists tick times in the view, aligned to local wall-clock multiples of step. */
export function ticks(view: View, step: number): number[] {
  const offset = -new Date(view.start * 1000).getTimezoneOffset() * 60;
  const first = Math.ceil((view.start + offset) / step) * step - offset;
  const out: number[] = [];
  for (let t = first; t <= view.end; t += step) out.push(t);
  return out;
}

/** formatTick renders HH:mm, or HH:mm:ss for steps under a minute. */
export function formatTick(time: number, step: number): string {
  const d = new Date(time * 1000);
  const pad = (n: number) => String(n).padStart(2, "0");
  const base = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return step < 60 ? `${base}:${pad(d.getSeconds())}` : base;
}

/** mergeSpans sorts spans and joins those that overlap or are separated by <= gap seconds. */
export function mergeSpans(spans: Span[], gap = 1): Span[] {
  const sorted = spans.filter((s) => s.end > s.start).sort((a, b) => a.start - b.start);
  const out: Span[] = [];
  for (const s of sorted) {
    const last = out[out.length - 1];
    if (last && s.start <= last.end + gap) last.end = Math.max(last.end, s.end);
    else out.push({ ...s });
  }
  return out;
}

/** coversTime reports whether any (merged, sorted) span contains the instant. */
export function coversTime(spans: Span[], time: number): boolean {
  return spans.some((s) => time >= s.start && time <= s.end);
}

export type Cluster = { time: number; first: number; last: number; count: number };

/**
 * clusterMarkers buckets sorted event times into clusters no narrower than minPx on screen,
 * so a dense day renders a handful of glyphs instead of thousands. Times outside the view
 * are dropped.
 */
export function clusterMarkers(times: number[], view: View, width: number, minPx = 6): Cluster[] {
  const out: Cluster[] = [];
  let bucket = NaN;
  for (const t of times) {
    if (t < view.start || t > view.end) continue;
    const b = Math.floor(timeToX(t, view, width) / minPx);
    const last = out[out.length - 1];
    if (last && b === bucket) {
      last.count += 1;
      last.last = t;
    } else {
      out.push({ time: t, first: t, last: t, count: 1 });
      bucket = b;
    }
  }
  return out;
}
