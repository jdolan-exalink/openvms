const dateTime = new Intl.DateTimeFormat("es-AR", { dateStyle: "short", timeStyle: "medium" });
const time = new Intl.DateTimeFormat("es-AR", { timeStyle: "medium" });

export function fmtDateTime(iso: string | Date | null | undefined): string {
  if (!iso) return "—";
  return dateTime.format(typeof iso === "string" ? new Date(iso) : iso);
}

export function fmtTime(iso: string | Date): string {
  return time.format(typeof iso === "string" ? new Date(iso) : iso);
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
