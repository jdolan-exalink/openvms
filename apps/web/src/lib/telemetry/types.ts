/**
 * Platform-neutral telemetry model. The same shapes are produced by the browser collector
 * and by a future native Windows client. Optional fields mean "not exposed by the platform";
 * collectors must never invent values for them.
 */

export type ClientRuntime = "browser" | "native";

export type VideoCodec = "h264" | "hevc" | "vp8" | "vp9" | "av1";

export type VideoProfile = {
  label: string;
  width: number;
  height: number;
  fps: number;
};

export type CodecCapability = {
  codec: VideoCodec;
  profile: VideoProfile;
  /** The codec string that was queried (for example `avc1.640028`). */
  codecString: string;
  supported: boolean;
  /** Only known when the platform answers through Media Capabilities. */
  smooth?: boolean;
  /** Reported as-is by the platform; it is not proof of hardware decoding. */
  powerEfficient?: boolean;
  source: "mediaCapabilities" | "isTypeSupported" | "unavailable";
};

export type ClientSystemInfo = {
  os?: string;
  osVersion?: string;
  architecture?: string;
  browser?: string;
  browserVersion?: string;
  userAgent?: string;
  screen?: { width: number; height: number; devicePixelRatio: number };
  /** Exact number of screens; only a native client can know it (browsers need a permission prompt). */
  screens?: number;
  /** Whether the desktop spans more than one screen (browser `screen.isExtended`), without a count. */
  multipleScreens?: boolean;
  viewport?: { width: number; height: number };
};

export type ClientCpuInfo = {
  logicalProcessors?: number;
  /** Not exposed by browsers; a native client may fill it. */
  model?: string;
};

export type ClientMemoryInfo = {
  reportedGB?: number;
  /** `deviceMemory` is a coarse, capped bucket, not an exact amount of RAM. */
  bucketed: boolean;
  source: "deviceMemory" | "native" | "unavailable";
};

export type ClientGpuInfo = {
  webgl: boolean;
  webgl2: boolean;
  vendor?: string;
  renderer?: string;
  maxTextureSize?: number;
  maxViewportDims?: [number, number];
};

export type ClientCapabilities = {
  runtime: ClientRuntime;
  collectedAt: number;
  system: ClientSystemInfo;
  cpu: ClientCpuInfo;
  memory: ClientMemoryInfo;
  gpu: ClientGpuInfo;
  codecs: CodecCapability[];
};

/** Aggregated live video statistics for one sampling interval (types only for now). */
export type VideoPerformanceMetrics = {
  timestamp: number;
  visibleCameras: number;
  activeStreams: number;
  mainStreams: number;
  subStreams: number;
  receivedMbps: number;
  decodedFrames: number;
  droppedFrames: number;
  droppedFrameRatio: number;
  presentationFps?: number;
  renderFrameTimeMs?: number;
  stalls?: number;
};
