import { useSyncExternalStore } from "react";
import { videoTelemetry, type VideoTelemetryService, type VideoTelemetrySnapshot } from "./videoTelemetry";

/** Latest 1 Hz telemetry snapshot; the reference only changes once per second. Defaults to the shared service. */
export function useVideoTelemetry(service: VideoTelemetryService = videoTelemetry): VideoTelemetrySnapshot {
  return useSyncExternalStore(service.subscribe, service.getSnapshot, service.getSnapshot);
}
