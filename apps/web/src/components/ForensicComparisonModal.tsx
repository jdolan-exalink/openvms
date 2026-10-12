import {
  Check,
  Columns2,
  Copy,
  Download,
  Loader2,
  RotateCcw,
  ShieldCheck,
  Sparkles,
  Split,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/cn";
import type { ForensicProcessOptions, ForensicResult } from "@/lib/enhancement/forensic";
import {
  IDENTITY_ZOOM,
  clampZoom,
  panBy,
  wheelPixels,
  zoomAtPoint,
  zoomTransform,
  type DigitalZoom,
} from "@/lib/live/digitalZoom";
import { useFocusTrap } from "@/lib/useFocusTrap";

export interface ForensicComparisonModalProps {
  isOpen: boolean;
  onClose: () => void;
  result: ForensicResult | null;
  isLoading?: boolean;
  onReprocess?: (options: ForensicProcessOptions) => void;
  cameraName?: string;
  timestamp?: number;
}

type ViewMode = "split" | "side-by-side";

export function ForensicComparisonModal({
  isOpen,
  onClose,
  result,
  isLoading = false,
  onReprocess: _onReprocess,
  cameraName: initialCameraName,
  timestamp: initialTimestamp,
}: ForensicComparisonModalProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  useFocusTrap(containerRef, onClose);

  const [viewMode, setViewMode] = useState<ViewMode>("split");
  const [sliderPos, setSliderPos] = useState<number>(50);
  const [copied, setCopied] = useState<boolean>(false);

  // Digital Zoom & Movement State (identical Avigilon/live behavior)
  const [zoom, setZoom] = useState<DigitalZoom>(IDENTITY_ZOOM);
  const zoomRef = useRef(zoom);
  useEffect(() => {
    zoomRef.current = zoom;
  }, [zoom]);

  const viewerRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{
    id: number;
    x: number;
    y: number;
    mode: "pan" | "slider";
  } | null>(null);
  const [isPanning, setIsPanning] = useState(false);

  // Convert canvases to data URLs for reliable rendering and export
  const originalUrl = useMemo(() => {
    if (!result?.originalCanvas) return "";
    try {
      return result.originalCanvas.toDataURL("image/png") || "data:image/png;base64,mock";
    } catch {
      return "data:image/png;base64,mock";
    }
  }, [result?.originalCanvas]);

  const enhancedUrl = useMemo(() => {
    if (!result?.enhancedCanvas) return "";
    try {
      return result.enhancedCanvas.toDataURL("image/png") || "data:image/png;base64,mock";
    } catch {
      return "data:image/png;base64,mock";
    }
  }, [result?.enhancedCanvas]);

  const applyZoom = useCallback((next: DigitalZoom) => {
    zoomRef.current = next;
    setZoom(next);
  }, []);

  // Mouse wheel zoom (zooms towards cursor point)
  useEffect(() => {
    const el = viewerRef.current;
    if (!el) return;

    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      e.stopPropagation();
      const rect = el.getBoundingClientRect();
      const width = rect.width > 0 ? rect.width : 800;
      const height = rect.height > 0 ? rect.height : 600;
      const next = zoomAtPoint(
        zoomRef.current,
        width,
        height,
        e.clientX - rect.left,
        e.clientY - rect.top,
        wheelPixels(e.deltaY, e.deltaMode),
      );
      applyZoom(next);
    };

    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  }, [applyZoom]);

  const updateSliderFromPointer = useCallback((clientX: number) => {
    const el = viewerRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    if (rect.width <= 0) return;
    const percent = ((clientX - rect.left) / rect.width) * 100;
    setSliderPos(Math.max(5, Math.min(95, percent)));
  }, []);

  // Pointer drag for panning movement and split divider
  const handlePointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    const target = e.target as HTMLElement;
    const isSliderHandle = !!target.closest("[data-testid='slider-handle']");
    const mode =
      isSliderHandle || (viewMode === "split" && zoomRef.current.scale <= 1)
        ? "slider"
        : "pan";

    dragRef.current = { id: e.pointerId, x: e.clientX, y: e.clientY, mode };
    e.currentTarget.setPointerCapture?.(e.pointerId);

    if (mode === "slider") {
      updateSliderFromPointer(e.clientX);
    } else {
      setIsPanning(true);
    }
  };

  const handlePointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    const current = dragRef.current;
    if (!current || current.id !== e.pointerId) return;

    if (current.mode === "slider") {
      updateSliderFromPointer(e.clientX);
    } else {
      const el = viewerRef.current;
      if (!el) return;
      const rect = el.getBoundingClientRect();
      const width = rect.width > 0 ? rect.width : 800;
      const height = rect.height > 0 ? rect.height : 600;
      const dx = e.clientX - current.x;
      const dy = e.clientY - current.y;
      current.x = e.clientX;
      current.y = e.clientY;
      const next = panBy(zoomRef.current, width, height, dx, dy);
      applyZoom(next);
    }
  };

  const handlePointerUp = (e: React.PointerEvent<HTMLDivElement>) => {
    if (dragRef.current && dragRef.current.id === e.pointerId) {
      dragRef.current = null;
      setIsPanning(false);
      try {
        e.currentTarget.releasePointerCapture?.(e.pointerId);
      } catch {
        // Ignored
      }
    }
  };

  // Double click resets zoom and pan back to 1x
  const handleDoubleClick = (e: React.MouseEvent<HTMLDivElement>) => {
    e.preventDefault();
    e.stopPropagation();
    applyZoom(IDENTITY_ZOOM);
  };

  const cameraName =
    result?.metadata?.cameraName || initialCameraName || "Cámara CCTV";
  const timestamp =
    result?.metadata?.timestamp || initialTimestamp || Math.round(Date.now() / 1000);

  const handleDownloadOriginal = () => {
    if (!result?.originalCanvas) return;
    try {
      const dataUrl = result.originalCanvas.toDataURL("image/png");
      const a = document.createElement("a");
      a.href = dataUrl;
      a.download = `${cameraName.toLowerCase().replace(/\s+/g, "_")}_original_${timestamp}.png`;
      a.click();
    } catch {
      // fallback
    }
  };

  const handleDownloadEnhanced = () => {
    if (!result?.enhancedCanvas) return;
    try {
      const dataUrl = result.enhancedCanvas.toDataURL("image/png");
      const a = document.createElement("a");
      a.href = dataUrl;
      a.download = `${cameraName.toLowerCase().replace(/\s+/g, "_")}_mejorada_${timestamp}.png`;
      a.click();
    } catch {
      // fallback
    }
  };

  const handleCopy = async () => {
    if (!result?.enhancedCanvas) return;
    try {
      if (typeof navigator !== "undefined" && navigator.clipboard) {
        result.enhancedCanvas.toBlob(async (blob) => {
          if (blob && "ClipboardItem" in window) {
            await navigator.clipboard.write([
              new ClipboardItem({ "image/png": blob }),
            ]);
            setCopied(true);
            setTimeout(() => setCopied(false), 2000);
          }
        }, "image/png");
      }
    } catch {
      // Clipboard write failed or unpermitted
    }
  };

  const handleZoomIn = () => {
    const el = viewerRef.current;
    const rect = el?.getBoundingClientRect() || { width: 800, height: 600 };
    const width = rect.width > 0 ? rect.width : 800;
    const height = rect.height > 0 ? rect.height : 600;
    const next = clampZoom(
      {
        ...zoomRef.current,
        scale: Math.min(8, Math.round((zoomRef.current.scale + 0.5) * 10) / 10),
      },
      width,
      height,
    );
    applyZoom(next);
  };

  const handleZoomOut = () => {
    const el = viewerRef.current;
    const rect = el?.getBoundingClientRect() || { width: 800, height: 600 };
    const width = rect.width > 0 ? rect.width : 800;
    const height = rect.height > 0 ? rect.height : 600;
    const next = clampZoom(
      {
        ...zoomRef.current,
        scale: Math.max(1, Math.round((zoomRef.current.scale - 0.5) * 10) / 10),
      },
      width,
      height,
    );
    applyZoom(next);
  };

  const handleResetZoom = () => {
    applyZoom(IDENTITY_ZOOM);
  };

  if (!isOpen) return null;
  if (!result && !isLoading) return null;

  const origW = result?.originalCanvas.width ?? 0;
  const origH = result?.originalCanvas.height ?? 0;
  const enhW = result?.enhancedCanvas.width ?? 0;
  const enhH = result?.enhancedCanvas.height ?? 0;

  const transformStyle: React.CSSProperties | undefined =
    zoom.scale > 1
      ? {
          transform: zoomTransform(zoom),
          transformOrigin: "0 0",
        }
      : undefined;

  const dialog = (
    <div
      className="fixed inset-0 z-[90] flex items-center justify-center bg-black/80 backdrop-blur-sm p-3 md:p-6"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={containerRef}
        role="dialog"
        aria-modal="true"
        aria-label="Mejora de Detalle Forense"
        className="flex max-h-[96vh] w-full max-w-6xl flex-col gap-3 rounded-m3-2xl bg-surface-1 border border-outline-variant/30 p-4 md:p-6 shadow-2xl overflow-hidden"
      >
        {/* Header */}
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-outline-variant/20 pb-3">
          <div className="flex items-center gap-3">
            <div className="flex size-10 items-center justify-center rounded-full bg-primary/10 text-primary">
              <Sparkles className="size-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h2 className="text-lg md:text-xl font-bold tracking-tight text-on-surface">
                  Mejora de Detalle Forense
                </h2>
                <span className="rounded-full bg-primary/15 px-2 py-0.5 text-xs font-semibold text-primary">
                  IA Forense
                </span>
              </div>
              <p className="text-xs text-on-surface-variant flex items-center gap-2 mt-0.5">
                <span>{cameraName}</span>
                <span>•</span>
                <span>
                  {origW}x{origH} → {enhW}x{enhH}
                </span>
                {result?.executionTimeMs !== undefined && (
                  <>
                    <span>•</span>
                    <span className="text-emerald-500 font-mono">
                      {result.executionTimeMs} ms
                    </span>
                  </>
                )}
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <button
              type="button"
              aria-label="Cerrar mejora forense"
              onClick={onClose}
              className="m3-press inline-flex size-10 items-center justify-center rounded-full hover:bg-on-surface/8 text-on-surface-variant"
            >
              <X className="size-5" aria-hidden />
            </button>
          </div>
        </div>

        {/* Loading state */}
        {isLoading ? (
          <div className="flex flex-col items-center justify-center py-24 gap-4 text-center">
            <Loader2 className="size-10 animate-spin text-primary" />
            <div className="space-y-1">
              <p className="text-base font-semibold text-on-surface">
                Procesando detalle forense...
              </p>
              <p className="text-xs text-on-surface-variant">
                Ejecutando deblur, reducción de ruido y super-resolución de alta fidelidad
              </p>
            </div>
          </div>
        ) : (
          result && (
            <>
              {/* Toolbar */}
              <div className="flex flex-wrap items-center justify-between gap-2 rounded-m3-lg bg-surface-2 p-2 text-xs">
                {/* View mode toggle */}
                <div className="flex items-center gap-1 rounded-m3-md bg-surface-3 p-1">
                  <button
                    type="button"
                    onClick={() => setViewMode("split")}
                    className={cn(
                      "flex items-center gap-1.5 px-3 py-1.5 rounded-m3-sm font-medium transition-colors",
                      viewMode === "split"
                        ? "bg-primary text-on-primary shadow-sm"
                        : "text-on-surface-variant hover:text-on-surface",
                    )}
                  >
                    <Split className="size-3.5" />
                    <span>Divisor Interactivo</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setViewMode("side-by-side")}
                    className={cn(
                      "flex items-center gap-1.5 px-3 py-1.5 rounded-m3-sm font-medium transition-colors",
                      viewMode === "side-by-side"
                        ? "bg-primary text-on-primary shadow-sm"
                        : "text-on-surface-variant hover:text-on-surface",
                    )}
                  >
                    <Columns2 className="size-3.5" />
                    <span>Lado a Lado</span>
                  </button>
                </div>

                {/* Digital Zoom controls (Wheel + Mouse pan compatible) */}
                <div className="flex items-center gap-1">
                  <button
                    type="button"
                    onClick={handleZoomOut}
                    aria-label="Reducir zoom"
                    className="p-1.5 rounded-m3-sm text-on-surface-variant hover:bg-surface-3 hover:text-on-surface"
                  >
                    <ZoomOut className="size-4" />
                  </button>
                  <span
                    data-testid="zoom-level-text"
                    className="font-mono text-xs w-12 text-center select-none"
                  >
                    {Math.round(zoom.scale * 100)}%
                  </span>
                  <button
                    type="button"
                    onClick={handleZoomIn}
                    aria-label="Aumentar zoom"
                    className="p-1.5 rounded-m3-sm text-on-surface-variant hover:bg-surface-3 hover:text-on-surface"
                  >
                    <ZoomIn className="size-4" />
                  </button>
                  <button
                    type="button"
                    onClick={handleResetZoom}
                    aria-label="Restablecer zoom"
                    className="p-1.5 rounded-m3-sm text-on-surface-variant hover:bg-surface-3 hover:text-on-surface"
                    title="Restablecer zoom (Doble clic)"
                  >
                    <RotateCcw className="size-3.5" />
                  </button>
                </div>

                {/* Actions: Download / Copy */}
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={handleCopy}
                    className="flex items-center gap-1.5 px-3 py-1.5 rounded-m3-sm bg-surface-3 hover:bg-surface-4 text-on-surface font-medium transition-colors"
                  >
                    {copied ? (
                      <Check className="size-3.5 text-emerald-400" />
                    ) : (
                      <Copy className="size-3.5" />
                    )}
                    <span>{copied ? "Copiado" : "Copiar"}</span>
                  </button>
                  <button
                    type="button"
                    onClick={handleDownloadOriginal}
                    className="flex items-center gap-1.5 px-3 py-1.5 rounded-m3-sm bg-surface-3 hover:bg-surface-4 text-on-surface font-medium transition-colors"
                  >
                    <Download className="size-3.5" />
                    <span>Descargar original</span>
                  </button>
                  <button
                    type="button"
                    onClick={handleDownloadEnhanced}
                    className="flex items-center gap-1.5 px-3 py-1.5 rounded-m3-sm bg-primary text-on-primary font-medium hover:brightness-110 shadow-sm transition-all"
                  >
                    <Download className="size-3.5" />
                    <span>Descargar mejorada</span>
                  </button>
                </div>
              </div>

              {/* Viewer Stage with Mouse Wheel Zoom and Pan/Movement Permissions */}
              <div className="relative flex-1 min-h-[360px] max-h-[58vh] rounded-m3-xl bg-black/90 border border-outline-variant/20 overflow-hidden flex items-center justify-center select-none">
                {viewMode === "split" ? (
                  <div
                    ref={viewerRef}
                    data-testid="forensic-viewer"
                    className={cn(
                      "relative w-full h-full flex items-center justify-center overflow-hidden touch-none",
                      zoom.scale > 1
                        ? isPanning
                          ? "cursor-grabbing select-none"
                          : "cursor-grab"
                        : "cursor-ew-resize",
                    )}
                    onPointerDown={handlePointerDown}
                    onPointerMove={handlePointerMove}
                    onPointerUp={handlePointerUp}
                    onPointerCancel={handlePointerUp}
                    onDoubleClick={handleDoubleClick}
                  >
                    {/* Enhanced (Underneath) */}
                    {enhancedUrl && (
                      <div
                        data-testid="enhanced-layer"
                        className="absolute inset-0 flex items-center justify-center pointer-events-none"
                        style={transformStyle}
                      >
                        <img
                          src={enhancedUrl}
                          alt="Mejorada"
                          className="max-h-[54vh] max-w-full object-contain pointer-events-none"
                          draggable={false}
                        />
                      </div>
                    )}

                    {/* Original (Clipped on Top with Identical Synchronized Zoom/Pan) */}
                    {originalUrl && (
                      <div
                        className="absolute inset-0 overflow-hidden pointer-events-none"
                        style={{
                          clipPath: `inset(0 ${100 - sliderPos}% 0 0)`,
                        }}
                      >
                        <div
                          data-testid="original-layer"
                          className="absolute inset-0 flex items-center justify-center pointer-events-none"
                          style={transformStyle}
                        >
                          <img
                            src={originalUrl}
                            alt="Original"
                            className="max-h-[54vh] max-w-full object-contain pointer-events-none"
                            draggable={false}
                          />
                        </div>
                      </div>
                    )}

                    {/* Divider Line & Draggable Handle */}
                    <div
                      className="absolute top-0 bottom-0 pointer-events-none z-10 flex items-center justify-center"
                      style={{
                        left: `${sliderPos}%`,
                        transform: "translateX(-50%)",
                      }}
                    >
                      <div className="w-0.5 h-full bg-white shadow-lg pointer-events-none" />
                      <div
                        data-testid="slider-handle"
                        className="pointer-events-auto size-7 rounded-full bg-white text-black flex items-center justify-center shadow-xl border border-black/20 text-[10px] font-bold cursor-ew-resize hover:scale-110 transition-transform"
                      >
                        <Split className="size-3.5 text-neutral-800" />
                      </div>
                    </div>

                    {/* Left/Right Label Badges */}
                    <div className="absolute top-3 left-3 z-20 rounded-m3-sm bg-black/60 backdrop-blur-md px-2.5 py-1 text-[11px] font-semibold text-white/90 border border-white/10 pointer-events-none">
                      Original
                    </div>
                    <div className="absolute top-3 right-3 z-20 rounded-m3-sm bg-primary/80 backdrop-blur-md px-2.5 py-1 text-[11px] font-semibold text-on-primary border border-primary/20 pointer-events-none">
                      Mejorada
                    </div>

                    {/* Reset zoom floating chip when magnified */}
                    {zoom.scale > 1 && (
                      <button
                        type="button"
                        onClick={handleResetZoom}
                        title="Restablecer zoom (Doble clic)"
                        className="absolute bottom-3 right-3 z-20 rounded-m3-sm bg-black/75 backdrop-blur-md px-2 py-0.5 text-[11px] font-mono text-white/90 hover:bg-black border border-white/10 shadow-lg cursor-pointer"
                      >
                        {zoom.scale.toFixed(1)}× Restablecer
                      </button>
                    )}
                  </div>
                ) : (
                  /* Side by Side Mode with Synchronized Wheel Zoom and Pan */
                  <div
                    ref={viewerRef}
                    data-testid="forensic-viewer"
                    className={cn(
                      "grid grid-cols-1 md:grid-cols-2 gap-3 w-full h-full p-3 overflow-hidden touch-none",
                      zoom.scale > 1
                        ? isPanning
                          ? "cursor-grabbing select-none"
                          : "cursor-grab"
                        : "cursor-default",
                    )}
                    onPointerDown={handlePointerDown}
                    onPointerMove={handlePointerMove}
                    onPointerUp={handlePointerUp}
                    onPointerCancel={handlePointerUp}
                    onDoubleClick={handleDoubleClick}
                  >
                    <div className="flex flex-col items-center justify-center bg-surface-2/40 rounded-m3-lg p-2 border border-outline-variant/10 relative overflow-hidden">
                      <div className="absolute top-2 left-2 z-10 rounded-m3-sm bg-black/60 px-2 py-0.5 text-[10px] font-semibold text-white/90 pointer-events-none">
                        Original ({origW}x{origH})
                      </div>
                      {originalUrl && (
                        <div
                          data-testid="original-layer"
                          className="w-full h-full flex items-center justify-center pointer-events-none"
                          style={transformStyle}
                        >
                          <img
                            src={originalUrl}
                            alt="Original"
                            className="max-h-[48vh] max-w-full object-contain pointer-events-none"
                            draggable={false}
                          />
                        </div>
                      )}
                    </div>

                    <div className="flex flex-col items-center justify-center bg-surface-2/40 rounded-m3-lg p-2 border border-outline-variant/10 relative overflow-hidden">
                      <div className="absolute top-2 left-2 z-10 rounded-m3-sm bg-primary/80 px-2 py-0.5 text-[10px] font-semibold text-on-primary pointer-events-none">
                        Mejorada ({enhW}x{enhH})
                      </div>
                      {enhancedUrl && (
                        <div
                          data-testid="enhanced-layer"
                          className="w-full h-full flex items-center justify-center pointer-events-none"
                          style={transformStyle}
                        >
                          <img
                            src={enhancedUrl}
                            alt="Mejorada"
                            className="max-h-[48vh] max-w-full object-contain pointer-events-none"
                            draggable={false}
                          />
                        </div>
                      )}
                    </div>
                  </div>
                )}
              </div>

              {/* Forensic provenance note and details */}
              <div className="flex flex-wrap items-center justify-between gap-3 text-xs text-on-surface-variant pt-1 border-t border-outline-variant/20">
                <div className="flex items-center gap-2 text-emerald-400">
                  <ShieldCheck className="size-4 shrink-0" />
                  <span>
                    Procesamiento digital no destructivo. La grabación y evidencia original se
                    mantienen 100% inalteradas.
                  </span>
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-on-surface-variant/70">
                    Operaciones: {result.operationsApplied.join(", ")}
                  </span>
                </div>
              </div>
            </>
          )
        )}
      </div>
    </div>
  );

  return typeof document !== "undefined" ? createPortal(dialog, document.body) : null;
}
