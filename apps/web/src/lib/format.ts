const dateTime = new Intl.DateTimeFormat("es-AR", { dateStyle: "short", timeStyle: "medium" });
const time = new Intl.DateTimeFormat("es-AR", { timeStyle: "medium" });

export function fmtDateTime(iso: string | Date | null | undefined): string {
  if (!iso) return "—";
  return dateTime.format(typeof iso === "string" ? new Date(iso) : iso);
}

export function fmtTime(iso: string | Date): string {
  return time.format(typeof iso === "string" ? new Date(iso) : iso);
}

/**
 * DEFAULT_WATERMARK_TIMEZONE mirrors internal/branding.DefaultTimezone (Go): used before
 * brandingQuery has loaded (or for a tenant with no branding row yet, which the API itself
 * already defaults server-side) so the overlay never renders with an undefined time zone.
 */
export const DEFAULT_WATERMARK_TIMEZONE = "America/Argentina/Buenos_Aires";

/**
 * fmtWatermarkTimestamp renders the plate detail watermark's date/time (PDW-2/PDW-7): same
 * text on screen (CSS overlay) and burned into downloads (internal/watermark.Text), in the
 * tenant's configured IANA time zone (branding.timezone), with its real numeric UTC offset —
 * e.g. "2026-09-28 10:05:30 -03:00" — computed from the actual tz database (via Intl's
 * "longOffset" time zone name), not hardcoded, so it reflects DST correctly for zones that
 * observe it. This must match Go's watermark.Text byte-for-byte for the same instant/zone,
 * since the point of a watermark is that what you see is what gets burned in.
 */
export function fmtWatermarkTimestamp(iso: string, timeZone: string): string {
  const d = new Date(iso);

  const dateTimeParts = new Intl.DateTimeFormat("en-US", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).formatToParts(d);
  const part = (type: string) => dateTimeParts.find((p) => p.type === type)?.value ?? "";
  // Some engines render midnight as hour "24" with hour12:false; normalize to "00" so the
  // date/time stay a valid ISO-shaped local timestamp (matches Go's time.Format, which never
  // does this).
  const hour = part("hour") === "24" ? "00" : part("hour");
  const datePart = `${part("year")}-${part("month")}-${part("day")}`;
  const timePart = `${hour}:${part("minute")}:${part("second")}`;

  const offsetParts = new Intl.DateTimeFormat("en-US", { timeZone, timeZoneName: "longOffset" }).formatToParts(d);
  const rawOffset = offsetParts.find((p) => p.type === "timeZoneName")?.value ?? "GMT+00:00";
  const offset = rawOffset.replace("GMT", "") || "+00:00";

  return `${datePart} ${timePart} ${offset}`;
}

export function fmtDuration(startIso: string, endIso?: string | null): string {
  if (!endIso) return "en curso";
  const s = Math.max(0, Math.round((new Date(endIso).getTime() - new Date(startIso).getTime()) / 1000));
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  return `${m} min ${s % 60} s`;
}

/** toLocalInput formats a Date for <input type="datetime-local">. */
export function toLocalInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** fromLocalInput turns a datetime-local value into an ISO string, or undefined. */
export function fromLocalInput(v: string): string | undefined {
  if (!v) return undefined;
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? undefined : d.toISOString();
}

const labelNames: Record<string, string> = {
  person: "Persona",
  car: "Auto",
  motorcycle: "Moto",
  bicycle: "Bicicleta",
  truck: "Camión",
  bus: "Colectivo",
  dog: "Perro",
  cat: "Gato",
  license_plate: "Patente",
  face: "Rostro",
  package: "Paquete",
};

export function labelName(l: string): string {
  return labelNames[l] ?? l;
}

export const commonLabels = ["person", "car", "motorcycle", "truck", "bicycle", "dog"];
