import { useEffect, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode, type RefObject } from "react";
import { cn } from "@/lib/cn";
import { IDENTITY_ZOOM, panBy, wheelPixels, zoomAtPoint, zoomTransform, type DigitalZoom } from "@/lib/live/digitalZoom";

type ZoomFrameProps = {
  /** Element measured for the wheel point and for intersection observers. */
  frameRef?: RefObject<HTMLDivElement | null>;
  /** Empty box the live `<video>` is attached to. It receives the zoom transform. */
  stageRef?: RefObject<HTMLDivElement | null>;
  /** Changing this (another camera, another recording) clears the zoom. */
  resetKey: string;
  className?: string;
  /** Notified whenever the zoom changes, including a reset to 1× on unmount. */
  onZoom?: (zoom: DigitalZoom) => void;
  /**
   * Picture rendered inside the transformed stage (recordings).
   * Live video is attached imperatively to `stageRef` instead.
   */
  picture?: (zoom: DigitalZoom) => ReactNode;
  /** When false, nothing local is scaled (the surface layer scales its own stage). */
  scalePicture?: boolean;
  children?: ReactNode;
};

/**
 * ZoomFrame is the Avigilon-style digital zoom: the wheel zooms toward the cursor and,
 * once zoomed, dragging with a hand pans the picture. The transform is on a wrapper.
 */
export function ZoomFrame({ frameRef, stageRef, resetKey, className, onZoom, picture, scalePicture = true, children }: ZoomFrameProps) {
  const [frameEl, setFrameEl] = useState<HTMLDivElement | null>(null);
  const [zoom, setZoom] = useState<DigitalZoom>(IDENTITY_ZOOM);
  const [dragging, setDragging] = useState(false);
  const [trackedKey, setTrackedKey] = useState(resetKey);
  const zoomRef = useRef(zoom);
  const drag = useRef<{ id: number; x: number; y: number } | null>(null);
  const onZoomRef = useRef(onZoom);
  if (trackedKey !== resetKey) {
    setTrackedKey(resetKey);
    setZoom(IDENTITY_ZOOM);
  }

  useEffect(() => {
    zoomRef.current = zoom;
  }, [zoom]);

  useEffect(() => {
    onZoomRef.current = onZoom;
  }, [onZoom]);

  useEffect(() => {
    onZoomRef.current?.(zoom);
  }, [zoom]);

  useEffect(() => {
    return () => onZoomRef.current?.(IDENTITY_ZOOM);
  }, []);

  useEffect(() => {
    if (!frameEl) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      event.stopPropagation();
      const rect = frameEl.getBoundingClientRect();
      const next = zoomAtPoint(
        zoomRef.current,
        rect.width,
        rect.height,
        event.clientX - rect.left,
        event.clientY - rect.top,
        wheelPixels(event.deltaY, event.deltaMode),
      );
      setZoom(next);
    };
    frameEl.addEventListener("wheel", onWheel, { passive: false });
    return () => frameEl.removeEventListener("wheel", onWheel);
  }, [frameEl]);

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || zoomRef.current.scale <= 1) return;
    const target = event.target as HTMLElement | null;
    if (target?.closest("button, a, input, textarea")) return;
    event.preventDefault();
    event.stopPropagation();
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = { id: event.pointerId, x: event.clientX, y: event.clientY };
    setDragging(true);
  };

  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const current = drag.current;
    if (!current || current.id !== event.pointerId || !frameEl) return;
    const rect = frameEl.getBoundingClientRect();
    const dx = event.clientX - current.x;
    const dy = event.clientY - current.y;
    current.x = event.clientX;
    current.y = event.clientY;
    setZoom(panBy(zoomRef.current, rect.width, rect.height, dx, dy));
  };

  const endDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!drag.current || drag.current.id !== event.pointerId) return;
    drag.current = null;
    setDragging(false);
  };

  const onDoubleClick = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (zoomRef.current.scale <= 1) return;
    event.preventDefault();
    event.stopPropagation();
    setZoom(IDENTITY_ZOOM);
  };

  const zoomed = zoom.scale > 1;
  const stageStyle = zoomed ? { transform: zoomTransform(zoom), transformOrigin: "0 0" } : undefined;

  return (
    <div
      ref={(node) => {
        if (frameRef) frameRef.current = node;
        setFrameEl((current) => (current === node ? current : node));
      }}
      className={cn("relative overflow-hidden bg-black", zoomed && (dragging ? "cursor-grabbing select-none" : "cursor-grab"), className)}
      style={zoomed ? { touchAction: "none" } : undefined}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      onDoubleClick={onDoubleClick}
    >
      {scalePicture && (
        <div ref={stageRef} className="absolute inset-0" style={stageStyle}>
          {picture?.(zoom)}
        </div>
      )}
      {children}
      {zoomed && (
        <button
          type="button"
          title="Restablecer zoom"
          aria-label={`Zoom ${zoom.scale.toFixed(1)}. Restablecer`}
          className="absolute bottom-1.5 right-1.5 z-[3] rounded bg-black/70 px-1.5 py-0.5 text-[11px] tabular-nums text-white hover:bg-white/20"
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => {
            event.stopPropagation();
            setZoom(IDENTITY_ZOOM);
          }}
        >
          {zoom.scale.toFixed(1)}×
        </button>
      )}
    </div>
  );
}
