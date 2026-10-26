/** Structured editing of the sparse camera patch shared by the profiles screen and the bulk dialog. */
export type Section = "detection" | "recording" | "analytics" | "streams";
export type Tri = "" | "on" | "off";
export type StreamDraft = { resolution: string; fps: string; codec: string; bitrate_kbps: string; gop: string };
export type PatchDraft = {
  detectionEnabled: Tri;
  detectionObjects: string;
  recordingEnabled: Tri;
  lpr: Tri;
  primary: StreamDraft;
  detection: StreamDraft;
  /** The original patch, so fields the form does not edit survive a save. */
  extra: Record<string, unknown>;
};

export const sectionLabel: Record<Section, string> = { detection: "Detección", recording: "Grabación", analytics: "Analíticas", streams: "Video" };
export const sections: Section[] = ["detection", "recording", "analytics", "streams"];
export const codecs = ["h264", "h265", "mjpeg", "mpeg4"];
const numeric = ["fps", "bitrate_kbps", "gop"] as const;
const roles = ["primary", "detection"] as const;

const emptyStream = (): StreamDraft => ({ resolution: "", fps: "", codec: "", bitrate_kbps: "", gop: "" });
export const emptyDraft = (): PatchDraft => ({ detectionEnabled: "", detectionObjects: "", recordingEnabled: "", lpr: "", primary: emptyStream(), detection: emptyStream(), extra: {} });

type Rec = Record<string, unknown>;
const rec = (v: unknown): Rec => (typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Rec) : {});
const tri = (v: unknown): Tri => (v === true ? "on" : v === false ? "off" : "");

export function patchToDraft(patch: Rec): PatchDraft {
  const d = emptyDraft();
  d.extra = structuredClone(patch);
  d.detectionEnabled = tri(rec(patch.detection).enabled);
  const objects = rec(patch.detection).objects;
  d.detectionObjects = Array.isArray(objects) ? objects.join(", ") : "";
  d.recordingEnabled = tri(rec(patch.recording).enabled);
  d.lpr = tri(rec(patch.analytics).lpr);
  for (const r of roles) {
    const s = rec(rec(patch.streams)[r]);
    const res = rec(s.resolution);
    d[r] = {
      resolution: typeof res.width === "number" && typeof res.height === "number" ? `${res.width}x${res.height}` : "",
      codec: typeof s.codec === "string" ? s.codec : "",
      fps: typeof s.fps === "number" ? String(s.fps) : "",
      bitrate_kbps: typeof s.bitrate_kbps === "number" ? String(s.bitrate_kbps) : "",
      gop: typeof s.gop === "number" ? String(s.gop) : "",
    };
  }
  return d;
}

const RES = /^(\d{2,5})\s*[x×]\s*(\d{2,5})$/i;

/** Field-level problems of the draft (Spanish), by form field. */
export function draftErrors(d: PatchDraft): Record<string, string> {
  const out: Record<string, string> = {};
  for (const r of roles) {
    const s = d[r];
    if (s.resolution.trim() && !RES.test(s.resolution.trim())) out[`${r}.resolution`] = "Usá el formato 1920x1080.";
    for (const n of numeric) if (s[n].trim() && !/^[1-9]\d*$/.test(s[n].trim())) out[`${r}.${n}`] = "Debe ser un número entero mayor a 0.";
  }
  return out;
}

const triValue = (t: Tri) => (t === "on" ? true : t === "off" ? false : undefined);

/** Removes the fields the form edits, so a blanked field leaves the patch instead of lingering. */
function stripManaged(p: Rec): Rec {
  const drop = (o: Rec, k: string) => {
    delete o[k];
    return o;
  };
  if (p.detection) { const d = rec(p.detection); drop(d, "enabled"); drop(d, "objects"); }
  if (p.recording) drop(rec(p.recording), "enabled");
  if (p.analytics) drop(rec(p.analytics), "lpr");
  for (const r of roles) {
    const s = rec(rec(p.streams)[r]);
    for (const k of ["resolution", "codec", ...numeric]) delete s[k];
  }
  return prune(p);
}

function prune(o: Rec): Rec {
  for (const k of Object.keys(o)) {
    const v = o[k];
    if (typeof v === "object" && v !== null && !Array.isArray(v) && Object.keys(prune(v as Rec)).length === 0) delete o[k];
  }
  return o;
}

/** Builds the sparse patch; `only` keeps a single section (bulk) and drops everything else. */
export function draftToPatch(d: PatchDraft, only?: Section): Rec {
  const out: Rec = only ? {} : stripManaged(structuredClone(d.extra));
  const put = (path: string[], v: unknown) => {
    if (v === undefined) return;
    let cur = out;
    for (const k of path.slice(0, -1)) cur = cur[k] = rec(cur[k]);
    cur[path[path.length - 1]!] = v;
  };
  const want = (s: Section) => !only || only === s;
  if (want("detection")) {
    put(["detection", "enabled"], triValue(d.detectionEnabled));
    const labels = d.detectionObjects.split(",").map((x) => x.trim()).filter(Boolean);
    if (labels.length) put(["detection", "objects"], labels);
  }
  if (want("recording")) put(["recording", "enabled"], triValue(d.recordingEnabled));
  if (want("analytics")) put(["analytics", "lpr"], triValue(d.lpr));
  if (want("streams")) {
    for (const r of roles) {
      const s = d[r];
      const m = RES.exec(s.resolution.trim());
      if (m) put(["streams", r, "resolution"], { width: Number(m[1]), height: Number(m[2]) });
      if (s.codec) put(["streams", r, "codec"], s.codec);
      for (const n of numeric) if (/^[1-9]\d*$/.test(s[n].trim())) put(["streams", r, n], Number(s[n]));
    }
  }
  return out;
}

/** Number of leaf fields set in a patch (an array counts as one). */
export function leafCount(v: unknown): number {
  const o = rec(v);
  return Object.values(o).reduce<number>((n, x) => n + (typeof x === "object" && x !== null && !Array.isArray(x) ? leafCount(x) : 1), 0);
}

/** Section names present in a patch. */
export function sectionsOf(patch: Rec): Section[] {
  return sections.filter((s) => Object.keys(rec(patch[s])).length > 0);
}

const triText = (v: unknown) => (v === true ? "activada" : v === false ? "desactivada" : "");
/** One-line Spanish summary of a profile patch, e.g. "Detección activada · Grabación activada · Video: 2 valores". */
export function summarizePatch(patch: Rec): string {
  const parts: string[] = [];
  const det = rec(patch.detection);
  if (det.enabled !== undefined) parts.push(`Detección ${triText(det.enabled)}`);
  if (Array.isArray(det.objects)) parts.push(`Objetos: ${det.objects.join(", ")}`);
  if (rec(patch.recording).enabled !== undefined) parts.push(`Grabación ${triText(rec(patch.recording).enabled)}`);
  if (rec(patch.analytics).lpr !== undefined) parts.push(`LPR ${triText(rec(patch.analytics).lpr)}`);
  const streams = leafCount(patch.streams);
  if (streams) parts.push(`Video: ${streams} ${streams === 1 ? "valor" : "valores"}`);
  return parts.join(" · ") || "Sin valores";
}
