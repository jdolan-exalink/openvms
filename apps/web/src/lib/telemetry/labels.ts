import type { BottleneckLayer } from "./bottleneck";
import type { HealthLayer, HealthReasonCode, HealthState } from "./performanceClassifier";

/** Operator-facing Spanish labels. Logic and diagnostics use the English identifiers. */

export const HEALTH_LABELS: Record<HealthState, string> = {
  optimal: "Óptimo",
  acceptable: "Aceptable",
  limit: "Al límite",
  saturated: "Saturado",
};

export const LAYER_LABELS: Record<HealthLayer, string> = {
  network: "Red",
  transport: "Recepción/transporte",
  decode: "Decodificación de video",
  render: "Render/composición",
};

export const BOTTLENECK_LABELS: Record<BottleneckLayer, string> = {
  none: "Sin cuello de botella",
  ...LAYER_LABELS,
};

export const REASON_LABELS: Record<HealthReasonCode, string> = {
  dropped_frames: "Frames descartados",
  decode_fps_short: "Cámaras por debajo del FPS esperado",
  presentation_fps: "FPS de presentación bajo",
  frame_time: "Tiempo de frame alto",
  stalls: "Interrupciones de reproducción",
  streams_missing: "Cámaras visibles sin recibir video",
  bitrate_short: "Bitrate recibido menor al esperado",
  insufficient_data: "Datos insuficientes",
};
