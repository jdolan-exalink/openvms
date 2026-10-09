import { useEffect, useRef, useState, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent, type ReactNode, type RefObject } from "react";
import { cn } from "@/lib/cn";
import { IDENTITY_ZOOM, panBy, pinchZoom, wheelPixels, zoomAtPoint, zoomTransform, type DigitalZoom } from "@/lib/live/digitalZoom";
import { TouchDoubleTap } from "@/lib/touch";
import { useIsPhoneDevice } from "@/lib/useIsPhoneDevice";

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

const touchPoint = (event: { clientX: number; clientY: number }) => ({ x: event.clientX, y: event.clientY });

/**
 * ZoomFrame is the Avigilon-style digital zoom: the wheel zooms toward the cursor and,
 * once zoomed, dragging with a hand pans the picture. The transform is on a wrapper.
 * On touch (not on phones, which have their own Live) two fingers pinch-zoom around their midpoint,
 * one finger pans once zoomed and a double tap resets. At 1x a single finger is left alone so the
 * page scrolls and tiles can be dragged (`touch-action: pan-y`); gestures are captured only while
 * two fingers are down or the picture is zoomed (`touch-action: none`).
 */
export function ZoomFrame({ frameRef, stageRef, resetKey, className, onZoom, picture, scalePicture = true, children }: ZoomFrameProps) {
  const [frameEl, setFrameEl] = useState<HTMLDivElement | null>(null);
  const [zoom, setZoom] = useState<DigitalZoom>(IDENTITY_ZOOM);
  const [dragging, setDragging] = useState(false);
  const [trackedKey, setTrackedKey] = useState(resetKey);
  const zoomRef = useRef(zoom);
  const drag = useRef<{ id: number; x: number; y: number } | null>(null);
  const phone = useIsPhoneDevice();
  const touchGestures = !phone;
  const [pinching, setPinching] = useState(false);
  const pointers = useRef(new Map<number, { x: number; y: number }>());
  const pinch = useRef<{ distance: number; mid: { x: number; y: number } } | null>(null);
  const [tap] = useState(() => new TouchDoubleTap<ReactPointerEvent<HTMLDivElement>>());
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

  const applyZoom = (next: DigitalZoom) => {
    zoomRef.current = next;
    setZoom(next);
  };

  useEffect(() => {
    tap.listen((event) => {
      if (zoomRef.current.scale <= 1) return;
      event.stopPropagation();
      applyZoom(IDENTITY_ZOOM);
    });
  });

  const pinchGeometry = () => {
    const [a, b] = [...pointers.current.values()];
    const rect = frameEl?.getBoundingClientRect();
    if (!a || !b || !rect) return null;
    return { rect, distance: Math.hypot(b.x - a.x, b.y - a.y), mid: { x: (a.x + b.x) / 2 - rect.left, y: (a.y + b.y) / 2 - rect.top } };
  };

  const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement | null;
    const touch = touchGestures && event.pointerType === "touch";
    if (touch && !target?.closest("button, a, input, textarea")) {
      tap.handlers.onPointerDown(event);
      pointers.current.set(event.pointerId, touchPoint(event));
      if (pointers.current.size === 2) {
        const geometry = pinchGeometry();
        if (geometry) {
          tap.handlers.onPointerCancel(event);
          drag.current = null;
          pinch.current = { distance: geometry.distance, mid: geometry.mid };
          setDragging(false);
          setPinching(true);
          event.preventDefault();
          event.stopPropagation();
          for (const id of pointers.current.keys()) event.currentTarget.setPointerCapture?.(id);
          return;
        }
      }
    }
    if (event.button !== 0 || zoomRef.current.scale <= 1) return;
    if (target?.closest("button, a, input, textarea")) return;
    event.preventDefault();
    event.stopPropagation();
    event.currentTarget.setPointerCapture?.(event.pointerId);
    drag.current = { id: event.pointerId, x: event.clientX, y: event.clientY };
    setDragging(true);
  };

  const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (pointers.current.has(event.pointerId)) pointers.current.set(event.pointerId, touchPoint(event));
    const gesture = pinch.current;
    if (gesture) {
      const geometry = pinchGeometry();
      if (!geometry) return;
      applyZoom(pinchZoom(zoomRef.current, geometry.rect.width, geometry.rect.height, gesture.mid, geometry.mid, gesture.distance, geometry.distance));
      pinch.current = { distance: geometry.distance, mid: geometry.mid };
      return;
    }
    const current = drag.current;
    if (!current || current.id !== event.pointerId || !frameEl) return;
    const rect = frameEl.getBoundingClientRect();
    const dx = event.clientX - current.x;
    const dy = event.clientY - current.y;
    current.x = event.clientX;
    current.y = event.clientY;
    applyZoom(panBy(zoomRef.current, rect.width, rect.height, dx, dy));
  };

  const endPointer = (event: ReactPointerEvent<HTMLDivElement>) => {
    pointers.current.delete(event.pointerId);
    if (pinch.current && pointers.current.size < 2) {
      pinch.current = null;
      setPinching(false);
      // The finger that stays down keeps panning if the picture is still zoomed.
      const [rest] = [...pointers.current.entries()];
      if (rest && zoomRef.current.scale > 1) {
        drag.current = { id: rest[0], x: rest[1].x, y: rest[1].y };
        setDragging(true);
      }
      return;
    }
    if (!drag.current || drag.current.id !== event.pointerId) return;
    drag.current = null;
    setDragging(false);
  };

  const onPointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    tap.handlers.onPointerUp(event);
    endPointer(event);
  };

  const onPointerCancel = (event: ReactPointerEvent<HTMLDivElement>) => {
    tap.handlers.onPointerCancel(event);
    endPointer(event);
  };

  const onDoubleClick = (event: ReactMouseEvent<HTMLDivElement>) => {
    // A touch double tap already reset (or toggled) things; swallow the click the browser may synthesize after it.
    if (zoomRef.current.scale <= 1 && !tap.justHandled()) return;
    event.preventDefault();
    event.stopPropagation();
    applyZoom(IDENTITY_ZOOM);
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
      style={zoomed || pinching ? { touchAction: "none" } : touchGestures ? { touchAction: "pan-y" } : undefined}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerCancel}
      // A zoomed picture pans with one finger, so a held touch must not also start a tile drag.
      onTouchStart={touchGestures && (zoomed || pinching) ? (event) => event.stopPropagation() : undefined}
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
