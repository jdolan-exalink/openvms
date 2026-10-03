import { toLocalInput } from "@/lib/format";

/** The Events filter form and its URL representation (applied filters live in the query string). */
export type EventsForm = {
  server: string;
  camera: string;
  label: string;
  severity: "" | "alert" | "detection";
  plate: string;
  from: string;
  to: string;
  pending: boolean;
  hasSnapshot: boolean;
  hasPreview: boolean;
  vehicleType: string;
  vehicleColor: string;
};

export const emptyEventsForm: EventsForm = {
  server: "",
  camera: "",
  label: "",
  severity: "",
  plate: "",
  from: "",
  to: "",
  pending: false,
  hasSnapshot: false,
  hasPreview: false,
  vehicleType: "",
  vehicleColor: "",
};

/** todayEventsRange is the local calendar day, 00:00 through 23:59, in datetime-local form. */
export function todayEventsRange(now = new Date()): { from: string; to: string } {
  const start = new Date(now);
  start.setHours(0, 0, 0, 0);
  const end = new Date(now);
  end.setHours(23, 59, 0, 0);
  return { from: toLocalInput(start), to: toLocalInput(end) };
}

/** EventsSearch is the validated query string of /events; absent keys mean "no filter". */
export type EventsSearch = {
  server?: string;
  camera?: string;
  label?: string;
  severity?: "alert" | "detection";
  plate?: string;
  from?: string;
  to?: string;
  pending?: true;
  snapshot?: true;
  preview?: true;
  vtype?: string;
  vcolor?: string;
};

// TanStack Router JSON-parses search values, so "123456" reaches us as a number and "true" as a
// boolean; accept both shapes and drop anything else.
const str = (v: unknown): string | undefined => {
  const s = typeof v === "number" ? String(v) : v;
  return typeof s === "string" && s !== "" ? s : undefined;
};
const flag = (v: unknown): true | undefined => (v === true || v === "true" ? true : undefined);

export function parseEventsSearch(s: Record<string, unknown>): EventsSearch {
  const out: EventsSearch = {
    server: str(s.server),
    camera: str(s.camera),
    label: str(s.label),
    severity: s.severity === "alert" || s.severity === "detection" ? s.severity : undefined,
    plate: str(s.plate),
    from: str(s.from),
    to: str(s.to),
    pending: flag(s.pending),
    snapshot: flag(s.snapshot),
    preview: flag(s.preview),
    vtype: str(s.vtype),
    vcolor: str(s.vcolor),
  };
  // Undefined keys would serialize as noise in the URL.
  return Object.fromEntries(Object.entries(out).filter(([, v]) => v !== undefined)) as EventsSearch;
}

export function searchToForm(s: EventsSearch, now = new Date()): EventsForm {
  const today = todayEventsRange(now);
  return {
    server: s.server ?? "",
    camera: s.camera ?? "",
    label: s.label ?? "",
    severity: s.severity ?? "",
    plate: s.plate ?? "",
    from: s.from ?? today.from,
    to: s.to ?? today.to,
    pending: !!s.pending,
    hasSnapshot: !!s.snapshot,
    hasPreview: !!s.preview,
    vehicleType: s.vtype ?? "",
    vehicleColor: s.vcolor ?? "",
  };
}

export function formToSearch(f: EventsForm, now = new Date()): EventsSearch {
  const today = todayEventsRange(now);
  return parseEventsSearch({
    server: f.server,
    camera: f.camera,
    label: f.label,
    severity: f.severity,
    plate: f.plate.trim(),
    from: f.from === today.from ? "" : f.from,
    to: f.to === today.to ? "" : f.to,
    pending: f.pending,
    snapshot: f.hasSnapshot,
    preview: f.hasPreview,
    vtype: f.vehicleType,
    vcolor: f.vehicleColor,
  });
}
