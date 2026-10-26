import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { capabilityList, deviceRows, statusChips, videoRows } from "./summary";

const camera = (over: Partial<Schemas["Camera"]> = {}) => ({ id: "c1", enabled: true, status: "online", lpr: false, tags: [], ...over }) as Schemas["Camera"];

describe("statusChips", () => {
  it("reports what the camera and the config say", () => {
    const chips = statusChips(camera({ lpr: true, onvif_endpoint: "http://10.0.0.5/onvif" }), { record: { enabled: true }, detect: { enabled: true } });
    expect(chips.map((c) => c.label)).toEqual(["En línea", "Grabando", "Detección activa", "LPR activo", "ONVIF"]);
    expect(chips.every((c) => c.tone === "ok")).toBe(true);
  });

  it("shows a dash instead of inventing a state the config does not know", () => {
    const chips = statusChips(camera(), {});
    expect(chips.find((c) => c.id === "record")).toMatchObject({ label: "Grabación —", tone: "unknown" });
    expect(chips.find((c) => c.id === "detect")).toMatchObject({ label: "Detección —", tone: "unknown" });
    expect(chips.find((c) => c.id === "lpr")).toMatchObject({ label: "LPR inactivo", tone: "off" });
    expect(chips.find((c) => c.id === "onvif")).toBeUndefined();
  });

  it("marks disabled and offline cameras", () => {
    expect(statusChips(camera({ enabled: false }), {})[0]).toMatchObject({ label: "Deshabilitada", tone: "off" });
    expect(statusChips(camera({ status: "offline" }), {})[0]).toMatchObject({ label: "Sin conexión", tone: "off" });
    expect(statusChips(undefined, {})[0]).toMatchObject({ label: "—", tone: "unknown" });
  });
});

describe("capabilityList", () => {
  it("joins only advertised capabilities", () => {
    const caps = capabilityList(camera({ lpr: true, onvif_capabilities: { has_ptz: true } }), { audio: { enabled: true } });
    expect(caps).toEqual(["ONVIF", "Audio", "PTZ", "LPR"]);
    expect(capabilityList(camera(), {})).toEqual([]);
  });
});

describe("videoRows", () => {
  const config = { ffmpeg: { inputs: [{ roles: ["record"] }, { roles: ["detect"] }] }, detect: { width: 640, height: 360, fps: 5 } };
  it("shows Principal and Detección and hides Grabación when it is the principal input", () => {
    const rows = videoRows(camera({ hq_codec: "h265", live_codec: "h264" }), config);
    expect(rows).toEqual([
      { id: "primary", label: "Principal", value: "H265" },
      { id: "detect", label: "Detección", value: "640×360 · 5 fps · H264" },
    ]);
  });

  it("adds Grabación only when the record input differs from the principal", () => {
    const rows = videoRows(camera(), { ffmpeg: { inputs: [{ roles: ["live"] }, { roles: ["detect"] }, { roles: ["record"] }] } });
    expect(rows.map((r) => r.id)).toEqual(["primary", "detect", "record"]);
  });

  it("uses a dash when nothing is known", () => {
    expect(videoRows(undefined, {}).map((r) => r.value)).toEqual(["—", "—"]);
  });
});

describe("deviceRows", () => {
  it("reads the ONVIF facts and the host", () => {
    const rows = deviceRows(camera({ onvif_endpoint: "http://10.0.0.5:80/onvif/device_service", onvif_capabilities: { manufacturer: "Hikvision", model: "DS-1", firmware_version: "5.7" } }), {});
    expect(rows).toEqual([
      { label: "Fabricante", value: "Hikvision" },
      { label: "Modelo", value: "DS-1" },
      { label: "IP", value: "10.0.0.5" },
      { label: "Firmware", value: "5.7" },
    ]);
  });

  it("falls back to the Frigate onvif host and dashes", () => {
    const rows = deviceRows(camera(), { onvif: { host: "10.0.0.2" } });
    expect(rows.map((r) => r.value)).toEqual(["—", "—", "10.0.0.2", "—"]);
  });
});

describe("videoRows with the normalized model", () => {
  const model = (streams: object) => ({ streams }) as Schemas["CameraConfig"];

  it("shows the real principal and detection values", () => {
    const rows = videoRows(camera(), {}, model({ primary: { width: 1920, height: 1080, fps: 15, codec: "h264", roles: [] }, detection: { width: 640, height: 360, fps: 5, codec: "h264", roles: [] } }));
    expect(rows.map((r) => `${r.label} ${r.value}`)).toEqual(["Principal 1920×1080 · 15 FPS · H.264", "Detección 640×360 · 5 FPS · H.264"]);
  });

  it("keeps the dash when the model has no values and adds Grabación only when distinct", () => {
    expect(videoRows(camera(), {}, model({}))[0]!.value).toBe("—");
    const rows = videoRows(camera(), {}, model({ recording: { width: 1280, height: 720, roles: [] } }));
    expect(rows.find((r) => r.id === "record")?.value).toBe("1280×720");
  });
});
