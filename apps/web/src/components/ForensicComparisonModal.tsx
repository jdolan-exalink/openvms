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
import React, { useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/lib/cn";
import type { ForensicProcessOptions, ForensicResult } from "@/lib/enhancement/forensic";
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
  onReprocess,
  cameraName: initialCameraName,
  timestamp: initialTimestamp,
}: ForensicComparisonModalProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  useFocusTrap(containerRef, onClose);

  const [viewMode, setViewMode] = useState<ViewMode>("split");
  const [sliderPos, setSliderPos] = useState<number>(50);
  const [isDragging, setIsDragging] = useState<boolean>(false);
  const [zoomLevel, setZoomLevel] = useState<number>(1);
  const [copied, setCopied] = useState<boolean>(false);

  // Convert canvases to data URLs for reliable rendering and export
  const originalUrl = useMemo(() => {
    if (!result?.originalCanvas) return "";
    try {
      return result.originalCanvas.toDataURL("image/png");
    } catch {
      return "";
    }
  }, [result?.originalCanvas]);

  const enhancedUrl = useMemo(() => {
    if (!result?.enhancedCanvas) return "";
    try {
      return result.enhancedCanvas.toDataURL("image/png");
    } catch {
      return "";
    }
  }, [result?.enhancedCanvas]);

  // Pointer drag for split slider
  const handlePointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    setIsDragging(true);
    e.currentTarget.setPointerCapture(e.pointerId);
    updateSliderFromPointer(e);
  };

  const handlePointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    if (!isDragging) return;
    updateSliderFromPointer(e);
  };

  const handlePointerUp = (e: React.PointerEvent<HTMLDivElement>) => {
    setIsDragging(false);
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      // Ignored
    }
  };

  const updateSliderFromPointer = (e: React.PointerEvent<HTMLDivElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    if (rect.width <= 0) return;
    const clientX = e.clientX;
    const percent = ((clientX - rect.left) / rect.width) * 100;
    setSliderPos(Math.max(5, Math.min(95, percent)));
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

  const handleZoomIn = () => setZoomLevel((z) => Math.min(4, Math.round((z + 0.25) * 100) / 100));
  const handleZoomOut = () => setZoomLevel((z) => Math.max(0.75, Math.round((z - 0.25) * 100) / 100));
  const handleResetZoom = () => setZoomLevel(1);

  if (!isOpen) return null;
  if (!result && !isLoading) return null;

  const origW = result?.originalCanvas.width ?? 0;
  const origH = result?.originalCanvas.height ?? 0;
  const enhW = result?.enhancedCanvas.width ?? 0;
  const enhH = result?.enhancedCanvas.height ?? 0;

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

                {/* Zoom controls */}
                <div className="flex items-center gap-1">
                  <button
                    type="button"
                    onClick={handleZoomOut}
                    aria-label="Reducir zoom"
                    className="p-1.5 rounded-m3-sm text-on-surface-variant hover:bg-surface-3 hover:text-on-surface"
                  >
                    <ZoomOut className="size-4" />
                  </button>
                  <span className="font-mono text-xs w-10 text-center">
                    {Math.round(zoomLevel * 100)}%
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

              {/* Viewer Stage */}
              <div className="relative flex-1 min-h-[360px] max-h-[58vh] rounded-m3-xl bg-black/90 border border-outline-variant/20 overflow-hidden flex items-center justify-center select-none">
                {viewMode === "split" ? (
                  <div
                    className="relative w-full h-full flex items-center justify-center cursor-ew-resize overflow-hidden touch-none"
                    onPointerDown={handlePointerDown}
                    onPointerMove={handlePointerMove}
                    onPointerUp={handlePointerUp}
                    onPointerCancel={handlePointerUp}
                  >
                    {/* Zoomable container */}
                    <div
                      className="relative max-w-full max-h-full flex items-center justify-center transition-transform duration-75"
                      style={{
                        transform: `scale(${zoomLevel})`,
                      }}
                    >
                      {/* Enhanced (Underneath) */}
                      {enhancedUrl && (
                        <img
                          src={enhancedUrl}
                          alt="Mejorada"
                          className="max-h-[54vh] max-w-full object-contain pointer-events-none"
                          draggable={false}
                        />
                      )}

                      {/* Original (Clipped on Top) */}
                      {originalUrl && (
                        <div
                          className="absolute inset-0 overflow-hidden pointer-events-none"
                          style={{
                            clipPath: `inset(0 ${100 - sliderPos}% 0 0)`,
                          }}
                        >
                          <img
                            src={originalUrl}
                            alt="Original"
                            className="w-full h-full object-contain"
                            draggable={false}
                          />
                        </div>
                      )}

                      {/* Divider Line & Handle */}
                      <div
                        className="absolute top-0 bottom-0 pointer-events-none z-10 flex items-center justify-center"
                        style={{
                          left: `${sliderPos}%`,
                          transform: "translateX(-50%)",
                        }}
                      >
                        <div className="w-0.5 h-full bg-white shadow-lg" />
                        <div className="absolute size-7 rounded-full bg-white text-black flex items-center justify-center shadow-xl border border-black/20 text-[10px] font-bold">
                          <Split className="size-3.5 text-neutral-800" />
                        </div>
                      </div>
                    </div>

                    {/* Left/Right Label Badges */}
                    <div className="absolute top-3 left-3 z-20 rounded-m3-sm bg-black/60 backdrop-blur-md px-2.5 py-1 text-[11px] font-semibold text-white/90 border border-white/10 pointer-events-none">
                      Original
                    </div>
                    <div className="absolute top-3 right-3 z-20 rounded-m3-sm bg-primary/80 backdrop-blur-md px-2.5 py-1 text-[11px] font-semibold text-on-primary border border-primary/20 pointer-events-none">
                      Mejorada
                    </div>
                  </div>
                ) : (
                  /* Side by Side Mode */
                  <div
                    className="grid grid-cols-1 md:grid-cols-2 gap-3 w-full h-full p-3 overflow-auto"
                    style={{
                      transform: `scale(${zoomLevel})`,
                    }}
                  >
                    <div className="flex flex-col items-center justify-center bg-surface-2/40 rounded-m3-lg p-2 border border-outline-variant/10 relative">
                      <div className="absolute top-2 left-2 rounded-m3-sm bg-black/60 px-2 py-0.5 text-[10px] font-semibold text-white/90">
                        Original ({origW}x{origH})
                      </div>
                      {originalUrl && (
                        <img
                          src={originalUrl}
                          alt="Original"
                          className="max-h-[48vh] max-w-full object-contain"
                        />
                      )}
                    </div>
                    <div className="flex flex-col items-center justify-center bg-surface-2/40 rounded-m3-lg p-2 border border-outline-variant/10 relative">
                      <div className="absolute top-2 left-2 rounded-m3-sm bg-primary/80 px-2 py-0.5 text-[10px] font-semibold text-on-primary">
                        Mejorada ({enhW}x{enhH})
                      </div>
                      {enhancedUrl && (
                        <img
                          src={enhancedUrl}
                          alt="Mejorada"
                          className="max-h-[48vh] max-w-full object-contain"
                        />
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
