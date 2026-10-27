import {
  type PointerEvent as ReactPointerEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Icon } from "@/components/Icon";
import { FisheyeDewarpEngine } from "@/lib/fisheye/engine";
import {
  generate3Fixed120Views,
  getEffectiveVirtualViews,
  getFisheyeConfig,
  getFisheyeViewMode,
  saveFisheyeConfig,
  saveFisheyeViewMode,
} from "@/lib/fisheye/storage";
import type {
  FisheyeConfig,
  FisheyeViewMode,
  VirtualFisheyeView,
} from "@/lib/fisheye/types";
import { VirtualPtzController } from "@/lib/fisheye/VirtualPtzController";
import { PlayerSession } from "@/lib/live/PlayerSession";
import type { SessionQuality } from "@/lib/live/PlayerSession";
import { cn } from "@/lib/cn";
import {
  Bookmark,
  Check,
  Compass,
  Plus,
  RotateCcw,
  Sliders,
  View,
  X,
} from "lucide-react";

export interface FisheyeViewerProps {
  cameraId: string;
  videoSrc?: string;
  videoElement?: HTMLVideoElement | null;
  className?: string;
  config?: FisheyeConfig;
  onConfigChange?: (config: FisheyeConfig) => void;
  onOpenCalibration?: () => void;
  compact?: boolean;
  initialMode?: FisheyeViewMode;
  targetViewId?: string;
  /** Source stream quality; Live can keep a previous-quality instance warm during handoff. */
  quality?: SessionQuality;
  onFirstFrame?: () => void;
}

function isSameVirtualView(a: VirtualFisheyeView | undefined, b: VirtualFisheyeView): boolean {
  return (
    !!a &&
    a.id === b.id &&
    a.cameraId === b.cameraId &&
    a.name === b.name &&
    a.projection === b.projection &&
    a.yaw === b.yaw &&
    a.pitch === b.pitch &&
    a.roll === b.roll &&
    a.fov === b.fov
  );
}

export function FisheyeViewer({
  cameraId,
  videoSrc,
  videoElement,
  className,
  config: externalConfig,
  onConfigChange,
  onOpenCalibration,
  compact = false,
  initialMode,
  targetViewId,
  quality = "main",
  onFirstFrame,
}: FisheyeViewerProps) {
  const [config, setConfig] = useState<FisheyeConfig>(() =>
    externalConfig || getFisheyeConfig(cameraId),
  );
  const [mode, setMode] = useState<FisheyeViewMode>(() =>
    targetViewId ? "virtual_ptz" : (initialMode || config.defaultMode || getFisheyeViewMode(cameraId)),
  );
  const cancelModeRef = useRef<FisheyeViewMode>(
    targetViewId ? "virtual_ptz" : (initialMode || config.defaultMode || getFisheyeViewMode(cameraId)),
  );
  const [savedFeedback, setSavedFeedback] = useState(false);

  const localVideoRef = useRef<HTMLVideoElement | null>(null);
  const sessionRef = useRef<PlayerSession | null>(null);
  const onFirstFrameRef = useRef(onFirstFrame);
  const firstFrameNotifiedRef = useRef(false);
  useEffect(() => {
    onFirstFrameRef.current = onFirstFrame;
  });
  useEffect(() => {
    firstFrameNotifiedRef.current = false;
  }, [cameraId, quality, videoElement, videoSrc]);
  const snapshotRef = useRef<HTMLImageElement | null>(null);
  const [, setSnapshotLoaded] = useState(false);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const engineRef = useRef<FisheyeDewarpEngine | null>(null);

  // Preload instant snapshot fallback so the view is never empty/black
  useEffect(() => {
    let active = true;
    let objectUrl: string | null = null;

    fetch(`/media/v1/cameras/${encodeURIComponent(cameraId)}/snapshot.jpg`, {
      credentials: "same-origin",
    })
      .then((res) => {
        if (!res.ok) throw new Error(`Snapshot HTTP ${res.status}`);
        return res.blob();
      })
      .then((blob) => {
        if (!active) return;
        objectUrl = URL.createObjectURL(blob);
        const img = new Image();
        img.onload = () => {
          if (!active) return;
          snapshotRef.current = img;
          setSnapshotLoaded(true);
        };
        img.src = objectUrl;
      })
      .catch((err) => {
        console.warn("Fisheye snapshot fetch failed:", err);
      });

    return () => {
      active = false;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
      snapshotRef.current = null;
    };
  }, [cameraId]);

  // Connect live MSE stream if no external videoElement is provided
  useEffect(() => {
    if (videoElement || videoSrc) return;

    try {
      const session = new PlayerSession({
        cameraId,
        quality,
        muted: true,
      });
      session.connect();
      sessionRef.current = session;

      return () => {
        session.close();
        sessionRef.current = null;
      };
    } catch (e) {
      console.warn("Failed to create PlayerSession for fisheye live stream:", e);
    }
  }, [cameraId, videoElement, videoSrc, quality]);

  // Active virtual view for PTZ
  const [activeView, setActiveView] = useState<VirtualFisheyeView>(() => {
    const effective = getEffectiveVirtualViews(config);
    if (targetViewId) {
      const match =
        effective.find((v) => v.id === targetViewId) ||
        config.views.find((v) => v.id === targetViewId);
      if (match) return match;
    }
    if (config.views.length > 0) return config.views[0]!;
    if (effective.length > 0) return effective[0]!;
    return {
      id: "default-ptz",
      cameraId,
      name: "PTZ 1",
      projection: "rectilinear",
      yaw: 0,
      pitch: config.mountType === "ceiling" ? -48 : 0,
      roll: 0,
      fov: 88,
    };
  });

  const ptzControllerRef = useRef<VirtualPtzController | null>(null);
  if (ptzControllerRef.current == null) {
    ptzControllerRef.current = new VirtualPtzController(activeView, (state) => {
      setActiveView((prev) => ({
        ...prev,
        yaw: state.yaw,
        pitch: state.pitch,
        roll: state.roll,
        fov: state.fov,
      }));
    });
  }

  // 360 wrap-around continuous scroll offset
  const [wrapScroll, setWrapScroll] = useState(0);

  // Drag interaction state
  const isDraggingRef = useRef(false);
  const lastPointerRef = useRef({ x: 0, y: 0 });

  // Multi-view states dynamically adapted to mount
  const threeViews = useMemo(
    () => generate3Fixed120Views(cameraId, config.mountType),
    [cameraId, config.mountType],
  );

  // Ref reads happen lazily inside the child's animation loop, never during render.
  const getVideoSource = useCallback(
    () => videoElement || sessionRef.current?.video || localVideoRef.current,
    [videoElement],
  );
  const getSnapshotImage = useCallback(() => snapshotRef.current, []);

  // Synchronize internal and external config
  // Adjusted during render (not in an effect) only when the external prop actually changes.
  const [syncedExternalConfig, setSyncedExternalConfig] = useState(externalConfig);
  if (externalConfig !== syncedExternalConfig) {
    setSyncedExternalConfig(externalConfig);
    if (externalConfig) {
      setConfig(externalConfig);
    }
  }

  // Synchronize internal mode with external initialMode changes (same render-time pattern).
  const [syncedModeInputs, setSyncedModeInputs] = useState({ initialMode, targetViewId });
  if (syncedModeInputs.initialMode !== initialMode || syncedModeInputs.targetViewId !== targetViewId) {
    setSyncedModeInputs({ initialMode, targetViewId });
    if (initialMode && !targetViewId) {
      setMode(initialMode);
    }
  }

  const targetView = useMemo(() => {
    if (!targetViewId) return undefined;
    return (
      getEffectiveVirtualViews(config).find((view) => view.id === targetViewId) ||
      config.views.find((view) => view.id === targetViewId)
    );
  }, [config, targetViewId]);

  // Adjust this component's state during render only when the controlling view itself changes
  // (identity or fields), so user pan/zoom on a targeted tile is not snapped back on every render.
  const [syncedTarget, setSyncedTarget] = useState<VirtualFisheyeView | undefined>(targetView);
  if (!targetView ? syncedTarget !== undefined : !isSameVirtualView(syncedTarget, targetView)) {
    setSyncedTarget(targetView);
    if (targetView) {
      setActiveView(targetView);
      setMode("virtual_ptz");
    }
  }

  useEffect(() => {
    if (syncedTarget) {
      ptzControllerRef.current?.setState(syncedTarget);
    }
  }, [syncedTarget]);

  // Synchronize with global fisheye-config-changed events
  useEffect(() => {
    const handleConfigChanged = (e: Event) => {
      const customEvent = e as CustomEvent<{ cameraId?: string }>;
      if (!customEvent.detail?.cameraId || customEvent.detail.cameraId === cameraId) {
        const fresh = getFisheyeConfig(cameraId);
        setConfig(fresh);
        if (!targetViewId && fresh.defaultMode) {
          setMode(fresh.defaultMode);
        }
      }
    };
    window.addEventListener("openvms:fisheye-config-changed", handleConfigChanged);
    return () => window.removeEventListener("openvms:fisheye-config-changed", handleConfigChanged);
  }, [cameraId, targetViewId]);

  const updateConfig = useCallback(
    (nextConfig: FisheyeConfig) => {
      setConfig(nextConfig);
      saveFisheyeConfig(nextConfig);
      onConfigChange?.(nextConfig);
    },
    [onConfigChange],
  );

  const handleModeChange = (newMode: FisheyeViewMode) => {
    setMode(newMode);
  };

  // Setup WebGL Dewarp Engine on canvas mount
  useEffect(() => {
    const canvas = canvasRef.current;
    if (canvas && !engineRef.current) {
      engineRef.current = new FisheyeDewarpEngine(canvas);
    }

    return () => {
      engineRef.current?.destroy();
      engineRef.current = null;
    };
  }, []);

  // Real-time animation / dewarping render loop
  useEffect(() => {
    let rafId: number;

    const tick = () => {
      const isMulti = mode === "3_fixed_120";
      if (isMulti) {
        rafId = requestAnimationFrame(tick);
        return;
      }

      if (!engineRef.current && canvasRef.current) {
        engineRef.current = new FisheyeDewarpEngine(canvasRef.current);
      }

      const engine = engineRef.current;
      const targetVideo = videoElement || sessionRef.current?.video || localVideoRef.current;
      const snapshot = snapshotRef.current;

      const isVideoReady = targetVideo && targetVideo.readyState >= 2;
      const isSnapshotReady = snapshot && snapshot.complete && snapshot.naturalWidth > 0;

      const source: TexImageSource | null = isVideoReady
        ? targetVideo
        : isSnapshotReady
        ? snapshot
        : null;

      if (engine && source && engine.isSupported) {
        // Adapt canvas internal buffer size to rendered viewport dimensions
        const c = canvasRef.current;
        if (c) {
          const dpr = typeof window !== "undefined" ? Math.min(window.devicePixelRatio || 1, 2) : 1;
          const w = Math.round((c.clientWidth || 640) * dpr);
          const h = Math.round((c.clientHeight || 480) * dpr);
          if (w > 0 && h > 0 && (c.width !== w || c.height !== h)) {
            c.width = w;
            c.height = h;
          }
        }

        const rendered = engine.render(source, {
          mode,
          calibration: config.calibration,
          mountType: config.mountType,
          projectionModel: config.projectionModel,
          view: activeView,
          wrapScroll,
        });
        if (rendered && isVideoReady && source === targetVideo && !firstFrameNotifiedRef.current) {
          firstFrameNotifiedRef.current = true;
          onFirstFrameRef.current?.();
        }
      }

      rafId = requestAnimationFrame(tick);
    };

    rafId = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafId);
  }, [mode, config, activeView, wrapScroll, videoElement]);

  // Pointer event handlers for Virtual PTZ & 360 scroll
  const handlePointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (mode === "original") return;
    e.preventDefault();
    e.currentTarget.setPointerCapture?.(e.pointerId);
    isDraggingRef.current = true;
    lastPointerRef.current = { x: e.clientX, y: e.clientY };
  };

  const handlePointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (!isDraggingRef.current) return;
    const dx = e.clientX - lastPointerRef.current.x;
    const dy = e.clientY - lastPointerRef.current.y;
    lastPointerRef.current = { x: e.clientX, y: e.clientY };

    const rect = e.currentTarget.getBoundingClientRect();

    if (mode === "360") {
      // Horizontal cylindrical wrap-around scroll
      setWrapScroll((prev) => (prev - dx / rect.width + 1) % 1);
    } else if (mode === "virtual_ptz") {
      ptzControllerRef.current?.panBy(dx, dy, rect.width, rect.height);
    }
  };

  const handlePointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (isDraggingRef.current) {
      e.currentTarget.releasePointerCapture?.(e.pointerId);
      isDraggingRef.current = false;
    }
  };

  const stageRef = useRef<HTMLDivElement | null>(null);

  // Active non-passive wheel zoom listener to prevent page scrolling without browser warnings
  useEffect(() => {
    const el = stageRef.current;
    if (!el) return;

    const onWheel = (e: WheelEvent) => {
      if (mode === "virtual_ptz") {
        e.preventDefault();
        ptzControllerRef.current?.zoomBy(e.deltaY);
      }
    };

    el.addEventListener("wheel", onWheel, { passive: false });
    return () => {
      el.removeEventListener("wheel", onWheel);
    };
  }, [mode]);

  const handleDoubleClick = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (mode === "virtual_ptz") {
      const rect = e.currentTarget.getBoundingClientRect();
      const clickX = e.clientX - rect.left;
      const clickY = e.clientY - rect.top;
      // Click-to-center
      ptzControllerRef.current?.clickToCenter(clickX, clickY, rect.width, rect.height);
    }
  };

  const handleCreateView = () => {
    const newName = `Vista ${config.views.length + 1}`;
    const newView: VirtualFisheyeView = {
      id: `${cameraId}-view-${Date.now()}`,
      cameraId,
      name: newName,
      projection: "rectilinear",
      yaw: activeView.yaw,
      pitch: activeView.pitch,
      roll: activeView.roll,
      fov: activeView.fov,
      presetType: "custom",
    };

    const nextConfig = {
      ...config,
      views: [...config.views, newView],
    };
    updateConfig(nextConfig);
  };

  const handleSaveModeAsDefault = () => {
    const viewsToSave =
      mode === "3_fixed_120"
        ? generate3Fixed120Views(cameraId, config.mountType)
        : [];

    const updated: FisheyeConfig = {
      ...config,
      defaultMode: mode,
      views: viewsToSave,
    };
    cancelModeRef.current = mode;
    setConfig(updated);
    saveFisheyeConfig(updated);
    saveFisheyeViewMode(cameraId, mode);
    onConfigChange?.(updated);
    setSavedFeedback(true);
    setTimeout(() => setSavedFeedback(false), 2000);
  };

  return (
    <div
      className={cn(
        "relative flex flex-col bg-black rounded-m3-lg overflow-hidden select-none border border-outline-variant/50 shadow-md",
        className,
      )}
    >
      {/* Top Floating Control Toolbar */}
      {!compact && (
        <div className="absolute top-2 left-2 right-2 z-30 flex flex-wrap items-center justify-between gap-1.5 pointer-events-auto">
          {/* View Mode Segmented Controls */}
          <div className="flex items-center gap-1 rounded-m3-md bg-black/75 backdrop-blur-md p-1 border border-white/10 shadow-lg text-[11px] font-semibold text-white/90">
            <button
              type="button"
              onClick={() => handleModeChange("original")}
              className={cn(
                "px-2 py-0.5 rounded transition-colors",
                mode === "original" ? "bg-primary text-on-primary font-bold" : "hover:bg-white/10",
              )}
              title="Vista circular original sin corrección"
            >
              Original
            </button>
            <button
              type="button"
              onClick={() => handleModeChange("360")}
              className={cn(
                "px-2 py-0.5 rounded transition-colors",
                mode === "360" ? "bg-primary text-on-primary font-bold" : "hover:bg-white/10",
              )}
              title="Panorámica 360° con desplazamiento continuo"
            >
              360°
            </button>
            <button
              type="button"
              onClick={() => handleModeChange("panorama")}
              className={cn(
                "px-2 py-0.5 rounded transition-colors",
                mode === "panorama" ? "bg-primary text-on-primary font-bold" : "hover:bg-white/10",
              )}
              title="Panorámica 2×180°"
            >
              2×180°
            </button>
            <button
              type="button"
              onClick={() => handleModeChange("virtual_ptz")}
              className={cn(
                "px-2 py-0.5 rounded transition-colors flex items-center gap-1",
                mode === "virtual_ptz" ? "bg-primary text-on-primary font-bold" : "hover:bg-white/10",
              )}
              title="PTZ Virtual interactivo"
            >
              <Icon icon={Compass} size="xs" />
              <span>PTZ Virtual</span>
            </button>
            <button
              type="button"
              onClick={() => handleModeChange("3_fixed_120")}
              className={cn(
                "px-2 py-0.5 rounded transition-colors flex items-center gap-1",
                mode === "3_fixed_120" ? "bg-primary text-on-primary font-bold" : "hover:bg-white/10",
              )}
              title="3 vistas fijas de 120° cubriendo 360°"
            >
              <Icon icon={View} size="xs" />
              <span>3 Fijas (120°)</span>
            </button>
          </div>

          {/* Presets & Actions Drawer */}
          <div className="flex items-center gap-1.5">
            {mode === "virtual_ptz" && config.views.length > 0 && (
              <select
                aria-label="Seleccionar preset"
                value={activeView.id}
                onChange={(e) => {
                  const found = config.views.find((v) => v.id === e.target.value);
                  if (found) {
                    setActiveView(found);
                    ptzControllerRef.current?.setState(found, true);
                  }
                }}
                className="rounded-m3-md bg-black/75 backdrop-blur-md px-2 py-1 text-[11px] font-semibold text-white/90 border border-white/10 cursor-pointer shadow-lg outline-none"
              >
                {config.views.map((v) => (
                  <option key={v.id} value={v.id} className="bg-neutral-900 text-white">
                    {v.name}
                  </option>
                ))}
              </select>
            )}

            {mode === "virtual_ptz" && (
              <>
                <button
                  type="button"
                  onClick={handleCreateView}
                  title="Guardar orientación actual como vista virtual"
                  className="flex items-center gap-1 rounded-m3-md bg-black/75 backdrop-blur-md px-2 py-1 text-[11px] font-semibold text-white/90 hover:bg-white/20 border border-white/10 shadow-lg"
                >
                  <Icon icon={Plus} size="xs" />
                  <span>Crear vista</span>
                </button>
                <button
                  type="button"
                  onClick={() => ptzControllerRef.current?.reset(undefined, true)}
                  title="Restablecer posición PTZ"
                  className="p-1 rounded-m3-md bg-black/75 backdrop-blur-md text-white/90 hover:bg-white/20 border border-white/10 shadow-lg"
                >
                  <Icon icon={RotateCcw} size="xs" />
                </button>
              </>
            )}

            {/* Guardar selección actual como predeterminada */}
            <button
              type="button"
              onClick={handleSaveModeAsDefault}
              title="Guardar selección para que se comporte así en la grilla"
              className={cn(
                "flex items-center gap-1.5 rounded-m3-md px-2.5 py-1 text-[11px] font-bold border shadow-md transition-all active:scale-95",
                savedFeedback
                  ? "bg-ok text-on-ok border-ok"
                  : "bg-primary text-on-primary border-primary hover:bg-primary-hover",
              )}
            >
              <Icon icon={savedFeedback ? Check : Bookmark} size="xs" />
              <span>{savedFeedback ? "Guardado" : "Guardar vista"}</span>
            </button>

            {/* Cancelar / Descartar cambios */}
            <button
              type="button"
              onClick={() => {
                setMode(cancelModeRef.current);
                if (mode === "virtual_ptz") {
                  ptzControllerRef.current?.reset();
                }
              }}
              title="Descartar cambios y volver al modo guardado"
              className="flex items-center gap-1 rounded-m3-md bg-black/75 backdrop-blur-md px-2 py-1 text-[11px] font-semibold text-white/80 hover:bg-white/20 hover:text-white border border-white/10 shadow-lg transition-colors"
            >
              <Icon icon={X} size="xs" />
              <span>Cancelar</span>
            </button>

            {onOpenCalibration && (
              <button
                type="button"
                onClick={onOpenCalibration}
                title="Calibrar lente Fisheye"
                className="p-1 rounded-m3-md bg-black/75 backdrop-blur-md text-white/90 hover:bg-white/20 border border-white/10 shadow-lg"
              >
                <Icon icon={Sliders} size="xs" />
              </button>
            )}
          </div>
        </div>
      )}

      {/* Main Viewport Stage */}
      {mode === "3_fixed_120" && (
        /* Multi-view Grid: 3 Fixed 120° Views side by side */
        <div className={cn("w-full h-full grid grid-cols-1 md:grid-cols-3 gap-1 bg-neutral-950 p-1", !compact && "min-h-[360px]")}>
          {threeViews.map((v) => (
            <div
              key={v.id}
              className="relative w-full h-full flex flex-col items-center justify-center bg-neutral-900 rounded overflow-hidden border border-white/10"
            >
              <div className="absolute top-2 left-2 z-20 px-2 py-0.5 rounded bg-black/80 text-[10px] font-mono text-cyan-300 border border-cyan-500/30">
                {v.name} · {v.fov}° FOV
              </div>
              <FisheyeSingleViewport
                getVideoElement={getVideoSource}
                getImageElement={getSnapshotImage}
                calibration={config.calibration}
                mountType={config.mountType}
                projectionModel={config.projectionModel}
                view={v}
              />
            </div>
          ))}
        </div>
      )}

      {/* Standard Single Viewport (Original, 360, Panorama, or Virtual PTZ) */}
      <div
        ref={stageRef}
        data-testid="fisheye-stage"
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
        onPointerCancel={handlePointerUp}
        onDoubleClick={handleDoubleClick}
        className={cn(
          "relative w-full h-full flex items-center justify-center bg-black overflow-hidden touch-none",
          !compact && "min-h-[360px]",
          mode === "virtual_ptz" || mode === "360" ? "cursor-grab active:cursor-grabbing" : "cursor-default",
          mode === "3_fixed_120" && "hidden",
        )}
      >
        {/* WebGL Dewarp Canvas */}
        <canvas
          ref={canvasRef}
          data-testid="fisheye-canvas"
          className="w-full h-full object-fill pointer-events-none"
        />

        {/* Hidden video element when videoSrc is passed directly */}
        {videoSrc && !videoElement && (
          <video
            ref={localVideoRef}
            src={videoSrc}
            muted
            playsInline
            autoPlay
            loop
            className="hidden"
          />
        )}

          {/* Floating PTZ Coordinates HUD (in Virtual PTZ mode) */}
          {mode === "virtual_ptz" && (
            <div className="absolute bottom-2 left-2 z-20 px-2 py-0.5 rounded-m3-sm bg-black/75 backdrop-blur-md text-[10px] font-mono text-white/80 border border-white/10 pointer-events-none">
              Yaw: {activeView.yaw.toFixed(1)}° · Pitch: {activeView.pitch.toFixed(1)}° · FOV:{" "}
              {activeView.fov.toFixed(0)}°
            </div>
          )}
        </div>
    </div>
  );
}

/**
 * Sub-viewport for Multi-view modes (3 fijas 120°, 4 cuadrantes)
 */
export function FisheyeSingleViewport({
  getVideoElement,
  getImageElement,
  calibration,
  mountType,
  projectionModel,
  view,
}: {
  getVideoElement?: () => HTMLVideoElement | null | undefined;
  getImageElement?: () => HTMLImageElement | null | undefined;
  calibration: FisheyeConfig["calibration"];
  mountType: FisheyeConfig["mountType"];
  projectionModel: FisheyeConfig["projectionModel"];
  view: VirtualFisheyeView;
}) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const engineRef = useRef<FisheyeDewarpEngine | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const engine = new FisheyeDewarpEngine(canvas);
    engineRef.current = engine;
    return () => {
      engine.destroy();
      engineRef.current = null;
    };
  }, []);

  useEffect(() => {
    let rafId: number;
    const tick = () => {
      if (!engineRef.current && canvasRef.current) {
        engineRef.current = new FisheyeDewarpEngine(canvasRef.current);
      }
      const engine = engineRef.current;
      const videoElement = getVideoElement?.();
      const imageElement = getImageElement?.();
      const isVideoReady = videoElement && videoElement.readyState >= 2;
      const isImageReady =
        imageElement && imageElement.complete && imageElement.naturalWidth > 0;
      const source: TexImageSource | null = isVideoReady
        ? videoElement
        : isImageReady
        ? imageElement
        : null;

      if (engine && source && engine.isSupported) {
        const c = canvasRef.current;
        if (c) {
          const dpr = typeof window !== "undefined" ? Math.min(window.devicePixelRatio || 1, 2) : 1;
          const w = Math.round((c.clientWidth || 480) * dpr);
          const h = Math.round((c.clientHeight || 320) * dpr);
          if (c.width !== w || c.height !== h) {
            c.width = w;
            c.height = h;
          }
        }
        engine.render(source, {
          mode: "virtual_ptz",
          calibration,
          mountType,
          projectionModel,
          view,
        });
      }
      rafId = requestAnimationFrame(tick);
    };
    rafId = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafId);
  }, [getVideoElement, getImageElement, calibration, mountType, projectionModel, view]);

  return (
    <canvas
      ref={canvasRef}
      className="w-full h-full object-fill pointer-events-none"
    />
  );
}
