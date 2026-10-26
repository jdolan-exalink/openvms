import type { ReactNode } from "react";
import type { Schemas } from "@/api/client";
import { type FormCtx } from "@/components/frigate/SchemaForm";
import { SectionPanel } from "@/components/frigate/SectionPanel";
import { Select, Switch, TextInput } from "@/components/ui";
import { getIn, type JSchema, schemaAt, sectionLabel, setIn } from "@/lib/frigateSchema";

export type GeneralFrigate = {
  root: JSchema;
  camProps: Record<string, JSchema>;
  config: Record<string, unknown>;
  doc: Schemas["FrigateCameraConfigDoc"];
  ctx: FormCtx;
  valueOf: (section: string) => unknown;
  setSection: (section: string, value: unknown) => void;
};

/** Sections the General tab owns; the sidebar does not list them again. */
export const GENERAL_SECTIONS = ["enabled", "audio", "lpr", "face_recognition", "timestamp_style", "audio_transcription", "best_image_timeout", "ffmpeg"];

const POSITIONS: Record<string, string> = { tl: "Arriba izquierda", tr: "Arriba derecha", bl: "Abajo izquierda", br: "Abajo derecha" };

/** A section exists for this Frigate version when its schema or the effective config knows it. */
export const knowsSection = (f: Pick<GeneralFrigate, "camProps" | "config">, s: string) => s in f.camProps || f.config[s] !== undefined;

/** Effective `enabled` of a toggle section: the draft value, else the schema default. */
export function sectionEnabled(f: GeneralFrigate, s: string): boolean {
  const v = f.valueOf(s);
  const raw = s === "enabled" ? v : getIn(v, ["enabled"]);
  if (typeof raw === "boolean") return raw;
  const def = s === "enabled" ? f.camProps[s]?.default : schemaAt(f.root, f.camProps[s] ?? {}, ["enabled"])?.default;
  return def === true || (s === "enabled" && def === undefined);
}

function More({ f, section, children }: { f: GeneralFrigate; section: string; children?: ReactNode }) {
  return (
    <details className="rounded-m3-lg bg-surface-1">
      <summary className="min-h-11 cursor-pointer px-3 py-2.5 text-xs font-bold">Más ajustes de {sectionLabel(section)}</summary>
      <div className="border-t border-outline-variant p-3">
        {children}
        <SectionPanel
          section={section}
          schema={f.camProps[section]}
          value={f.valueOf(section)}
          onChange={(v) => f.setSection(section, v)}
          ctx={f.ctx}
          camera={f.doc}
          config={f.config}
        />
      </div>
    </details>
  );
}

function ToggleRow({ f, section, label, hint, disabled, more = true }: { f: GeneralFrigate; section: string; label: string; hint?: string; disabled?: boolean; more?: boolean }) {
  if (!knowsSection(f, section)) return null;
  return (
    <div className="flex flex-col gap-1.5 rounded-m3-lg bg-surface-2 p-3">
      <Switch
        label={label}
        checked={sectionEnabled(f, section)}
        disabled={f.ctx.readOnly || disabled}
        onChange={(next) => f.setSection(section, setIn(f.valueOf(section), ["enabled"], next))}
      />
      {hint && <p className="text-xs text-muted">{hint}</p>}
      {more && <More f={f} section={section} />}
    </div>
  );
}

function Timestamp({ f }: { f: GeneralFrigate }) {
  const s = "timestamp_style";
  if (!knowsSection(f, s)) return null;
  const node = (k: string) => schemaAt(f.root, f.camProps[s] ?? {}, [k]);
  const val = (k: string) => getIn(f.valueOf(s), [k]);
  const set = (k: string, v: unknown) => f.setSection(s, setIn(f.valueOf(s), [k], v));
  const positions = node("position")?.enum?.map(String) ?? [];
  const position = typeof val("position") === "string" ? (val("position") as string) : "";
  const format = typeof val("format") === "string" ? (val("format") as string) : "";
  return (
    <div className="flex flex-col gap-2 rounded-m3-lg bg-surface-2 p-3">
      <span className="text-sm font-medium">Marca de tiempo</span>
      <p className="text-xs text-muted">Frigate dibuja siempre la marca; aquí se elige dónde y con qué formato.</p>
      <div className="grid gap-3 sm:grid-cols-2">
        {positions.length > 0 && (
          <label className="flex flex-col gap-1 text-sm">
            <span className="font-medium">Posición</span>
            <Select disabled={f.ctx.readOnly} value={position} onChange={(e) => set("position", e.target.value)}>
              {(position === "" || !positions.includes(position)) && <option value={position}>{position === "" ? "(por defecto)" : `${position} (actual)`}</option>}
              {positions.map((p) => <option key={p} value={p}>{POSITIONS[p] ?? p}</option>)}
            </Select>
          </label>
        )}
        {node("format") && (
          <label className="flex flex-col gap-1 text-sm">
            <span className="font-medium">Formato</span>
            <TextInput disabled={f.ctx.readOnly} value={format} placeholder={typeof node("format")?.default === "string" ? (node("format")!.default as string) : undefined} onChange={(e) => set("format", e.target.value)} />
          </label>
        )}
      </div>
      <More f={f} section={s} />
    </div>
  );
}

function BestImage({ f }: { f: GeneralFrigate }) {
  const s = "best_image_timeout";
  const present = knowsSection(f, s);
  const v = f.valueOf(s);
  const node = f.camProps[s] ? schemaAt(f.root, f.camProps[s], []) : undefined;
  return (
    <label className="flex flex-col gap-1 rounded-m3-lg bg-surface-2 p-3 text-sm">
      <span className="font-medium">Tiempo de mejor imagen <span className="font-normal text-muted">(segundos)</span></span>
      <TextInput
        type="number"
        min={node?.minimum ?? 0}
        disabled={!present || f.ctx.readOnly}
        placeholder={node?.default !== undefined ? String(node.default) : undefined}
        value={typeof v === "number" ? v : ""}
        onChange={(e) => f.setSection(s, e.target.value === "" ? undefined : Number(e.target.value))}
      />
      <span className="text-xs text-muted">
        {present ? "Cuánto espera Frigate para elegir la mejor imagen de un objeto." : "Esta versión de Frigate no la expone."}
      </span>
    </label>
  );
}

/** Frigate-backed General fields; every control edits the shared section drafts. */
export function FrigateGeneralFields({ f }: { f: GeneralFrigate }) {
  const audioOn = sectionEnabled(f, "audio") && knowsSection(f, "audio");
  return (
    <div className="flex flex-col gap-4">
      {knowsSection(f, "ffmpeg") && (
        <div className="rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4">
          <SectionPanel
            section="ffmpeg"
            schema={f.camProps.ffmpeg}
            value={f.valueOf("ffmpeg")}
            onChange={(v) => f.setSection("ffmpeg", v)}
            ctx={f.ctx}
            camera={f.doc}
            config={f.config}
          />
        </div>
      )}
      <div className="grid gap-3 md:grid-cols-2">
        <ToggleRow f={f} section="audio" label="Audio" />
        <ToggleRow
          f={f}
          section="audio_transcription"
          label="Transcripción de audio"
          disabled={!audioOn}
          hint={audioOn ? undefined : "Requiere activar el audio."}
        />
        <ToggleRow f={f} section="lpr" label="LPR" hint="Lectura de matrículas con el motor de Frigate (refleja el tipo «LPR · motor de Frigate»)." />
        <ToggleRow f={f} section="face_recognition" label="Reconocimiento facial" />
        <Timestamp f={f} />
        <BestImage f={f} />
      </div>
    </div>
  );
}
