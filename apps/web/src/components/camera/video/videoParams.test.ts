import { describe, expect, it } from "vitest";
import { codecLabel, differsFromCurrent, rangeError, reasonText, reconcileForCodec, streamSummary } from "./videoParams";

const h265 = { codec: "h265", resolutions: [{ width: 2560, height: 1440 }, { width: 1920, height: 1080 }], frame_rates: [], fps: { min: 1, max: 20 }, bitrate_kbps: { min: 512, max: 8192 }, gop: { min: 1, max: 100 }, bitrate_modes: [] };

describe("videoParams", () => {
  it("formats the stream summary like the PRD", () => {
    expect(streamSummary({ width: 1920, height: 1080, fps: 15, codec: "h264" })).toBe("1920×1080 · 15 FPS · H.264");
    expect(streamSummary({ width: 640, height: 360, fps: 5, codec: "h264" })).toBe("640×360 · 5 FPS · H.264");
    expect(streamSummary({})).toBeUndefined();
    expect(codecLabel("mjpeg")).toBe("MJPEG");
  });

  it("maps every reason code to plain Spanish", () => {
    expect(reasonText("no_onvif")).toBe("Esta cámara no expone ONVIF");
    expect(reasonText("no_credentials")).toBe("Faltan las credenciales de la cámara");
    expect(reasonText("unreachable")).toBe("No se pudo contactar a la cámara");
    expect(reasonText("unsupported")).toBe("La cámara no permite cambiar estos valores");
  });

  it("enforces ranges", () => {
    expect(rangeError(25, { min: 1, max: 20 })).toBe("Debe estar entre 1 y 20.");
    expect(rangeError(10, { min: 1, max: 20 })).toBeUndefined();
    expect(rangeError(Number.NaN, undefined)).toBe("Ingresa un número entero.");
  });

  it("moves only unsupported values when the codec changes", () => {
    const patch = reconcileForCodec(h265, { codec: "h264", resolution: { width: 640, height: 360 }, fps: 30, bitrate_kbps: 4096, gop: 50 });
    expect(patch).toEqual({ codec: "h265", resolution: { width: 2560, height: 1440 }, fps: 20 });
  });

  it("detects a real difference only", () => {
    const cur = { codec: "h264", fps: 15 };
    expect(differsFromCurrent(cur, { fps: 15 })).toBe(false);
    expect(differsFromCurrent(cur, undefined)).toBe(false);
    expect(differsFromCurrent(cur, { fps: 10 })).toBe(true);
  });
});
