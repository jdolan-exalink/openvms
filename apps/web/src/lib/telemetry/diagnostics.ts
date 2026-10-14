import type { BottleneckEstimate } from "./bottleneck";
import type { HealthAssessment } from "./performanceClassifier";
import type { ClientCapabilities, VideoPerformanceMetrics, VideoTelemetrySnapshot } from "./types";

/**
 * Builds the "Copiar diagnóstico" payload: a JSON-serializable object plus a compact text block
 * for support. Everything that leaves this module is redacted: tokens, passwords, credentials,
 * URLs (which may embed stream credentials) and long opaque secrets are masked, and camera stream
 * URLs are never copied (only camera ids and names). Framework-free; the clipboard is injectable.
 */

/** Most recent error entries kept. */
export const MAX_RECENT_ERRORS = 20;
/** Error messages are cut to this many characters (after redaction). */
export const MAX_ERROR_MESSAGE_LENGTH = 200;
/** History samples kept (matches the aggregator's ring buffer). */
export const MAX_HISTORY_SAMPLES = 60;
export const DIAGNOSTICS_SCHEMA = "openvms.client-diagnostics/v1";

const REDACTED = "[REDACTED]";

export type DiagnosticsError = { at: number; code: string; message?: string };
export type DiagnosticsCamera = { id: string; name?: string };

export type DiagnosticsInput = {
  appVersion?: string;
  commit?: string;
  capabilities?: ClientCapabilities;
  telemetry?: VideoTelemetrySnapshot;
  history?: readonly VideoPerformanceMetrics[];
  health?: HealthAssessment;
  bottleneck?: BottleneckEstimate;
  /** Benchmark result; opaque to this module and redacted like everything else. */
  benchmark?: unknown;
  recentErrors?: readonly DiagnosticsError[];
  /** Camera ids and names only; any other property (stream URLs included) is dropped. */
  visibleCameras?: readonly DiagnosticsCamera[];
  /** Epoch milliseconds. */
  generatedAt: number;
};

export type Diagnostics = {
  schema: typeof DIAGNOSTICS_SCHEMA;
  generatedAt: string;
  appVersion?: string;
  commit?: string;
  capabilities?: ClientCapabilities;
  telemetry?: VideoTelemetrySnapshot;
  history?: VideoPerformanceMetrics[];
  health?: HealthAssessment;
  bottleneck?: BottleneckEstimate;
  benchmark?: unknown;
  recentErrors?: DiagnosticsError[];
  visibleCameras?: DiagnosticsCamera[];
};

// ---------------------------------------------------------------------------------------------
// Redaction
// ---------------------------------------------------------------------------------------------

const URL_RE = /\b[a-z][a-z0-9+.-]*:\/\/\S+/gi;
const JWT_RE = /\beyJ[\w-]+\.[\w-]+\.[\w-]*/g;
const AUTH_HEADER_RE = /\bauthorization\b(["']?\s*[:=]\s*["']?)(?:(?:bearer|basic|digest|token)\s+)?[^\s,;"'}]+/gi;
const BEARER_RE = /\bbearer\s+[\w.~+/=-]{6,}/gi;
const KEY_VALUE_RE = /\b([\w-]*(?:token|password|passwd|pwd|secret|api[_-]?key|signature))(["']?\s*[:=]\s*["']?)[^\s&"',;}]+/gi;
const USERINFO_RE = /\b[^\s/@:]+:[^\s/@]+@(?=[\w.-]+)/g;
const LONG_HEX_RE = /\b[a-f0-9]{32,}\b/gi;
const LONG_OPAQUE_RE = /\b(?=[\w+/-]*\d)(?=[\w+/-]*[A-Za-z])[\w+/-]{40,}={0,2}/g;
const SENSITIVE_KEY_RE = /token|password|passwd|pwd|secret|authorization|cookie|credential|api[_-]?key/i;

function redactStructured(value: string): string {
  return value
    .replace(URL_RE, "[url]")
    .replace(JWT_RE, REDACTED)
    .replace(AUTH_HEADER_RE, `authorization$1${REDACTED}`)
    .replace(BEARER_RE, `Bearer ${REDACTED}`)
    .replace(KEY_VALUE_RE, `$1$2${REDACTED}`)
    .replace(USERINFO_RE, `${REDACTED}@`);
}

/**
 * Masks secrets in free-form text such as error messages: URLs, JWTs, Authorization headers,
 * `token=`-style pairs, `user:pass@host` and long hex/base64 blobs.
 */
export function redactText(text: string): string {
  return redactStructured(text).replace(LONG_HEX_RE, REDACTED).replace(LONG_OPAQUE_RE, REDACTED);
}

/**
 * Recursively copies a value into plain JSON data. Strings pass the structured redaction (not the
 * long-blob rules, so dash-less camera ids survive); values under secret-named keys are masked;
 * non-finite numbers become null; undefined properties are dropped.
 */
function sanitize(value: unknown): unknown {
  if (typeof value === "string") return redactStructured(value);
  if (typeof value === "number") return Number.isFinite(value) ? value : null;
  if (value === null || typeof value === "boolean") return value;
  if (Array.isArray(value)) return value.map((v) => sanitize(v === undefined ? null : v));
  if (typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      if (v === undefined || typeof v === "function") continue;
      out[k] = SENSITIVE_KEY_RE.test(k) ? REDACTED : sanitize(v);
    }
    return out;
  }
  return undefined;
}

// ---------------------------------------------------------------------------------------------
// Builder
// ---------------------------------------------------------------------------------------------

function truncate(text: string): string {
  return text.length > MAX_ERROR_MESSAGE_LENGTH ? `${text.slice(0, MAX_ERROR_MESSAGE_LENGTH)}…` : text;
}

export function buildDiagnostics(input: DiagnosticsInput): Diagnostics {
  const errors = input.recentErrors?.slice(-MAX_RECENT_ERRORS).map((e) => ({
    at: e.at,
    code: redactStructured(e.code),
    ...(e.message === undefined ? {} : { message: truncate(redactText(e.message)) }),
  }));
  const cameras = input.visibleCameras?.map((c) => ({ id: c.id, ...(c.name === undefined ? {} : { name: c.name }) }));
  const raw = {
    schema: DIAGNOSTICS_SCHEMA,
    generatedAt: new Date(input.generatedAt).toISOString(),
    appVersion: input.appVersion,
    commit: input.commit,
    capabilities: input.capabilities,
    telemetry: input.telemetry,
    history: input.history?.slice(-MAX_HISTORY_SAMPLES),
    health: input.health,
    bottleneck: input.bottleneck,
    benchmark: input.benchmark,
    recentErrors: errors,
    visibleCameras: cameras,
  };
  return sanitize(raw) as Diagnostics;
}

// ---------------------------------------------------------------------------------------------
// Text
// ---------------------------------------------------------------------------------------------

function codecLine(c: ClientCapabilities["codecs"][number]): string {
  const parts = c.supported
    ? ["supported", ...(c.smooth === undefined ? [] : [c.smooth ? "smooth" : "not smooth"]), ...(c.powerEfficient ? ["power efficient"] : [])]
    : ["not supported"];
  return `${c.codec.toUpperCase()} ${c.profile.label}: ${parts.join(" / ")}`;
}

/** A compact, support-facing block (English keys), derived from an already redacted Diagnostics. */
export function formatDiagnosticsText(diag: Diagnostics): string {
  const lines: string[] = ["OpenVMS Client Diagnostics", `Generated: ${diag.generatedAt}`];
  const push = (label: string, value: string | number | undefined) => {
    if (value !== undefined && value !== "") lines.push(`${label}: ${value}`);
  };
  push("App", [diag.appVersion, diag.commit && `(${diag.commit})`].filter(Boolean).join(" ") || undefined);

  const cap = diag.capabilities;
  if (cap) {
    lines.push("");
    push("Browser", [cap.system.browser, cap.system.browserVersion].filter(Boolean).join(" ") || undefined);
    push("OS", [cap.system.os, cap.system.osVersion].filter(Boolean).join(" ") || undefined);
    push("CPU threads", cap.cpu.logicalProcessors);
    if (cap.memory.reportedGB !== undefined) push("Memory reported", `${cap.memory.reportedGB} GB${cap.memory.bucketed ? " (bucketed)" : ""}`);
    push("GPU/WebGL", cap.gpu.renderer);
    lines.push(`WebGL2: ${cap.gpu.webgl2 ? "Yes" : "No"}`);
    if (cap.system.screen) push("Screen", `${cap.system.screen.width}x${cap.system.screen.height} @${cap.system.screen.devicePixelRatio}x`);
    if (cap.codecs.length > 0) lines.push("", "Video", ...cap.codecs.map(codecLine));
  }

  const m = diag.telemetry?.metrics;
  if (m) {
    lines.push("", "Videowall");
    push("Visible cameras", m.visibleCameras);
    push("Active streams", m.activeStreams);
    push("Main streams", m.mainStreams);
    push("Substreams", m.subStreams);
    push("Traffic", `${m.receivedMbps.toFixed(1)} Mbps (OpenVMS video only)`);
    push("Dropped frames", `${(m.droppedFrameRatio * 100).toFixed(1)}%`);
    if (m.presentationFps !== undefined) push("Render", `${Math.round(m.presentationFps)} FPS`);
    if (m.renderFrameTimeP95Ms !== undefined) push("Frame time p95", `${m.renderFrameTimeP95Ms.toFixed(1)} ms`);
  }

  if (diag.health) {
    lines.push("", `Performance state: ${diag.health.state.toUpperCase()}`);
    lines.push(`Layers: ${Object.entries(diag.health.layers).map(([k, v]) => `${k}=${v}`).join(", ")}`);
    for (const r of diag.health.reasons) lines.push(`- ${r.layer}/${r.code}: ${r.state}`);
  }
  if (diag.bottleneck) {
    const b = diag.bottleneck;
    lines.push(`Estimated bottleneck: ${b.layer} (${b.estimated ? "estimate" : "measured"}, confidence ${b.confidence})`);
    for (const e of b.evidence) lines.push(`- ${e}`);
  }
  if (diag.recentErrors && diag.recentErrors.length > 0) {
    lines.push("", "Recent errors");
    for (const e of diag.recentErrors) lines.push(`${new Date(e.at).toISOString()} ${e.code}${e.message ? `: ${e.message}` : ""}`);
  }
  return lines.join("\n");
}

// ---------------------------------------------------------------------------------------------
// Clipboard
// ---------------------------------------------------------------------------------------------

export type ClipboardLike = { writeText(text: string): Promise<void> };

/** Copies the text; resolves false (never throws) when the clipboard is missing or refuses. */
export async function copyDiagnostics(text: string, clipboard: ClipboardLike | undefined = typeof navigator === "undefined" ? undefined : navigator.clipboard): Promise<boolean> {
  if (!clipboard) return false;
  try {
    await clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
