import type { Schemas } from "@/api/client";
import { parseCoordinates, serializeCoordinates } from "@/lib/zoneGeometry";
import type { ZoneEditorValue } from "@/components/zones/zoneDraft";

export type PersistedZone = Schemas["CameraZone"];
export type ZoneInput = Schemas["CameraZoneInput"];
export type ZoneSyncStatus = NonNullable<PersistedZone["sync_status"]>;

/** Sync state by persisted zone ID, as shown next to each OpenVMS-owned zone. */
export type ZoneSyncStates = Record<string, { status: ZoneSyncStatus; error?: string }>;

/** Frigate keeps OpenVMS-owned zones under this prefix; the worker adds it, never the editor. */
const OVMS_PREFIX = "ovms_";

type ZoneMap = Record<string, Record<string, unknown>>;

/** True while at least one zone still waits for the sync worker, so the list should be polled. */
export const hasPendingZoneSync = (zones: PersistedZone[] | undefined) => !!zones?.some((z) => z.sync_status === "pending");

export function zoneSyncStates(zones: PersistedZone[]): ZoneSyncStates {
  const out: ZoneSyncStates = {};
  for (const z of zones) if (z.sync_status) out[z.id] = { status: z.sync_status, error: z.sync_error };
  return out;
}

/** Persisted zones become editor zones (keyed by name) plus the identity metadata of each. */
export function persistedToEditorValue(zones: PersistedZone[]): Pick<ZoneEditorValue, "zones" | "managedZones"> {
  const out: ZoneMap = {};
  const managedZones: NonNullable<ZoneEditorValue["managedZones"]> = {};
  for (const z of zones) {
    out[z.name] = { ...z.settings, coordinates: serializeCoordinates(z.points) };
    managedZones[z.name] = { id: z.id };
  }
  return { zones: out, managedZones };
}

/**
 * Splits the zones of the Frigate config: OpenVMS-owned keys are controlled by the persisted
 * API and must reach the Frigate patch untouched (`preserved`), everything else is editable.
 */
export function splitFrigateZones(zones: ZoneMap, reservedNames: ReadonlySet<string> = new Set()): { foreign: ZoneMap; preserved: ZoneMap } {
  const foreign: ZoneMap = {};
  const preserved: ZoneMap = {};
  // A foreign zone that shares a name with a persisted one cannot be shown beside it; it is kept as is.
  for (const [name, cfg] of Object.entries(zones)) (name.startsWith(OVMS_PREFIX) || reservedNames.has(name) ? preserved : foreign)[name] = cfg;
  return { foreign, preserved };
}

export interface ZoneChangePlan {
  create: ZoneInput[];
  update: { id: string; input: ZoneInput }[];
  remove: string[];
}

const round3 = (n: number) => Math.round(n * 1000) / 1000;
const samePoints = (a: ZoneInput["points"], b: ZoneInput["points"]) =>
  a.length === b.length && a.every((p, i) => round3(p.x) === round3(b[i]!.x) && round3(p.y) === round3(b[i]!.y));
const sameSettings = (a: unknown, b: unknown) => JSON.stringify(sortKeys(a)) === JSON.stringify(sortKeys(b));

function sortKeys(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(sortKeys);
  if (v && typeof v === "object") return Object.fromEntries(Object.entries(v).sort(([a], [b]) => a.localeCompare(b)).map(([k, x]) => [k, sortKeys(x)]));
  return v;
}

/** Diffs the editor result against the persisted zones; unchanged zones produce no request. */
export function planManagedZoneChanges(persisted: PersistedZone[], next: Pick<ZoneEditorValue, "zones" | "managedZones">): ZoneChangePlan {
  const plan: ZoneChangePlan = { create: [], update: [], remove: [] };
  const kept = new Set<string>();
  for (const [name, meta] of Object.entries(next.managedZones ?? {})) {
    const { coordinates, ...settings } = next.zones[name] ?? {};
    const input: ZoneInput = { name, points: parseCoordinates(coordinates), settings };
    const before = meta.id ? persisted.find((z) => z.id === meta.id) : undefined;
    if (!before) {
      plan.create.push(input);
      continue;
    }
    kept.add(before.id);
    if (before.name !== name || !samePoints(before.points, input.points) || !sameSettings(before.settings, settings)) plan.update.push({ id: before.id, input });
  }
  plan.remove = persisted.filter((z) => !kept.has(z.id)).map((z) => z.id);
  return plan;
}
