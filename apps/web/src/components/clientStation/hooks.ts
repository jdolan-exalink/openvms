import { useEffect, useState, useSyncExternalStore } from "react";
import { collectClientCapabilities, telemetryRuntime, type ClientCapabilities, type TelemetryRuntime, type TelemetryView } from "@/lib/telemetry";

/**
 * Subscribes to the app-wide telemetry runtime. While `enabled` the component holds the runtime
 * (it samples at 1 Hz and re-renders once per tick); when disabled or unmounted it lets go, and the
 * runtime stops its timers when nobody else holds it.
 */
export function useVideoTelemetry(enabled = true, runtime: TelemetryRuntime = telemetryRuntime): TelemetryView {
  useEffect(() => {
    if (!enabled) return;
    runtime.acquire();
    return () => runtime.release();
  }, [enabled, runtime]);
  return useSyncExternalStore(runtime.subscribe, runtime.getView, runtime.getView);
}

export type CapabilitiesState =
  | { status: "loading"; capabilities?: undefined }
  | { status: "ready"; capabilities: ClientCapabilities }
  | { status: "error"; capabilities?: undefined };

/** Runs the full capability collection once per mount (it creates WebGL contexts and queries every codec). */
export function useClientCapabilities(collect: () => Promise<ClientCapabilities> = () => collectClientCapabilities()): CapabilitiesState {
  const [state, setState] = useState<CapabilitiesState>({ status: "loading" });
  useEffect(() => {
    let alive = true;
    collect().then(
      (capabilities) => alive && setState({ status: "ready", capabilities }),
      () => alive && setState({ status: "error" }),
    );
    return () => {
      alive = false;
    };
    // The collector is a stable entry point; the capabilities are gathered once per mount.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return state;
}
