import { describe, expect, it, vi } from "vitest";
import type { Schemas } from "@/api/client";
import { downloadPlateReadsCsv, formatPlateReadsToCsv } from "./lprExport";

function sampleRead(overrides: Partial<Schemas["PlateRead"]> = {}): Schemas["PlateRead"] {
  return {
    id: "read-1",
    site_id: "site-1",
    site_name: "Central",
    server_id: "srv-1",
    server_name: "NVR-01",
    camera_id: "cam-1",
    camera_name: "Ingreso Norte",
    plate: "AB123CD",
    plate_normalized: "AB123CD",
    score: 0.954,
    label: "car",
    zones: ["ingreso", "barrera"],
    seen_at: "2026-10-31T03:00:00Z",
    event_id: null,
    vehicle: {
      type: "suv",
      type_confidence: 0.9,
      color: "black",
      color_confidence: 0.88,
      color_quality: "good",
      brand: "Toyota",
      model: "Corolla Cross",
    },
    ...overrides,
  };
}

describe("formatPlateReadsToCsv", () => {
  it("generates CSV with UTF-8 BOM, headers, and vehicle attributes", () => {
    const reads = [sampleRead()];
    const csv = formatPlateReadsToCsv(reads);

    expect(csv.startsWith("\uFEFF")).toBe(true);
    const lines = csv.slice(1).split("\r\n");
    expect(lines[0]).toBe("Fecha / Hora,Patente,Cámara,Sitio,Servidor,Confianza (%),Tipo Vehículo,Color,Marca,Modelo,Zonas");
    expect(lines[1]).toContain("2026-10-31 03:00:00,AB123CD,Ingreso Norte,Central,NVR-01,95%,suv,black,Toyota,Corolla Cross,ingreso; barrera");
  });

  it("escapes fields with commas, quotes, and newlines properly according to RFC 4180", () => {
    const reads = [
      sampleRead({
        camera_name: 'Cam "Principal", Entrada',
        vehicle: {
          type: "car",
          type_confidence: 0.9,
          color: "blue",
          color_confidence: 0.9,
          color_quality: "good",
          brand: 'Ford, "Special"',
          model: "Focus\nTitanium",
        },
      }),
    ];
    const csv = formatPlateReadsToCsv(reads);
    const content = csv.slice(1);
    expect(content).toContain('"Cam ""Principal"", Entrada"');
    expect(content).toContain('"Ford, ""Special"""');
    expect(content).toContain('"Focus\nTitanium"');
  });

  it("handles missing vehicle and null scores gracefully", () => {
    const reads = [
      sampleRead({
        score: null,
        vehicle: undefined,
        zones: [],
      }),
    ];
    const csv = formatPlateReadsToCsv(reads);
    const lines = csv.slice(1).split("\r\n");
    expect(lines[1]).toContain("AB123CD,Ingreso Norte,Central,NVR-01,,car,,,,");
  });
});

describe("downloadPlateReadsCsv", () => {
  it("creates an anchor and triggers download", () => {
    const fakeUrl = "blob:fake-csv-url";
    const createObjectURL = vi.fn(() => fakeUrl);
    const revokeObjectURL = vi.fn();
    vi.stubGlobal("URL", { createObjectURL, revokeObjectURL });

    const clickSpy = vi.fn();
    const originalCreateElement = document.createElement.bind(document);
    vi.spyOn(document, "createElement").mockImplementation((tagName: string) => {
      const el = originalCreateElement(tagName);
      if (tagName === "a") {
        el.click = clickSpy;
      }
      return el;
    });

    const reads = [sampleRead()];
    downloadPlateReadsCsv(reads, "test-plates.csv");

    expect(createObjectURL).toHaveBeenCalled();
    expect(clickSpy).toHaveBeenCalled();
    expect(revokeObjectURL).toHaveBeenCalledWith(fakeUrl);
  });
});
