import { describe, expect, it } from "vitest";
import {
  calculateAttainableDays,
  calculatePerCameraDays,
  calculateUsableStorage,
  extractCamerasFromYaml,
  formatBytes,
  patchRetentionInYaml,
  type CameraPriorityItem,
} from "./storageRetention";

describe("storageRetention", () => {
  describe("calculateUsableStorage", () => {
    it("deducts 15% safety buffer from total storage", () => {
      const total = 1_000_000_000_000; // 1 TB
      const { usableBytes, reservedBytes } = calculateUsableStorage(total, 15);

      expect(reservedBytes).toBe(150_000_000_000);
      expect(usableBytes).toBe(850_000_000_000);
    });

    it("handles zero or negative storage gracefully", () => {
      const { usableBytes, reservedBytes } = calculateUsableStorage(0);
      expect(usableBytes).toBe(0);
      expect(reservedBytes).toBe(0);
    });
  });

  describe("calculateAttainableDays", () => {
    it("calculates realistic days for 1 camera with 850GB usable at 2.0 Mbps (~21.6 GB/day)", () => {
      const usableBytes = 850_000_000_000; // 850 GB
      const days = calculateAttainableDays({
        usableBytes,
        cameraCount: 1,
        bitrateMbps: 2.0,
      });

      // 850 GB / 21.6 GB/day ≈ 39.35 days -> floor 39
      expect(days).toBe(39);
    });

    it("calculates attainable days for 4 cameras with 850GB usable at 2.0 Mbps", () => {
      const usableBytes = 850_000_000_000; // 850 GB
      const days = calculateAttainableDays({
        usableBytes,
        cameraCount: 4,
        bitrateMbps: 2.0,
      });

      // 850 GB / (4 * 21.6 GB/day) = 850 / 86.4 ≈ 9.83 days -> floor 9
      expect(days).toBe(9);
    });

    it("ensures minimum 1 day even with constrained space", () => {
      const days = calculateAttainableDays({
        usableBytes: 5_000_000_000, // 5 GB
        cameraCount: 10,
        bitrateMbps: 2.0,
      });
      expect(days).toBe(1);
    });
  });

  describe("calculatePerCameraDays", () => {
    it("distributes days according to camera priority weights", () => {
      const cameras: CameraPriorityItem[] = [
        { id: "cam1", name: "Entrada Principal", priority: "critical" }, // 2.0x
        { id: "cam2", name: "Patio", priority: "normal" },              // 1.0x
        { id: "cam3", name: "Depósito", priority: "low" },               // 0.5x
      ];
      // Total weights: 2.0 + 1.0 + 0.5 = 3.5. Average: 3.5 / 3 = 1.1666
      // Base days: 14
      const result = calculatePerCameraDays(cameras, 14);

      // cam1: 14 * (2.0 / 1.1666) ≈ 24 days
      // cam2: 14 * (1.0 / 1.1666) ≈ 12 days
      // cam3: 14 * (0.5 / 1.1666) ≈ 6 days
      expect(result["cam1"]).toBe(24);
      expect(result["cam2"]).toBe(12);
      expect(result["cam3"]).toBe(6);
    });

    it("returns identical days when all cameras have normal priority", () => {
      const cameras: CameraPriorityItem[] = [
        { id: "cam1", name: "Cam 1", priority: "normal" },
        { id: "cam2", name: "Cam 2", priority: "normal" },
      ];
      const result = calculatePerCameraDays(cameras, 10);
      expect(result["cam1"]).toBe(10);
      expect(result["cam2"]).toBe(10);
    });
  });

  describe("extractCamerasFromYaml", () => {
    it("extracts camera names defined under cameras section", () => {
      const yaml = `mqtt:
  host: 10.0.0.1
cameras:
  front_door:
    ffmpeg:
      inputs: []
  backyard:
    ffmpeg:
      inputs: []
record:
  retain:
    days: 7
`;
      const cameras = extractCamerasFromYaml(yaml);
      expect(cameras).toEqual(["front_door", "backyard"]);
    });

    it("returns empty array if no cameras defined in YAML", () => {
      const yaml = `mqtt:\n  host: 10.0.0.1\n`;
      expect(extractCamerasFromYaml(yaml)).toEqual([]);
    });
  });

  describe("patchRetentionInYaml", () => {
    it("updates global record.retain.days in YAML", () => {
      const yaml = `record:
  enabled: true
  retain:
    days: 7
    mode: motion
`;
      const patched = patchRetentionInYaml(yaml, 21);
      expect(patched).toContain("days: 21");
      expect(patched).toContain("mode: motion");
    });

    it("adds record block if missing from YAML", () => {
      const yaml = `mqtt:\n  host: 10.0.0.1\n`;
      const patched = patchRetentionInYaml(yaml, 14);
      expect(patched).toContain("record:");
      expect(patched).toContain("days: 14");
    });
  });

  describe("formatBytes", () => {
    it("formats bytes accurately to GB and TB", () => {
      expect(formatBytes(1_000_000_000)).toBe("1.00 GB");
      expect(formatBytes(850_000_000_000)).toBe("850.00 GB");
      expect(formatBytes(2_000_000_000_000)).toBe("2.00 TB");
    });
  });
});
