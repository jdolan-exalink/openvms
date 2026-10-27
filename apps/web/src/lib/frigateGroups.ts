/**
 * Groups of the global Frigate configuration screen (PRD §35). Every top-level key of the
 * server's config.yml belongs to exactly one group; keys no group claims land under "other"
 * so nothing is lost. The functions are pure and read the YAML text the server editor holds.
 */
export type FrigateGroupId = "detector" | "model" | "objects" | "mqtt" | "storage" | "acceleration" | "defaults" | "birdseye";

export interface FrigateGroup {
  id: FrigateGroupId;
  label: string;
  /** Top-level config.yml keys the group owns. */
  keys: string[];
}

export const FRIGATE_GROUPS: FrigateGroup[] = [
  { id: "detector", label: "Detector", keys: ["detectors"] },
  { id: "model", label: "Modelo", keys: ["model"] },
  { id: "objects", label: "Objetos disponibles", keys: ["objects"] },
  { id: "mqtt", label: "MQTT", keys: ["mqtt"] },
  { id: "storage", label: "Almacenamiento", keys: ["record", "snapshots", "database"] },
  { id: "acceleration", label: "Aceleración", keys: ["ffmpeg"] },
  { id: "defaults", label: "Defaults heredados por las cámaras", keys: ["detect", "motion", "review", "live", "audio", "timestamp_style", "ui"] },
  { id: "birdseye", label: "Birdseye", keys: ["birdseye"] },
];

const KEY_LINE = /^([A-Za-z0-9_-]+):/;

/** Top-level keys of a YAML document, in order, without duplicates. */
export function topLevelKeys(yaml: string): string[] {
  const keys: string[] = [];
  for (const line of yaml.split("\n")) {
    const m = KEY_LINE.exec(line);
    if (m?.[1] && !keys.includes(m[1])) keys.push(m[1]);
  }
  return keys;
}

/** The text of one top-level block (its key line and indented body), or null when absent. */
export function topLevelBlock(yaml: string, key: string): string | null {
  const lines = yaml.split("\n");
  const start = lines.findIndex((l) => l.startsWith(`${key}:`));
  if (start < 0) return null;
  let end = start + 1;
  while (end < lines.length && !KEY_LINE.test(lines[end] ?? "")) end++;
  return lines.slice(start, end).join("\n").trimEnd();
}

export interface GroupCoverage {
  id: FrigateGroupId;
  label: string;
  present: string[];
}

/** Keys present per group plus every key no group claims ("Otros parámetros"). */
export function groupCoverage(yaml: string): { groups: GroupCoverage[]; other: string[] } {
  const keys = topLevelKeys(yaml);
  const claimed = new Set(FRIGATE_GROUPS.flatMap((g) => g.keys));
  return {
    groups: FRIGATE_GROUPS.map((g) => ({ id: g.id, label: g.label, present: g.keys.filter((k) => keys.includes(k)) })),
    other: keys.filter((k) => !claimed.has(k)),
  };
}

/** Scalar value at a nested path of 2-space indented YAML (maps only), without quotes. */
export function yamlScalar(yaml: string, path: string[]): string | undefined {
  const block = topLevelBlock(yaml, path[0] ?? "");
  if (block === null) return undefined;
  let depth = 0;
  for (const line of block.split("\n")) {
    const m = /^(\s*)([A-Za-z0-9_-]+):\s*(.*)$/.exec(line);
    if (!m || (m[1] ?? "").length !== depth * 2 || m[2] !== path[depth]) continue;
    if (depth === path.length - 1) return (m[3] ?? "").trim().replace(/^["']|["']$/g, "") || undefined;
    depth++;
  }
  return undefined;
}

/** Sets `section.key` (one nesting level) to a scalar, or removes it when value is empty; other lines stay. */
export function setSectionScalar(yaml: string, section: string, key: string, value: string): string {
  const block = topLevelBlock(yaml, section);
  if (block === null && value === "") return yaml;
  if (block === null) return `${yaml.trimEnd()}\n\n${section}:\n  ${key}: ${value}\n`;
  const lines = block.split("\n");
  const at = lines.findIndex((l, i) => i > 0 && new RegExp(`^  ${key}:`).test(l));
  if (at >= 0) {
    // Drop the old value and any nested lines it owned (list/map items indented deeper).
    let end = at + 1;
    while (end < lines.length && /^ {3,}|^ {2}-/.test(lines[end] ?? "")) end++;
    lines.splice(at, end - at, ...(value === "" ? [] : [`  ${key}: ${value}`]));
  } else if (value !== "") {
    lines.splice(1, 0, `  ${key}: ${value}`);
  }
  return yaml.replace(block, () => lines.join("\n"));
}

/** Acceleration presets offered for `ffmpeg.hwaccel_args` (Frigate's documented preset names). */
export const HWACCEL_PRESETS: { value: string; label: string }[] = [
  { value: "", label: "Sin aceleración (CPU)" },
  { value: "preset-vaapi", label: "Intel/AMD VAAPI" },
  { value: "preset-intel-qsv-h264", label: "Intel QuickSync (H.264)" },
  { value: "preset-intel-qsv-h265", label: "Intel QuickSync (H.265)" },
  { value: "preset-nvidia", label: "NVIDIA (NVDEC)" },
  { value: "preset-rpi-64-h264", label: "Raspberry Pi 64-bit (H.264)" },
];
