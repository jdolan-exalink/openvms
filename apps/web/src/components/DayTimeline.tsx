import { Maximize, ZoomIn, ZoomOut } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Button } from "@/components/ui";
import {
  centerView, clusterMarkers, DAY_S, formatTick, MIN_SPAN_S, panView, tickStep, ticks, timeToX, xToTime, zoomView,
  type Span, type View,
} from "@/lib/timeScale";

export type TimelineCamera = { id: string; name: string; spans: Span[] };
export type TimelineEvent = { time: number; severity: string };

const RULER_H = 20;
const UNION_H = 14;
const EVENTS_H = 16;
const ROW_H = 6;
const ROW_GAP = 1;
/** Per-camera coverage rows are drawn only up to this many cameras. */
export const MAX_CAMERA_ROWS = 8;
const CLICK_SLOP_PX = 4;
const HANDLE_PX = 7;
const CLUSTER_PX = 7;

type Drag =
  | { kind: "pan"; startX: number; view: View; moved: boolean }
  | { kind: "scrub"; startX: number; moved: boolean };

function cssVar(el: HTMLElement, name: string, fallback: string): string {
  return getComputedStyle(el).getPropertyValue(name).trim() || fallback;
}

/**
 * DayTimeline is the docked timeline of the REC mode: recording coverage (union and, for a
 * few cameras, one row each), clustered event markers and the shared playhead, drawn on a
 * single canvas so a dense day never creates thousands of DOM nodes. Wheel zooms around the
 * cursor (24 h down to 2 min), drag pans, click seeks, and shift-drag (or dragging the
 * playhead) scrubs. It reports seeks in unix seconds; it never plays anything itself.
 */
export function DayTimeline({
  day,
  cameras,
  events,
  position,
  now,
  onSeek,
}: {
  /** Start of the shown day (unix seconds, local midnight). */
  day: number;
  cameras: TimelineCamera[];
  /** Event times sorted ascending. */
  events: TimelineEvent[];
  position: number;
  now: number;
  onSeek: (time: number) => void;
}) {
  const wrap = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const bounds = useMemo<View>(() => ({ start: day, end: day + DAY_S }), [day]);
  const [view, setView] = useState<View>(bounds);
  const [width, setWidth] = useState(0);
  const [scrub, setScrub] = useState<number | null>(null);
  const [hover, setHover] = useState<{ x: number; text: string } | null>(null);
  const [interacting, setInteracting] = useState(false);
  const drag = useRef<Drag | null>(null);
  const viewRef = useRef(view);
  const widthRef = useRef(width);
  useEffect(() => {
    viewRef.current = view;
    widthRef.current = width;
  });

  const rows = useMemo(() => (cameras.length > 1 && cameras.length <= MAX_CAMERA_ROWS ? cameras : []), [cameras]);
  const height = RULER_H + UNION_H + EVENTS_H + (rows.length ? rows.length * (ROW_H + ROW_GAP) + 4 : 0);

  // A different day resets the window to the whole day.
  const [lastDay, setLastDay] = useState(day);
  if (lastDay !== day) {
    setLastDay(day);
    setView(bounds);
  }

  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    setWidth(el.clientWidth);
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => setWidth(el.clientWidth));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // Keep the playhead visible: when it moves outside the window (playing on, stepping) and the
  // user is not dragging, re-center. Adjusted during render, keyed on the position change.
  const [followed, setFollowed] = useState(position);
  if (followed !== position) {
    setFollowed(position);
    if (!interacting && position >= day && position <= day + DAY_S && (position < view.start || position > view.end)) {
      setView(centerView(view, position, bounds));
    }
  }

  // Wheel needs a non-passive listener to prevent the page from scrolling while zooming.
  useEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      const rect = el.getBoundingClientRect();
      const v = viewRef.current;
      const w = widthRef.current || rect.width;
      if (event.shiftKey || Math.abs(event.deltaX) > Math.abs(event.deltaY)) {
        const delta = event.deltaX || event.deltaY;
        setView(panView(v, (delta / w) * (v.end - v.start), bounds));
        return;
      }
      // ctrl+wheel is a trackpad pinch: its deltas are small, so it needs a larger gain.
      const gain = event.ctrlKey ? 0.01 : 0.002;
      const factor = Math.exp(-event.deltaY * gain);
      setView(zoomView(v, xToTime(event.clientX - rect.left, v, w), factor, bounds));
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [bounds]);

  const clusters = useMemo(
    () => clusterMarkers(events.map((e) => e.time), view, Math.max(width, 1), CLUSTER_PX).map((c) => ({
      ...c,
      alert: events.some((e) => e.time >= c.first && e.time <= c.last && e.severity === "alert"),
    })),
    [events, view, width],
  );

  const shownPosition = scrub ?? position;

  useEffect(() => {
    const el = canvas.current;
    const host = wrap.current;
    if (!el || !host || width <= 0) return;
    const ctx = el.getContext("2d");
    if (!ctx) return;
    const dpr = window.devicePixelRatio || 1;
    el.width = Math.round(width * dpr);
    el.height = Math.round(height * dpr);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);
    const accent = cssVar(host, "--accent", "#3b82f6");
    const line = cssVar(host, "--border-default", "#444");
    const muted = cssVar(host, "--text-secondary", "#999");
    const ink = cssVar(host, "--text-primary", "#fff");
    const bad = cssVar(host, "--danger", "#ef4444");
    const warn = cssVar(host, "--warning", "#f59e0b");
    const x = (t: number) => timeToX(t, view, width);

    // Ticks and labels.
    const step = tickStep(view.end - view.start, width);
    ctx.font = "10px ui-monospace, monospace";
    ctx.textBaseline = "top";
    ctx.textAlign = "center";
    for (const t of ticks(view, step)) {
      const tx = Math.round(x(t)) + 0.5;
      ctx.strokeStyle = line;
      ctx.globalAlpha = 0.5;
      ctx.beginPath();
      ctx.moveTo(tx, RULER_H - 4);
      ctx.lineTo(tx, height);
      ctx.stroke();
      ctx.globalAlpha = 1;
      ctx.fillStyle = muted;
      ctx.fillText(formatTick(t, step), Math.min(Math.max(tx, 16), width - 16), 3);
    }

    // Future (nothing recorded yet) is dimmed.
    if (now < view.end) {
      const from = Math.max(0, x(now));
      ctx.fillStyle = line;
      ctx.globalAlpha = 0.25;
      ctx.fillRect(from, RULER_H, width - from, height - RULER_H);
      ctx.globalAlpha = 1;
    }

    const drawSpans = (spans: Span[], y: number, h: number, alpha: number) => {
      ctx.fillStyle = accent;
      ctx.globalAlpha = alpha;
      for (const s of spans) {
        if (s.end < view.start || s.start > view.end) continue;
        const x0 = Math.max(0, x(s.start));
        const x1 = Math.min(width, x(s.end));
        ctx.fillRect(x0, y, Math.max(1, x1 - x0), h);
      }
      ctx.globalAlpha = 1;
    };
    const union = cameras.flatMap((c) => c.spans);
    drawSpans(union, RULER_H + 2, UNION_H - 4, 0.6);
    rows.forEach((c, i) => drawSpans(c.spans, RULER_H + UNION_H + EVENTS_H + 2 + i * (ROW_H + ROW_GAP), ROW_H, 0.5));

    // Event markers, clustered by pixel bucket.
    const ey = RULER_H + UNION_H;
    for (const c of clusters) {
      const cx = x(c.time);
      ctx.fillStyle = c.alert ? bad : warn;
      if (c.count === 1) {
        ctx.fillRect(Math.round(cx) - 1, ey + 2, 2, EVENTS_H - 4);
      } else {
        ctx.beginPath();
        ctx.arc(cx, ey + EVENTS_H / 2, 6, 0, Math.PI * 2);
        ctx.fill();
        ctx.fillStyle = "#000";
        ctx.textAlign = "center";
        ctx.textBaseline = "middle";
        ctx.font = "bold 8px ui-monospace, monospace";
        ctx.fillText(c.count > 99 ? "99+" : String(c.count), cx, ey + EVENTS_H / 2 + 0.5);
        ctx.textBaseline = "top";
      }
    }

    // Playhead.
    const px = Math.round(x(shownPosition)) + 0.5;
    if (px >= 0 && px <= width) {
      ctx.strokeStyle = ink;
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.moveTo(px, RULER_H - 6);
      ctx.lineTo(px, height);
      ctx.stroke();
      ctx.lineWidth = 1;
      ctx.fillStyle = ink;
      ctx.beginPath();
      ctx.moveTo(px - 5, RULER_H - 8);
      ctx.lineTo(px + 5, RULER_H - 8);
      ctx.lineTo(px, RULER_H - 1);
      ctx.fill();
    }
  }, [view, width, height, cameras, rows, clusters, shownPosition, now]);

  const localX = (event: React.PointerEvent) => event.clientX - event.currentTarget.getBoundingClientRect().left;
  const nearPlayhead = (px: number) => Math.abs(px - timeToX(position, view, width)) <= HANDLE_PX;

  const onPointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || width <= 0) return;
    const px = localX(event);
    drag.current = event.shiftKey || nearPlayhead(px) ? { kind: "scrub", startX: px, moved: false } : { kind: "pan", startX: px, view, moved: false };
    event.currentTarget.setPointerCapture(event.pointerId);
    setHover(null);
    setInteracting(true);
  };

  const onPointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    const px = localX(event);
    const d = drag.current;
    if (!d) {
      const t = xToTime(px, view, width);
      const y = event.clientY - event.currentTarget.getBoundingClientRect().top;
      const cluster = clusters.find((c) => Math.abs(timeToX(c.time, view, width) - px) <= CLUSTER_PX);
      const rowIndex = Math.floor((y - (RULER_H + UNION_H + EVENTS_H + 2)) / (ROW_H + ROW_GAP));
      const parts = [formatTick(t, 1)];
      if (cluster && y >= RULER_H + UNION_H && y < RULER_H + UNION_H + EVENTS_H) parts.push(cluster.count === 1 ? "1 evento" : `${cluster.count} eventos`);
      else if (rowIndex >= 0 && rows[rowIndex]) parts.push(rows[rowIndex].name);
      setHover({ x: px, text: parts.join(" · ") });
      return;
    }
    if (Math.abs(px - d.startX) > CLICK_SLOP_PX) d.moved = true;
    if (!d.moved) return;
    if (d.kind === "pan") {
      const span = d.view.end - d.view.start;
      setView(panView(d.view, ((d.startX - px) / width) * span, bounds));
    } else {
      setScrub(Math.min(Math.max(xToTime(px, view, width), day), day + DAY_S));
    }
  };

  const onPointerUp = (event: React.PointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    drag.current = null;
    setInteracting(false);
    if (!d) return;
    const px = localX(event);
    if (d.kind === "scrub") {
      setScrub(null);
      onSeek(Math.floor(Math.min(Math.max(xToTime(px, view, width), day), day + DAY_S)));
    } else if (!d.moved) {
      onSeek(Math.floor(xToTime(px, view, width)));
    }
  };

  const zoomBy = (factor: number) => setView((v) => zoomView(v, shownPosition >= v.start && shownPosition <= v.end ? shownPosition : (v.start + v.end) / 2, factor, bounds));
  const spanLabel = (() => {
    const s = view.end - view.start;
    return s >= 3600 ? `${Math.round((s / 3600) * 10) / 10} h` : s >= 60 ? `${Math.round(s / 60)} min` : `${Math.round(s)} s`;
  })();

  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-2 text-xs text-muted">
        <span>Ventana: {spanLabel}</span>
        <span className="flex items-center gap-1">
          <Button aria-label="Alejar" title="Alejar" className="px-2 py-1" onClick={() => zoomBy(1 / 1.6)} disabled={view.end - view.start >= DAY_S}>
            <ZoomOut className="size-4" aria-hidden />
          </Button>
          <Button aria-label="Acercar" title="Acercar" className="px-2 py-1" onClick={() => zoomBy(1.6)} disabled={view.end - view.start <= MIN_SPAN_S}>
            <ZoomIn className="size-4" aria-hidden />
          </Button>
          <Button aria-label="Ver día completo" title="Ver día completo" className="px-2 py-1" onClick={() => setView(bounds)}>
            <Maximize className="size-4" aria-hidden />
          </Button>
        </span>
      </div>
      <div
        ref={wrap}
        role="slider"
        tabIndex={0}
        aria-label="Línea de tiempo del día"
        aria-valuemin={day}
        aria-valuemax={day + DAY_S}
        aria-valuenow={Math.round(shownPosition)}
        aria-valuetext={formatTick(shownPosition, 1)}
        className="relative w-full cursor-crosshair touch-none select-none overflow-hidden rounded border border-line bg-raised"
        style={{ height }}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerCancel={() => {
          drag.current = null;
          setInteracting(false);
          setScrub(null);
        }}
        onPointerLeave={() => setHover(null)}
      >
        <canvas ref={canvas} className="absolute inset-0 size-full" aria-hidden />
        {hover && (
          <div
            className="pointer-events-none absolute top-0 z-10 whitespace-nowrap rounded border border-line bg-surface px-1.5 py-0.5 text-[10px] shadow"
            style={{ left: Math.min(Math.max(hover.x, 60), Math.max(width - 60, 60)), transform: "translate(-50%, 100%)", marginTop: 2 }}
          >
            {hover.text}
          </div>
        )}
      </div>
    </div>
  );
}
