import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { json, renderPage, stubApi } from "@/test-utils";
import type { ClientCapabilities, StreamStatsSample, StreamStatsSource } from "@/lib/telemetry";
import { ClientStation } from "./ClientStation";

const h = vi.hoisted(() => ({
  clock: 0,
  streams: [] as unknown[],
  tick: undefined as (() => void) | undefined,
  collect: undefined as undefined | (() => Promise<unknown>),
}));

vi.mock("@/lib/telemetry/runtime", async (orig) => {
  const actual = await orig<typeof import("@/lib/telemetry/runtime")>();
  const telemetryRuntime = actual.createTelemetryRuntime({
    now: () => h.clock,
    schedule: (fn) => {
      h.tick = fn;
      return () => {
        h.tick = undefined;
      };
    },
    sessions: () => h.streams as StreamStatsSource[],
  });
  return { ...actual, telemetryRuntime };
});

vi.mock("@/lib/telemetry/capabilities", async (orig) => {
  const actual = await orig<typeof import("@/lib/telemetry/capabilities")>();
  return { ...actual, collectClientCapabilities: () => (h.collect ?? (() => new Promise(() => undefined)))() };
});

import { telemetryRuntime } from "@/lib/telemetry/runtime";

class FakeStream implements StreamStatsSource {
  bytes = 0;
  decoded = 0;
  constructor(private readonly cameraId: string, private readonly quality: "sub" | "main" = "sub") {}
  streamStats(): StreamStatsSample {
    return { cameraId: this.cameraId, quality: this.quality, state: "ACTIVE", bytesReceived: this.bytes, decodedFrames: this.decoded, droppedFrames: 0, width: 640, height: 360, reconnects: 0, stalls: 0 };
  }
}

function caps(patch: Partial<ClientCapabilities> = {}): ClientCapabilities {
  return {
    runtime: "browser",
    collectedAt: 1,
    system: {
      os: "Windows",
      osVersion: "10.0.0",
      architecture: "x86",
      browser: "Chrome",
      browserVersion: "126.0.0.0",
      userAgent: "Mozilla/5.0 TestAgent",
      screen: { width: 1920, height: 1080, devicePixelRatio: 1.25 },
      viewport: { width: 1900, height: 960 },
      multipleScreens: true,
    },
    cpu: { logicalProcessors: 16 },
    memory: { reportedGB: 16, bucketed: true, source: "deviceMemory" },
    gpu: { webgl: true, webgl2: true, vendor: "NVIDIA Corporation", renderer: "NVIDIA GeForce RTX 3060", maxTextureSize: 16384, maxViewportDims: [32767, 32767] },
    codecs: [
      { codec: "h264", profile: { label: "360p@15", width: 640, height: 360, fps: 15 }, codecString: "avc1.42E01E", supported: true, smooth: true, powerEfficient: false, source: "mediaCapabilities" },
      { codec: "hevc", profile: { label: "1080p@30", width: 1920, height: 1080, fps: 30 }, codecString: "hvc1", supported: false, smooth: false, powerEfficient: false, source: "mediaCapabilities" },
    ],
    ...patch,
  };
}

function stubFetch() {
  vi.stubGlobal(
    "fetch",
    vi.fn(stubApi({ "/api/v1/system/info": () => json({ name: "openvms-api", version: "v0.1.0", commit: "abc1234", build_time: "t", go_version: "go", schema_version: 1 }) })),
  );
}

function tick(streams: FakeStream[] = []) {
  h.clock += 1000;
  for (const s of streams) {
    s.bytes += 2_400_000; // 19.2 Mbps each over 1 s
    s.decoded += 15;
  }
  act(() => h.tick?.());
}

beforeEach(() => {
  h.clock = 1_000;
  h.streams = [];
  h.collect = async () => caps();
  localStorage.clear();
  stubFetch();
});
afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

describe("ClientStation: capabilities", () => {
  it("shows a loading state, then system, CPU, memory and GPU", async () => {
    let finish: (c: ClientCapabilities) => void = () => undefined;
    h.collect = () => new Promise<ClientCapabilities>((r) => (finish = r));
    renderPage(ClientStation);
    expect(await screen.findByText("Detectando capacidades de esta estación…")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Estación cliente" })).toBeInTheDocument();
    await act(async () => finish(caps()));
    expect(screen.queryByText("Detectando capacidades de esta estación…")).not.toBeInTheDocument();
    expect(await screen.findByText("16 procesadores lógicos")).toBeInTheDocument();
    expect(screen.getByText("Memoria reportada por navegador: 16 GB o más")).toBeInTheDocument();
    expect(screen.getByText("Windows")).toBeInTheDocument();
    expect(screen.getByText("Chrome 126.0.0.0")).toBeInTheDocument();
    expect(screen.getByText("1920×1080 px, escala 1.25×")).toBeInTheDocument();
    expect(screen.getByText("Varias pantallas")).toBeInTheDocument();
    expect(screen.getByText("NVIDIA GeForce RTX 3060")).toBeInTheDocument();
    expect(screen.getByText("16384")).toBeInTheDocument();
    // The CPU model is never invented.
    expect(screen.getByText("El navegador no informa el modelo de CPU.")).toBeInTheDocument();
  });

  it("keeps the user agent inside a collapsed technical details block", async () => {
    renderPage(ClientStation);
    const ua = await screen.findByText("Mozilla/5.0 TestAgent");
    const details = ua.closest("details");
    expect(details).not.toBeNull();
    expect(details).not.toHaveAttribute("open");
  });

  it("does not claim multiple screens when the browser does not say", async () => {
    h.collect = async () => caps({ system: { os: "Linux" } });
    renderPage(ClientStation);
    await screen.findByText("16 procesadores lógicos");
    expect(screen.queryByText("Varias pantallas")).not.toBeInTheDocument();
  });

  it("reports unavailable memory, CPU and WebGL without treating them as errors", async () => {
    h.collect = async () => caps({ cpu: {}, memory: { bucketed: false, source: "unavailable" }, gpu: { webgl: false, webgl2: false } });
    renderPage(ClientStation);
    expect(await screen.findByText("Memoria reportada por navegador: no disponible")).toBeInTheDocument();
    expect(screen.getByText(/WebGL no está disponible/)).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows an error note when the capabilities cannot be collected", async () => {
    h.collect = async () => {
      throw new Error("boom");
    };
    renderPage(ClientStation);
    expect(await screen.findByRole("alert")).toHaveTextContent("No se pudieron detectar las capacidades de esta estación.");
  });
});

describe("ClientStation: codecs", () => {
  it("renders codec by profile with separate supported, smooth and efficient columns", async () => {
    renderPage(ClientStation);
    const table = await screen.findByRole("table", { name: "Códecs de video por perfil" });
    const headers = within(table).getAllByRole("columnheader").map((c) => c.textContent);
    expect(headers).toEqual(["Códec", "Perfil", "Soportado", "Fluido", "Eficiente"]);
    const rows = within(table).getAllByRole("row");
    expect(within(rows[1]!).getAllByRole("cell").map((c) => c.textContent)).toEqual(["H.264", "360p@15", "Sí", "Sí", "No"]);
    expect(within(rows[2]!).getAllByRole("cell").map((c) => c.textContent)).toEqual(["HEVC", "1080p@30", "No", "No", "No"]);
    expect(screen.queryByText(/aceleración por hardware/i)).not.toBeInTheDocument();
  });

  it("shows unknown smooth/efficient values and the source when media capabilities are not available", async () => {
    h.collect = async () =>
      caps({
        codecs: [{ codec: "vp9", profile: { label: "360p@15", width: 640, height: 360, fps: 15 }, codecString: "vp09", supported: true, source: "isTypeSupported" }],
      });
    renderPage(ClientStation);
    const table = await screen.findByRole("table", { name: "Códecs de video por perfil" });
    const cells = within(within(table).getAllByRole("row")[1]!).getAllByRole("cell").map((c) => c.textContent);
    expect(cells).toEqual(["VP9", "360p@15", "Sí", "—", "—"]);
    expect(screen.getByText(/MediaSource\.isTypeSupported/)).toBeInTheDocument();
  });
});

describe("ClientStation: bandwidth and health", () => {
  it("shows an empty state, and no verdict, when no stream is active", async () => {
    renderPage(ClientStation);
    expect(await screen.findByText("Sin streams activos. Abrí Vivo para medir el videowall.")).toBeInTheDocument();
    expect(screen.getByTestId("health-state")).toHaveTextContent("Sin datos");
  });

  it("acquires the runtime while mounted and releases it on unmount", async () => {
    const { unmount } = renderPage(ClientStation);
    await screen.findByRole("heading", { name: "Estación cliente" });
    expect(telemetryRuntime.consumers()).toBe(1);
    unmount();
    expect(telemetryRuntime.consumers()).toBe(0);
    expect(h.tick).toBeUndefined();
  });

  it("shows the OpenVMS-only total, the chart summary and the per-camera table", async () => {
    const streams = [new FakeStream("cam-1"), new FakeStream("cam-2", "main")];
    h.streams = streams;
    renderPage(ClientStation);
    await screen.findByText("16 procesadores lógicos");
    tick(streams);
    expect(screen.getByText("Recepción de video: 38.4 Mbps")).toBeInTheDocument();
    expect(screen.getByText(/Solo tráfico de video de OpenVMS/)).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /últimos 1 s: mínimo 38\.4 Mbps, máximo 38\.4 Mbps, actual 38\.4 Mbps/ })).toBeInTheDocument();
    const table = screen.getByRole("table", { name: "Tráfico por cámara" });
    expect(within(table).getAllByRole("columnheader").map((c) => c.textContent)).toEqual(["Cámara", "Stream", "Resolución", "FPS", "Bitrate"]);
    const rows = within(table).getAllByRole("row");
    expect(within(rows[1]!).getAllByRole("cell").map((c) => c.textContent)).toEqual(["cam-1", "Sub", "640×360", "15", "19.2 Mbps"]);
    expect(within(rows[2]!).getAllByRole("cell")[1]).toHaveTextContent("Main");
  });

  it("labels the bottleneck as an estimate once there is enough data", async () => {
    const streams = [new FakeStream("cam-1")];
    h.streams = streams;
    renderPage(ClientStation);
    await screen.findByText("16 procesadores lógicos");
    for (let i = 0; i < 4; i++) tick(streams);
    expect(screen.getByTestId("health-state")).not.toHaveTextContent("Sin datos");
    expect(screen.getByText(/Cuello de botella probable:|Sin cuello de botella/)).toHaveTextContent("(estimación)");
    expect(screen.getByRole("table", { name: "Estado por capa" })).toBeInTheDocument();
  });
});

describe("ClientStation: advanced diagnostics", () => {
  it("keeps the diagnostic text collapsed, with the app version when available", async () => {
    renderPage(ClientStation);
    const pre = await screen.findByText(/OpenVMS Client Diagnostics/);
    expect(pre.tagName).toBe("PRE");
    expect(pre.closest("details")).not.toHaveAttribute("open");
    await waitFor(() => expect(pre.textContent).toContain("v0.1.0"));
  });

  it("copies the diagnostics and confirms", async () => {
    const writeText = vi.fn(async () => undefined);
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    renderPage(ClientStation);
    await screen.findByText("16 procesadores lógicos");
    fireEvent.click(screen.getByRole("button", { name: "Copiar diagnóstico" }));
    expect(await screen.findByRole("button", { name: "Copiado" })).toBeInTheDocument();
    expect(writeText).toHaveBeenCalledTimes(1);
    expect((writeText.mock.calls[0] as unknown as [string])[0]).toContain("OpenVMS Client Diagnostics");
  });

  it("tells the operator when the clipboard refuses", async () => {
    const writeText = vi.fn(async () => {
      throw new Error("denied");
    });
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    renderPage(ClientStation);
    await screen.findByText("16 procesadores lógicos");
    fireEvent.click(screen.getByRole("button", { name: "Copiar diagnóstico" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("No se pudo copiar");
    expect(screen.queryByRole("button", { name: "Copiado" })).not.toBeInTheDocument();
  });
});

describe("ClientStation: overlay toggle", () => {
  it("is off by default and persists the choice in localStorage", async () => {
    renderPage(ClientStation);
    const toggle = await screen.findByRole("switch", { name: "Overlay de desarrollo en Vivo" });
    expect(toggle).toHaveAttribute("aria-checked", "false");
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-checked", "true");
    expect(localStorage.getItem("openvms.live.telemetryOverlay")).toBe("1");
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-checked", "false");
    expect(localStorage.getItem("openvms.live.telemetryOverlay")).not.toBe("1");
  });

  it("starts on when the preference was saved", async () => {
    localStorage.setItem("openvms.live.telemetryOverlay", "1");
    renderPage(ClientStation);
    expect(await screen.findByRole("switch", { name: "Overlay de desarrollo en Vivo" })).toHaveAttribute("aria-checked", "true");
  });
});
