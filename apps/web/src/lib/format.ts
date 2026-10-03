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

const vehicleTypeNames: Record<string, string> = {
  car: "Auto",
  sedan: "Sedán",
  hatchback: "Hatchback",
  suv: "SUV",
  pickup: "Pickup",
  van: "Utilitario",
  station_wagon: "Familiar",
  micro: "Citadino",
  truck: "Camión",
  truck_trailer: "Camión con acoplado",
  bus: "Colectivo",
  motorcycle: "Moto",
};

const feminineTypes = new Set(["suv", "pickup", "van", "motorcycle", "station_wagon"]);

const colorWords: Record<string, [string, string]> = {
  black: ["Negro", "Negra"],
  white: ["Blanco", "Blanca"],
  gray: ["Gris", "Gris"],
  silver: ["Plata", "Plata"],
  red: ["Rojo", "Roja"],
  blue: ["Azul", "Azul"],
  green: ["Verde", "Verde"],
  yellow: ["Amarillo", "Amarilla"],
  orange: ["Naranja", "Naranja"],
  brown: ["Marrón", "Marrón"],
  beige: ["Beige", "Beige"],
  purple: ["Violeta", "Violeta"],
};

function colorWord(color: string, feminine: boolean): string {
  const pair = colorWords[color];
  if (!pair) return color;
  return feminine ? pair[1] : pair[0];
}

type VehicleReading = { type?: string; type_confidence?: number; color?: string; color_confidence?: number };

function firstVehicle(labels: string[]): string {
  for (const label of labels) {
    if (label === "car-verified") return "car";
    if (label === "bus" || label === "truck" || label === "car" || label === "motorcycle") return label;
  }
  return "";
}

/** The first vehicle label is the subject. A car that Frigate listed first stays an auto. */
export function vehicleQualification(labels: string[], v?: VehicleReading | null): string {
  const primary = firstVehicle(labels);
  let type = v?.type && v.type !== "unknown" ? v.type : primary;
  if (primary === "car" && type === "motorcycle") type = "car";
  if (!type) return labels.includes("person") ? "Persona" : "—";
  return vehicleTypeNames[type] ?? type;
}

const paintable = new Set(["car", "motorcycle", "truck", "bus", "truck_trailer"]);

/** Paint is shown for cars, motorcycles, trucks and buses. An unresolved reading stays visible. */
export function vehicleColorText(labels: string[], v?: VehicleReading | null): string | null {
  const primary = firstVehicle(labels);
  const type = v?.type && v.type !== "unknown" ? v.type : primary;
  if (!paintable.has(primary) && !paintable.has(type ?? "")) return null;
  if (!v?.color || v.color === "unknown" || v.color === "other") return "No detectado";
  return colorWord(v.color, primary === "motorcycle");
}

/** Spanish name and swatch for a clothing or paint color. An unresolved reading stays visible. */
export function appearanceColor(color?: string, pending = false): { text: string; paint: string | null } {
  if (pending) return { text: "Detectando", paint: null };
  if (!color || color === "unknown" || color === "other") return { text: "No detectado", paint: null };
  return { text: colorWord(color, false), paint: paintHex[color] ?? null };
}

/** vehicleHeadline is the card line once enrichment is confident enough to show. */
export function vehicleHeadline(v?: VehicleReading): string | null {
  if (!v) return null;
  const typeOk = !!v.type && v.type !== "unknown" && (v.type_confidence ?? 0) >= 0.6;
  const colorOk = !!v.color && v.color !== "unknown" && v.color !== "other" && (v.color_confidence ?? 0) >= 0.55;
  const type = typeOk ? (vehicleTypeNames[v.type!] ?? v.type!) : "";
  const color = colorOk ? colorWord(v.color!, typeOk && feminineTypes.has(v.type!)) : "";
  if (type && color) return `${type} · ${color}`;
  if (type) return type;
  if (color) return `Vehículo · ${colorWord(v.color!, false)}`;
  return null;
}

const paintHex: Record<string, string> = {
  black: "#1c1c1c",
  white: "#f5f5f4",
  gray: "#6b7280",
  silver: "#c5ccd6",
  red: "#dc2626",
  blue: "#2563eb",
  green: "#16a34a",
  yellow: "#eab308",
  orange: "#f97316",
  brown: "#92400e",
  beige: "#d6c4a8",
  purple: "#7c3aed",
};

/** vehiclePaint is the swatch for any named color. Callers decide when a car should show it. */
export function vehiclePaint(v?: { color?: string }): string | null {
  if (!v?.color || v.color === "unknown" || v.color === "other") return null;
  return paintHex[v.color] ?? null;
}

export const vehicleTypeOptions = Object.entries(vehicleTypeNames).map(([value, label]) => ({ value, label }));
export const vehicleColorOptions = [
  ["black", "Negro"],
  ["white", "Blanco"],
  ["gray", "Gris"],
  ["silver", "Plata"],
  ["red", "Rojo"],
  ["blue", "Azul"],
  ["green", "Verde"],
  ["yellow", "Amarillo"],
  ["orange", "Naranja"],
  ["brown", "Marrón"],
  ["beige", "Beige"],
  ["purple", "Violeta"],
].map(([value, label]) => ({ value, label }));

export const commonLabels = ["person", "car", "motorcycle", "truck", "bicycle", "dog"];

/** Every label with a display name, common ones first: suggestions for label inputs. */
export const knownLabels = [...new Set([...commonLabels, ...Object.keys(labelNames)])];
