import { useSyncExternalStore } from "react";
import { videoTelemetry, type VideoTelemetrySnapshot } from "./videoTelemetry";

/** Latest 1 Hz telemetry snapshot; the reference only changes once per second. */
export function useVideoTelemetry(): VideoTelemetrySnapshot {
  return useSyncExternalStore(videoTelemetry.subscribe, videoTelemetry.getSnapshot, videoTelemetry.getSnapshot);
}
