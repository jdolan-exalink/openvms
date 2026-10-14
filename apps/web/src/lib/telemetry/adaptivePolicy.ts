import { useEffect, useSyncExternalStore } from "react";
import type { MessageKey } from "@/i18n";
import type { BottleneckEstimate } from "./bottleneck";
import type { HealthLayer, HealthState } from "./performanceClassifier";
import { telemetryRuntime, type TelemetryRuntime } from "./runtime";

/**
 * Degradation steps defined in docs/telemetria-client.md §11:
 * 1. Reduce secondary streams FPS (requires backend/go2rtc support)
 * 2. Use lower-resolution substream (requires backend/go2rtc support)
 * 3. Pause streams completely hidden/not visible (e.g. tiles hidden by maximize)
 * 4. Reduce secondary visual work (e.g. blur effects, animations)
 * 5. Limit expensive overlays
 * 6. Warn operator when hardware is saturated
 */
export const UNSUPPORTED_DEGRADATION_STEPS = ["secondary_fps", "substream_low"] as const;
export type UnsupportedDegradationStep = (typeof UNSUPPORTED_DEGRADATION_STEPS)[number];

export type AdaptivePolicyInput = {
  enabled: boolean;
  health: HealthState;
  bottleneck?: BottleneckEstimate;
  visibleCameras?: number;
  totalCameras?: number;
  isMaximized?: boolean;
};

export type OperatorWarning = {
  code: "hardware_saturated";
  bottleneck?: HealthLayer;
  messageKey: MessageKey;
};

export type AdaptivePolicyActions = {
  /** True when streams hidden by maximize should pause playback/decode instead of running in background. */
  suspendHidden: boolean;
  /** True when visual overhead (e.g. blur, transitions) should be omitted to free GPU/CPU. */
  trimVisualWork: boolean;
  /** Warning surfaced to operator when station is saturated. */
  operatorWarning?: OperatorWarning;
  /** Declares capabilities that require future transport / gateway support. */
  unsupportedCapabilities: readonly UnsupportedDegradationStep[];
  /** Guaranteed: a maximized stream is NEVER silently degraded. */
  maximizedStreamProtected: true;
};

/**
 * Evaluates adaptive actions based on station health and layout state.
 * Pure function: deterministic and platform-agnostic.
 */
export function evaluateAdaptiveVideoPolicy(input: AdaptivePolicyInput): AdaptivePolicyActions {
  const { enabled, health, bottleneck, isMaximized = false } = input;

  if (!enabled) {
    return {
      suspendHidden: false,
      trimVisualWork: false,
      operatorWarning: undefined,
      unsupportedCapabilities: UNSUPPORTED_DEGRADATION_STEPS,
      maximizedStreamProtected: true,
    };
  }

  // Under limit or saturation:
  const isSaturated = health === "saturated";
  const isLimitOrAbove = health === "limit" || isSaturated;

  // Step 3: pause hidden streams when maximized or when station reaches limit/saturated
  const suspendHidden = isMaximized || isLimitOrAbove;

  // Steps 4 & 5: reduce visual work when at limit or saturated
  const trimVisualWork = isLimitOrAbove;

  const bottleneckLayer = bottleneck && bottleneck.layer !== "none" ? bottleneck.layer : undefined;

  // Step 6: warn operator if hardware cannot sustain layout
  const operatorWarning: OperatorWarning | undefined = isSaturated
    ? {
        code: "hardware_saturated",
        bottleneck: bottleneckLayer,
        messageKey: "clientStation.adaptiveWarning",
      }
    : undefined;

  return {
    suspendHidden,
    trimVisualWork,
    operatorWarning,
    unsupportedCapabilities: UNSUPPORTED_DEGRADATION_STEPS,
    maximizedStreamProtected: true,
  };
}

export type UseAdaptiveVideoPolicyOptions = {
  enabled: boolean;
  isMaximized?: boolean;
  totalCameras?: number;
  runtime?: TelemetryRuntime;
};

const NO_OP_SUBSCRIBE = () => () => undefined;

/**
 * React hook connecting Live grid to the adaptive video policy.
 * Ref-counts telemetryRuntime only while enabled.
 */
export function useAdaptiveVideoPolicy({
  enabled,
  isMaximized = false,
  totalCameras = 0,
  runtime = telemetryRuntime,
}: UseAdaptiveVideoPolicyOptions): AdaptivePolicyActions {
  useEffect(() => {
    if (!enabled) return;
    runtime.acquire();
    return () => runtime.release();
  }, [runtime, enabled]);

  const view = useSyncExternalStore(
    enabled ? runtime.subscribe : NO_OP_SUBSCRIBE,
    runtime.getView,
  );

  return evaluateAdaptiveVideoPolicy({
    enabled,
    health: view.state,
    bottleneck: view.bottleneck,
    isMaximized,
    totalCameras,
  });
}
