import { useMemo, useState } from "react";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Button, Select, Switch } from "@/components/ui";
import { detectFisheyeProfile } from "@/lib/fisheye/profiles";
import {
  generate3Fixed120Views,
  generate4QuadrantsViews,
  getFisheyeConfig,
  saveFisheyeConfig,
} from "@/lib/fisheye/storage";
import type {
  FisheyeConfig,
  FisheyeLensType,
  FisheyeMountType,
  FisheyeProjectionModel,
} from "@/lib/fisheye/types";
import {
  Check,
  Compass,
  Eye,
  Grid2X2,
  Sliders,
  Sparkles,
  Trash2,
  View,
} from "lucide-react";
import { FisheyeCalibrationView } from "./FisheyeCalibrationView";

export interface FisheyeSectionProps {
  cameraId: string;
  cameraName?: string;
  snapshotUrl?: string;
  canManage?: boolean;
  onConfigChange?: (config: FisheyeConfig) => void;
}

export function FisheyeSection({
  cameraId,
  cameraName,
  snapshotUrl,
  canManage = true,
  onConfigChange,
}: FisheyeSectionProps) {
  const [savedConfig, setSavedConfig] = useState<FisheyeConfig>(() =>
    getFisheyeConfig(cameraId),
  );
  const [config, setConfig] = useState<FisheyeConfig>(() =>
    getFisheyeConfig(cameraId),
  );
  const [isCalibrating, setIsCalibrating] = useState(false);
  const [detectedBadge, setDetectedBadge] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);

  const hasChanges = useMemo(() => {
    return JSON.stringify(config) !== JSON.stringify(savedConfig);
  }, [config, savedConfig]);

  const updateConfig = (next: FisheyeConfig) => {
    setConfig(next);
  };

  const handleSave = () => {
    saveFisheyeConfig(config);
    setSavedConfig(config);
    onConfigChange?.(config);
    setSaveSuccess(true);
    setTimeout(() => setSaveSuccess(false), 2000);
  };

  const handleCancel = () => {
    setConfig(savedConfig);
  };

  const handleAutoDetect = () => {
    const profile = detectFisheyeProfile({
      display_name: cameraName,
      tags: ["fisheye"],
    });

    if (profile) {
      const next: FisheyeConfig = {
        ...config,
        enabled: true,
        lensType: profile.lensType,
        mountType: profile.mountType,
        projectionModel: profile.projectionModel,
        calibration: { ...profile.calibration },
      };
      updateConfig(next);
      setDetectedBadge(`${profile.manufacturer} (${profile.lensType})`);
    } else {
      setDetectedBadge("Cámara genérica 360°");
    }
  };

  const handleApply3Fixed120 = () => {
    const views = generate3Fixed120Views(cameraId, config.mountType);
    const next: FisheyeConfig = {
      ...config,
      views,
      defaultMode: "3_fixed_120",
    };
    updateConfig(next);
  };

  const handleApply4Quadrants = () => {
    const views = generate4QuadrantsViews(cameraId, config.mountType);
    const next: FisheyeConfig = {
      ...config,
      views,
      defaultMode: "4_quadrants",
    };
    updateConfig(next);
  };

  const handleDeleteView = (viewId: string) => {
    const next: FisheyeConfig = {
      ...config,
      views: config.views.filter((v) => v.id !== viewId),
    };
    updateConfig(next);
  };

  return (
    <div className="flex flex-col gap-5 p-4 rounded-m3-xl bg-surface-1 border border-outline-variant/60 shadow-sm">
      {/* Header with Title and Enable Switch */}
      <div className="flex items-center justify-between pb-3 border-b border-outline-variant/40">
        <div className="flex items-center gap-2.5">
          <div className="size-8 rounded-m3-md bg-primary/10 text-primary flex items-center justify-center">
            <Icon icon={Eye} size="sm" />
          </div>
          <div>
            <h3 className="text-sm font-bold text-on-surface">
              Modo Fisheye / 360° / 180°
            </h3>
            <p className="text-xs text-muted">
              Dewarping por GPU, navegación PTZ digital y vistas virtuales multi-view.
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Switch
            label={config.enabled ? "Habilitado" : "Deshabilitado"}
            checked={config.enabled}
            onChange={(checked) =>
              updateConfig({ ...config, enabled: checked })
            }
            disabled={!canManage}
          />
        </div>
      </div>

      {config.enabled && (
        <div className="flex flex-col gap-5">
          {/* Lens & Mounting Parameters */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <div>
              <label className="font-semibold text-xs text-muted block mb-1">
                Tipo de lente
              </label>
              <Select
                value={config.lensType}
                onChange={(e) =>
                  updateConfig({
                    ...config,
                    lensType: e.target.value as FisheyeLensType,
                  })
                }
                disabled={!canManage}
                className="w-full text-xs"
              >
                <option value="auto">Automático</option>
                <option value="fisheye_360">Fisheye 360°</option>
                <option value="fisheye_180">Fisheye 180°</option>
                <option value="panoramic">Panorámica</option>
                <option value="normal">Normal</option>
              </Select>
            </div>

            <div>
              <label className="font-semibold text-xs text-muted block mb-1">
                Montaje
              </label>
              <Select
                value={config.mountType}
                onChange={(e) =>
                  updateConfig({
                    ...config,
                    mountType: e.target.value as FisheyeMountType,
                  })
                }
                disabled={!canManage}
                className="w-full text-xs"
              >
                <option value="ceiling">Techo (Cenital)</option>
                <option value="wall">Pared (Horizontal)</option>
                <option value="floor">Piso (Hacia arriba)</option>
              </Select>
            </div>

            <div>
              <label className="font-semibold text-xs text-muted block mb-1">
                Modelo de proyección
              </label>
              <Select
                value={config.projectionModel}
                onChange={(e) =>
                  updateConfig({
                    ...config,
                    projectionModel: e.target.value as FisheyeProjectionModel,
                  })
                }
                disabled={!canManage}
                className="w-full text-xs"
              >
                <option value="equidistant">Equidistante (Estándar)</option>
                <option value="equisolid">Equisólida (Baja distorsión)</option>
                <option value="stereographic">Estereográfica</option>
                <option value="orthographic">Ortográfica</option>
              </Select>
            </div>
          </div>

          {/* Quick Actions: Auto-detect & Visual Calibration */}
          <div className="flex flex-wrap items-center gap-2 pt-1">
            <Button
              variant="tonal"
              size="sm"
              onClick={handleAutoDetect}
              title="Detectar automáticamente modelo y calibración"
              className="text-xs"
            >
              <Icon icon={Sparkles} size="xs" />
              <span>Autodetectar fabricante</span>
            </Button>

            <Button
              variant="filled"
              size="sm"
              onClick={() => setIsCalibrating(true)}
              title="Abrir herramienta de calibración visual del círculo útil"
              className="text-xs"
            >
              <Icon icon={Sliders} size="xs" />
              <span>Calibrar círculo útil</span>
            </Button>

            {detectedBadge && (
              <span className="text-[11px] font-mono px-2 py-0.5 rounded-m3-sm bg-primary/20 text-primary border border-primary/30">
                Detectado: {detectedBadge}
              </span>
            )}
          </div>

          {/* Virtual Views & Presets Deck */}
          <div className="flex flex-col gap-3 p-3 rounded-m3-lg bg-surface-2 border border-outline-variant/40">
            <div className="flex items-center justify-between">
              <div>
                <h4 className="text-xs font-bold text-on-surface">
                  Vistas Virtuales y Presets (Multi-view)
                </h4>
                <p className="text-[11px] text-muted">
                  Genera múltiples cámaras virtuales independientes a partir de este stream 360°.
                </p>
              </div>

              {/* Template generator buttons */}
              <div className="flex items-center gap-1.5">
                <Button
                  variant="outlined"
                  size="sm"
                  onClick={handleApply3Fixed120}
                  title="Generar 3 vistas virtuales fijas de 120° cubriendo 360°"
                  className="bg-surface-1"
                >
                  <Icon icon={View} size="xs" />
                  <span>3 Fijas de 120°</span>
                </Button>

                <Button
                  variant="outlined"
                  size="sm"
                  onClick={handleApply4Quadrants}
                  title="Generar 4 vistas cuadrantes de 90°"
                  className="bg-surface-1"
                >
                  <Icon icon={Grid2X2} size="xs" />
                  <span>4 Cuadrantes</span>
                </Button>
              </div>
            </div>

            {/* List of configured virtual views */}
            {config.views.length > 0 ? (
              <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-2 pt-1">
                {config.views.map((v) => (
                  <div
                    key={v.id}
                    className="flex items-center justify-between p-2 rounded-m3-sm bg-surface-1 border border-outline-variant/40 text-xs shadow-xs"
                  >
                    <div className="flex items-center gap-2">
                      <Icon icon={Compass} size="xs" className="text-primary" />
                      <div>
                        <span className="font-semibold text-on-surface block leading-tight">
                          {v.name}
                        </span>
                        <span className="text-[10px] text-muted font-mono">
                          Yaw: {v.yaw}° · Pitch: {v.pitch}° · FOV: {v.fov}°
                        </span>
                      </div>
                    </div>

                    {canManage && (
                      <button
                        type="button"
                        onClick={() => handleDeleteView(v.id)}
                        className="p-1 rounded text-muted hover:text-bad transition-colors"
                        title="Eliminar vista virtual"
                      >
                        <Icon icon={Trash2} size="xs" />
                      </button>
                    )}
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-xs text-muted italic">
                No hay vistas virtuales configuradas. Usa los botones de plantilla ("3 Fijas de 120°" o "4 Cuadrantes") o crea vistas navegando en el player.
              </p>
            )}
          </div>
        </div>
      )}

      {/* Configurator Action Footer: Guardar y Cancelar */}
      <div className="flex items-center justify-between pt-3 border-t border-outline-variant/40 mt-1">
        <div className="text-xs">
          {saveSuccess ? (
            <span className="text-ok font-semibold flex items-center gap-1">
              <Icon icon={Check} size="xs" /> Configuración guardada
            </span>
          ) : hasChanges ? (
            <span className="text-amber-400 font-medium">
              Hay cambios pendientes de guardar
            </span>
          ) : (
            <span className="text-muted">
              Configuración al día
            </span>
          )}
        </div>

        <div className="flex items-center gap-2">
          <Button
            variant="outlined"
            size="sm"
            onClick={handleCancel}
            disabled={!hasChanges}
            title="Descartar cambios no guardados"
            className="text-xs"
          >
            Cancelar
          </Button>

          <Button
            variant="filled"
            size="sm"
            onClick={handleSave}
            disabled={!hasChanges || !canManage}
            title="Guardar cambios de calibración y vistas"
            className="text-xs"
          >
            <Icon icon={Check} size="xs" />
            <span>Guardar</span>
          </Button>
        </div>
      </div>

      {/* Visual Calibration Modal */}
      {isCalibrating && (
        <Modal
          title={`Calibración Visual Fisheye · ${cameraName || "Cámara"}`}
          onClose={() => setIsCalibrating(false)}
        >
          <FisheyeCalibrationView
            snapshotUrl={snapshotUrl || `/media/v1/cameras/${cameraId}/snapshot.jpg`}
            initialCalibration={config.calibration}
            initialMountType={config.mountType}
            initialLensType={config.lensType}
            initialProjectionModel={config.projectionModel}
            onSave={(updated) => {
              updateConfig({
                ...config,
                calibration: updated.calibration,
                mountType: updated.mountType,
                lensType: updated.lensType,
                projectionModel: updated.projectionModel,
              });
              setIsCalibrating(false);
            }}
            onClose={() => setIsCalibrating(false)}
          />
        </Modal>
      )}
    </div>
  );
}
