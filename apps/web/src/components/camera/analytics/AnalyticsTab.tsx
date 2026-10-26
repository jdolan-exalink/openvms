import { LprSettings } from "./LprSettings";
import { FaceSettings } from "./FaceSettings";
import { ModuleCard } from "./ModuleCard";
import { type AnalyticsModule, draftEnabled, enabledPatch, minAreaPatch } from "./draft";

type Props = {
  cameraId: string;
  /** Modules of the normalized configuration; undefined while it loads. */
  modules: AnalyticsModule[] | undefined;
  /** Unsaved draft of the shared change context. */
  patch: Record<string, unknown>;
  setPatch: (partial: Record<string, unknown>) => void;
  readOnly?: boolean;
};

const FACE_IDS = new Set(["face_detection", "face_recognition"]);

/**
 * The Analíticas tab: every analytic is a module with a switch, and its detail only shows once
 * the module is on. Edits go through the shared change context (review before applying).
 */
export function AnalyticsTab({ cameraId, modules, patch, setPatch, readOnly }: Props) {
  if (!modules) return <p className="text-sm text-muted">Cargando analíticas…</p>;
  const byId = (id: string) => modules.find((m) => m.id === id);
  const detection = byId("face_detection");
  const recognition = byId("face_recognition");
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted">Las analíticas se manejan como módulos: activá un módulo para ver su configuración.</p>
      {modules.map((m) => {
        if (FACE_IDS.has(m.id)) {
          // The two facial modules share one group, rendered once at the first of them.
          return m.id === "face_detection" && detection && recognition ? (
            <FaceSettings key="face" detection={detection} recognition={recognition} patch={patch} setPatch={setPatch} readOnly={readOnly} />
          ) : null;
        }
        return (
          <ModuleCard key={m.id} module={m} enabled={draftEnabled(patch, m)} onToggle={(v) => setPatch(enabledPatch(m.id, v))} readOnly={readOnly}>
            {m.id === "lpr" && <LprSettings cameraId={cameraId} module={m} patch={patch} onMinArea={(v) => setPatch(minAreaPatch(m.id, v))} readOnly={readOnly} />}
          </ModuleCard>
        );
      })}
    </div>
  );
}
