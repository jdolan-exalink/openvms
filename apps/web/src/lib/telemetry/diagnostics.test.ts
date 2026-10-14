import { describe, expect, it, vi } from "vitest";
import { buildDiagnostics, copyDiagnostics, formatDiagnosticsText, MAX_ERROR_MESSAGE_LENGTH, MAX_RECENT_ERRORS, redactText } from "./diagnostics";
import { classifyHealth } from "./performanceClassifier";
import { estimateBottleneck } from "./bottleneck";
import type { ClientCapabilities, VideoPerformanceMetrics, VideoTelemetrySnapshot } from "./types";

const metrics: VideoPerformanceMetrics = {
  timestamp: 5_000,
  visibleCameras: 48,
  activeStreams: 48,
  mainStreams: 1,
  subStreams: 47,
  receivedMbps: 42.8,
  decodedFrames: 1000,
  droppedFrames: 7,
  droppedFrameRatio: 0.007,
  presentationFps: 59,
  renderFrameTimeMs: 14,
  renderFrameTimeP95Ms: 18,
  stalls: 0,
};

const capabilities: ClientCapabilities = {
  runtime: "browser",
  collectedAt: 1,
  system: { os: "Windows", osVersion: "11", browser: "Chrome", browserVersion: "141", userAgent: "Mozilla/5.0 Chrome/141", screen: { width: 1920, height: 1080, devicePixelRatio: 1 } },
  cpu: { logicalProcessors: 16 },
  memory: { reportedGB: 8, bucketed: true, source: "deviceMemory" },
  gpu: { webgl: true, webgl2: true, renderer: "NVIDIA GeForce RTX 3070" },
  codecs: [
    { codec: "h264", profile: { label: "1080p30", width: 1920, height: 1080, fps: 30 }, codecString: "avc1.640028", supported: true, smooth: true, powerEfficient: true, source: "mediaCapabilities" },
    { codec: "hevc", profile: { label: "1080p30", width: 1920, height: 1080, fps: 30 }, codecString: "hvc1", supported: false, source: "isTypeSupported" },
  ],
};

const telemetry: VideoTelemetrySnapshot = {
  metrics,
  streams: [{ cameraId: "cam-1", quality: "sub", state: "ACTIVE", width: 640, height: 360, fps: 10, bitrateBps: 620_000 }],
};

const window = Array.from({ length: 10 }, (_, i) => ({ ...metrics, timestamp: i * 1000 }));

function full() {
  return buildDiagnostics({
    appVersion: "1.2.3",
    commit: "abc1234",
    capabilities,
    telemetry,
    history: window,
    health: classifyHealth(window),
    bottleneck: estimateBottleneck(window),
    visibleCameras: [{ id: "cam-1", name: "Acceso Norte" }],
    recentErrors: [{ at: 1_000, code: "NETWORK", message: "boom" }],
    generatedAt: Date.UTC(2026, 0, 2, 3, 4, 5),
  });
}

describe("buildDiagnostics", () => {
  it("returns a JSON-serializable object with the expected sections", () => {
    const d = full();
    expect(JSON.parse(JSON.stringify(d))).toEqual(d);
    expect(d.generatedAt).toBe("2026-01-02T03:04:05.000Z");
    expect(d.appVersion).toBe("1.2.3");
    expect(d.capabilities?.gpu.renderer).toBe("NVIDIA GeForce RTX 3070");
    expect(d.telemetry?.metrics?.receivedMbps).toBe(42.8);
    expect(d.health?.state).toBe("optimal");
    expect(d.bottleneck?.estimated).toBe(true);
    expect(d.visibleCameras).toEqual([{ id: "cam-1", name: "Acceso Norte" }]);
  });

  it("works with only generatedAt", () => {
    const d = buildDiagnostics({ generatedAt: 0 });
    expect(JSON.parse(JSON.stringify(d))).toEqual(d);
  });

  it("keeps only camera ids and names, never stream URLs", () => {
    const d = buildDiagnostics({
      generatedAt: 0,
      visibleCameras: [{ id: "c1", name: "Gate", url: "rtsp://admin:hunter2@10.0.0.5/stream", streamUrl: "wss://host/live?token=abc" } as never],
    });
    const json = JSON.stringify(d);
    expect(json).not.toContain("rtsp");
    expect(json).not.toContain("hunter2");
    expect(json).not.toContain("wss://");
    expect(d.visibleCameras).toEqual([{ id: "c1", name: "Gate" }]);
  });

  it("caps recent errors to the latest ones and truncates long messages", () => {
    const recentErrors = Array.from({ length: MAX_RECENT_ERRORS + 5 }, (_, i) => ({ at: i, code: `E${i}`, message: "x ".repeat(500) }));
    const d = buildDiagnostics({ generatedAt: 0, recentErrors });
    expect(d.recentErrors).toHaveLength(MAX_RECENT_ERRORS);
    expect(d.recentErrors?.[MAX_RECENT_ERRORS - 1]?.code).toBe(`E${MAX_RECENT_ERRORS + 4}`);
    expect(d.recentErrors?.every((e) => (e.message?.length ?? 0) <= MAX_ERROR_MESSAGE_LENGTH + 1)).toBe(true);
  });

  it("caps the history", () => {
    const d = buildDiagnostics({ generatedAt: 0, history: Array.from({ length: 200 }, () => metrics) });
    expect(d.history?.length).toBe(60);
  });

  it("replaces non-finite numbers so the output stays valid JSON", () => {
    const d = buildDiagnostics({ generatedAt: 0, history: [{ ...metrics, receivedMbps: Number.NaN, renderFrameTimeMs: Number.POSITIVE_INFINITY }] });
    expect(d.history?.[0]?.receivedMbps).toBeNull();
  });
});

describe("redaction", () => {
  const jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r";
  const hex = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08";
  const b64 = "dGhpcy1pcy1hLXZlcnktbG9uZy1iYXNlNjQtc2VjcmV0LXZhbHVlLTEyMzQ1Ng";
  const cases: Array<[string, string, string]> = [
    ["Bearer token", "request failed Authorization: Bearer abc.def-ghi_123 on fetch", "abc.def-ghi_123"],
    ["Basic authorization", "Authorization: Basic dXNlcjpwYXNz", "dXNlcjpwYXNz"],
    ["JWT", `bad session ${jwt}`, jwt],
    ["token= query", "GET /live?token=s3cr3tvalue&x=1", "s3cr3tvalue"],
    ["access_token= query", "GET /v?access_token=AT-12345-secret", "AT-12345-secret"],
    ["password= param", "login password=hunter2 rejected", "hunter2"],
    ["secret= param", "client secret=topsecretvalue", "topsecretvalue"],
    ["JSON password", '{"password":"p4ssw0rd!"}', "p4ssw0rd!"],
    ["URL userinfo", "connect rtsp://admin:hunter2@10.0.0.5/live failed", "hunter2"],
    ["bare userinfo", "dial admin:hunter2@10.0.0.5 failed", "hunter2"],
    ["long hex", `session ${hex}`, hex],
    ["long base64", `blob ${b64}`, b64],
  ];

  it.each(cases)("masks %s in error messages (JSON and text)", (_name, message, secret) => {
    const d = buildDiagnostics({ generatedAt: 0, recentErrors: [{ at: 1, code: "X", message }] });
    expect(JSON.stringify(d)).not.toContain(secret);
    expect(formatDiagnosticsText(d)).not.toContain(secret);
  });

  it("masks secrets in structured strings and secret-named keys", () => {
    const d = buildDiagnostics({
      generatedAt: 0,
      appVersion: "1.0 token=abc123secret",
      benchmark: { note: "Bearer zzz.yyy.xxx", password: "plain-pass", nested: { authorization: "Basic QWxhZGRpbg==" } },
    });
    const json = JSON.stringify(d);
    for (const s of ["abc123secret", "zzz.yyy.xxx", "plain-pass", "QWxhZGRpbg"]) expect(json).not.toContain(s);
    expect(formatDiagnosticsText(d)).not.toContain("abc123secret");
  });

  it("keeps harmless content: user agent, UUID camera ids and plain messages", () => {
    const id = "123e4567-e89b-12d3-a456-426614174000";
    const d = buildDiagnostics({ generatedAt: 0, capabilities, visibleCameras: [{ id, name: "Acceso" }], recentErrors: [{ at: 1, code: "DECODE", message: "decoder reset" }] });
    expect(d.capabilities?.system.userAgent).toBe("Mozilla/5.0 Chrome/141");
    expect(d.visibleCameras?.[0]?.id).toBe(id);
    expect(d.recentErrors?.[0]?.message).toBe("decoder reset");
  });

  it("redactText leaves ordinary text untouched", () => {
    expect(redactText("video decoder stalled after 3 retries")).toBe("video decoder stalled after 3 retries");
  });
});

describe("formatDiagnosticsText", () => {
  it("renders the support block", () => {
    const text = formatDiagnosticsText(full());
    for (const line of [
      "Browser: Chrome 141",
      "OS: Windows 11",
      "CPU threads: 16",
      "GPU/WebGL: NVIDIA GeForce RTX 3070",
      "WebGL2: Yes",
      "H264 1080p30: supported / smooth / power efficient",
      "HEVC 1080p30: not supported",
      "Visible cameras: 48",
      "Main streams: 1",
      "Traffic: 42.8 Mbps",
      "Dropped frames: 0.7%",
      "Render: 59 FPS",
      "Performance state: OPTIMAL",
      "Estimated bottleneck: none (estimate",
    ]) {
      expect(text).toContain(line);
    }
  });

  it("omits sections that were not provided", () => {
    const text = formatDiagnosticsText(buildDiagnostics({ generatedAt: 0 }));
    expect(text).toContain("Client Diagnostics");
    expect(text).not.toContain("Traffic:");
  });

  it("lists recent errors", () => {
    expect(formatDiagnosticsText(full())).toContain("NETWORK: boom");
  });

  it("never throws on non-finite metrics and prints n/a instead", () => {
    const bad = {
      ...metrics,
      receivedMbps: Number.NaN,
      droppedFrameRatio: Number.POSITIVE_INFINITY,
      presentationFps: Number.NaN,
      renderFrameTimeP95Ms: Number.NEGATIVE_INFINITY,
    };
    const diag = buildDiagnostics({ generatedAt: 0, telemetry: { metrics: bad, streams: [] }, history: [bad] });
    let text = "";
    expect(() => {
      text = formatDiagnosticsText(diag);
    }).not.toThrow();
    expect(text).toContain("Traffic: n/a");
    expect(text).toContain("Dropped frames: n/a");
    expect(text).toContain("Render: n/a");
    expect(text).toContain("Frame time p95: n/a");
    expect(text).not.toContain("0 FPS");
    expect(text).not.toContain("null");
  });

  it("prints n/a for null counters and an unknown performance state", () => {
    const diag = buildDiagnostics({
      generatedAt: 0,
      telemetry: { metrics: { ...metrics, visibleCameras: Number.NaN }, streams: [] },
      health: classifyHealth([]),
    });
    const text = formatDiagnosticsText(diag);
    expect(text).toContain("Visible cameras: n/a");
    expect(text).toContain("Performance state: UNKNOWN");
  });

  it("never throws on invalid timestamps", () => {
    for (const bad of [Number.NaN, Number.POSITIVE_INFINITY, 8.64e15 + 1]) {
      let diag!: ReturnType<typeof buildDiagnostics>;
      expect(() => {
        diag = buildDiagnostics({ generatedAt: bad, recentErrors: [{ at: bad, code: "NETWORK", message: "boom" }] });
      }).not.toThrow();
      expect(diag.generatedAt).toBeNull();
      const text = formatDiagnosticsText(diag);
      expect(text).toContain("Generated: invalid time");
      expect(text).toContain("invalid time NETWORK: boom");
    }
  });

  it("formats a valid timestamp as ISO", () => {
    const diag = buildDiagnostics({ generatedAt: 0, recentErrors: [{ at: 1000, code: "X" }] });
    expect(diag.generatedAt).toBe("1970-01-01T00:00:00.000Z");
    expect(formatDiagnosticsText(diag)).toContain("1970-01-01T00:00:01.000Z X");
  });
});

describe("copyDiagnostics", () => {
  it("returns true when the clipboard accepts the text", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    await expect(copyDiagnostics("hello", { writeText })).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith("hello");
  });

  it("returns false when the clipboard rejects or is missing", async () => {
    await expect(copyDiagnostics("x", { writeText: vi.fn().mockRejectedValue(new Error("denied")) })).resolves.toBe(false);
    await expect(copyDiagnostics("x", undefined)).resolves.toBe(false);
  });
describe("benchmark in diagnostics", () => {
  const benchmark = {
    id: "b1",
    startedAt: 1_000,
    finishedAt: 5_000,
    durationMs: 4_000,
    clip: { codec: "avc1.42E01E", width: 640, height: 360, fps: 15 },
    levels: [
      { streams: 4, ran: true, state: "optimal", decodedFps: 15, droppedRatio: 0, stalls: 0, decoderErrors: 0, presentationFps: 60, frameTimeP95Ms: 17, reasons: [] },
      { streams: 9, ran: true, state: "limit", decodedFps: 12.5, droppedRatio: 0.062, stalls: 1, decoderErrors: 0, presentationFps: 45, frameTimeP95Ms: 41, reasons: ["dropped_frames"] },
      { streams: 16, ran: false, state: "unknown", stalls: 0, decoderErrors: 0, reasons: [] },
    ],
    maxStableStreams: 4,
    clientPerformanceScore: 12,
    limits: { firstDegradedStreams: 9, bindingFactors: ["dropped_frames"] },
    stoppedReason: "completed",
  };

  it("keeps the raw benchmark result in the JSON next to the score", () => {
    const diag = buildDiagnostics({ generatedAt: 6_000, benchmark });
    expect(diag.benchmark).toMatchObject({ clientPerformanceScore: 12, maxStableStreams: 4, levels: [{ streams: 4 }, { streams: 9 }, { streams: 16 }] });
  });

  it("prints a support block that says what the benchmark did not measure and never claims HQ", () => {
    const text = formatDiagnosticsText(buildDiagnostics({ generatedAt: 6_000, benchmark }));
    expect(text).toContain("Benchmark");
    expect(text).toContain("sample clip");
    expect(text).toContain("not measured: network, HQ main stream");
    expect(text).toContain("4 streams LQ: optimal");
    expect(text).toContain("9 streams LQ: limit");
    expect(text).toContain("dropped 6.2%");
    expect(text).toContain("16 streams LQ: not run");
    expect(text).toContain("Max stable: 4 streams");
    expect(text).toContain("Score (internal): 12");
    expect(text).not.toMatch(/\d+ streams HQ/);
  });

  it("omits the block without a benchmark and tolerates a malformed one", () => {
    expect(formatDiagnosticsText(buildDiagnostics({ generatedAt: 6_000 }))).not.toContain("Benchmark");
    expect(() => formatDiagnosticsText(buildDiagnostics({ generatedAt: 6_000, benchmark: { levels: "nope" } }))).not.toThrow();
    expect(() => formatDiagnosticsText(buildDiagnostics({ generatedAt: 6_000, benchmark: { levels: [null, { streams: Number.NaN }] } }))).not.toThrow();
  });
});
});
