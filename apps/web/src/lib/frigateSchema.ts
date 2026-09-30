/**
 * Pure helpers behind the Frigate camera config editor: JSON-schema resolution (pydantic
 * flavoured: $ref, anyOf with null, allOf), field classification, basic validation and the
 * before/after diff that becomes the PATCH body. No React in here so it stays unit-testable.
 */

export type JSchema = {
  type?: string | string[];
  title?: string;
  description?: string;
  default?: unknown;
  enum?: unknown[];
  properties?: Record<string, JSchema>;
  additionalProperties?: boolean | JSchema;
  items?: JSchema;
  minimum?: number;
  maximum?: number;
  exclusiveMinimum?: number;
  exclusiveMaximum?: number;
  minItems?: number;
  maxItems?: number;
  $ref?: string;
  anyOf?: JSchema[];
  oneOf?: JSchema[];
  allOf?: JSchema[];
  $defs?: Record<string, JSchema>;
  definitions?: Record<string, JSchema>;
};

export type Resolved = { node: JSchema; nullable: boolean };

const MAX_DEPTH = 24;

function lookupRef(ref: string, root: JSchema): JSchema | undefined {
  const m = /^#\/(\$defs|definitions)\/(.+)$/.exec(ref);
  if (!m) return undefined;
  return (m[1] === "$defs" ? root.$defs : root.definitions)?.[decodeURIComponent(m[2] ?? "")];
}

const isNull = (s: JSchema) => s.type === "null";

/**
 * resolve follows $ref, merges allOf and strips `null` out of anyOf/oneOf. The result is one
 * concrete node plus whether the value may be null. Unions of several concrete shapes are
 * returned as-is (kind "json") because no friendly control exists for them.
 */
export function resolve(schema: JSchema | undefined, root: JSchema, depth = 0): Resolved {
  if (!schema || depth > MAX_DEPTH) return { node: {}, nullable: false };
  let node: JSchema = schema;
  let nullable = false;
  if (node.$ref) {
    const target = lookupRef(node.$ref, root);
    if (!target) return { node: {}, nullable: false };
    const inner = resolve(target, root, depth + 1);
    // Siblings of $ref (title, description, default) override the target's.
    const { $ref: _ref, ...siblings } = node;
    void _ref;
    return { node: { ...inner.node, ...siblings }, nullable: inner.nullable };
  }
  if (node.allOf?.length) {
    const { allOf, ...rest } = node;
    let merged: JSchema = { ...rest };
    for (const part of allOf) {
      const r = resolve(part, root, depth + 1);
      nullable = nullable || r.nullable;
      merged = { ...r.node, ...merged, properties: { ...r.node.properties, ...merged.properties } };
    }
    node = merged;
  }
  const union = node.anyOf ?? node.oneOf;
  if (union?.length) {
    const concrete = union.filter((s) => !isNull(s));
    nullable = nullable || concrete.length !== union.length;
    if (concrete.length === 1) {
      const { anyOf: _a, oneOf: _o, ...rest } = node;
      void _a;
      void _o;
      const r = resolve(concrete[0], root, depth + 1);
      return { node: { ...r.node, ...rest }, nullable: nullable || r.nullable };
    }
  }
  if (Array.isArray(node.type)) {
    const concrete = node.type.filter((t) => t !== "null");
    nullable = nullable || concrete.length !== node.type.length;
    if (concrete.length === 1) node = { ...node, type: concrete[0] };
  }
  return { node, nullable };
}

export type FieldKind = "boolean" | "enum" | "integer" | "number" | "string" | "tags" | "object" | "map" | "array" | "json";

const PRIMITIVES = new Set(["string", "integer", "number", "boolean"]);

/** kindOf picks the control a resolved node is rendered with. */
export function kindOf(node: JSchema, root: JSchema): FieldKind {
  if (node.enum?.length) return "enum";
  if (node.anyOf || node.oneOf) return "json";
  const t = node.type;
  if (t === "boolean") return "boolean";
  if (t === "integer") return "integer";
  if (t === "number") return "number";
  if (t === "string") return "string";
  if (t === "array") {
    const item = resolve(node.items, root).node;
    return typeof item.type === "string" && PRIMITIVES.has(item.type) && !item.enum ? "tags" : "array";
  }
  if (t === "object" || node.properties) {
    if (node.properties && Object.keys(node.properties).length) return "object";
    if (node.additionalProperties && typeof node.additionalProperties === "object") return "map";
    return node.properties ? "object" : "json";
  }
  return "json";
}

/** schemaAt walks a value path (object keys, map keys and array indexes) down the schema. */
export function schemaAt(root: JSchema, start: JSchema, path: (string | number)[]): JSchema | undefined {
  let cur = resolve(start, root).node;
  for (const step of path) {
    if (typeof step === "number") {
      if (!cur.items) return undefined;
      cur = resolve(cur.items, root).node;
    } else if (cur.properties?.[step]) {
      cur = resolve(cur.properties[step], root).node;
    } else if (cur.additionalProperties && typeof cur.additionalProperties === "object") {
      cur = resolve(cur.additionalProperties, root).node;
    } else {
      return undefined;
    }
  }
  return cur;
}

/** Fields Frigate fills in itself and that mean nothing to an operator. */
export const HIDDEN_FIELDS = new Set(["enabled_in_config", "name"]);

export function humanizeKey(key: string): string {
  const s = key.replace(/_/g, " ").trim();
  return s.charAt(0).toUpperCase() + s.slice(1);
}

// --- sections ---------------------------------------------------------------------------------

export const SECTION_ORDER = [
  "enabled", "ffmpeg", "detect", "objects", "motion", "record", "snapshots", "zones", "review", "audio", "birdseye",
  "lpr", "face_recognition", "semantic_search", "onvif", "live", "ui", "timestamp_style", "mqtt", "notifications", "genai",
];

export const SECTION_LABELS: Record<string, string> = {
  enabled: "Cámara habilitada",
  ffmpeg: "FFmpeg y streams",
  detect: "Detección",
  objects: "Objetos",
  motion: "Movimiento",
  record: "Grabación",
  snapshots: "Capturas",
  zones: "Zonas",
  review: "Revisión",
  audio: "Audio",
  audio_transcription: "Transcripción de audio",
  birdseye: "Birdseye",
  lpr: "Matrículas (LPR)",
  face_recognition: "Reconocimiento facial",
  semantic_search: "Búsqueda semántica",
  onvif: "ONVIF / PTZ",
  live: "En vivo",
  ui: "Interfaz",
  timestamp_style: "Marca de tiempo",
  mqtt: "MQTT",
  notifications: "Notificaciones",
  genai: "IA generativa",
  type: "Tipo",
  webui_url: "URL web",
  best_image_timeout: "Tiempo de mejor imagen",
};

export const sectionLabel = (s: string) => SECTION_LABELS[s] ?? humanizeKey(s);

/** orderSections lists the known sections first (in SECTION_ORDER) and the rest alphabetically. */
export function orderSections(names: string[]): string[] {
  const set = new Set(names.filter((n) => !HIDDEN_FIELDS.has(n)));
  const known = SECTION_ORDER.filter((n) => set.has(n));
  const rest = [...set].filter((n) => !SECTION_ORDER.includes(n)).sort();
  return [...known, ...rest];
}

/** Sections Frigate 0.17+ hot-reloads (verified in FC-1). Frigate 0.16 applies nothing live. */
const LIVE_SECTIONS = new Set([
  "enabled", "detect", "objects", "motion", "record", "snapshots", "zones", "review", "audio", "audio_transcription",
  "birdseye", "notifications", "semantic_search",
]);

export function parseVersion(v: string): [number, number] | null {
  const m = /(\d+)\.(\d+)/.exec(v);
  return m ? [Number(m[1]), Number(m[2])] : null;
}

export function isLiveSection(section: string, version: string): boolean {
  const p = parseVersion(version);
  if (!p || (p[0] === 0 && p[1] < 17)) return false;
  return LIVE_SECTIONS.has(section);
}

// --- secrets ----------------------------------------------------------------------------------

const SECRET_PATHS = ["ffmpeg.inputs", "onvif.user", "onvif.password"];

/** isSecretPath reports credential-bearing fields (stream URLs, ONVIF login). */
export function isSecretPath(path: (string | number)[]): boolean {
  const joined = path.filter((p) => typeof p === "string").join(".");
  return SECRET_PATHS.some((s) => joined === s || joined.startsWith(`${s}.`));
}

// --- values -----------------------------------------------------------------------------------

export function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (typeof a !== "object" || typeof b !== "object" || a === null || b === null) return false;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((v, i) => deepEqual(v, b[i]));
  const ka = Object.keys(a as object);
  const kb = Object.keys(b as object);
  return ka.length === kb.length && ka.every((k) => deepEqual((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]));
}

const isObj = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v);

export type Change = { path: string[]; before: unknown; after: unknown };

/** diffValues lists the leaf changes between two values; arrays are compared and reported whole. */
export function diffValues(before: unknown, after: unknown, path: string[] = []): Change[] {
  if (deepEqual(before, after)) return [];
  if (isObj(before) && isObj(after)) {
    const keys = new Set([...Object.keys(before), ...Object.keys(after)]);
    return [...keys].flatMap((k) => diffValues(before[k], after[k], [...path, k]));
  }
  return [{ path, before, after }];
}

/**
 * buildPatch returns the smallest value Frigate has to receive: objects are merged on the API
 * side, so only changed keys are sent; arrays and scalars are sent whole. A key removed from a
 * map (zones, per-label filters) becomes null because a merge cannot express deletion.
 */
export function buildPatch(before: unknown, after: unknown, isMap: (path: string[]) => boolean, path: string[] = []): unknown {
  if (isObj(before) && isObj(after)) {
    const out: Record<string, unknown> = {};
    for (const k of new Set([...Object.keys(before), ...Object.keys(after)])) {
      if (deepEqual(before[k], after[k])) continue;
      if (!(k in after) || after[k] === undefined) {
        if (isMap(path)) out[k] = null;
        continue;
      }
      out[k] = k in before ? buildPatch(before[k], after[k], isMap, [...path, k]) : after[k];
    }
    return out;
  }
  return after;
}

export const pathLabel = (path: string[]) => path.map((p, i) => (i === 0 ? sectionLabel(p) : p)).join(" › ");

/** formatValue renders a value for the diff view and read-only fields. */
export function formatValue(v: unknown): string {
  if (v === undefined) return "(sin valor)";
  if (v === null) return "(por defecto)";
  if (typeof v === "boolean") return v ? "Sí" : "No";
  if (Array.isArray(v) && v.every((x) => typeof x !== "object")) return v.length ? v.join(", ") : "(vacío)";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

// --- validation -------------------------------------------------------------------------------

/** validateValue does the basic client-side checks (type, range, enum, item count). */
export function validateValue(node: JSchema, value: unknown, kind: FieldKind): string | undefined {
  if (value === undefined || value === null || value === "") return undefined;
  if (kind === "integer" || kind === "number") {
    if (typeof value !== "number" || Number.isNaN(value)) return "Debe ser un número.";
    if (kind === "integer" && !Number.isInteger(value)) return "Debe ser un número entero.";
    if (node.minimum !== undefined && value < node.minimum) return `Debe ser al menos ${node.minimum}.`;
    if (node.maximum !== undefined && value > node.maximum) return `Debe ser como máximo ${node.maximum}.`;
    if (node.exclusiveMinimum !== undefined && value <= node.exclusiveMinimum) return `Debe ser mayor que ${node.exclusiveMinimum}.`;
    if (node.exclusiveMaximum !== undefined && value >= node.exclusiveMaximum) return `Debe ser menor que ${node.exclusiveMaximum}.`;
  }
  if (kind === "enum" && !node.enum?.includes(value)) return "Valor no permitido.";
  if (kind === "string" && typeof value !== "string") return "Debe ser texto.";
  if (Array.isArray(value)) {
    if (node.minItems !== undefined && value.length < node.minItems) return `Necesita al menos ${node.minItems} elemento(s).`;
    if (node.maxItems !== undefined && value.length > node.maxItems) return `Admite como máximo ${node.maxItems} elemento(s).`;
  }
  return undefined;
}

/** validateTree walks a section value against its schema and collects errors by dotted path. */
export function validateTree(root: JSchema, schema: JSchema, value: unknown, path: (string | number)[] = [], out: Record<string, string> = {}, depth = 0): Record<string, string> {
  if (depth > MAX_DEPTH) return out;
  const { node } = resolve(schema, root);
  const kind = kindOf(node, root);
  const err = validateValue(node, value, kind);
  if (err) out[path.join(".")] = err;
  if (kind === "object" && isObj(value)) {
    for (const [k, child] of Object.entries(node.properties ?? {})) validateTree(root, child, value[k], [...path, k], out, depth + 1);
  } else if (kind === "map" && isObj(value) && typeof node.additionalProperties === "object") {
    for (const [k, v] of Object.entries(value)) validateTree(root, node.additionalProperties, v, [...path, k], out, depth + 1);
  } else if (kind === "array" && Array.isArray(value) && node.items) {
    value.forEach((v, i) => validateTree(root, node.items as JSchema, v, [...path, i], out, depth + 1));
  }
  return out;
}

// --- paths ------------------------------------------------------------------------------------

export function getIn(v: unknown, path: (string | number)[]): unknown {
  let cur = v;
  for (const p of path) {
    if (typeof cur !== "object" || cur === null) return undefined;
    cur = (cur as Record<string | number, unknown>)[p];
  }
  return cur;
}

/** setIn returns a copy of v with the value at path replaced (objects are created on the way). */
export function setIn(v: unknown, path: (string | number)[], next: unknown): unknown {
  if (path.length === 0) return next;
  const [head, ...rest] = path as [string | number, ...(string | number)[]];
  if (Array.isArray(v)) {
    const copy = [...v];
    copy[head as number] = setIn(copy[head as number], rest, next);
    return copy;
  }
  const base = isObj(v) ? v : {};
  return { ...base, [head]: setIn(base[head], rest, next) };
}

// --- text diff --------------------------------------------------------------------------------

export type DiffLine = { kind: "same" | "add" | "del"; text: string };

/**
 * lineDiff compares two texts line by line (LCS over the part that differs once the common
 * head and tail are trimmed). Very large differing middles fall back to delete-all/add-all.
 */
export function lineDiff(before: string, after: string): DiffLine[] {
  const a = before.split("\n");
  const b = after.split("\n");
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) head++;
  let tail = 0;
  while (tail < a.length - head && tail < b.length - head && a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail++;
  const ma = a.slice(head, a.length - tail);
  const mb = b.slice(head, b.length - tail);
  const out: DiffLine[] = a.slice(0, head).map((text) => ({ kind: "same", text }));
  if (ma.length * mb.length > 4_000_000) {
    out.push(...ma.map((text): DiffLine => ({ kind: "del", text })), ...mb.map((text): DiffLine => ({ kind: "add", text })));
  } else {
    const w = mb.length + 1;
    const dp = new Uint32Array((ma.length + 1) * w);
    for (let i = ma.length - 1; i >= 0; i--) {
      for (let j = mb.length - 1; j >= 0; j--) {
        dp[i * w + j] = ma[i] === mb[j] ? (dp[(i + 1) * w + j + 1] ?? 0) + 1 : Math.max(dp[(i + 1) * w + j] ?? 0, dp[i * w + j + 1] ?? 0);
      }
    }
    let i = 0;
    let j = 0;
    while (i < ma.length && j < mb.length) {
      if (ma[i] === mb[j]) {
        out.push({ kind: "same", text: ma[i] as string });
        i++;
        j++;
      } else if ((dp[(i + 1) * w + j] ?? 0) >= (dp[i * w + j + 1] ?? 0)) {
        out.push({ kind: "del", text: ma[i++] as string });
      } else {
        out.push({ kind: "add", text: mb[j++] as string });
      }
    }
    while (i < ma.length) out.push({ kind: "del", text: ma[i++] as string });
    while (j < mb.length) out.push({ kind: "add", text: mb[j++] as string });
  }
  out.push(...a.slice(a.length - tail).map((text): DiffLine => ({ kind: "same", text })));
  return out;
}
