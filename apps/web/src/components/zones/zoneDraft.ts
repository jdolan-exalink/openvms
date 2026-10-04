import {
  type FrameSize,
  type MaskFormat,
  type MaskItem,
  type Point,
  newId,
  parseCoordinates,
  parseMasks,
  serializeCoordinates,
  serializeMasks,
  usesDictMasks,
} from "@/lib/zoneGeometry";

export type ItemKind = "zone" | "motion" | "object";

/** Value exchanged with the parent. Mask fields keep the raw config shape of the running Frigate version. */
export interface ZoneEditorValue {
  /** `zones` section keyed by zone name; `coordinates` is a "x1,y1,..." string. */
  zones: Record<string, Record<string, unknown>>;
  /** `motion.mask` */
  motionMask: unknown;
  /** `objects.mask` (global object mask) */
  objectMask: unknown;
  /** `objects.filters.<label>.mask` keyed by label */
  objectFilterMasks: Record<string, unknown>;
}

export interface EditorItem {
  uid: string;
  kind: ItemKind;
  /** Object masks only: "" = global (`objects.mask`), otherwise the label of `objects.filters.<label>.mask`. */
  scope: string;
  /** Zone key, or mask friendly name. */
  name: string;
  points: Point[];
  enabled: boolean;
  /** Mask id (dict formats) used when serializing back. */
  maskId?: string;
  /** Unknown keys of a dict mask. */
  extra?: Record<string, unknown>;
  /** Zone config without `coordinates`. */
  config?: Record<string, unknown>;
  colorIndex: number;
}

export interface Draft {
  items: EditorItem[];
  formats: { motion: MaskFormat; objectGlobal: MaskFormat; filters: Record<string, MaskFormat> };
  nextColor: number;
}

const maskItem = (m: MaskItem, kind: ItemKind, scope: string, colorIndex: number): EditorItem => ({
  uid: newId("item"),
  kind,
  scope,
  name: m.name,
  points: m.points,
  enabled: m.enabled,
  maskId: m.id,
  extra: m.extra,
  colorIndex,
});

export function buildDraft(value: ZoneEditorValue, frame: Partial<FrameSize> | undefined, version: string | null | undefined): Draft {
  const items: EditorItem[] = [];
  let c = 0;
  for (const [name, cfg] of Object.entries(value.zones ?? {})) {
    const { coordinates, ...config } = cfg ?? {};
    items.push({
      uid: newId("item"),
      kind: "zone",
      scope: "",
      name,
      points: parseCoordinates(coordinates, frame),
      enabled: true,
      config,
      colorIndex: c++,
    });
  }
  const motion = parseMasks(value.motionMask, frame, version);
  motion.items.forEach((m) => items.push(maskItem(m, "motion", "", c++)));
  const global = parseMasks(value.objectMask, frame, version);
  global.items.forEach((m) => items.push(maskItem(m, "object", "", c++)));
  const filters: Record<string, MaskFormat> = {};
  for (const [label, raw] of Object.entries(value.objectFilterMasks ?? {})) {
    const l = parseMasks(raw, frame, version);
    filters[label] = l.format;
    l.items.forEach((m) => items.push(maskItem(m, "object", label, c++)));
  }
  return { items, formats: { motion: motion.format, objectGlobal: global.format, filters }, nextColor: c };
}

const toMask = (it: EditorItem): MaskItem => ({
  id: it.maskId ?? newId("mask"),
  name: it.name,
  enabled: it.enabled,
  points: it.points,
  extra: it.extra,
});

/** Serializes the draft back to the config shapes, preserving the original mask formats. */
export function draftToValue(draft: Draft, version: string | null | undefined): ZoneEditorValue {
  const fallback: MaskFormat = usesDictMasks(version) ? "dict-map" : "list";
  const zones: ZoneEditorValue["zones"] = {};
  for (const it of draft.items.filter((i) => i.kind === "zone" && i.points.length >= 3)) {
    zones[it.name] = { ...it.config, coordinates: serializeCoordinates(it.points) };
  }
  const masks = (kind: ItemKind, scope: string) => draft.items.filter((i) => i.kind === kind && i.scope === scope).map(toMask);
  const objectFilterMasks: Record<string, unknown> = {};
  const labels = new Set([...Object.keys(draft.formats.filters), ...draft.items.filter((i) => i.kind === "object" && i.scope).map((i) => i.scope)]);
  for (const label of labels) {
    objectFilterMasks[label] = serializeMasks({ format: draft.formats.filters[label] ?? fallback, items: masks("object", label) });
  }
  return {
    zones,
    motionMask: serializeMasks({ format: draft.formats.motion, items: masks("motion", "") }),
    objectMask: serializeMasks({ format: draft.formats.objectGlobal, items: masks("object", "") }),
    objectFilterMasks,
  };
}

/** True for dict mask formats, where per-mask friendly name and enabled flag are persisted. */
export const isDictFormat = (f: MaskFormat) => f === "dict-list" || f === "dict-map";

export function maskFormatOf(draft: Draft, item: EditorItem): MaskFormat {
  if (item.kind === "motion") return draft.formats.motion;
  return item.scope ? (draft.formats.filters[item.scope] ?? draft.formats.objectGlobal) : draft.formats.objectGlobal;
}

/** Item colors are stored on persisted zone configuration (user data), so they are fixed hex values. */
export const ITEM_COLORS = ["#ef4444", "#3b82f6", "#22c55e", "#eab308", "#a855f7", "#f97316", "#06b6d4", "#ec4899", "#84cc16", "#14b8a6"];
export const colorOf = (it: EditorItem) => ITEM_COLORS[it.colorIndex % ITEM_COLORS.length]!;

export interface ItemIssues {
  errors: string[];
  warnings: string[];
}
