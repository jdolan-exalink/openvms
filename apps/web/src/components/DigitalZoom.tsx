import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  type RefObject,
} from "react";
import { EnhancementControl } from "@/components/EnhancementControl";
import { cn } from "@/lib/cn";
import { extractRoiRect } from "@/lib/enhancement/analysis";
import { ImageEnhancementEngineImpl } from "@/lib/enhancement/engine";
import {
  DEFAULT_ADJUSTMENTS,
  type EnhancementAdjustments,
  type EnhancementMode,
} from "@/lib/enhancement/types";
import { generateCssFilterString } from "@/lib/enhancement/webgl";
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
   * Zoom owned by the caller (the live tile), so every quality layer of one tile shows the same
   * zoom/pan. When set, `resetKey` no longer clears it: the owner resets it.
   */
  zoom?: DigitalZoom;
  /** Called with each zoom the user produces when `zoom` is controlled. */
  onZoomChange?: (zoom: DigitalZoom) => void;
  /**
   * Picture rendered inside the transformed stage (recordings).
   * Live video is attached imperatively to `stageRef` instead.
   */
  picture?: (zoom: DigitalZoom) => ReactNode;
  /** When false, nothing local is scaled (the surface layer scales its own stage). */
  scalePicture?: boolean;
  /**
   * Picture element sampled by the auto enhancement when the video is not inside this frame
   * (the surface layer hosts it). Defaults to the first `<video>`/`<canvas>` in the stage.
   */
  mediaSource?: () => HTMLVideoElement | HTMLCanvasElement | null;
  /** Receives the CSS filter of the current enhancement ("none" when neutral), for pictures hosted elsewhere. */
  onEnhance?: (filter: string) => void;
  /** Enable dynamic visual enhancement engine and UI controls. Defaults to true. */
  enhanceable?: boolean;
  children?: ReactNode;
};

/** locateMedia finds the picture to sample: the supplied element, else the first video/canvas in the stage. */
function locateMedia(source: (() => HTMLVideoElement | HTMLCanvasElement | null) | undefined, stage: HTMLElement): HTMLVideoElement | HTMLCanvasElement | null {
  return source?.() ?? stage.querySelector<HTMLVideoElement | HTMLCanvasElement>("video, canvas");
}

const touchPoint = (event: { clientX: number; clientY: number }) => ({ x: event.clientX, y: event.clientY });

/**
 * ZoomFrame is the Avigilon-style digital zoom: the wheel zooms toward the cursor and,
 * once zoomed, dragging with a hand pans the picture. The transform is on a wrapper.
 * On touch (not on phones, which have their own Live) two fingers pinch-zoom around their midpoint,
 * one finger pans once zoomed and a double tap resets. At 1x a single finger is left alone so the
 * page scrolls and tiles can be dragged (`touch-action: pan-y`); gestures are captured only while
 * two fingers are down or the picture is zoomed (`touch-action: none`).
 */
export function ZoomFrame({
  frameRef,
  stageRef,
  resetKey,
  className,
  onZoom,
  zoom: controlledZoom,
  onZoomChange,
  picture,
  scalePicture = true,
  mediaSource,
  onEnhance,
  enhanceable = true,
  children,
}: ZoomFrameProps) {
  const [frameEl, setFrameEl] = useState<HTMLDivElement | null>(null);
  const [ownZoom, setOwnZoom] = useState<DigitalZoom>(IDENTITY_ZOOM);
  const controlled = controlledZoom !== undefined;
  const zoom = controlledZoom ?? ownZoom;
  const onZoomChangeRef = useRef(onZoomChange);
  useEffect(() => {
    onZoomChangeRef.current = onZoomChange;
  }, [onZoomChange]);
  const setZoom = useCallback((next: DigitalZoom) => {
    setOwnZoom(next);
    onZoomChangeRef.current?.(next);
  }, []);
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

  // Dynamic visual enhancement engine
  const [engine] = useState(() => new ImageEnhancementEngineImpl());
  const [enhancementMode, setEnhancementMode] = useState<EnhancementMode>("auto");
  const [adjustments, setAdjustments] = useState<EnhancementAdjustments>(() => engine.getAdjustments());

  useEffect(() => {
    return () => {
      engine.destroy();
    };
  }, [engine]);

  const handleModeChange = useCallback((nextMode: EnhancementMode) => {
    engine.setMode(nextMode);
    setEnhancementMode(nextMode);
    if (nextMode === "off") {
      setAdjustments({ ...DEFAULT_ADJUSTMENTS });
    } else {
      setAdjustments({ ...engine.getAdjustments() });
    }
  }, [engine]);

  const handleAdjustmentsChange = useCallback((patch: Partial<EnhancementAdjustments>) => {
    engine.setManualAdjustments(patch);
    setAdjustments({ ...engine.getAdjustments() });
  }, [engine]);

  const handleReset = useCallback(() => {
    engine.reset();
    setEnhancementMode(engine.mode);
    setAdjustments({ ...engine.getAdjustments() });
  }, [engine]);

  const mediaSourceRef = useRef(mediaSource);
  useEffect(() => {
    mediaSourceRef.current = mediaSource;
  }, [mediaSource]);

  const updateEnhancements = useCallback((currentZoom: DigitalZoom) => {
    if (!enhanceable || enhancementMode === "off") return;
    const el = frameEl;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const roi = extractRoiRect(currentZoom, rect.width, rect.height);
    const mediaEl = locateMedia(mediaSourceRef.current, stageRef?.current ?? el);
    const now = typeof performance !== "undefined" ? performance.now() : Date.now();
    if (mediaEl) {
      const adj = engine.update(mediaEl, roi, now);
      setAdjustments({ ...adj });
    } else {
      setAdjustments({ ...engine.getAdjustments() });
    }
  }, [enhanceable, enhancementMode, engine, frameEl, stageRef]);

  // Recalculate ROI and adjustments when zoom or configuration changes
  useEffect(() => {
    updateEnhancements(zoom);
  }, [zoom, updateEnhancements]);

  // Throttled RAF loop for live frame smoothing/updates in auto/manual mode.
  // In grid views (1, 4, 9, 16 cameras), background tiles (1x zoom) poll at 600ms,
  // while active zoomed tiles poll at 150ms. Re-renders are skipped if values are unchanged.
  useEffect(() => {
    if (!enhanceable || enhancementMode === "off") return;
    let rafId: number;
    let lastTime = 0;

    const hasSignificantChange = (prev: EnhancementAdjustments, next: EnhancementAdjustments): boolean => {
      return (
        Math.abs(prev.brightness - next.brightness) >= 0.5 ||
        Math.abs(prev.contrast - next.contrast) >= 0.5 ||
        Math.abs(prev.gamma - next.gamma) >= 0.02 ||
        Math.abs(prev.saturation - next.saturation) >= 0.5 ||
        Math.abs(prev.sharpness - next.sharpness) >= 0.5 ||
        Math.abs(prev.blackLevel - next.blackLevel) >= 0.005 ||
        Math.abs(prev.whiteLevel - next.whiteLevel) >= 0.005 ||
        Math.abs(prev.temperature - next.temperature) >= 0.5
      );
    };

    const tick = (now: number) => {
      const minInterval = zoomRef.current.scale > 1 ? 150 : 600;
      if (now - lastTime >= minInterval) {
        lastTime = now;
        const el = frameEl;
        if (el) {
          const rect = el.getBoundingClientRect();
          const roi = extractRoiRect(zoomRef.current, rect.width, rect.height);
          const mediaEl = locateMedia(mediaSourceRef.current, stageRef?.current ?? el);
          if (mediaEl) {
            const adj = engine.update(mediaEl, roi, now);
            setAdjustments((prev) => (hasSignificantChange(prev, adj) ? { ...adj } : prev));
          }
        }
      }
      rafId = requestAnimationFrame(tick);
    };

    rafId = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(rafId);
    };
  }, [enhanceable, enhancementMode, engine, frameEl, stageRef]);

  if (trackedKey !== resetKey) {
    setTrackedKey(resetKey);
    if (!controlled) setOwnZoom(IDENTITY_ZOOM);
  }

  useEffect(() => {
    zoomRef.current = zoom;
  }, [zoom]);

  useEffect(() => {
    onZoomRef.current = onZoom;
  }, [onZoom]);

  // Also runs when `onZoom` changes (another session behind the same frame) so the new target gets the zoom.
  useEffect(() => {
    onZoomRef.current?.(zoom);
  }, [zoom, onZoom]);

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
  }, [frameEl, setZoom]);

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
  const cssFilter = enhanceable ? generateCssFilterString(adjustments) : "none";
  const onEnhanceRef = useRef(onEnhance);
  useEffect(() => {
    onEnhanceRef.current = onEnhance;
  }, [onEnhance]);
  // Also runs when `onEnhance` changes (another session behind the frame) so the new target gets the filter.
  useEffect(() => {
    onEnhanceRef.current?.(cssFilter);
  }, [cssFilter, onEnhance]);
  useEffect(() => {
    return () => onEnhanceRef.current?.("none");
  }, []);
  const stageStyle: React.CSSProperties | undefined =
    zoomed || cssFilter !== "none"
      ? {
          ...(zoomed ? { transform: zoomTransform(zoom), transformOrigin: "0 0" } : {}),
          ...(cssFilter !== "none" ? { filter: cssFilter } : {}),
        }
      : undefined;

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
      {enhanceable && (
        <EnhancementControl
          mode={enhancementMode}
          onModeChange={handleModeChange}
          adjustments={adjustments}
          onAdjustmentsChange={handleAdjustmentsChange}
          onReset={handleReset}
          className="absolute bottom-1.5 left-1.5 z-[3]"
        />
      )}
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
