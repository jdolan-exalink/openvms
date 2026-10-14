import { describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import {
  evaluateAdaptiveVideoPolicy,
  useAdaptiveVideoPolicy,
  UNSUPPORTED_DEGRADATION_STEPS,
  type AdaptivePolicyInput,
} from "./adaptivePolicy";
import { createTelemetryRuntime, type TelemetryRuntime } from "./runtime";
import { createFrameSampler } from "./frameSampler";

describe("evaluateAdaptiveVideoPolicy", () => {
  it("never enables degradation actions when adaptive policy is disabled", () => {
    const input: AdaptivePolicyInput = {
      enabled: false,
      health: "saturated",
      bottleneck: { layer: "decode", confidence: "high", estimated: true, evidence: ["dropped frames"] },
      isMaximized: true,
      visibleCameras: 1,
      totalCameras: 16,
    };
    const actions = evaluateAdaptiveVideoPolicy(input);
    expect(actions.suspendHidden).toBe(false);
    expect(actions.trimVisualWork).toBe(false);
    expect(actions.operatorWarning).toBeUndefined();
    expect(actions.maximizedStreamProtected).toBe(true);
    expect(actions.unsupportedCapabilities).toEqual(UNSUPPORTED_DEGRADATION_STEPS);
  });

  it("never degrades a maximized camera under any health condition", () => {
    const input: AdaptivePolicyInput = {
      enabled: true,
      health: "saturated",
      bottleneck: { layer: "render", confidence: "high", estimated: true, evidence: ["slow frames"] },
      isMaximized: true,
      visibleCameras: 1,
      totalCameras: 48,
    };
    const actions = evaluateAdaptiveVideoPolicy(input);
    expect(actions.maximizedStreamProtected).toBe(true);
  });

  it("declares steps 1 and 2 as unsupported capabilities pending backend/go2rtc support", () => {
    expect(UNSUPPORTED_DEGRADATION_STEPS).toContain("secondary_fps");
    expect(UNSUPPORTED_DEGRADATION_STEPS).toContain("substream_low");
  });

  it("suspends hidden streams when a tile is maximized in optimal or acceptable health", () => {
    const normal = evaluateAdaptiveVideoPolicy({
      enabled: true,
      health: "optimal",
      isMaximized: false,
      visibleCameras: 16,
      totalCameras: 16,
    });
    expect(normal.suspendHidden).toBe(false);
    expect(normal.trimVisualWork).toBe(false);
    expect(normal.operatorWarning).toBeUndefined();

    const maximized = evaluateAdaptiveVideoPolicy({
      enabled: true,
      health: "optimal",
      isMaximized: true,
      visibleCameras: 1,
      totalCameras: 16,
    });
    expect(maximized.suspendHidden).toBe(true);
    expect(maximized.trimVisualWork).toBe(false);
  });

  it("trims visual work when health reaches limit", () => {
    const actions = evaluateAdaptiveVideoPolicy({
      enabled: true,
      health: "limit",
      bottleneck: { layer: "render", confidence: "medium", estimated: true, evidence: ["p95 frame time"] },
      isMaximized: false,
      visibleCameras: 25,
      totalCameras: 25,
    });
    expect(actions.suspendHidden).toBe(true);
    expect(actions.trimVisualWork).toBe(true);
    expect(actions.operatorWarning).toBeUndefined();
  });

  it("issues operator warning when station is saturated", () => {
    const actions = evaluateAdaptiveVideoPolicy({
      enabled: true,
      health: "saturated",
      bottleneck: { layer: "decode", confidence: "high", estimated: true, evidence: ["dropped frames > 15%"] },
      isMaximized: false,
      visibleCameras: 48,
      totalCameras: 48,
    });
    expect(actions.suspendHidden).toBe(true);
    expect(actions.trimVisualWork).toBe(true);
    expect(actions.operatorWarning).toEqual({
      code: "hardware_saturated",
      bottleneck: "decode",
      messageKey: "clientStation.adaptiveWarning",
    });
  });
});

describe("useAdaptiveVideoPolicy", () => {
  const fakeSampler = createFrameSampler(
    () => 1,
    () => undefined,
    () => 0,
  );

  const makeRuntime = (): TelemetryRuntime =>
    createTelemetryRuntime({
      now: () => 1000,
      schedule: () => () => undefined,
      sessions: () => [],
      presentation: fakeSampler,
    });

  it("acquires runtime while enabled and releases on unmount", () => {
    const runtime = makeRuntime();
    const acquireSpy = vi.spyOn(runtime, "acquire");
    const releaseSpy = vi.spyOn(runtime, "release");

    const { unmount } = renderHook(() =>
      useAdaptiveVideoPolicy({
        enabled: true,
        isMaximized: false,
        totalCameras: 4,
        runtime,
      }),
    );

    expect(acquireSpy).toHaveBeenCalledTimes(1);
    expect(releaseSpy).not.toHaveBeenCalled();

    unmount();
    expect(releaseSpy).toHaveBeenCalledTimes(1);
  });

  it("does not acquire runtime when disabled", () => {
    const runtime = makeRuntime();
    const acquireSpy = vi.spyOn(runtime, "acquire");

    renderHook(() =>
      useAdaptiveVideoPolicy({
        enabled: false,
        isMaximized: false,
        totalCameras: 4,
        runtime,
      }),
    );

    expect(acquireSpy).not.toHaveBeenCalled();
  });
});
