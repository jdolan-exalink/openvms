import type { Schemas } from "@/api/client";

/**
 * Escapes a single cell value for CSV output in accordance with RFC 4180.
 * Any value containing commas, double quotes, or newlines is wrapped in quotes,
 * and internal double quotes are escaped as double quotes ("").
 */
function escapeCsvCell(value: unknown): string {
  if (value === null || value === undefined) {
    return "";
  }
  const str = String(value);
  if (str.includes(",") || str.includes('"') || str.includes("\n") || str.includes("\r")) {
    return `"${str.replace(/"/g, '""')}"`;
  }
  return str;
}

/**
 * Formats an ISO timestamp (e.g. "2026-10-31T03:00:00Z") to clean "YYYY-MM-DD HH:mm:ss".
 */
function formatTimestamp(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return iso.replace("T", " ").replace("Z", "").slice(0, 19);
}

const HEADERS = [
  "Fecha / Hora",
  "Patente",
  "Cámara",
  "Sitio",
  "Servidor",
  "Confianza (%)",
  "Tipo Vehículo",
  "Color",
  "Marca",
  "Modelo",
  "Zonas",
];

/**
 * Converts a list of PlateRead objects to an RFC 4180 compliant CSV string with a UTF-8 BOM.
 */
export function formatPlateReadsToCsv(reads: Schemas["PlateRead"][]): string {
  const rows: string[] = [HEADERS.map(escapeCsvCell).join(",")];

  for (const r of reads) {
    const scoreStr = r.score != null ? `${Math.round(r.score * 100)}%` : "";
    const vehicleType = r.vehicle?.type ?? r.label ?? "";
    const vehicleColor = r.vehicle?.color ?? "";
    const brand = r.vehicle?.brand ?? "";
    const model = r.vehicle?.model ?? "";
    const zonesStr = (r.zones ?? []).join("; ");

    const row = [
      formatTimestamp(r.seen_at),
      r.plate_normalized || r.plate,
      r.camera_name,
      r.site_name,
      r.server_name,
      scoreStr,
      vehicleType,
      vehicleColor,
      brand,
      model,
      zonesStr,
    ];

    rows.push(row.map(escapeCsvCell).join(","));
  }

  // UTF-8 BOM (\uFEFF) ensures proper rendering in Excel / Numbers
  return "\uFEFF" + rows.join("\r\n");
}

/**
 * Initiates browser download of plate reads as a CSV file.
 */
export function downloadPlateReadsCsv(reads: Schemas["PlateRead"][], filename?: string): void {
  const content = formatPlateReadsToCsv(reads);
  const blob = new Blob([content], { type: "text/csv;charset=utf-8;" });
  const url = URL.createObjectURL(blob);

  const finalName = filename || `lpr-lecturas-${new Date().toISOString().slice(0, 10)}.csv`;
  const link = document.createElement("a");
  link.href = url;
  link.download = finalName;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);

  URL.revokeObjectURL(url);
}
