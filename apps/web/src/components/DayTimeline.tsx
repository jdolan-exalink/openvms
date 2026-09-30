import { Car, Maximize, PawPrint, ScanLine, Tag, User, ZoomIn, ZoomOut, type LucideIcon } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { Button } from "@/components/ui";
import { CATEGORY_COLOR, detectionCategory, detectionTitle, type DetectionCategory } from "@/lib/detections";
import { labelName } from "@/lib/format";
import {
  centerView, clusterExtent, clusterItems, defaultView, findClusterAt, formatTick, MIN_SPAN_S, panView, rangeBounds, tickStep, ticks,
  timeToX, xToTime, zoomView, type ItemCluster, type Span, type View,
} from "@/lib/timeScale";

export type TimelineCamera = {
  id: string;
  name: string;
  spans: Span[];
  /** Camera is online and recording: its last segment is extended up to "now". */
  live?: boolean;
};
export type TimelineEvent = {
  id?: string;
  time: number;
  /** End of the detection when known; otherwise a small fixed-width marker is drawn. */
  end?: number;
  severity: string;
  cameraId?: string;
  label?: string;
  /** Plate text or recognized name, when there is one. */
  detail?: string;
};

const RULER_H = 20;
export const ROW_H = 24;
const LABEL_W = 132;
const BAR_H = 8;
const MARK_H = 14;
const CLICK_SLOP_PX = 4;
const HANDLE_PX = 7;
const CLUSTER_PX = 7;
/** Width of a single detection without a known end, and the minimum of a cluster pill. */
const MARK_MIN_PX = 4;
const PILL_MIN_PX = 14;

type Drag =
  | { kind: "pan"; startX: number; view: View; moved: boolean }
  | { kind: "scrub"; startX: number; moved: boolean };

type RowData = { cam: TimelineCamera; items: TimelineEvent[]; clusters: ItemCluster[] };
type Tip = { x: number; y: number; row: RowData; cluster: ItemCluster };

const CATEGORY_ICON: Record<DetectionCategory, LucideIcon> = { person: User, vehicle: Car, plate: ScanLine, animal: PawPrint, other: Tag };

function cssVar(el: HTMLElement, name: string, fallback: string): string {
  return getComputedStyle(el).getPropertyValue(name).trim() || fallback;
}

const pad = (n: number) => String(n).padStart(2, "0");
const clock = (t: number) => {
  const d = new Date(t * 1000);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
};
const durationText = (s: number) => (s < 60 ? `${Math.max(1, Math.round(s))} s` : `${Math.floor(s / 60)} min ${Math.round(s % 60)} s`);
/** A recording segment that ended no longer ago than this is assumed to still be growing. */
const LIVE_GAP_S = 180;
const GLOW_MS = 2000;
const markWidth = (c: ItemCluster) => (c.count > 1 ? PILL_MIN_PX : MARK_MIN_PX);

const eventKey = (e: TimelineEvent) => e.id ?? `${e.cameraId}:${e.time}:${e.label}`;

function prefersReducedMotion(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/**
 * useLiveClock is "now" at a finer grain than the page's 30 s tick, so recording bars grow
 * continuously. Paused while the tab is hidden; with reduced motion it steps every 10 s.
 */
function useLiveClock(base: number): number {
  const [tick, setTick] = useState(() => Math.floor(Date.now() / 1000));
  useEffect(() => {
    const update = () => setTick(Math.floor(Date.now() / 1000));
    const period = prefersReducedMotion() ? 10_000 : 1000;
    let id: ReturnType<typeof setInterval> | undefined;
    const start = () => {
      if (id === undefined && !document.hidden) id = setInterval(update, period);
    };
    const onVisibility = () => {
      if (document.hidden) {
        clearInterval(id);
        id = undefined;
      } else {
        update();
        start();
      }
    };
    start();
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      clearInterval(id);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, []);
  return Math.max(base, tick);
}

/** growSpans extends the last segment of a recording camera up to now, unless it stopped a while ago. */
function growSpans(cam: TimelineCamera, now: number): Span[] {
  const last = cam.spans[cam.spans.length - 1];
  if (!cam.live || !last || now <= last.end || now - last.end > LIVE_GAP_S) return cam.spans;
  return [...cam.spans.slice(0, -1), { start: last.start, end: now }];
}

/** glowOf is the fade-in strength (0..1) of the newest detection of a cluster. */
function glowOf(items: TimelineEvent[], c: ItemCluster, born: Map<string, number>, t: number): number {
  if (born.size === 0) return 0;
  let g = 0;
  for (let i = c.from; i < c.to; i++) {
    const at = born.get(eventKey(items[i]!));
    if (at !== undefined) g = Math.max(g, 1 - (t - at) / GLOW_MS);
  }
  return Math.max(g, 0);
}

/** dominant category of the members of a cluster, by count. */
function dominant(items: TimelineEvent[], c: ItemCluster): DetectionCategory {
  const counts = new Map<DetectionCategory, number>();
  for (let i = c.from; i < c.to; i++) {
    const cat = detectionCategory(items[i]!.label ?? "");
    counts.set(cat, (counts.get(cat) ?? 0) + 1);
  }
  return [...counts.entries()].sort((a, b) => b[1] - a[1])[0]?.[0] ?? "other";
}

/**
 * DayTimeline is the docked timeline of the REC mode. A fixed ruler sits over one row per
 * grid camera (name on the left, recording coverage bar with its detections drawn on top),
 * the rows scrolling inside a bounded area. Rows and ruler are canvases so a dense day never
 * creates thousands of DOM nodes. The range ends at min(day end, now + 1 h). Wheel zooms
 * around the cursor, drag pans, click seeks (a detection seeks to its start) and shift-drag
 * (or dragging the playhead) scrubs. Hovering a detection shows a tooltip. It reports seeks
 * in unix seconds; it never plays anything itself.
 */
export function DayTimeline({
  day,
  cameras,
  events,
  position,
  now,
  onSeek,
  selectedId,
  onSelectCamera,
}: {
  /** Start of the shown day (unix seconds, local midnight). */
  day: number;
  cameras: TimelineCamera[];
  /** Events sorted by time ascending. */
  events: TimelineEvent[];
  position: number;
  now: number;
  /** `detection` is set when the click landed on a detection (the first of a cluster). */
  onSeek: (time: number, detection?: TimelineEvent & { cameraId: string }) => void;
  /** Camera of the selected grid tile: its row is highlighted. */
  selectedId?: string;
  onSelectCamera?: (cameraId: string) => void;
}) {
  const rulerEl = useRef<HTMLDivElement>(null);
  const rowsEl = useRef<HTMLDivElement>(null);
  const rulerCanvas = useRef<HTMLCanvasElement>(null);
  const rowsCanvas = useRef<HTMLCanvasElement>(null);
  const tipEl = useRef<HTMLDivElement>(null);
  // Bounds change once a minute at most, so the pan/zoom limits do not churn every tick.
  const liveNow = useLiveClock(now);
  const nowMinute = Math.floor(liveNow / 60) * 60;
  const bounds = useMemo(() => rangeBounds(day, nowMinute), [day, nowMinute]);
  const [view, setView] = useState<View>(() => defaultView(bounds, now));
  const [width, setWidth] = useState(0);
  const [scrub, setScrub] = useState<number | null>(null);
  const [tip, setTip] = useState<Tip | null>(null);
  const [tipPos, setTipPos] = useState<{ left: number; top: number } | null>(null);
  const [interacting, setInteracting] = useState(false);
  const drag = useRef<Drag | null>(null);
  const viewRef = useRef(view);
  const widthRef = useRef(width);
  useEffect(() => {
    viewRef.current = view;
    widthRef.current = width;
  });

  // New detections fade in with a ~2 s glow (skipped with reduced motion). The first load of a day is not "new".
  const born = useRef(new Map<string, number>());
  const seen = useRef<{ day: number; ids: Set<string> } | null>(null);
  const [frame, setFrame] = useState(0);
  const [glowing, setGlowing] = useState(false);
  useEffect(() => {
    const ids = events.map(eventKey);
    if (!seen.current || seen.current.day !== day) {
      seen.current = { day, ids: new Set(ids) };
      born.current.clear();
      return;
    }
    const known = seen.current.ids;
    let fresh = false;
    for (const id of ids) {
      if (known.has(id)) continue;
      known.add(id);
      if (!prefersReducedMotion()) {
        born.current.set(id, performance.now());
        fresh = true;
      }
    }
    if (fresh) setGlowing(true);
  }, [events, day]);
  useEffect(() => {
    if (!glowing) return;
    let raf = 0;
    const step = () => {
      const t = performance.now();
      for (const [id, at] of born.current) if (t - at > GLOW_MS) born.current.delete(id);
      setFrame((f) => f + 1);
      if (born.current.size > 0) raf = requestAnimationFrame(step);
      else setGlowing(false);
    };
    raf = requestAnimationFrame(step);
    return () => cancelAnimationFrame(raf);
  }, [glowing]);

  const rowsH = Math.max(cameras.length, 1) * ROW_H;

  // A different day resets the window.
  const [lastDay, setLastDay] = useState(day);
  if (lastDay !== day) {
    setLastDay(day);
    setView(defaultView(bounds, now));
  }

  // The window can never extend past the range (now + 1 h shrinks it when the clock is set back).
  const clamped = useMemo(() => {
    const span = Math.min(view.end - view.start, bounds.end - bounds.start);
    const start = Math.min(Math.max(view.start, bounds.start), bounds.end - span);
    return start === view.start && start + span === view.end ? view : { start, end: start + span };
  }, [view, bounds]);
  if (clamped !== view) setView(clamped);

  useEffect(() => {
    const el = rulerEl.current;
    if (!el) return;
    setWidth(el.clientWidth);
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => setWidth(el.clientWidth));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // Keep the playhead visible: when it moves outside the window and the user is not dragging, re-center.
  const [followed, setFollowed] = useState(position);
  if (followed !== position) {
    setFollowed(position);
    if (!interacting && position >= bounds.start && position <= bounds.end && (position < view.start || position > view.end)) {
      setView(centerView(view, position, bounds));
    }
  }

  // Wheel needs a non-passive listener to prevent the page from scrolling while zooming.
  useEffect(() => {
    const els = [rulerEl.current, rowsEl.current].filter((e): e is HTMLDivElement => !!e);
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      const rect = rulerEl.current!.getBoundingClientRect();
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
    els.forEach((el) => el.addEventListener("wheel", onWheel, { passive: false }));
    return () => els.forEach((el) => el.removeEventListener("wheel", onWheel));
  }, [bounds]);

  const eventsByCamera = useMemo(() => {
    const map = new Map<string, TimelineEvent[]>();
    for (const e of events) {
      if (!e.cameraId) continue;
      const list = map.get(e.cameraId);
      if (list) list.push(e);
      else map.set(e.cameraId, [e]);
    }
    return map;
  }, [events]);

  const rows = useMemo<RowData[]>(
    () =>
      cameras.map((cam) => {
        const items = eventsByCamera.get(cam.id) ?? [];
        return { cam, items, clusters: clusterItems(items, view, Math.max(width, 1), CLUSTER_PX) };
      }),
    [cameras, eventsByCamera, view, width],
  );

  const shownPosition = scrub ?? position;

  const setupCanvas = (el: HTMLCanvasElement, height: number) => {
    const ctx = el.getContext("2d");
    if (!ctx) return null;
    const dpr = window.devicePixelRatio || 1;
    el.width = Math.round(width * dpr);
    el.height = Math.round(height * dpr);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, width, height);
    return ctx;
  };

  // Ruler: ticks, labels and the playhead handle.
  useEffect(() => {
    const el = rulerCanvas.current;
    const host = rulerEl.current;
    if (!el || !host || width <= 0) return;
    const ctx = setupCanvas(el, RULER_H);
    if (!ctx) return;
    const muted = cssVar(host, "--text-secondary", "#999");
    const ink = cssVar(host, "--text-primary", "#fff");
    const line = cssVar(host, "--border-default", "#444");
    const x = (t: number) => timeToX(t, view, width);
    const step = tickStep(view.end - view.start, width);
    ctx.font = "10px ui-monospace, monospace";
    ctx.textBaseline = "top";
    ctx.textAlign = "center";
    for (const t of ticks(view, step)) {
      const tx = Math.round(x(t)) + 0.5;
      ctx.strokeStyle = line;
      ctx.beginPath();
      ctx.moveTo(tx, RULER_H - 4);
      ctx.lineTo(tx, RULER_H);
      ctx.stroke();
      ctx.fillStyle = muted;
      ctx.fillText(formatTick(t, step), Math.min(Math.max(tx, 16), width - 16), 3);
    }
    const px = Math.round(x(shownPosition)) + 0.5;
    if (px >= 0 && px <= width) {
      ctx.fillStyle = ink;
      ctx.beginPath();
      ctx.moveTo(px - 5, RULER_H - 8);
      ctx.lineTo(px + 5, RULER_H - 8);
      ctx.lineTo(px, RULER_H - 1);
      ctx.fill();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, width, shownPosition]);

  // Rows: gridlines, coverage bars, detections and the playhead.
  useEffect(() => {
    const el = rowsCanvas.current;
    const host = rowsEl.current;
    if (!el || !host || width <= 0) return;
    const ctx = setupCanvas(el, rowsH);
    if (!ctx) return;
    const accent = cssVar(host, "--accent", "#3b82f6");
    const line = cssVar(host, "--border-default", "#444");
    const ink = cssVar(host, "--text-primary", "#fff");
    const x = (t: number) => timeToX(t, view, width);

    const step = tickStep(view.end - view.start, width);
    ctx.strokeStyle = line;
    ctx.globalAlpha = 0.35;
    for (const t of ticks(view, step)) {
      const tx = Math.round(x(t)) + 0.5;
      ctx.beginPath();
      ctx.moveTo(tx, 0);
      ctx.lineTo(tx, rowsH);
      ctx.stroke();
    }
    ctx.globalAlpha = 1;

    // Future (nothing recorded yet) is dimmed.
    if (liveNow < view.end) {
      const from = Math.max(0, x(liveNow));
      ctx.fillStyle = line;
      ctx.globalAlpha = 0.25;
      ctx.fillRect(from, 0, width - from, rowsH);
      ctx.globalAlpha = 1;
    }

    rows.forEach(({ cam, items, clusters }, i) => {
      const top = i * ROW_H;
      if (cam.id === selectedId) {
        ctx.fillStyle = accent;
        ctx.globalAlpha = 0.1;
        ctx.fillRect(0, top, width, ROW_H);
        ctx.globalAlpha = 1;
      }
      ctx.strokeStyle = line;
      ctx.globalAlpha = 0.4;
      ctx.beginPath();
      ctx.moveTo(0, top + ROW_H - 0.5);
      ctx.lineTo(width, top + ROW_H - 0.5);
      ctx.stroke();
      ctx.globalAlpha = 1;

      // Coverage.
      ctx.fillStyle = accent;
      ctx.globalAlpha = 0.4;
      for (const s of growSpans(cam, liveNow)) {
        if (s.end < view.start || s.start > view.end) continue;
        const x0 = Math.max(0, x(s.start));
        const x1 = Math.min(width, x(s.end));
        ctx.fillRect(x0, top + (ROW_H - BAR_H) / 2, Math.max(1, x1 - x0), BAR_H);
      }
      ctx.globalAlpha = 1;

      // Detections on top, colored by category; clusters are wider pills with their count.
      for (const c of clusters) {
        const [x0, x1] = clusterExtent(c, view, width, markWidth(c));
        const color = CATEGORY_COLOR[dominant(items, c)];
        const y = top + (ROW_H - MARK_H) / 2;
        ctx.fillStyle = color;
        ctx.globalAlpha = 0.95;
        ctx.beginPath();
        ctx.roundRect(x0, y, x1 - x0, MARK_H, 2);
        ctx.fill();
        ctx.globalAlpha = 1;
        const glow = glowOf(items, c, born.current, performance.now());
        if (glow > 0) {
          ctx.strokeStyle = color;
          ctx.globalAlpha = glow * 0.8;
          ctx.lineWidth = 2;
          ctx.beginPath();
          ctx.roundRect(x0 - 2, y - 2, x1 - x0 + 4, MARK_H + 4, 4);
          ctx.stroke();
          ctx.lineWidth = 1;
          ctx.globalAlpha = 1;
        }
        if (c.count > 1 && x1 - x0 >= 14) {
          ctx.fillStyle = "#000";
          ctx.font = "bold 9px ui-monospace, monospace";
          ctx.textAlign = "center";
          ctx.textBaseline = "middle";
          ctx.fillText(c.count > 99 ? "99+" : String(c.count), (x0 + x1) / 2, y + MARK_H / 2 + 0.5);
        }
      }
    });

    // Live edge.
    const lx = Math.round(x(liveNow)) + 0.5;
    if (lx >= 0 && lx <= width) {
      ctx.strokeStyle = cssVar(host, "--success", "#21b45b");
      ctx.globalAlpha = 0.7;
      ctx.beginPath();
      ctx.moveTo(lx, 0);
      ctx.lineTo(lx, rowsH);
      ctx.stroke();
      ctx.globalAlpha = 1;
    }

    const px = Math.round(x(shownPosition)) + 0.5;
    if (px >= 0 && px <= width) {
      ctx.strokeStyle = ink;
      ctx.lineWidth = 2;
      ctx.beginPath();
      ctx.moveTo(px, 0);
      ctx.lineTo(px, rowsH);
      ctx.stroke();
      ctx.lineWidth = 1;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, width, rowsH, rows, shownPosition, liveNow, selectedId, frame]);

  // Tooltip: follows the pointer, clamped to the viewport, above the pointer unless there is no room.
  useLayoutEffect(() => {
    const el = tipEl.current;
    if (!tip || !el) {
      setTipPos(null);
      return;
    }
    const w = el.offsetWidth;
    const h = el.offsetHeight;
    const left = Math.min(Math.max(tip.x + 12, 8), Math.max(window.innerWidth - w - 8, 8));
    const top = tip.y - h - 12 >= 8 ? tip.y - h - 12 : Math.min(tip.y + 16, Math.max(window.innerHeight - h - 8, 8));
    setTipPos({ left, top });
  }, [tip]);

  const localX = (event: React.PointerEvent) => event.clientX - event.currentTarget.getBoundingClientRect().left;
  const nearPlayhead = (px: number) => Math.abs(px - timeToX(position, view, width)) <= HANDLE_PX;
  const clampTime = (t: number) => Math.min(Math.max(t, bounds.start), bounds.end);

  const hitAt = (event: React.PointerEvent<HTMLDivElement>, px: number): { row: RowData; cluster: ItemCluster } | undefined => {
    if (event.currentTarget !== rowsEl.current) return undefined;
    const y = event.clientY - event.currentTarget.getBoundingClientRect().top;
    const row = rows[Math.floor(y / ROW_H)];
    if (!row) return undefined;
    const cluster = findClusterAt(row.clusters, view, width, px, PILL_MIN_PX, 2);
    return cluster && { row, cluster };
  };

  const onPointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || width <= 0) return;
    const px = localX(event);
    drag.current = event.shiftKey || nearPlayhead(px) ? { kind: "scrub", startX: px, moved: false } : { kind: "pan", startX: px, view, moved: false };
    event.currentTarget.setPointerCapture?.(event.pointerId);
    setTip(null);
    setInteracting(true);
  };

  const onPointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    const px = localX(event);
    const d = drag.current;
    if (!d) {
      const hit = hitAt(event, px);
      setTip(hit ? { x: event.clientX, y: event.clientY, ...hit } : null);
      return;
    }
    if (Math.abs(px - d.startX) > CLICK_SLOP_PX) d.moved = true;
    if (!d.moved) return;
    if (d.kind === "pan") {
      const span = d.view.end - d.view.start;
      setView(panView(d.view, ((d.startX - px) / width) * span, bounds));
    } else {
      setScrub(clampTime(xToTime(px, view, width)));
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
      onSeek(Math.floor(clampTime(xToTime(px, view, width))));
    } else if (!d.moved) {
      const hit = hitAt(event, px);
      if (!hit) {
        onSeek(Math.floor(clampTime(xToTime(px, view, width))));
      } else {
        const first = hit.row.items[hit.cluster.from];
        onSeek(Math.floor(hit.cluster.time), first?.id ? { ...first, cameraId: hit.row.cam.id } : undefined);
      }
    }
  };

  const zoomBy = (factor: number) => setView((v) => zoomView(v, shownPosition >= v.start && shownPosition <= v.end ? shownPosition : (v.start + v.end) / 2, factor, bounds));
  const spanLabel = (() => {
    const s = view.end - view.start;
    return s >= 3600 ? `${Math.round((s / 3600) * 10) / 10} h` : s >= 60 ? `${Math.round(s / 60)} min` : `${Math.round(s)} s`;
  })();

  const tipContent = tip && (() => {
    const { row, cluster } = tip;
    const first = row.items[cluster.from]!;
    if (cluster.count === 1) {
      const cat = detectionCategory(first.label ?? "");
      const Icon = CATEGORY_ICON[cat];
      const duration = first.end !== undefined ? durationText(first.end - first.time) : "";
      return (
        <>
          <div className="flex items-center gap-1.5 font-medium">
            <Icon className="size-3.5 shrink-0" style={{ color: CATEGORY_COLOR[cat] }} aria-hidden />
            {detectionTitle(first.label ?? "", first.detail)}
          </div>
          <div className="text-muted">
            {clock(first.time)}
            {duration && ` · ${duration}`}
          </div>
          <div className="text-muted">{row.cam.name}</div>
        </>
      );
    }
    const counts = new Map<string, number>();
    for (let i = cluster.from; i < cluster.to; i++) {
      const l = row.items[i]!.label ?? "";
      counts.set(l, (counts.get(l) ?? 0) + 1);
    }
    const sorted = [...counts.entries()].sort((a, b) => b[1] - a[1]);
    return (
      <>
        <div className="font-medium">{cluster.count} detecciones</div>
        <ul className="flex flex-col gap-0.5">
          {sorted.slice(0, 5).map(([label, n]) => {
            const cat = detectionCategory(label);
            const Icon = CATEGORY_ICON[cat];
            return (
              <li key={label} className="flex items-center gap-1.5">
                <Icon className="size-3.5 shrink-0" style={{ color: CATEGORY_COLOR[cat] }} aria-hidden />
                <span>{label ? labelName(label) : "Evento"}</span>
                <span className="ml-auto pl-3 font-mono text-muted">×{n}</span>
              </li>
            );
          })}
          {sorted.length > 5 && <li className="text-muted">+{sorted.length - 5} más</li>}
        </ul>
        <div className="text-muted">
          {clock(cluster.time)}
          {cluster.endTime - cluster.time >= 1 && ` – ${clock(cluster.endTime)}`}
        </div>
        <div className="text-muted">{row.cam.name}</div>
      </>
    );
  })();

  const handlers = {
    onPointerDown,
    onPointerMove,
    onPointerUp,
    onPointerCancel: () => {
      drag.current = null;
      setInteracting(false);
      setScrub(null);
    },
    onPointerLeave: () => setTip(null),
  };

  return (
    <div className="flex min-h-0 flex-col gap-1">
      <div className="flex items-center justify-between gap-2 text-xs text-muted">
        <span>Ventana: {spanLabel}</span>
        <span className="flex items-center gap-1">
          <Button aria-label="Alejar" title="Alejar" className="px-2 py-1" onClick={() => zoomBy(1 / 1.6)} disabled={view.end - view.start >= bounds.end - bounds.start}>
            <ZoomOut className="size-4" aria-hidden />
          </Button>
          <Button aria-label="Acercar" title="Acercar" className="px-2 py-1" onClick={() => zoomBy(1.6)} disabled={view.end - view.start <= MIN_SPAN_S}>
            <ZoomIn className="size-4" aria-hidden />
          </Button>
          <Button aria-label="Ajustar a todo el rango" title="Ajustar a todo el rango" className="px-2 py-1" onClick={() => setView(bounds)}>
            <Maximize className="size-4" aria-hidden />
          </Button>
        </span>
      </div>
      <div className="flex min-h-0 flex-col overflow-hidden rounded border border-line bg-raised">
        <div className="flex shrink-0 border-b border-line">
          <div style={{ width: LABEL_W }} className="shrink-0 border-r border-line" aria-hidden />
          <div
            ref={rulerEl}
            role="slider"
            tabIndex={0}
            aria-label="Línea de tiempo del día"
            aria-valuemin={bounds.start}
            aria-valuemax={bounds.end}
            aria-valuenow={Math.round(shownPosition)}
            aria-valuetext={formatTick(shownPosition, 1)}
            className="relative min-w-0 flex-1 cursor-crosshair touch-none select-none"
            style={{ height: RULER_H }}
            {...handlers}
          >
            <canvas ref={rulerCanvas} className="absolute inset-0 size-full" aria-hidden />
            {width > 0 && liveNow >= view.start && liveNow <= view.end && (
              <span className="pointer-events-none absolute bottom-0.5 size-2 -translate-x-1/2" style={{ left: timeToX(liveNow, view, width) }} title="Ahora" aria-hidden>
                <span className="absolute inset-0 rounded-full bg-ok motion-safe:animate-ping opacity-60" />
                <span className="absolute inset-0 rounded-full bg-ok" />
              </span>
            )}
          </div>
        </div>
        <div className="flex max-h-[24vh] min-h-0 overflow-y-auto" data-testid="timeline-rows">
          <div style={{ width: LABEL_W }} className="shrink-0 border-r border-line">
            {cameras.map((cam) => (
              <button
                key={cam.id}
                type="button"
                title={cam.name}
                aria-label={`Seleccionar ${cam.name}`}
                aria-current={cam.id === selectedId ? "true" : undefined}
                onClick={() => onSelectCamera?.(cam.id)}
                style={{ height: ROW_H }}
                className={`block w-full truncate border-b border-line/40 px-2 text-left text-[11px] hover:text-ink ${cam.id === selectedId ? "bg-accent/10 font-medium text-ink" : "text-muted"}`}
              >
                {cam.name}
              </button>
            ))}
          </div>
          <div ref={rowsEl} className="relative min-w-0 flex-1 cursor-crosshair touch-none select-none" style={{ height: rowsH }} {...handlers}>
            <canvas ref={rowsCanvas} className={`absolute inset-0 size-full ${tip ? "cursor-pointer" : ""}`} aria-hidden />
          </div>
        </div>
      </div>
      {tip &&
        createPortal(
          <div
            ref={tipEl}
            role="tooltip"
            aria-live="polite"
            className="pointer-events-none fixed z-50 flex w-max max-w-64 flex-col gap-0.5 rounded-lg border border-line bg-surface px-2.5 py-1.5 text-xs shadow-lg"
            style={{ left: tipPos?.left ?? -9999, top: tipPos?.top ?? -9999 }}
          >
            {tipContent}
          </div>,
          document.body,
        )}
    </div>
  );
}
