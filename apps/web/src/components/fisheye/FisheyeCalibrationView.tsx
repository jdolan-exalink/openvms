import {
  type PointerEvent as ReactPointerEvent,
  useCallback,
  useRef,
  useState,
} from "react";
import { Icon } from "@/components/Icon";
import { Button, Select } from "@/components/ui";
import {
  DEFAULT_FISHEYE_CALIBRATION,
  type FisheyeCalibration,
  type FisheyeLensType,
  type FisheyeMountType,
  type FisheyeProjectionModel,
} from "@/lib/fisheye/types";
import { Check, RotateCcw, Target } from "lucide-react";

export interface FisheyeCalibrationViewProps {
  snapshotUrl?: string;
  initialCalibration?: FisheyeCalibration;
  initialMountType?: FisheyeMountType;
  initialLensType?: FisheyeLensType;
  initialProjectionModel?: FisheyeProjectionModel;
  onSave: (data: {
    calibration: FisheyeCalibration;
    mountType: FisheyeMountType;
    lensType: FisheyeLensType;
    projectionModel: FisheyeProjectionModel;
  }) => void;
  onClose?: () => void;
}

export function FisheyeCalibrationView({
  snapshotUrl,
  initialCalibration,
  initialMountType = "ceiling",
  initialLensType = "fisheye_360",
  initialProjectionModel = "equidistant",
  onSave,
  onClose,
}: FisheyeCalibrationViewProps) {
  const [calibration, setCalibration] = useState<FisheyeCalibration>(
    initialCalibration || DEFAULT_FISHEYE_CALIBRATION,
  );
  const [mountType, setMountType] = useState<FisheyeMountType>(initialMountType);
  const [lensType, setLensType] = useState<FisheyeLensType>(initialLensType);
  const [projectionModel, setProjectionModel] =
    useState<FisheyeProjectionModel>(initialProjectionModel);

  const containerRef = useRef<HTMLDivElement>(null);
  const [dragMode, setDragMode] = useState<"center" | "radius" | null>(null);

  const handlePointerDown = (mode: "center" | "radius", e: ReactPointerEvent) => {
    e.preventDefault();
    e.stopPropagation();
    e.currentTarget.setPointerCapture?.(e.pointerId);
    setDragMode(mode);
  };

  const handlePointerMove = (e: ReactPointerEvent) => {
    if (!dragMode || !containerRef.current) return;
    const rect = containerRef.current.getBoundingClientRect();
    const x = (e.clientX - rect.left) / rect.width;
    const y = (e.clientY - rect.top) / rect.height;

    if (dragMode === "center") {
      setCalibration((prev) => ({
        ...prev,
        centerX: Math.max(0.1, Math.min(0.9, Math.round(x * 1000) / 1000)),
        centerY: Math.max(0.1, Math.min(0.9, Math.round(y * 1000) / 1000)),
      }));
    } else if (dragMode === "radius") {
      const dx = x - calibration.centerX;
      const dy = y - calibration.centerY;
      const dist = Math.hypot(dx, dy);
      setCalibration((prev) => ({
        ...prev,
        radius: Math.max(0.1, Math.min(0.5, Math.round(dist * 1000) / 1000)),
      }));
    }
  };

  const handlePointerUp = (e: ReactPointerEvent) => {
    if (dragMode) {
      e.currentTarget.releasePointerCapture?.(e.pointerId);
      setDragMode(null);
    }
  };

  const handleReset = useCallback(() => {
    setCalibration({ ...DEFAULT_FISHEYE_CALIBRATION });
  }, []);

  // Compute SVG circle parameters based on percentage
  const cxPct = calibration.centerX * 100;
  const cyPct = calibration.centerY * 100;
  const rPct = calibration.radius * 100;

  return (
    <div className="flex flex-col lg:flex-row gap-6 p-4 bg-surface-1 rounded-m3-xl border border-outline-variant/60 shadow-lg max-w-5xl mx-auto">
      {/* Interactive Calibration Viewport */}
      <div className="flex-1 flex flex-col items-center justify-center">
        <div
          ref={containerRef}
          data-testid="calibration-viewport"
          className="relative aspect-square w-full max-w-[460px] bg-black rounded-m3-lg overflow-hidden select-none touch-none shadow-inner border border-outline-variant/40"
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
          onPointerCancel={handlePointerUp}
        >
          {snapshotUrl ? (
            <img
              src={snapshotUrl}
              alt="Fisheye Frame"
              className="w-full h-full object-contain pointer-events-none"
              draggable={false}
            />
          ) : (
            <div className="w-full h-full flex flex-col items-center justify-center text-muted/60 text-xs">
              <Icon icon={Target} size="lg" className="mb-2 opacity-50" />
              <span>Vista previa de lente Fisheye</span>
            </div>
          )}

          {/* SVG Overlay: Center crosshair, Boundary circle, and Handles */}
          <svg className="absolute inset-0 w-full h-full pointer-events-none">
            {/* Dark mask outside the useful lens radius */}
            <defs>
              <mask id="lens-mask">
                <rect width="100%" height="100%" fill="white" />
                <circle cx={`${cxPct}%`} cy={`${cyPct}%`} r={`${rPct}%`} fill="black" />
              </mask>
            </defs>
            <rect width="100%" height="100%" fill="rgba(0, 0, 0, 0.45)" mask="url(#lens-mask)" />

            {/* Lens boundary circle */}
            <circle
              cx={`${cxPct}%`}
              cy={`${cyPct}%`}
              r={`${rPct}%`}
              fill="none"
              stroke="#06b6d4"
              strokeWidth="2.5"
              strokeDasharray="6 4"
            />

            {/* Optical center crosshair */}
            <line
              x1={`${cxPct - 3}%`}
              y1={`${cyPct}%`}
              x2={`${cxPct + 3}%`}
              y2={`${cyPct}%`}
              stroke="#06b6d4"
              strokeWidth="2"
            />
            <line
              x1={`${cxPct}%`}
              y1={`${cyPct - 3}%`}
              x2={`${cxPct}%`}
              y2={`${cyPct + 3}%`}
              stroke="#06b6d4"
              strokeWidth="2"
            />
          </svg>

          {/* Draggable Center Handle */}
          <button
            type="button"
            data-testid="center-handle"
            aria-label="Arrastrar centro óptico"
            style={{
              left: `${cxPct}%`,
              top: `${cyPct}%`,
              transform: "translate(-50%, -50%)",
            }}
            onPointerDown={(e) => handlePointerDown("center", e)}
            className="absolute size-7 rounded-full bg-cyan-400 text-black flex items-center justify-center shadow-lg border-2 border-white cursor-move hover:scale-125 transition-transform z-20 pointer-events-auto"
            title="Mover centro óptico"
          >
            <div className="size-2 rounded-full bg-black" />
          </button>

          {/* Draggable Radius Handle (placed at right edge of circle) */}
          <button
            type="button"
            data-testid="radius-handle"
            aria-label="Ajustar radio útil"
            style={{
              left: `${cxPct + rPct}%`,
              top: `${cyPct}%`,
              transform: "translate(-50%, -50%)",
            }}
            onPointerDown={(e) => handlePointerDown("radius", e)}
            className="absolute size-6 rounded-full bg-yellow-400 text-black flex items-center justify-center shadow-lg border-2 border-white cursor-ew-resize hover:scale-125 transition-transform z-20 pointer-events-auto"
            title="Ajustar radio del lente"
          >
            <div className="size-1.5 rounded-full bg-black" />
          </button>

          {/* Floating guidance badge */}
          <div className="absolute top-2 left-2 z-10 px-2 py-1 rounded-m3-sm bg-black/75 backdrop-blur-md text-[11px] font-mono text-white/90 border border-white/10 pointer-events-none">
            Círculo útil: {(calibration.radius * 2 * 100).toFixed(0)}% · Centro: (
            {calibration.centerX.toFixed(2)}, {calibration.centerY.toFixed(2)})
          </div>
        </div>

        <p className="text-[11px] text-muted text-center mt-2">
          Arrastra el círculo cian para centrar y el control amarillo para calibrar el radio útil.
        </p>
      </div>

      {/* Settings & Sliders Deck */}
      <div className="w-full lg:w-72 flex flex-col gap-4">
        <div>
          <h3 className="text-sm font-bold text-on-surface">Calibración de Lente Fisheye</h3>
          <p className="text-xs text-muted">Ajusta los parámetros ópticos y montaje de la cámara.</p>
        </div>

        <div className="flex flex-col gap-3 text-xs">
          {/* Montaje */}
          <div>
            <label className="font-semibold text-muted block mb-1">Montaje de Cámara</label>
            <Select
              value={mountType}
              onChange={(e) => setMountType(e.target.value as FisheyeMountType)}
              className="w-full text-xs"
            >
              <option value="ceiling">Techo (Vista cenital)</option>
              <option value="wall">Pared (Horizontal)</option>
              <option value="floor">Piso (Hacia arriba)</option>
            </Select>
          </div>

          {/* Tipo de Lente */}
          <div>
            <label className="font-semibold text-muted block mb-1">Tipo de Lente</label>
            <Select
              value={lensType}
              onChange={(e) => setLensType(e.target.value as FisheyeLensType)}
              className="w-full text-xs"
            >
              <option value="fisheye_360">Fisheye 360°</option>
              <option value="fisheye_180">Fisheye 180°</option>
              <option value="panoramic">Panorámica</option>
              <option value="normal">Normal</option>
            </Select>
          </div>

          {/* Modelo de Proyección */}
          <div>
            <label className="font-semibold text-muted block mb-1">Modelo de Proyección</label>
            <Select
              value={projectionModel}
              onChange={(e) => setProjectionModel(e.target.value as FisheyeProjectionModel)}
              className="w-full text-xs"
            >
              <option value="equidistant">Equidistante (Estándar)</option>
              <option value="equisolid">Equisólida (Baja distorsión)</option>
              <option value="stereographic">Estereográfica (Conforme)</option>
              <option value="orthographic">Ortográfica</option>
            </Select>
          </div>

          {/* Rotación de Sensor */}
          <div>
            <div className="flex justify-between text-muted mb-1 font-mono">
              <span>Rotación de sensor</span>
              <span>{calibration.rotation}°</span>
            </div>
            <input
              type="range"
              min={-180}
              max={180}
              step={1}
              value={calibration.rotation}
              onChange={(e) =>
                setCalibration((prev) => ({ ...prev, rotation: Number(e.target.value) }))
              }
              className="w-full accent-primary"
            />
          </div>

          {/* Apertura FOV */}
          <div>
            <div className="flex justify-between text-muted mb-1 font-mono">
              <span>Apertura FOV</span>
              <span>{calibration.fov}°</span>
            </div>
            <input
              type="range"
              min={180}
              max={360}
              step={5}
              value={calibration.fov}
              onChange={(e) =>
                setCalibration((prev) => ({ ...prev, fov: Number(e.target.value) }))
              }
              className="w-full accent-primary"
            />
          </div>
        </div>

        {/* Action Buttons */}
        <div className="flex items-center gap-2 pt-2 border-t border-outline-variant/40 mt-auto">
          <Button
            variant="tonal"
            size="sm"
            onClick={handleReset}
            title="Restablecer a valores por defecto"
            className="flex-1"
          >
            <Icon icon={RotateCcw} size="xs" />
            <span>Restablecer</span>
          </Button>
          <Button
            variant="filled"
            size="sm"
            onClick={() => {
              onSave({
                calibration,
                mountType,
                lensType,
                projectionModel,
              });
              onClose?.();
            }}
            className="flex-1"
          >
            <Icon icon={Check} size="xs" />
            <span>Guardar</span>
          </Button>
        </div>
      </div>
    </div>
  );
}
