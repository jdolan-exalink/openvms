import type { MessageKey } from "@/i18n";
import type { HealthLayer, HealthReasonCode, HealthState } from "@/lib/telemetry";
import type { PillTone } from "@/components/ui";

/** i18n keys for the telemetry state codes; the English identifiers stay in logic and diagnostics. */
export const HEALTH_KEYS: Record<HealthState, MessageKey> = {
  optimal: "clientStation.healthOptimal",
  acceptable: "clientStation.healthAcceptable",
  limit: "clientStation.healthLimit",
  saturated: "clientStation.healthSaturated",
  unknown: "clientStation.healthUnknown",
};

export const HEALTH_TONES: Record<HealthState, PillTone> = {
  optimal: "ok",
  acceptable: "info",
  limit: "warn",
  saturated: "bad",
  unknown: "neutral",
};

export const LAYER_KEYS: Record<HealthLayer, MessageKey> = {
  network: "clientStation.layerNetwork",
  transport: "clientStation.layerTransport",
  decode: "clientStation.layerDecode",
  render: "clientStation.layerRender",
};

export const LAYER_ORDER: readonly HealthLayer[] = ["network", "transport", "decode", "render"];

export const REASON_KEYS: Record<HealthReasonCode, MessageKey> = {
  dropped_frames: "clientStation.reasonDroppedFrames",
  decode_fps_short: "clientStation.reasonDecodeFpsShort",
  presentation_fps: "clientStation.reasonPresentationFps",
  frame_time: "clientStation.reasonFrameTime",
  stalls: "clientStation.reasonStalls",
  streams_missing: "clientStation.reasonStreamsMissing",
  bitrate_short: "clientStation.reasonBitrateShort",
  insufficient_data: "clientStation.reasonInsufficientData",
};
