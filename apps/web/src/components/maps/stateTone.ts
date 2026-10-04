import type { CameraDisplayState } from "@/lib/maps/types";

/** Role-token classes per camera display state (chrome counterpart of the canvas palette). */
export const STATE_DOT: Record<CameraDisplayState, string> = {
  ONLINE: "bg-ok",
  DEGRADED: "bg-warn",
  OFFLINE: "bg-muted",
  NO_SIGNAL: "bg-muted",
  RECORDING_ERROR: "bg-warn",
  UNREACHABLE: "bg-muted",
  ALARM: "bg-bad",
};

export const STATE_TEXT: Record<CameraDisplayState, string> = {
  ONLINE: "text-ok",
  DEGRADED: "text-warn",
  OFFLINE: "text-muted",
  NO_SIGNAL: "text-muted",
  RECORDING_ERROR: "text-warn",
  UNREACHABLE: "text-muted",
  ALARM: "text-bad",
};
