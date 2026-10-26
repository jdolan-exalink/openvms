import { describe, expect, it } from "vitest";
import { FRIGATE_GROUPS, groupCoverage, setSectionScalar, topLevelBlock, topLevelKeys, yamlScalar } from "./frigateGroups";

const yaml = `mqtt:
  host: 10.0.0.1
detectors:
  cpu1:
    type: cpu
model:
  path: /models/custom.tflite
ffmpeg:
  hwaccel_args: preset-vaapi
  global_args:
    - -hide_banner
go2rtc:
  streams: {}
cameras:
  patio: {}
`;

describe("frigateGroups", () => {
  it("covers the PRD list", () => {
    expect(FRIGATE_GROUPS.map((g) => g.label)).toEqual(
      expect.arrayContaining(["Detector", "Modelo", "Objetos disponibles", "MQTT", "Almacenamiento", "Aceleración", "Defaults heredados por las cámaras"]),
    );
  });

  it("assigns every key to one group or to other", () => {
    const { groups, other } = groupCoverage(yaml);
    expect(groups.find((g) => g.id === "model")?.present).toEqual(["model"]);
    expect(groups.find((g) => g.id === "acceleration")?.present).toEqual(["ffmpeg"]);
    expect(other).toEqual(["go2rtc", "cameras"]);
    const all = [...groups.flatMap((g) => g.present), ...other].sort();
    expect(all).toEqual(topLevelKeys(yaml).sort());
  });

  it("owns each key in a single group", () => {
    const keys = FRIGATE_GROUPS.flatMap((g) => g.keys);
    expect(new Set(keys).size).toBe(keys.length);
  });

  it("reads blocks and scalars", () => {
    expect(topLevelBlock(yaml, "model")).toBe("model:\n  path: /models/custom.tflite");
    expect(yamlScalar(yaml, ["ffmpeg", "hwaccel_args"])).toBe("preset-vaapi");
    expect(yamlScalar(yaml, ["model", "width"])).toBeUndefined();
  });

  it("sets, replaces and removes a section scalar without touching other lines", () => {
    const next = setSectionScalar(yaml, "ffmpeg", "hwaccel_args", "preset-nvidia");
    expect(yamlScalar(next, ["ffmpeg", "hwaccel_args"])).toBe("preset-nvidia");
    expect(next).toContain("    - -hide_banner");
    expect(setSectionScalar(yaml, "ffmpeg", "hwaccel_args", "")).not.toContain("hwaccel_args");
    expect(setSectionScalar(yaml, "ffmpeg", "output_args", "x")).toContain("  output_args: x");
    expect(setSectionScalar("mqtt:\n  host: a\n", "ffmpeg", "hwaccel_args", "preset-vaapi")).toContain("ffmpeg:\n  hwaccel_args: preset-vaapi");
    expect(setSectionScalar("mqtt:\n  host: a\n", "ffmpeg", "hwaccel_args", "")).toBe("mqtt:\n  host: a\n");
  });
});
