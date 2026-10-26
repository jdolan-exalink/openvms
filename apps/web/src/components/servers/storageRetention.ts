export type CameraPriority = "low" | "normal" | "high" | "critical";

export interface CameraPriorityItem {
  id: string;
  name: string;
  priority: CameraPriority;
}

export const PRIORITY_CONFIG: Record<
  CameraPriority,
  { label: string; weight: number; colorClass: string }
> = {
  low: { label: "Baja (0.5x)", weight: 0.5, colorClass: "text-muted" },
  normal: { label: "Normal (1.0x)", weight: 1.0, colorClass: "text-primary" },
  high: { label: "Alta (1.5x)", weight: 1.5, colorClass: "text-warn" },
  critical: { label: "Crítica (2.0x)", weight: 2.0, colorClass: "text-danger" },
};

export const DEFAULT_SAFETY_BUFFER_PERCENT = 15;
export const DEFAULT_BITRATE_MBPS = 2.0;

/**
 * Calculates usable CCTV storage and 15% safety buffer.
 */
export function calculateUsableStorage(
  totalBytes: number,
  bufferPercent: number = DEFAULT_SAFETY_BUFFER_PERCENT
): { usableBytes: number; reservedBytes: number } {
  if (totalBytes <= 0) {
    return { usableBytes: 0, reservedBytes: 0 };
  }
  const reservedBytes = Math.floor(totalBytes * (bufferPercent / 100));
  const usableBytes = Math.max(0, totalBytes - reservedBytes);
  return { usableBytes, reservedBytes };
}

/**
 * Estimates maximum continuous recording days possible with usable storage,
 * number of cameras, and camera stream bitrate (in Mbps).
 *
 * Formula:
 * 1 Mbps = 1,000,000 bits/sec
 * Bytes per sec = bits / 8
 * GB per day = (Mbps * 1,000,000 * 3600 * 24) / (8 * 1,000,000,000) = Mbps * 10.8
 * e.g. 2.0 Mbps -> 21.6 GB/day per camera.
 */
export function calculateAttainableDays({
  usableBytes,
  cameraCount,
  bitrateMbps = DEFAULT_BITRATE_MBPS,
}: {
  usableBytes: number;
  cameraCount: number;
  bitrateMbps?: number;
}): number {
  const count = Math.max(1, cameraCount);
  const gbPerDayPerCam = (bitrateMbps * 86400) / 8000;
  const totalDailyGB = count * gbPerDayPerCam;
  const usableGB = usableBytes / 1_000_000_000;

  if (totalDailyGB <= 0 || usableGB <= 0) {
    return 1;
  }
  return Math.max(1, Math.floor(usableGB / totalDailyGB));
}

/**
 * Distributes attainable days across cameras based on their assigned priority weights.
 */
export function calculatePerCameraDays(
  cameras: CameraPriorityItem[],
  baseAttainableDays: number
): Record<string, number> {
  if (cameras.length === 0) return {};

  const totalWeight = cameras.reduce(
    (sum, c) => sum + (PRIORITY_CONFIG[c.priority]?.weight ?? 1.0),
    0
  );
  const avgWeight = totalWeight / cameras.length;

  const result: Record<string, number> = {};
  for (const c of cameras) {
    const weight = PRIORITY_CONFIG[c.priority]?.weight ?? 1.0;
    const days = Math.max(
      1,
      Math.round((baseAttainableDays * weight) / (avgWeight || 1.0))
    );
    result[c.id] = days;
  }
  return result;
}

/**
 * Extracts camera names declared under the `cameras:` section in Frigate YAML.
 */
export function extractCamerasFromYaml(yaml: string): string[] {
  const lines = yaml.split("\n");
  let inCameras = false;
  const cameras: string[] = [];

  for (const line of lines) {
    if (!inCameras) {
      if (/^cameras:\s*$/.test(line)) {
        inCameras = true;
      }
    } else {
      // Top-level key marks the end of cameras block
      if (/^[a-zA-Z0-9_-]+:/.test(line)) {
        break;
      }
      // Camera declaration indented by 2 spaces
      const match = line.match(/^ {2}([a-zA-Z0-9_-]+):\s*$/);
      if (match && match[1]) {
        cameras.push(match[1]);
      }
    }
  }
  return cameras;
}

/**
 * Patches global record retain days in Frigate YAML.
 */
export function patchRetentionInYaml(yaml: string, days: number): string {
  const lines = yaml.split("\n");
  let inRecord = false;
  let inRetain = false;
  let daysReplaced = false;
  const output: string[] = [];

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]!;

    if (!inRecord) {
      if (/^record:\s*$/.test(line)) {
        inRecord = true;
      }
      output.push(line);
    } else {
      // Check if another top-level section started
      if (/^[a-zA-Z0-9_-]+:/.test(line)) {
        inRecord = false;
        output.push(line);
        continue;
      }

      if (/^ {2}retain:\s*$/.test(line)) {
        inRetain = true;
        output.push(line);
        continue;
      }

      if (inRetain) {
        if (/^ {4}days:\s*\d+/.test(line)) {
          output.push(`    days: ${days}`);
          daysReplaced = true;
          continue;
        }
        // If exiting retain
        if (!line.startsWith("    ")) {
          inRetain = false;
        }
      }

      output.push(line);
    }
  }

  if (!daysReplaced) {
    // If record block didn't exist or didn't have retain.days, append/replace standard record block
    if (!inRecord && !yaml.includes("record:")) {
      return `${yaml.trimEnd()}\n\nrecord:\n  enabled: true\n  retain:\n    days: ${days}\n    mode: motion\n`;
    }
  }

  return output.join("\n");
}

/**
 * Formats bytes to human-readable GB or TB.
 */
export function formatBytes(bytes: number): string {
  if (bytes <= 0) return "0.00 GB";
  const gb = bytes / 1_000_000_000;
  if (gb >= 1000) {
    return `${(gb / 1000).toFixed(2)} TB`;
  }
  return `${gb.toFixed(2)} GB`;
}

/**
 * Reads one scalar from Frigate YAML by key path (two-space indentation, as Frigate writes it).
 * Line based like the other helpers in this file; it is not a YAML parser.
 */
export function extractYamlValue(yaml: string, path: string[]): string | undefined {
  let depth = 0;
  for (const line of yaml.split("\n")) {
    const match = line.match(/^(\s*)([a-zA-Z0-9_-]+):\s*(.*)$/);
    if (!match) continue;
    const indent = (match[1] ?? "").length;
    if (indent === depth * 2 && match[2] === path[depth]) {
      if (depth === path.length - 1) return (match[3] ?? "").trim().replace(/^["']|["']$/g, "");
      depth++;
    }
  }
  return undefined;
}

export interface RecordingSummary {
  retainDays?: string;
  retainMode?: string;
  snapshotsRetainDays?: string;
}

/** Recording settings shown in the server detail panel; undefined fields are not set in the config. */
export function summarizeRecording(yaml: string): RecordingSummary {
  return {
    retainDays: extractYamlValue(yaml, ["record", "retain", "days"]),
    retainMode: extractYamlValue(yaml, ["record", "retain", "mode"]),
    snapshotsRetainDays: extractYamlValue(yaml, ["snapshots", "retain", "default"]),
  };
}
