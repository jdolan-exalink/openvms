import type { Schemas } from "@/api/client";

type Camera = Schemas["Camera"];
type Rec = Record<string, unknown>;

const isRec = (v: unknown): v is Rec => typeof v === "object" && v !== null && !Array.isArray(v);
const num = (v: unknown) => (typeof v === "number" && Number.isFinite(v) ? v : undefined);

export const NONE = "—";

export type ChipTone = "ok" | "off" | "unknown";
export type StatusChip = { id: string; label: string; tone: ChipTone };

const STATUS_LABEL: Record<string, string> = { online: "En línea", degraded: "Degradada", offline: "Sin conexión", unknown: "Estado desconocido" };

/** `enabled` of a Frigate toggle section, undefined when the config does not say. */
function toggle(config: Rec, section: string): boolean | undefined {
  const s = config[section];
  return isRec(s) && typeof s.enabled === "boolean" ? s.enabled : undefined;
}

/** Quick state chips; every value comes from the camera row or the effective Frigate config. */
export function statusChips(camera: Camera | undefined, config: Rec): StatusChip[] {
  const chips: StatusChip[] = [];
  if (camera) {
    if (!camera.enabled) chips.push({ id: "online", label: "Deshabilitada", tone: "off" });
    else chips.push({ id: "online", label: STATUS_LABEL[camera.status] ?? "Estado desconocido", tone: camera.status === "online" ? "ok" : camera.status === "unknown" ? "unknown" : "off" });
  } else chips.push({ id: "online", label: NONE, tone: "unknown" });

  const rec = toggle(config, "record");
  chips.push({ id: "record", label: rec === undefined ? `Grabación ${NONE}` : rec ? "Grabando" : "Sin grabar", tone: rec === undefined ? "unknown" : rec ? "ok" : "off" });
  const detect = toggle(config, "detect");
  chips.push({ id: "detect", label: detect === undefined ? `Detección ${NONE}` : detect ? "Detección activa" : "Detección inactiva", tone: detect === undefined ? "unknown" : detect ? "ok" : "off" });
  const lpr = camera ? camera.lpr || toggle(config, "lpr") === true : toggle(config, "lpr");
  chips.push({ id: "lpr", label: lpr === undefined ? `LPR ${NONE}` : lpr ? "LPR activo" : "LPR inactivo", tone: lpr === undefined ? "unknown" : lpr ? "ok" : "off" });
  if (camera?.onvif_capabilities || camera?.onvif_endpoint) chips.push({ id: "onvif", label: "ONVIF", tone: "ok" });
  return chips;
}

export function hostOf(endpoint: string | null | undefined): string | undefined {
  if (!endpoint) return undefined;
  try {
    return new URL(endpoint).hostname;
  } catch {
    return endpoint;
  }
}

/** Capabilities the camera really advertises, e.g. "ONVIF · Audio · PTZ · LPR". */
export function capabilityList(camera: Camera | undefined, config: Rec): string[] {
  const caps = camera?.onvif_capabilities;
  const out: string[] = [];
  if (caps || camera?.onvif_endpoint) out.push("ONVIF");
  if (toggle(config, "audio") === true) out.push("Audio");
  if (caps?.has_ptz || camera?.tags?.includes("ptz")) out.push("PTZ");
  if (caps?.has_analytics) out.push("Analítica");
  if (camera?.lpr || toggle(config, "lpr") === true) out.push("LPR");
  return out;
}

export type StreamRow = { id: "primary" | "detect" | "record"; label: string; value: string };

/**
 * Video summary rows. Principal is the first input, Detección the detect-role input with its
 * resolution and fps, Grabación only when the record input is a different one.
 */
export function videoRows(camera: Camera | undefined, config: Rec): StreamRow[] {
  const ffmpeg = isRec(config.ffmpeg) ? config.ffmpeg : undefined;
  const inputs = Array.isArray(ffmpeg?.inputs) ? ffmpeg.inputs.filter(isRec) : [];
  const roles = (i: Rec) => (Array.isArray(i.roles) ? i.roles.filter((r): r is string => typeof r === "string") : []);
  const detectIdx = inputs.findIndex((i) => roles(i).includes("detect"));
  const recordIdx = inputs.findIndex((i) => roles(i).includes("record"));
  const detect = isRec(config.detect) ? config.detect : undefined;
  const w = num(detect?.width), h = num(detect?.height), fps = num(detect?.fps) ?? (camera?.fps ? Math.round(camera.fps) : undefined);
  const size = w && h ? `${w}×${h}` : undefined;
  const join = (parts: Array<string | undefined>) => parts.filter(Boolean).join(" · ") || NONE;

  const rows: StreamRow[] = [{ id: "primary", label: "Principal", value: join([camera?.hq_codec?.toUpperCase()]) }];
  rows.push({ id: "detect", label: "Detección", value: join([size, fps ? `${fps} fps` : undefined, camera?.live_codec?.toUpperCase()]) });
  if (recordIdx > 0 && recordIdx !== detectIdx) rows.push({ id: "record", label: "Grabación", value: join(["entrada " + (recordIdx + 1)]) });
  return rows;
}

export type DeviceRow = { label: string; value: string };

export function deviceRows(camera: Camera | undefined, config: Rec): DeviceRow[] {
  const caps = camera?.onvif_capabilities;
  const onvif = isRec(config.onvif) ? config.onvif : undefined;
  const ip = hostOf(camera?.onvif_endpoint) ?? (typeof onvif?.host === "string" && onvif.host ? onvif.host : undefined);
  return [
    { label: "Fabricante", value: caps?.manufacturer || NONE },
    { label: "Modelo", value: caps?.model || NONE },
    { label: "IP", value: ip || NONE },
    { label: "Firmware", value: caps?.firmware_version || NONE },
  ];
}
