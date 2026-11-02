import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { formatRecognitionsToCsv } from "./faceExport";

const rec = (over: Partial<Schemas["FaceRecognition"]> = {}): Schemas["FaceRecognition"] =>
  ({ event_id: "e1", person_id: "p1", person_name: "Ana", camera_id: "c1", camera_name: "Acceso", site_id: "s1", site_name: "Sede", server_name: "h01", start_time: "2026-11-02T10:05:07Z", has_thumbnail: false, has_snapshot: false, ...over }) as Schemas["FaceRecognition"];

describe("formatRecognitionsToCsv", () => {
  it("writes a BOM, a header and one row per recognition with a clean timestamp", () => {
    const csv = formatRecognitionsToCsv([rec()]);
    expect(csv.startsWith("﻿")).toBe(true);
    expect(csv.slice(1).split("\r\n")).toEqual(["Fecha / Hora,Persona,Cámara,Sitio,Servidor", "2026-11-02 10:05:07,Ana,Acceso,Sede,h01"]);
  });

  it("quotes commas, quotes and line breaks", () => {
    const csv = formatRecognitionsToCsv([rec({ person_name: 'Díaz, "Marcos"', camera_name: "a\nb" })]);
    expect(csv).toContain('"Díaz, ""Marcos"""');
    expect(csv).toContain('"a\nb"');
  });

  it("keeps a name that looks like a formula as text", () => {
    const csv = formatRecognitionsToCsv([rec({ person_name: "=HYPERLINK(\"http://x\")" }), rec({ person_name: "@cmd" }), rec({ person_name: "-1+1" })]);
    const lines = csv.split("\r\n");
    expect(lines[1]).toContain(`"'=HYPERLINK(""http://x"")"`);
    expect(lines[2]).toContain("'@cmd");
    expect(lines[3]).toContain("'-1+1");
  });

  it("returns only the header for an empty list", () => {
    expect(formatRecognitionsToCsv([]).slice(1)).toBe("Fecha / Hora,Persona,Cámara,Sitio,Servidor");
  });
});
