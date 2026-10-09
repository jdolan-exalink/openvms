import { getLocale, LOCALES, translate, type MessageKey } from "@/i18n";

function localeTag(): string {
  return LOCALES.find((item) => item.id === getLocale())?.html ?? "es-AR";
}

export function fmtDateTime(iso: string | Date | null | undefined): string {
  if (!iso) return "—";
  return new Intl.DateTimeFormat(localeTag(), { dateStyle: "short", timeStyle: "medium" }).format(typeof iso === "string" ? new Date(iso) : iso);
}

export function fmtTime(iso: string | Date): string {
  return new Intl.DateTimeFormat(localeTag(), { timeStyle: "medium" }).format(typeof iso === "string" ? new Date(iso) : iso);
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

const objectKeys: Record<string, MessageKey> = {
  person: "labels.objectPerson",
  car: "labels.objectCar",
  motorcycle: "labels.objectMotorcycle",
  bicycle: "labels.objectBicycle",
  truck: "labels.objectTruck",
  bus: "labels.objectBus",
  dog: "labels.objectDog",
  cat: "labels.objectCat",
  license_plate: "labels.objectPlate",
  face: "labels.objectFace",
  package: "labels.objectPackage",
};

const typeKeys: Record<string, MessageKey> = {
  car: "labels.typeCar",
  sedan: "labels.typeSedan",
  hatchback: "labels.typeHatchback",
  suv: "labels.typeSuv",
  pickup: "labels.typePickup",
  van: "labels.typeVan",
  station_wagon: "labels.typeWagon",
  micro: "labels.typeMicro",
  truck: "labels.typeTruck",
  truck_trailer: "labels.typeTrailer",
  bus: "labels.typeBus",
  motorcycle: "labels.typeMotorcycle",
};

const colorKeys: Record<string, [MessageKey, MessageKey]> = {
  black: ["labels.colorBlack", "labels.colorBlackF"],
  white: ["labels.colorWhite", "labels.colorWhiteF"],
  gray: ["labels.colorGray", "labels.colorGrayF"],
  silver: ["labels.colorSilver", "labels.colorSilverF"],
  red: ["labels.colorRed", "labels.colorRedF"],
  blue: ["labels.colorBlue", "labels.colorBlueF"],
  green: ["labels.colorGreen", "labels.colorGreenF"],
  yellow: ["labels.colorYellow", "labels.colorYellowF"],
  orange: ["labels.colorOrange", "labels.colorOrangeF"],
  brown: ["labels.colorBrown", "labels.colorBrownF"],
  beige: ["labels.colorBeige", "labels.colorBeigeF"],
  purple: ["labels.colorPurple", "labels.colorPurpleF"],
};

function phrase(key: MessageKey): string {
  return translate(getLocale(), key);
}

export function labelName(l: string): string {
  const base = l.endsWith("-verified") ? l.slice(0, -"-verified".length) : l;
  const key = objectKeys[base] ?? objectKeys[l];
  return key ? phrase(key) : l;
}

/** detectionNames is the Spanish list on a card. Verified copies and plates are not repeated. */
export function detectionNames(labels?: string[] | null): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of labels ?? []) {
    const base = raw.endsWith("-verified") ? raw.slice(0, -"-verified".length) : raw;
    if (base === "license_plate") continue;
    const key = objectKeys[base];
    const name = key ? phrase(key) : undefined;
    if (!name || seen.has(name)) continue;
    seen.add(name);
    out.push(name);
  }
  return out;
}

const feminineTypes = new Set(["suv", "pickup", "van", "motorcycle", "station_wagon"]);

function typeName(type: string): string {
  const key = typeKeys[type];
  return key ? phrase(key) : type;
}

function colorWord(color: string, feminine: boolean): string {
  const pair = colorKeys[color];
  if (!pair) return color;
  return phrase(feminine ? pair[1] : pair[0]);
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
  if (!type) return labels.includes("person") ? phrase("labels.person") : "—";
  return typeName(type);
}

const paintable = new Set([
  "car", "sedan", "hatchback", "suv", "pickup", "van", "station_wagon", "micro",
  "motorcycle", "truck", "bus", "truck_trailer",
]);

/** Paint is shown for every vehicle the classifier or Frigate can name. */
export function vehicleColorText(labels: string[], v?: VehicleReading | null): string | null {
  const primary = firstVehicle(labels);
  const type = v?.type && v.type !== "unknown" ? v.type : primary;
  if (!paintable.has(primary) && !paintable.has(type ?? "")) return null;
  if (!v?.color || v.color === "unknown" || v.color === "other") return phrase("labels.undetected");
  return colorWord(v.color, feminineTypes.has(type ?? "") || primary === "motorcycle");
}

/** Spanish name and swatch for a clothing or paint color. An unresolved reading stays visible. */
export function appearanceColor(color?: string, pending = false): { text: string; paint: string | null } {
  if (pending) return { text: phrase("labels.detecting"), paint: null };
  if (!color || color === "unknown" || color === "other") return { text: phrase("labels.undetected"), paint: null };
  return { text: colorWord(color, false), paint: paintHex[color] ?? null };
}

/** vehicleHeadline is the card line once enrichment is confident enough to show. */
export function vehicleHeadline(v?: VehicleReading): string | null {
  if (!v) return null;
  const typeOk = !!v.type && v.type !== "unknown" && (v.type_confidence ?? 0) >= 0.6;
  const colorOk = !!v.color && v.color !== "unknown" && v.color !== "other" && (v.color_confidence ?? 0) >= 0.55;
  const type = typeOk ? typeName(v.type!) : "";
  const color = colorOk ? colorWord(v.color!, typeOk && feminineTypes.has(v.type!)) : "";
  if (type && color) return `${type} · ${color}`;
  if (type) return type;
  if (color) return `${phrase("labels.vehicle")} · ${colorWord(v.color!, false)}`;
  return null;
}

/** Physical vehicle paint swatches (data colors for the detected car color, not theme colors). */
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

export function vehicleTypeOptions() {
  return Object.keys(typeKeys).map((value) => ({ value, label: typeName(value) }));
}
export function vehicleColorOptions() {
  return Object.keys(colorKeys).map((value) => ({ value, label: colorWord(value, false) }));
}

export const commonLabels = ["person", "car", "motorcycle", "truck", "bicycle", "dog"];

/** Classified types are stored after enrichment, not as a Frigate label. */
export const classifiedVehicleTypes = ["sedan", "hatchback", "suv", "pickup", "van", "station_wagon", "micro", "truck_trailer"];

export function isClassifiedVehicleType(value: string): boolean {
  return classifiedVehicleTypes.includes(value);
}

/** Object filter: detector labels, then the fine types the classifier can store. */
export function objectFilterOptions() {
  return [
    ...["person", "car", "motorcycle", "truck", "bus", "bicycle", "dog"].map((value) => ({ value, label: labelName(value) })),
    ...classifiedVehicleTypes.map((value) => ({ value, label: typeName(value) })),
  ];
}

/** Every label with a display name, common ones first: suggestions for label inputs. */
export const knownLabels = [...new Set([...commonLabels, ...Object.keys(objectKeys)])];
