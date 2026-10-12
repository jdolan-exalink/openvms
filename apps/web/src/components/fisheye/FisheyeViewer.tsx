import {
  type PointerEvent as ReactPointerEvent,
  type WheelEvent as ReactWheelEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { Icon } from "@/components/Icon";
import { FisheyeDewarpEngine } from "@/lib/fisheye/engine";
import {
  generate3Fixed120Views,
  generate4QuadrantsViews,
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
import { cn } from "@/lib/cn";
import {
  Compass,
  Grid2X2,
  Plus,
  RotateCcw,
  Sliders,
  View,
} from "lucide-react";

export interface FisheyeViewerProps {
  cameraId: string;
  videoSrc?: string;
  videoElement?: HTMLVideoElement | null;
  className?: string;
  config?: FisheyeConfig;
  onConfigChange?: (config: FisheyeConfig) => void;
  onOpenCalibration?: () => void;
}

export function FisheyeViewer({
  cameraId,
  videoSrc,
  videoElement,
  className,
  config: externalConfig,
  onConfigChange,
  onOpenCalibration,
}: FisheyeViewerProps) {
  const [config, setConfig] = useState<FisheyeConfig>(() =>
    externalConfig || getFisheyeConfig(cameraId),
  );
  const [mode, setMode] = useState<FisheyeViewMode>(() =>
    getFisheyeViewMode(cameraId),
  );

  const localVideoRef = useRef<HTMLVideoElement | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const engineRef = useRef<FisheyeDewarpEngine | null>(null);

  // Active virtual view for PTZ
  const [activeView, setActiveView] = useState<VirtualFisheyeView>(() => {
    if (config.views.length > 0) return config.views[0]!;
    return {
      id: "default-ptz",
      cameraId,
      name: "PTZ 1",
      projection: "rectilinear",
      yaw: 0,
      pitch: -25,
      roll: 0,
      fov: 90,
    };
  });

  const ptzControllerRef = useRef<VirtualPtzController | null>(null);
  if (!ptzControllerRef.current) {
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

  // Multi-view states
  const [threeViews] = useState<VirtualFisheyeView[]>(() =>
    generate3Fixed120Views(cameraId),
  );
  const [fourViews] = useState<VirtualFisheyeView[]>(() =>
    generate4QuadrantsViews(cameraId),
  );

  // Synchronize internal and external config
  useEffect(() => {
    if (externalConfig) {
      setConfig(externalConfig);
    }
  }, [externalConfig]);

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
    saveFisheyeViewMode(cameraId, newMode);
  };

  // Setup WebGL Dewarp Engine on canvas mount
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

  // Real-time animation / dewarping render loop
  useEffect(() => {
    let rafId: number;

    const tick = () => {
      const engine = engineRef.current;
      const targetVideo = videoElement || localVideoRef.current;

      if (
        engine &&
        targetVideo &&
        targetVideo.readyState >= 2 &&
        engine.isSupported
      ) {
        // Adapt canvas internal buffer size to video element
        const c = canvasRef.current;
        if (c) {
          const w = targetVideo.videoWidth || c.clientWidth || 640;
          const h = targetVideo.videoHeight || c.clientHeight || 480;
          if (c.width !== w || c.height !== h) {
            c.width = w;
            c.height = h;
          }
        }

        engine.render(targetVideo, {
          mode,
          calibration: config.calibration,
          mountType: config.mountType,
          projectionModel: config.projectionModel,
          view: activeView,
          wrapScroll,
        });
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
    } else if (mode === "virtual_ptz" || mode === "panorama") {
      ptzControllerRef.current?.panBy(dx, dy, rect.width, rect.height);
    }
  };

  const handlePointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (isDraggingRef.current) {
      e.currentTarget.releasePointerCapture?.(e.pointerId);
      isDraggingRef.current = false;
    }
  };

  const handleWheel = (e: ReactWheelEvent<HTMLDivElement>) => {
    if (mode === "virtual_ptz") {
      e.preventDefault();
      ptzControllerRef.current?.zoomBy(e.deltaY);
    }
  };

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

  return (
    <div
      className={cn(
        "relative flex flex-col bg-black rounded-m3-lg overflow-hidden select-none border border-outline-variant/50 shadow-md",
        className,
      )}
    >
      {/* Top Floating Control Toolbar */}
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
            title="Panorama plano 180°"
          >
            Panorama
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
          <button
            type="button"
            onClick={() => handleModeChange("4_quadrants")}
            className={cn(
              "px-2 py-0.5 rounded transition-colors flex items-center gap-1",
              mode === "4_quadrants" ? "bg-primary text-on-primary font-bold" : "hover:bg-white/10",
            )}
            title="4 cuadrantes de 90°"
          >
            <Icon icon={Grid2X2} size="xs" />
            <span>4 Cuadrantes</span>
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

      {/* Main Viewport Stage */}
      {mode === "3_fixed_120" ? (
        /* Multi-view Grid: 3 Fixed 120° Views side by side */
        <div className="w-full h-full min-h-[360px] grid grid-cols-1 md:grid-cols-3 gap-1 bg-neutral-950 p-1">
          {threeViews.map((v) => (
            <div
              key={v.id}
              className="relative aspect-video md:aspect-auto flex flex-col items-center justify-center bg-neutral-900 rounded overflow-hidden border border-white/10"
            >
              <div className="absolute top-2 left-2 z-20 px-2 py-0.5 rounded bg-black/80 text-[10px] font-mono text-cyan-300 border border-cyan-500/30">
                {v.name} · {v.fov}° FOV
              </div>
              <FisheyeSingleViewport
                videoElement={videoElement || localVideoRef.current}
                calibration={config.calibration}
                mountType={config.mountType}
                projectionModel={config.projectionModel}
                view={v}
              />
            </div>
          ))}
        </div>
      ) : mode === "4_quadrants" ? (
        /* Multi-view Grid: 4 Quadrants in a 2x2 grid */
        <div className="w-full h-full min-h-[360px] grid grid-cols-2 grid-rows-2 gap-1 bg-neutral-950 p-1">
          {fourViews.map((v) => (
            <div
              key={v.id}
              className="relative aspect-video flex flex-col items-center justify-center bg-neutral-900 rounded overflow-hidden border border-white/10"
            >
              <div className="absolute top-2 left-2 z-20 px-2 py-0.5 rounded bg-black/80 text-[10px] font-mono text-yellow-300 border border-yellow-500/30">
                {v.name}
              </div>
              <FisheyeSingleViewport
                videoElement={videoElement || localVideoRef.current}
                calibration={config.calibration}
                mountType={config.mountType}
                projectionModel={config.projectionModel}
                view={v}
              />
            </div>
          ))}
        </div>
      ) : (
        /* Standard Single Viewport (Original, 360, Panorama, or Virtual PTZ) */
        <div
          data-testid="fisheye-stage"
          onPointerDown={handlePointerDown}
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
          onPointerCancel={handlePointerUp}
          onWheel={handleWheel}
          onDoubleClick={handleDoubleClick}
          className={cn(
            "relative w-full h-full min-h-[360px] flex items-center justify-center bg-black overflow-hidden touch-none",
            mode === "virtual_ptz" || mode === "360" ? "cursor-grab active:cursor-grabbing" : "cursor-default",
          )}
        >
          {/* WebGL Dewarp Canvas */}
          <canvas
            ref={canvasRef}
            data-testid="fisheye-canvas"
            className="w-full h-full object-contain pointer-events-none"
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
      )}
    </div>
  );
}

/**
 * Sub-viewport for Multi-view modes (3 fijas 120°, 4 cuadrantes)
 */
function FisheyeSingleViewport({
  videoElement,
  calibration,
  mountType,
  projectionModel,
  view,
}: {
  videoElement: HTMLVideoElement | null;
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
      const engine = engineRef.current;
      if (engine && videoElement && videoElement.readyState >= 2 && engine.isSupported) {
        const c = canvasRef.current;
        if (c) {
          const w = videoElement.videoWidth || c.clientWidth || 480;
          const h = videoElement.videoHeight || c.clientHeight || 320;
          if (c.width !== w || c.height !== h) {
            c.width = w;
            c.height = h;
          }
        }
        engine.render(videoElement, {
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
  }, [videoElement, calibration, mountType, projectionModel, view]);

  return (
    <canvas
      ref={canvasRef}
      className="w-full h-full object-cover pointer-events-none"
    />
  );
}
