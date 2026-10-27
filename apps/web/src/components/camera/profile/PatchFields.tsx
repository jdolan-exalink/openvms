import { Field, Select, TextInput } from "@/components/ui";
import { codecs, draftErrors, type PatchDraft, type Section, type StreamDraft, type Tri } from "./patchForm";

const triOptions = (on: string, off: string) => (
  <>
    <option value="">No cambiar</option>
    <option value="on">{on}</option>
    <option value="off">{off}</option>
  </>
);

type Props = { draft: PatchDraft; onChange: (next: PatchDraft) => void; only?: Section; disabled?: boolean; serverErrors?: Record<string, string> };

/** Compact editor of the patch fields profiles and bulk changes may hold; blank means "do not touch". */
export function PatchFields({ draft, onChange, only, disabled, serverErrors }: Props) {
  const errors = { ...draftErrors(draft), ...serverErrors };
  const show = (s: Section) => !only || only === s;
  const set = (p: Partial<PatchDraft>) => onChange({ ...draft, ...p });
  const stream = (role: "primary" | "detection", title: string) => {
    const s = draft[role];
    const up = (p: Partial<StreamDraft>) => set({ [role]: { ...s, ...p } } as Partial<PatchDraft>);
    return (
      <fieldset key={role} className="grid gap-3 sm:grid-cols-2" disabled={disabled}>
        <legend className="mb-1 text-sm font-semibold">{title}</legend>
        <Field label={`${title}: resolución`} hint="Ej. 1920x1080" error={errors[`${role}.resolution`]}>
          <TextInput value={s.resolution} onChange={(e) => up({ resolution: e.target.value })} aria-invalid={!!errors[`${role}.resolution`]} />
        </Field>
        <Field label={`${title}: FPS`} error={errors[`${role}.fps`]}>
          <TextInput inputMode="numeric" value={s.fps} onChange={(e) => up({ fps: e.target.value })} aria-invalid={!!errors[`${role}.fps`]} />
        </Field>
        <Field label={`${title}: códec`}>
          <Select value={s.codec} onChange={(e) => up({ codec: e.target.value })}>
            <option value="">No cambiar</option>
            {codecs.map((c) => <option key={c} value={c}>{c}</option>)}
          </Select>
        </Field>
        <Field label={`${title}: bitrate (kbps)`} error={errors[`${role}.bitrate_kbps`]}>
          <TextInput inputMode="numeric" value={s.bitrate_kbps} onChange={(e) => up({ bitrate_kbps: e.target.value })} aria-invalid={!!errors[`${role}.bitrate_kbps`]} />
        </Field>
        <Field label={`${title}: keyframe (GOP)`} error={errors[`${role}.gop`]}>
          <TextInput inputMode="numeric" value={s.gop} onChange={(e) => up({ gop: e.target.value })} aria-invalid={!!errors[`${role}.gop`]} />
        </Field>
      </fieldset>
    );
  };
  return (
    <div className="flex flex-col gap-4">
      {show("detection") && (
        <fieldset className="grid gap-3 sm:grid-cols-2" disabled={disabled}>
          <legend className="mb-1 text-sm font-semibold">Detección</legend>
          <Field label="Detección de objetos">
            <Select value={draft.detectionEnabled} onChange={(e) => set({ detectionEnabled: e.target.value as Tri })}>{triOptions("Activada", "Desactivada")}</Select>
          </Field>
          <Field label="Objetos a detectar" hint="Etiquetas del motor separadas por coma, ej. person, car">
            <TextInput value={draft.detectionObjects} onChange={(e) => set({ detectionObjects: e.target.value })} />
          </Field>
        </fieldset>
      )}
      {show("recording") && (
        <fieldset disabled={disabled}>
          <legend className="mb-1 text-sm font-semibold">Grabación</legend>
          <Field label="Grabación">
            <Select value={draft.recordingEnabled} onChange={(e) => set({ recordingEnabled: e.target.value as Tri })}>{triOptions("Activada", "Desactivada")}</Select>
          </Field>
        </fieldset>
      )}
      {show("analytics") && (
        <fieldset disabled={disabled}>
          <legend className="mb-1 text-sm font-semibold">Analíticas</legend>
          <Field label="Lectura de patentes (LPR)">
            <Select value={draft.lpr} onChange={(e) => set({ lpr: e.target.value as Tri })}>{triOptions("Activada", "Desactivada")}</Select>
          </Field>
        </fieldset>
      )}
      {show("streams") && (
        <>
          {stream("primary", "Stream principal")}
          {stream("detection", "Stream de detección")}
        </>
      )}
    </div>
  );
}
