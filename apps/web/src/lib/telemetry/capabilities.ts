/**
 * Client station capability collector. Framework-free and fully injectable: every browser
 * global is read through `CapabilitiesEnv`, so tests (and a future native client) can supply
 * their own sources. Values a platform does not expose stay `undefined`; nothing is invented
 * and nothing here prompts the user for a permission. Results are not cached.
 */

import type {
  ClientCapabilities,
  ClientGpuInfo,
  ClientMemoryInfo,
  ClientSystemInfo,
  CodecCapability,
  VideoCodec,
  VideoProfile,
} from "./types";

export type UserAgentDataLike = {
  platform?: string;
  getHighEntropyValues?: (hints: string[]) => Promise<{
    platform?: string;
    platformVersion?: string;
    architecture?: string;
    fullVersionList?: { brand: string; version: string }[];
  }>;
};

export type NavigatorLike = {
  userAgent?: string;
  hardwareConcurrency?: number;
  deviceMemory?: number;
  userAgentData?: UserAgentDataLike;
};

export type ScreenLike = { width: number; height: number; isExtended?: boolean };

export type WebGLContextLike = {
  VENDOR: number;
  RENDERER: number;
  MAX_TEXTURE_SIZE: number;
  MAX_VIEWPORT_DIMS: number;
  getExtension(name: string): unknown;
  getParameter(param: number): unknown;
};

export type DecodingConfig = {
  type: "media-source";
  video: { contentType: string; width: number; height: number; framerate: number; bitrate: number };
};

export type MediaCapabilitiesLike = {
  decodingInfo(config: DecodingConfig): Promise<{ supported: boolean; smooth?: boolean; powerEfficient?: boolean }>;
};

export type CapabilitiesEnv = {
  navigator: NavigatorLike;
  screen?: ScreenLike;
  viewport?: { width: number; height: number };
  devicePixelRatio?: number;
  createWebGLContext(kind: "webgl" | "webgl2"): WebGLContextLike | null;
  mediaCapabilities?: MediaCapabilitiesLike;
  isTypeSupported?: (mimeType: string) => boolean;
  now(): number;
};

export type CollectOptions = {
  /** `light` checks only the videowall profile per codec, for instant startup analysis. */
  codecs?: "full" | "light";
};

export const CCTV_PROFILES: readonly (VideoProfile & { bitrate: number })[] = [
  { label: "360p@10", width: 640, height: 360, fps: 10, bitrate: 600_000 },
  { label: "360p@15", width: 640, height: 360, fps: 15, bitrate: 800_000 },
  { label: "720p@15", width: 1280, height: 720, fps: 15, bitrate: 2_000_000 },
  { label: "1080p@15", width: 1920, height: 1080, fps: 15, bitrate: 4_000_000 },
  { label: "1080p@30", width: 1920, height: 1080, fps: 30, bitrate: 6_000_000 },
  { label: "2160p@15", width: 3840, height: 2160, fps: 15, bitrate: 12_000_000 },
];

/** The tile profile used by the videowall: the only one checked in light mode. */
export const VIDEOWALL_PROFILE_LABEL = "360p@15";

type CodecSpec = { codec: VideoCodec; container: "mp4" | "webm"; codecString: (p: VideoProfile) => string };

// H.264 uses Baseline for tile-sized profiles and High 4.0 for larger ones.
const CODEC_SPECS: readonly CodecSpec[] = [
  { codec: "h264", container: "mp4", codecString: (p) => (p.height <= 360 ? "avc1.42E01E" : "avc1.640028") },
  { codec: "hevc", container: "mp4", codecString: () => "hvc1.1.6.L120.90" },
  { codec: "vp8", container: "webm", codecString: () => "vp8" },
  { codec: "vp9", container: "webm", codecString: () => "vp09.00.10.08" },
  { codec: "av1", container: "mp4", codecString: () => "av01.0.04M.08" },
];

export function describeCpu(caps: Pick<ClientCapabilities, "cpu">): string {
  const n = caps.cpu.logicalProcessors;
  if (n === undefined) return "unavailable";
  return `${n} logical processor${n === 1 ? "" : "s"}`;
}

export function describeMemory(caps: Pick<ClientCapabilities, "memory">): string {
  const { reportedGB, bucketed } = caps.memory;
  if (reportedGB === undefined) return "unavailable";
  return `${bucketed ? ">= " : ""}${reportedGB} GB`;
}

export function createBrowserEnv(): CapabilitiesEnv {
  const nav = typeof navigator !== "undefined" ? (navigator as unknown as NavigatorLike) : {};
  const hasWindow = typeof window !== "undefined";
  return {
    navigator: nav,
    screen: hasWindow ? (window.screen as ScreenLike) : undefined,
    viewport: hasWindow ? { width: window.innerWidth, height: window.innerHeight } : undefined,
    devicePixelRatio: hasWindow ? window.devicePixelRatio : undefined,
    createWebGLContext: (kind) => {
      if (typeof document === "undefined") return null;
      return document.createElement("canvas").getContext(kind) as unknown as WebGLContextLike | null;
    },
    mediaCapabilities: (nav as { mediaCapabilities?: MediaCapabilitiesLike }).mediaCapabilities,
    isTypeSupported:
      typeof MediaSource !== "undefined" && typeof MediaSource.isTypeSupported === "function"
        ? (mime) => MediaSource.isTypeSupported(mime)
        : undefined,
    now: () => Date.now(),
  };
}

export async function collectClientCapabilities(
  env: Partial<CapabilitiesEnv> = {},
  options: CollectOptions = {},
): Promise<ClientCapabilities> {
  const e: CapabilitiesEnv = { ...createBrowserEnv(), ...env };
  const [system, gpu, codecs] = await Promise.all([
    collectSystem(e),
    Promise.resolve(collectGpu(e)),
    collectCodecs(e, options.codecs ?? "full"),
  ]);
  const cpu = e.navigator.hardwareConcurrency ? { logicalProcessors: e.navigator.hardwareConcurrency } : {};
  return { runtime: "browser", collectedAt: e.now(), system, cpu, memory: collectMemory(e), gpu, codecs };
}

function collectMemory(e: CapabilitiesEnv): ClientMemoryInfo {
  const gb = e.navigator.deviceMemory;
  if (typeof gb !== "number") return { bucketed: false, source: "unavailable" };
  return { reportedGB: gb, bucketed: true, source: "deviceMemory" };
}

type ParsedUa = Pick<ClientSystemInfo, "os" | "browser" | "browserVersion">;

export function parseUserAgent(ua: string): ParsedUa {
  let os: string | undefined;
  if (/Windows/.test(ua)) os = "Windows";
  else if (/Android/.test(ua)) os = "Android";
  else if (/iPhone|iPad|iPod/.test(ua)) os = "iOS";
  else if (/Mac OS X|Macintosh/.test(ua)) os = "macOS";
  else if (/Linux|X11/.test(ua)) os = "Linux";

  const tokens: [string, RegExp][] = [
    ["Edge", /Edg(?:e|A|iOS)?\/([\d.]+)/],
    ["Firefox", /(?:Firefox|FxiOS)\/([\d.]+)/],
    ["Chrome", /(?:Chrome|CriOS)\/([\d.]+)/],
    ["Safari", /Version\/([\d.]+).*Safari\//],
  ];
  for (const [browser, re] of tokens) {
    const m = re.exec(ua);
    if (m) return { os, browser, browserVersion: m[1] };
  }
  return { os };
}

const BRAND_NAMES: Record<string, string> = { "Google Chrome": "Chrome", "Microsoft Edge": "Edge", Chromium: "Chromium", Opera: "Opera" };

async function collectSystem(e: CapabilitiesEnv): Promise<ClientSystemInfo> {
  const ua = e.navigator.userAgent;
  const system: ClientSystemInfo = ua ? { ...parseUserAgent(ua), userAgent: ua } : {};
  const uad = e.navigator.userAgentData;
  if (uad) {
    if (uad.platform) system.os = uad.platform;
    try {
      const hi = await uad.getHighEntropyValues?.(["platform", "platformVersion", "architecture", "fullVersionList"]);
      if (hi) {
        if (hi.platform) system.os = hi.platform;
        if (hi.platformVersion) system.osVersion = hi.platformVersion;
        if (hi.architecture) system.architecture = hi.architecture;
        // Prefer a named browser brand over the generic Chromium one.
        const brands = (hi.fullVersionList ?? []).filter((b) => b.brand in BRAND_NAMES);
        const best = brands.find((b) => b.brand !== "Chromium") ?? brands[0];
        if (best) {
          system.browser = BRAND_NAMES[best.brand];
          system.browserVersion = best.version;
        }
      }
    } catch {
      // High entropy hints can be denied; keep the userAgent-derived values.
    }
  }
  if (e.screen) {
    system.screen = { width: e.screen.width, height: e.screen.height, devicePixelRatio: e.devicePixelRatio ?? 1 };
    // `isExtended` needs no permission; getScreenDetails() does, so it is never called.
    if (typeof e.screen.isExtended === "boolean") system.screens = e.screen.isExtended ? 2 : 1;
  }
  if (e.viewport) system.viewport = { ...e.viewport };
  return system;
}

function probeContext(e: CapabilitiesEnv, kind: "webgl" | "webgl2"): WebGLContextLike | null {
  try {
    return e.createWebGLContext(kind);
  } catch {
    return null;
  }
}

function loseContext(gl: WebGLContextLike) {
  try {
    (gl.getExtension("WEBGL_lose_context") as { loseContext?: () => void } | null)?.loseContext?.();
  } catch {
    // Releasing the context is best effort.
  }
}

function collectGpu(e: CapabilitiesEnv): ClientGpuInfo {
  const gl2 = probeContext(e, "webgl2");
  const gl1 = probeContext(e, "webgl");
  const gpu: ClientGpuInfo = { webgl: gl1 !== null || gl2 !== null, webgl2: gl2 !== null };
  const primary = gl2 ?? gl1;
  if (primary) {
    try {
      const dbg = primary.getExtension("WEBGL_debug_renderer_info") as
        | { UNMASKED_VENDOR_WEBGL: number; UNMASKED_RENDERER_WEBGL: number }
        | null;
      const vendor = primary.getParameter(dbg ? dbg.UNMASKED_VENDOR_WEBGL : primary.VENDOR);
      const renderer = primary.getParameter(dbg ? dbg.UNMASKED_RENDERER_WEBGL : primary.RENDERER);
      if (typeof vendor === "string") gpu.vendor = vendor;
      if (typeof renderer === "string") gpu.renderer = renderer;
      const tex = primary.getParameter(primary.MAX_TEXTURE_SIZE);
      if (typeof tex === "number") gpu.maxTextureSize = tex;
      const dims = primary.getParameter(primary.MAX_VIEWPORT_DIMS) as ArrayLike<number> | undefined;
      if (dims && dims.length >= 2) gpu.maxViewportDims = [Number(dims[0]), Number(dims[1])];
    } catch {
      // Partial GPU info is still useful; absence is not an error.
    }
  }
  for (const gl of [gl2, gl1]) if (gl) loseContext(gl);
  return gpu;
}

async function queryCodec(
  e: CapabilitiesEnv,
  spec: CodecSpec,
  profile: VideoProfile & { bitrate: number },
): Promise<CodecCapability> {
  const codecString = spec.codecString(profile);
  const { bitrate, ...plain } = profile;
  const base = { codec: spec.codec, profile: plain, codecString };
  const contentType = `video/${spec.container}; codecs="${codecString}"`;
  if (e.mediaCapabilities) {
    try {
      const info = await e.mediaCapabilities.decodingInfo({
        type: "media-source",
        video: { contentType, width: profile.width, height: profile.height, framerate: profile.fps, bitrate },
      });
      return { ...base, supported: info.supported, smooth: info.smooth, powerEfficient: info.powerEfficient, source: "mediaCapabilities" };
    } catch {
      return { ...base, supported: false, source: "mediaCapabilities" };
    }
  }
  if (e.isTypeSupported) {
    try {
      return { ...base, supported: e.isTypeSupported(contentType), source: "isTypeSupported" };
    } catch {
      return { ...base, supported: false, source: "isTypeSupported" };
    }
  }
  return { ...base, supported: false, source: "unavailable" };
}

function collectCodecs(e: CapabilitiesEnv, mode: "full" | "light"): Promise<CodecCapability[]> {
  const profiles = mode === "light" ? CCTV_PROFILES.filter((p) => p.label === VIDEOWALL_PROFILE_LABEL) : CCTV_PROFILES;
  return Promise.all(CODEC_SPECS.flatMap((spec) => profiles.map((p) => queryCodec(e, spec, p))));
}
