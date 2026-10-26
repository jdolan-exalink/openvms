import type { Schemas } from "@/api/client";

export type StreamOptions = Schemas["CameraStreamVideoOptions"];
export type CodecOptions = StreamOptions["codecs"][number];
export type VideoReason = Schemas["CameraVideoReason"];
export type Role = "primary" | "detection";

/** Values of one stream: what the device reports, or what a sparse patch asks for. */
export type StreamValues = {
  codec?: string;
  resolution?: { width: number; height: number };
  fps?: number;
  bitrate_kbps?: number;
  gop?: number;
};

export const NONE = "—";

export const RESTART_NOTICE = "Este cambio reiniciará el stream durante aproximadamente algunos segundos.";

const REASONS: Record<VideoReason, string> = {
  no_onvif: "Esta cámara no expone ONVIF",
  no_credentials: "Faltan las credenciales de la cámara",
  unreachable: "No se pudo contactar a la cámara",
  unsupported: "La cámara no permite cambiar estos valores",
};

export const reasonText = (reason: VideoReason | undefined): string => REASONS[reason ?? "unsupported"] ?? REASONS.unsupported;

export function codecLabel(codec: string | undefined): string | undefined {
  if (!codec) return undefined;
  const c = codec.toLowerCase();
  if (c === "h264") return "H.264";
  if (c === "h265" || c === "hevc") return "H.265";
  return codec.toUpperCase();
}

export const resolutionLabel = (r: { width: number; height: number } | undefined) => (r ? `${r.width}×${r.height}` : undefined);

type SummarySource = { codec?: string; width?: number; height?: number; fps?: number };

/** "1920×1080 · 15 FPS · H.264"; undefined when the stream has none of those values. */
export function streamSummary(s: SummarySource | undefined): string | undefined {
  if (!s) return undefined;
  const parts = [s.width && s.height ? `${s.width}×${s.height}` : undefined, s.fps ? `${s.fps} FPS` : undefined, codecLabel(s.codec)].filter(Boolean);
  return parts.length ? parts.join(" · ") : undefined;
}

/** What the stream uses now: the device's own report first, then the normalized model. */
export function currentValues(options: StreamOptions | undefined, model: SummarySource & { bitrate_kbps?: number; gop?: number } | undefined): StreamValues {
  const c = options?.current;
  const w = c?.width ?? model?.width;
  const h = c?.height ?? model?.height;
  return {
    codec: c?.codec ?? model?.codec,
    resolution: w && h ? { width: w, height: h } : undefined,
    fps: c?.fps ?? model?.fps,
    bitrate_kbps: c?.bitrate_kbps ?? model?.bitrate_kbps,
    gop: c?.gop ?? model?.gop,
  };
}

/** Current values with the pending (sparse) patch laid over them. */
export const effectiveValues = (current: StreamValues, patch: StreamValues | undefined): StreamValues => ({ ...current, ...(patch ?? {}) });

const sameRes = (a?: { width: number; height: number }, b?: { width: number; height: number }) => a?.width === b?.width && a?.height === b?.height;

/** True when the patch really changes at least one parameter. */
export function differsFromCurrent(current: StreamValues, patch: StreamValues | undefined): boolean {
  if (!patch) return false;
  return (
    (patch.codec !== undefined && patch.codec !== current.codec) ||
    (patch.resolution !== undefined && !sameRes(patch.resolution, current.resolution)) ||
    (patch.fps !== undefined && patch.fps !== current.fps) ||
    (patch.bitrate_kbps !== undefined && patch.bitrate_kbps !== current.bitrate_kbps) ||
    (patch.gop !== undefined && patch.gop !== current.gop)
  );
}

export const codecOptionsFor = (options: StreamOptions | undefined, codec: string | undefined): CodecOptions | undefined =>
  options?.codecs.find((c) => c.codec === codec);

export type Range = { min: number; max: number } | undefined;

export function rangeError(value: number, range: Range): string | undefined {
  if (!Number.isFinite(value) || !Number.isInteger(value)) return "Ingresa un número entero.";
  if (range && (value < range.min || value > range.max)) return `Debe estar entre ${range.min} y ${range.max}.`;
  return undefined;
}

const clamp = (v: number, r: Range) => (r ? Math.min(Math.max(v, r.min), r.max) : v);

/**
 * Switching codec can leave the other values outside what the new codec supports; returns the
 * sparse patch that moves only those values to the nearest supported ones.
 */
export function reconcileForCodec(next: CodecOptions, eff: StreamValues): StreamValues {
  const out: StreamValues = { codec: next.codec };
  if (next.resolutions.length && !next.resolutions.some((r) => sameRes(r, eff.resolution))) out.resolution = { ...next.resolutions[0]! };
  if (eff.fps !== undefined) {
    if (next.frame_rates.length && !next.frame_rates.includes(eff.fps)) {
      out.fps = next.frame_rates.reduce((best, f) => (Math.abs(f - eff.fps!) < Math.abs(best - eff.fps!) ? f : best));
    } else if (!next.frame_rates.length && next.fps && clamp(eff.fps, next.fps) !== eff.fps) out.fps = clamp(eff.fps, next.fps);
  }
  if (eff.bitrate_kbps !== undefined && next.bitrate_kbps && clamp(eff.bitrate_kbps, next.bitrate_kbps) !== eff.bitrate_kbps) out.bitrate_kbps = clamp(eff.bitrate_kbps, next.bitrate_kbps);
  if (eff.gop !== undefined && next.gop && clamp(eff.gop, next.gop) !== eff.gop) out.gop = clamp(eff.gop, next.gop);
  return out;
}

export const applyViaHint = (via: Schemas["CameraVideoApplyVia"]): string | undefined =>
  via === "onvif" ? "Se aplica en la cámara" : via === "frigate" ? "Se aplica en Frigate" : undefined;
