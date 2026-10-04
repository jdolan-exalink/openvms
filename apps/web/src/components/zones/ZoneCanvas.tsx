import { type PointerEvent as ReactPointerEvent, useEffect, useRef, useState } from "react";
import {
  type Point,
  hitEdge,
  hitVertex,
  insertVertex,
  normalizePoint,
  pointInPolygon,
  translatePolygon,
} from "@/lib/zoneGeometry";
import { type EditorItem, colorOf } from "./zoneDraft";

/**
 * Canvas overlay ink. These sit on top of the camera snapshot, not on a themed surface, so they
 * stay theme-independent named colors: white handles with a black halo read on any frame.
 */
const CANVAS_INK = "white";
const CANVAS_HALO = "black";

const VERTEX_TOL = 12;
const EDGE_TOL = 8;

export interface DrawingState {
  color: string;
  points: Point[];
}

interface Drag {
  type: "vertex" | "polygon";
  index: number;
  start: Point;
  orig: Point[];
}

/**
 * Camera snapshot with an SVG polygon overlay. The image keeps its aspect ratio (object-contain
 * equivalent: the box is fitted to the container) and all interaction is resolved in relative 0-1 space.
 * Dragging is pointer-captured, so it works with mouse, touch and pen.
 */
export function ZoneCanvas({
  items,
  selectedUid,
  hidden,
  drawing,
  selectedVertex,
  snapshotSrc,
  fallbackAspect,
  onNaturalSize,
  onChangePoints,
  onSelect,
  onSelectVertex,
  onRemoveVertex,
  onDrawPoint,
  onDrawClose,
}: {
  items: EditorItem[];
  selectedUid: string | null;
  hidden: Set<string>;
  drawing: DrawingState | null;
  selectedVertex: number | null;
  snapshotSrc: string;
  fallbackAspect: number;
  onNaturalSize: (w: number, h: number) => void;
  onChangePoints: (uid: string, points: Point[]) => void;
  onSelect: (uid: string | null) => void;
  onSelectVertex: (i: number | null) => void;
  onRemoveVertex: (i: number) => void;
  onDrawPoint: (p: Point) => void;
  onDrawClose: () => void;
}) {
  const hostRef = useRef<HTMLDivElement>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const drag = useRef<Drag | null>(null);
  const [box, setBox] = useState({ w: 0, h: 0 });
  const [aspect, setAspect] = useState(fallbackAspect);
  const [cursor, setCursor] = useState<Point | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    const el = hostRef.current;
    if (!el) return;
    const measure = () => setBox({ w: el.clientWidth, h: el.clientHeight });
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Fit the frame inside the container keeping its aspect ratio.
  const fitW = box.w > 0 && box.h > 0 ? Math.min(box.w, box.h * aspect) : 0;
  const fitH = fitW / aspect;
  const size = { width: fitW, height: fitH };

  const rel = (e: { clientX: number; clientY: number }): Point => {
    const r = svgRef.current!.getBoundingClientRect();
    const x = r.width ? (e.clientX - r.left) / r.width : 0;
    const y = r.height ? (e.clientY - r.top) / r.height : 0;
    return { x: Math.min(1, Math.max(0, x)), y: Math.min(1, Math.max(0, y)) };
  };

  const selected = items.find((i) => i.uid === selectedUid && !hidden.has(i.uid)) ?? null;

  function onPointerDown(e: ReactPointerEvent<SVGSVGElement>) {
    if (e.button !== 0) return;
    const p = rel(e);
    if (drawing) {
      if (drawing.points.length >= 3 && hitVertex(drawing.points.slice(0, 1), p, VERTEX_TOL, size) === 0) onDrawClose();
      else onDrawPoint(normalizePoint(p));
      return;
    }
    e.currentTarget.setPointerCapture?.(e.pointerId);
    if (selected) {
      const v = hitVertex(selected.points, p, VERTEX_TOL, size);
      if (v >= 0) {
        onSelectVertex(v);
        drag.current = { type: "vertex", index: v, start: p, orig: selected.points };
        return;
      }
      const edge = hitEdge(selected.points, p, EDGE_TOL, size);
      if (edge) {
        const next = insertVertex(selected.points, edge.index, edge.point);
        onChangePoints(selected.uid, next);
        onSelectVertex(edge.index + 1);
        drag.current = { type: "vertex", index: edge.index + 1, start: p, orig: next };
        return;
      }
      if (pointInPolygon(p, selected.points)) {
        onSelectVertex(null);
        drag.current = { type: "polygon", index: -1, start: p, orig: selected.points };
        return;
      }
    }
    const hit = [...items].reverse().find((i) => !hidden.has(i.uid) && pointInPolygon(p, i.points));
    onSelectVertex(null);
    onSelect(hit?.uid ?? null);
  }

  function onPointerMove(e: ReactPointerEvent<SVGSVGElement>) {
    const p = rel(e);
    if (drawing) {
      setCursor(p);
      return;
    }
    const d = drag.current;
    if (!d || !selected) return;
    if (d.type === "vertex") onChangePoints(selected.uid, d.orig.map((q, i) => (i === d.index ? p : q)));
    else onChangePoints(selected.uid, translatePolygon(d.orig, p.x - d.start.x, p.y - d.start.y));
  }

  function endDrag(e: ReactPointerEvent<SVGSVGElement>) {
    if (drag.current) e.currentTarget.releasePointerCapture?.(e.pointerId);
    drag.current = null;
  }

  const px = (p: Point) => `${p.x * fitW},${p.y * fitH}`;
  const centroid = (pts: Point[]) => ({
    x: (pts.reduce((s, p) => s + p.x, 0) / pts.length) * fitW,
    y: (pts.reduce((s, p) => s + p.y, 0) / pts.length) * fitH,
  });

  return (
    <div ref={hostRef} className="flex size-full items-center justify-center overflow-hidden bg-video">
      <div className="relative" style={{ width: fitW, height: fitH }}>
        {failed && <p className="absolute inset-0 flex items-center justify-center text-sm text-muted">No se pudo cargar la imagen de la cámara.</p>}
        <img
          src={snapshotSrc}
          alt="Imagen actual de la cámara"
          draggable={false}
          className="absolute inset-0 size-full object-contain select-none"
          onLoad={(e) => {
            const { naturalWidth: w, naturalHeight: h } = e.currentTarget;
            setFailed(false);
            if (w && h) {
              setAspect(w / h);
              onNaturalSize(w, h);
            }
          }}
          onError={() => {
            setFailed(true);
            onNaturalSize(0, 0);
          }}
        />
        <svg
          ref={svgRef}
          role="application"
          aria-label="Lienzo de edición de zonas y máscaras"
          width={fitW}
          height={fitH}
          className="absolute inset-0 touch-none"
          style={{ cursor: drawing ? "crosshair" : "default" }}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={endDrag}
          onPointerCancel={endDrag}
          onPointerLeave={() => setCursor(null)}
          onContextMenu={(e) => {
            e.preventDefault();
            if (drawing || !selected) return;
            const v = hitVertex(selected.points, rel(e), VERTEX_TOL, size);
            if (v >= 0) onRemoveVertex(v);
          }}
        >
          {items
            .filter((i) => !hidden.has(i.uid) && i.points.length >= 2)
            .map((i) => {
              const color = colorOf(i);
              const isSel = i.uid === selectedUid;
              const c = centroid(i.points);
              return (
                <g key={i.uid} style={{ pointerEvents: "none" }}>
                  <polygon
                    points={i.points.map(px).join(" ")}
                    fill={color}
                    fillOpacity={isSel ? 0.35 : 0.2}
                    stroke={color}
                    strokeWidth={isSel ? 2.5 : 1.5}
                    strokeDasharray={i.kind === "zone" ? undefined : "6 4"}
                  />
                  <text x={c.x} y={c.y} textAnchor="middle" fontSize="12" fill="white" stroke="black" strokeWidth="3" paintOrder="stroke">
                    {i.name}
                  </text>
                  {isSel &&
                    i.points.map((p, k) => (
                      <circle key={k} cx={p.x * fitW} cy={p.y * fitH} r={k === selectedVertex ? 7 : 5} fill={k === selectedVertex ? CANVAS_INK : color} stroke={CANVAS_HALO} strokeWidth="1.5" />
                    ))}
                </g>
              );
            })}
          {drawing && (
            <g style={{ pointerEvents: "none" }}>
              <polyline
                points={[...drawing.points, ...(cursor ? [cursor] : [])].map(px).join(" ")}
                fill={drawing.color}
                fillOpacity={0.15}
                stroke={drawing.color}
                strokeWidth={2}
              />
              {drawing.points.map((p, k) => (
                <circle key={k} cx={p.x * fitW} cy={p.y * fitH} r={k === 0 ? 8 : 4} fill={k === 0 ? CANVAS_INK : drawing.color} stroke={CANVAS_HALO} strokeWidth="1.5" />
              ))}
            </g>
          )}
        </svg>
      </div>
    </div>
  );
}
