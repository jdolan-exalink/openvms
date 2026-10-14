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

/**
 * Cumulative counters and current values for one live video stream, read cheaply from the
 * player (no per-frame work). Counters are monotonic per session; consumers derive rates from
 * deltas and must tolerate resets (a reconnect may restart the decoder counters).
 * Optional fields mean "not exposed by the platform" and are never invented.
 */
export type StreamStatsSample = {
  cameraId: string;
  quality: "sub" | "main";
  /** Player lifecycle state (for example `ACTIVE`, `WARM`, `RECONNECTING`). */
  state: string;
  /** Media bytes received from OpenVMS over the stream transport (control messages excluded). */
  bytesReceived: number;
  decodedFrames?: number;
  droppedFrames?: number;
  /** Decoded picture size; undefined until the first frame is decoded. */
  width?: number;
  height?: number;
  /** Connections beyond the first one. */
  reconnects: number;
  /** Times playback ran out of buffered data while the stream was active. */
  stalls: number;
  lastErrorCode?: string;
};

/** Anything that can report the stats of one live stream (a player session, a native stream, ...). */
export type StreamStatsSource = {
  streamStats(): StreamStatsSample;
};

/**
 * Aggregated live video statistics for one sampling interval.
 *
 * Network scope: every byte and rate here is OpenVMS video traffic only. It is never the
 * total traffic of the machine's network interface.
 */
export type VideoPerformanceMetrics = {
  timestamp: number;
  visibleCameras: number;
  /** Streams currently delivering media (player state `ACTIVE` or `WARM`). */
  activeStreams: number;
  mainStreams: number;
  subStreams: number;
  /** Received OpenVMS video bitrate over the interval, in megabits per second. */
  receivedMbps: number;
  /** Frames decoded / dropped during the interval (deltas, not cumulative). */
  decodedFrames: number;
  droppedFrames: number;
  droppedFrameRatio: number;
  presentationFps?: number;
  renderFrameTimeMs?: number;
  renderFrameTimeP95Ms?: number;
  /** Stalls during the interval. */
  stalls?: number;
};

/** One row of the per-camera bandwidth table. */
export type StreamTelemetryRow = {
  cameraId: string;
  quality: "sub" | "main";
  state: string;
  width?: number;
  height?: number;
  /** Decoded frames per second over the interval; undefined when the platform does not report frames. */
  fps?: number;
  droppedFps?: number;
  /** Received bitrate of this stream over the interval, in bits per second. */
  bitrateBps: number;
};

/** What a UI reads (useSyncExternalStore): replaced by a new object once per tick. */
export type VideoTelemetrySnapshot = {
  /** Latest interval; null until the first tick. */
  metrics: VideoPerformanceMetrics | null;
  streams: readonly StreamTelemetryRow[];
};
