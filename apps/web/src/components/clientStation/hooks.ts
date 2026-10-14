import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import {
  benchmarkController,
  collectClientCapabilities,
  startupProfile,
  telemetryRuntime,
  type BenchmarkController,
  type BenchmarkRecord,
  type BenchmarkResult,
  type BenchmarkRunState,
  type ClientCapabilities,
  type StartupProfile,
  type StartupProfileSnapshot,
  type TelemetryRuntime,
  type TelemetryView,
} from "@/lib/telemetry";

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

export type BenchmarkView = {
  run: BenchmarkRunState;
  startup: StartupProfileSnapshot;
  running: boolean;
  /** Capabilities are known, so the result can be filed against this station. */
  canRun: boolean;
  start(): void;
  cancel(): void;
  /** The element the test tiles are mounted in; it is on screen only while a run is in progress. */
  attachStage: (element: HTMLDivElement | null) => void;
  /** What to show and export: the result of this session's run, else the cached one. */
  display: { result: BenchmarkResult; record?: BenchmarkRecord; fresh: boolean } | undefined;
};

export type UseBenchmarkOptions = {
  /** Full capabilities of the page; the light startup ones are used until then. */
  capabilities?: ClientCapabilities;
  appVersion?: string;
  controller?: BenchmarkController;
  profile?: StartupProfile;
};

/**
 * Container hook for the benchmark section: joins the run controller and the startup profile.
 * Mounting never runs a benchmark; leaving the page cancels one in progress (the tiles go away with it).
 */
export function useBenchmark({ capabilities, appVersion, controller = benchmarkController, profile = startupProfile }: UseBenchmarkOptions = {}): BenchmarkView {
  const run = useSyncExternalStore(controller.subscribe, controller.getState, controller.getState);
  const startup = useSyncExternalStore(profile.subscribe, profile.getSnapshot, profile.getSnapshot);
  // A callback ref into state: the element exists as soon as the section is mounted.
  const [stage, attachStage] = useState<HTMLDivElement | null>(null);
  useEffect(() => profile.start(), [profile]);
  useEffect(() => {
    if (appVersion) profile.setAppVersion(appVersion);
  }, [profile, appVersion]);
  useEffect(() => () => controller.cancel(), [controller]);

  const known = capabilities ?? startup.capabilities;
  const running = run.status === "running";
  const start = useCallback(() => {
    if (!stage || !known) return;
    void controller.start(stage, { capabilities: known, appVersion });
  }, [controller, stage, known, appVersion]);
  const cancel = useCallback(() => controller.cancel(), [controller]);

  const display: BenchmarkView["display"] =
    run.status === "done" && run.result
      ? { result: run.result, fresh: true }
      : startup.latest
        ? { result: startup.latest.result, record: startup.latest, fresh: false }
        : undefined;
  return { run, startup, running, canRun: known !== undefined && !running, start, cancel, attachStage, display };
}
