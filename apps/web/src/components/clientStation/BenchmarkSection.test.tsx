import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { BenchmarkDeps, BenchmarkLevelResult, BenchmarkOptions, BenchmarkResult } from "@/lib/telemetry/benchmark";
import { buildBenchmarkRecord, type BenchmarkRecord } from "@/lib/telemetry/benchmarkCache";
import { createBenchmarkController } from "@/lib/telemetry/benchmarkRuntime";
import { createStartupProfile } from "@/lib/telemetry/startupProfile";
import type { ClientCapabilities } from "@/lib/telemetry";
import { BenchmarkSection } from "./BenchmarkSection";
import { useBenchmark } from "./hooks";

const NOW = 2_000_000_000_000;

function caps(renderer = "RTX 3060"): ClientCapabilities {
  return {
    runtime: "browser",
    collectedAt: 1,
    system: { browser: "Chrome", browserVersion: "126.0.1" },
    cpu: {},
    memory: { bucketed: false, source: "unavailable" },
    gpu: { webgl: true, webgl2: true, vendor: "NVIDIA", renderer },
    codecs: [],
  };
}

function level(streams: number, state: BenchmarkLevelResult["state"], ran = true, patch: Partial<BenchmarkLevelResult> = {}): BenchmarkLevelResult {
  return ran
    ? { streams, ran, state, decodedFps: 15, droppedRatio: 0.0123, stalls: 0, decoderErrors: 0, presentationFps: 60, frameTimeP95Ms: 17.4, reasons: [], ...patch }
    : { streams, ran: false, state: "unknown", stalls: 0, decoderErrors: 0, reasons: [] };
}

function result(patch: Partial<BenchmarkResult> = {}): BenchmarkResult {
  return {
    id: "r1",
    startedAt: NOW - 4000,
    finishedAt: NOW,
    durationMs: 4000,
    clip: { codec: "avc1.42E01E", width: 640, height: 360, fps: 15 },
    levels: [level(4, "optimal"), level(9, "optimal"), level(16, "limit", true, { droppedRatio: 0.07, decodedFps: 12.5 }), level(25, "saturated"), level(32, "unknown", false), level(48, "unknown", false)],
    maxStableStreams: 9,
    clientPerformanceScore: 20,
    limits: { bindingFactors: [], stoppedAtStreams: 25, saturatedAtStreams: 25, firstDegradedStreams: 16 },
    stoppedReason: "saturated",
    ...patch,
  };
}

type Run = (deps: BenchmarkDeps, opts?: BenchmarkOptions) => Promise<BenchmarkResult>;

function setup(run: Run, cached?: BenchmarkRecord, props: { capabilities?: ClientCapabilities | null; appVersion?: string } = {}) {
  const save = vi.fn(async () => undefined);
  // The controller needs the profile to refresh, and the profile needs the cache: tie the knot lazily.
  const refs: { profile?: ReturnType<typeof createStartupProfile> } = {};
  const controller = createBenchmarkController({
    run,
    createDeps: (container) => ({ container }) as unknown as BenchmarkDeps,
    cache: { save },
    profile: { refresh: async () => refs.profile?.refresh() },
  });
  const profile = createStartupProfile({
    collect: async () => caps(),
    cache: { latest: async () => cached },
    schedule: (fn) => {
      fn();
      return () => undefined;
    },
    now: () => NOW + 1000,
    loadAppVersion: async () => "v1.0.0",
  });
  refs.profile = profile;
  function Harness() {
    const bench = useBenchmark({ capabilities: props.capabilities === null ? undefined : (props.capabilities ?? caps()), appVersion: props.appVersion ?? "v1.0.0", controller, profile });
    return <BenchmarkSection bench={bench} />;
  }
  const view = render(<Harness />);
  return { ...view, controller, profile, save };
}

const stage = () => screen.getByTestId("benchmark-stage");

describe("BenchmarkSection", () => {
  it("explains the scope and offers to run when nothing was benchmarked", async () => {
    setup(async () => result());
    expect(screen.getByRole("heading", { name: "Benchmark de estación" })).toBeInTheDocument();
    expect(screen.getByText(/clip de muestra local/)).toHaveTextContent("No mide la red ni el stream principal (HQ)");
    expect(await screen.findByText("Todavía no se ejecutó ningún benchmark en este navegador.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Ejecutar benchmark" })).toBeEnabled();
    // The test area exists but is not shown, and no dialog is announced.
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(stage()).toBeEmptyDOMElement();
  });

  it("is disabled, with a reason, until the capabilities are known", () => {
    setup(async () => result(), undefined, { capabilities: null });
    expect(screen.getByRole("button", { name: "Ejecutar benchmark" })).toBeDisabled();
    expect(screen.getByText("Disponible cuando termine la detección de capacidades.")).toBeInTheDocument();
  });

  it("runs on the visible test area, shows progress and a cancel button, then the result", async () => {
    let finish: (r: BenchmarkResult) => void = () => undefined;
    let containerSeen: HTMLElement | undefined;
    let report: BenchmarkOptions["onProgress"];
    const { save } = setup((deps, opts) => {
      containerSeen = deps.container;
      report = opts?.onProgress;
      return new Promise<BenchmarkResult>((r) => (finish = r));
    });
    fireEvent.click(screen.getByRole("button", { name: "Ejecutar benchmark" }));
    const dialog = await screen.findByRole("dialog", { name: "Prueba de benchmark en curso" });
    expect(within(dialog).getByRole("button", { name: "Cancelar" })).toBeInTheDocument();
    expect(containerSeen).toBe(stage());
    expect(dialog).toContainElement(stage());
    expect(screen.getByRole("button", { name: "Ejecutar benchmark" })).toBeDisabled();
    act(() => report?.({ phase: "loading" }));
    expect(within(dialog).getByText("Cargando el clip de muestra…")).toBeInTheDocument();
    act(() => report?.({ phase: "testing", streams: 16, levelIndex: 2, levelCount: 6 }));
    expect(within(dialog).getByText("Probando 16 streams…")).toBeInTheDocument();

    await act(async () => finish(result()));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(save).toHaveBeenCalledTimes(1);

    const summary = screen.getByRole("heading", { name: "Capacidad estimada de esta estación" }).closest("div")!;
    expect(within(summary).getByText("25 cámaras LQ: Saturado")).toBeInTheDocument();
    expect(within(summary).getByText("16 cámaras LQ: Al límite")).toBeInTheDocument();
    expect(within(summary).getByText("9 cámaras LQ: Óptimo")).toBeInTheDocument();
    expect(within(summary).queryByText(/32 cámaras/)).not.toBeInTheDocument();
    // HQ is never claimed.
    expect(within(summary).getByText("Cámaras HQ (stream principal): no medido")).toBeInTheDocument();
    expect(summary.textContent).not.toMatch(/\d+ cámaras HQ/);
    expect(screen.getByText("Máximo estable medido: 9 cámaras LQ.")).toBeInTheDocument();
    expect(screen.getByText("La prueba se detuvo en 25 streams para no degradar la estación.")).toBeInTheDocument();

    const table = screen.getByRole("table", { name: "Resultado del benchmark por cantidad de streams" });
    expect(within(table).getAllByRole("columnheader").map((c) => c.textContent)).toEqual(["Streams", "Estado", "FPS decodificado", "Descartados", "Frame time p95"]);
    const rows = within(table).getAllByRole("row");
    expect(within(rows[3]!).getAllByRole("cell").map((c) => c.textContent)).toEqual(["16", "Al límite", "12.5", "7.0%", "17 ms"]);
    expect(within(rows[5]!).getAllByRole("cell").map((c) => c.textContent)).toEqual(["32", "No ejecutado", "—", "—", "—"]);
    expect(screen.getByRole("button", { name: "Repetir benchmark" })).toBeEnabled();
  });

  it("cancels the run, keeps nothing and says so", async () => {
    let signal: AbortSignal | undefined;
    const { save } = setup(async (_deps, opts) => {
      signal = opts?.signal;
      await new Promise<void>((resolve) => signal!.addEventListener("abort", () => resolve()));
      return result({ stoppedReason: "aborted" });
    });
    fireEvent.click(screen.getByRole("button", { name: "Ejecutar benchmark" }));
    const dialog = await screen.findByRole("dialog");
    await act(async () => fireEvent.click(within(dialog).getByRole("button", { name: "Cancelar" })));
    expect(signal?.aborted).toBe(true);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByText("Benchmark cancelado. No se guardó ningún resultado.")).toBeInTheDocument();
    expect(save).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Ejecutar benchmark" })).toBeEnabled();
  });

  it("cancels a run still going when the section unmounts", async () => {
    let signal: AbortSignal | undefined;
    const { unmount } = setup((_deps, opts) => {
      signal = opts?.signal;
      return new Promise<BenchmarkResult>(() => undefined);
    });
    fireEvent.click(screen.getByRole("button", { name: "Ejecutar benchmark" }));
    await screen.findByRole("dialog");
    unmount();
    expect(signal?.aborted).toBe(true);
  });

  it("reports a failed run as an alert", async () => {
    setup(async () => result({ stoppedReason: "error", error: "clip 404", levels: [] }));
    fireEvent.click(screen.getByRole("button", { name: "Ejecutar benchmark" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("No se pudo completar el benchmark: clip 404");
  });

  it("tells the operator when the tab was hidden during the run", async () => {
    setup(async () => result({ stoppedReason: "hidden" }));
    fireEvent.click(screen.getByRole("button", { name: "Ejecutar benchmark" }));
    expect(await screen.findByText(/porque la pestaña quedó oculta/)).toBeInTheDocument();
  });

  it("warns when the result could not be saved", async () => {
    const { save } = setup(async () => result());
    save.mockRejectedValueOnce(new Error("quota"));
    fireEvent.click(screen.getByRole("button", { name: "Ejecutar benchmark" }));
    expect(await screen.findByText(/No se pudo guardar el resultado en este navegador/)).toBeInTheDocument();
  });
});

describe("BenchmarkSection: previous result", () => {
  it("shows the cached result with its date and says it is still valid", async () => {
    setup(async () => result(), buildBenchmarkRecord(result(), caps(), "v1.0.0"));
    expect(await screen.findByText("Último benchmark de este navegador")).toBeInTheDocument();
    expect(screen.getByText(/Realizado el/)).toBeInTheDocument();
    expect(screen.getByText("Este resultado sigue vigente para esta estación.")).toBeInTheDocument();
    expect(screen.queryByText(/Se recomienda repetir/)).not.toBeInTheDocument();
    expect(screen.getByRole("table", { name: "Resultado del benchmark por cantidad de streams" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Repetir benchmark" })).toBeEnabled();
  });

  it("recommends repeating, with every reason, when the station changed", async () => {
    const old = buildBenchmarkRecord(result({ finishedAt: NOW - 40 * 24 * 60 * 60 * 1000 }), caps("Intel UHD"), "v1.0.0");
    setup(async () => result(), old);
    const note = await screen.findByText(/Se recomienda repetir:/);
    expect(note).toHaveTextContent("cambió la GPU");
    expect(note).toHaveTextContent("el resultado tiene más de 30 días");
    expect(screen.queryByText("Este resultado sigue vigente para esta estación.")).not.toBeInTheDocument();
  });

  it("replaces the previous result with the fresh one after a run", async () => {
    const old = buildBenchmarkRecord(result({ maxStableStreams: 4, limits: { bindingFactors: [], stoppedAtStreams: 9 } }), caps(), "v1.0.0");
    setup(async () => result({ id: "fresh" }), old);
    await screen.findByText("Último benchmark de este navegador");
    fireEvent.click(screen.getByRole("button", { name: "Repetir benchmark" }));
    await screen.findByText("Máximo estable medido: 9 cámaras LQ.");
    expect(screen.queryByText("Último benchmark de este navegador")).not.toBeInTheDocument();
  });
});
