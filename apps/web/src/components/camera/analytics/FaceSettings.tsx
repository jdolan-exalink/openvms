import { Field, TextInput } from "@/components/ui";
import { Advanced } from "./LprSettings";
import { ModuleCard } from "./ModuleCard";
import { type AnalyticsModule, draftEnabled, draftMinArea, enabledPatch, minAreaPatch } from "./draft";

type Props = {
  detection: AnalyticsModule;
  recognition: AnalyticsModule;
  patch: Record<string, unknown>;
  setPatch: (partial: Record<string, unknown>) => void;
  readOnly?: boolean;
};

/**
 * Facial analytics shown as two modules. Frigate has no "detect without recognising" mode, so the
 * detection card mirrors recognition and says why it has no switch of its own (the engine's reason).
 */
export function FaceSettings({ detection, recognition, patch, setPatch, readOnly }: Props) {
  return (
    <section aria-label="Reconocimiento / detección facial" className="flex flex-col gap-3">
      <h3 className="text-sm font-bold text-on-surface">Reconocimiento / detección facial</h3>
      <ModuleCard module={detection} enabled={draftEnabled(patch, recognition)} onToggle={(v) => setPatch(enabledPatch(detection.id, v))} readOnly={readOnly}>
        <Advanced items={detection.advanced} />
      </ModuleCard>
      <ModuleCard module={recognition} enabled={draftEnabled(patch, recognition)} onToggle={(v) => setPatch(enabledPatch(recognition.id, v))} readOnly={readOnly}>
        <div className="flex flex-col gap-3">
          {recognition.writable && (
            <Field label="Área mínima del rostro (px²)" hint="Rostros más pequeños no se reconocen.">
              <TextInput
                type="number"
                min={0}
                disabled={readOnly}
                value={draftMinArea(patch, recognition) ?? ""}
                onChange={(e) => e.target.value !== "" && setPatch(minAreaPatch(recognition.id, Math.max(0, Math.round(Number(e.target.value)))))}
              />
            </Field>
          )}
          <Advanced items={recognition.advanced} />
        </div>
      </ModuleCard>
    </section>
  );
}
