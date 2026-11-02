import type { Schemas } from "@/api/client";

const HEADERS = ["Fecha / Hora", "Persona", "Cámara", "Sitio", "Servidor"];

/**
 * A name typed by an operator can start with = + - or @, which a spreadsheet would run as a formula when
 * the file is opened. A leading apostrophe keeps it as text.
 */
function cell(value: unknown): string {
  let text = value === null || value === undefined ? "" : String(value);
  if (/^[=+\-@\t\r]/.test(text)) text = `'${text}`;
  return /[",\r\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
}

const stamp = (iso: string) => (Number.isNaN(new Date(iso).getTime()) ? iso : iso.replace("T", " ").replace("Z", "").slice(0, 19));

/** formatRecognitionsToCsv is RFC 4180 with a UTF-8 BOM so Excel opens accents correctly. */
export function formatRecognitionsToCsv(items: Schemas["FaceRecognition"][]): string {
  const rows = [HEADERS.map(cell).join(",")];
  for (const r of items) rows.push([stamp(r.start_time), r.person_name, r.camera_name, r.site_name, r.server_name].map(cell).join(","));
  return "﻿" + rows.join("\r\n");
}

export function downloadRecognitionsCsv(items: Schemas["FaceRecognition"][], filename?: string): void {
  const blob = new Blob([formatRecognitionsToCsv(items)], { type: "text/csv;charset=utf-8;" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename || `rostros-reconocimientos-${new Date().toISOString().slice(0, 10)}.csv`;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}
