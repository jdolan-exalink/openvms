/** The Events filter form and its URL representation (applied filters live in the query string). */
export type EventsForm = {
  site: string;
  camera: string;
  cameraGroup: string;
  label: string;
  zone: string;
  subLabel: string;
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
  site: "",
  camera: "",
  cameraGroup: "",
  label: "",
  zone: "",
  subLabel: "",
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

/** EventsSearch is the validated query string of /events; absent keys mean "no filter". */
export type EventsSearch = {
  site?: string;
  camera?: string;
  group?: string;
  label?: string;
  zone?: string;
  sub?: string;
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
    site: str(s.site),
    camera: str(s.camera),
    group: str(s.group),
    label: str(s.label),
    zone: str(s.zone),
    sub: str(s.sub),
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

export function searchToForm(s: EventsSearch): EventsForm {
  return {
    site: s.site ?? "",
    camera: s.camera ?? "",
    cameraGroup: s.group ?? "",
    label: s.label ?? "",
    zone: s.zone ?? "",
    subLabel: s.sub ?? "",
    severity: s.severity ?? "",
    plate: s.plate ?? "",
    from: s.from ?? "",
    to: s.to ?? "",
    pending: !!s.pending,
    hasSnapshot: !!s.snapshot,
    hasPreview: !!s.preview,
    vehicleType: s.vtype ?? "",
    vehicleColor: s.vcolor ?? "",
  };
}

export function formToSearch(f: EventsForm): EventsSearch {
  return parseEventsSearch({
    site: f.site,
    camera: f.camera,
    group: f.cameraGroup,
    label: f.label,
    zone: f.zone.trim(),
    sub: f.subLabel.trim(),
    severity: f.severity,
    plate: f.plate.trim(),
    from: f.from,
    to: f.to,
    pending: f.pending,
    snapshot: f.hasSnapshot,
    preview: f.hasPreview,
    vtype: f.vehicleType,
    vcolor: f.vehicleColor,
  });
}
