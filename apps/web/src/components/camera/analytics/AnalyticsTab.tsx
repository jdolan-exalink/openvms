import { LprSettings } from "./LprSettings";
import { FaceSettings } from "./FaceSettings";
import { Advanced } from "./LprSettings";
import { ModuleCard } from "./ModuleCard";
import { IntegrationsTab, type IntegrationEntry } from "../integrations/IntegrationsTab";
import { type AnalyticsModule, draftEnabled, enabledPatch, enhancementPatch, minAreaPatch } from "./draft";

type Props = {
  cameraId: string;
  /** Integrations of the normalized configuration. */
  integrations?: readonly IntegrationEntry[];
  /** Modules of the normalized configuration; undefined while it loads. */
  modules: AnalyticsModule[] | undefined;
  /** Unsaved draft of the shared change context. */
  patch: Record<string, unknown>;
  setPatch: (partial: Record<string, unknown>) => void;
  readOnly?: boolean;
};

const FACE_IDS = new Set(["face_detection", "face_recognition"]);

/**
 * The Analíticas tab: displays the integrations/services status controls at the top,
 * and every analytic module below with clear origin and switchable settings.
 */
export function AnalyticsTab({ cameraId, integrations, modules, patch, setPatch, readOnly }: Props) {
  if (!modules) return <p className="text-sm text-muted">Cargando analíticas…</p>;
  const byId = (id: string) => modules.find((m) => m.id === id);
  const detection = byId("face_detection");
  const recognition = byId("face_recognition");

  return (
    <div className="flex flex-col gap-6">
      {integrations && integrations.length > 0 && (
        <section aria-label="Estado de integraciones" className="flex flex-col gap-2.5">
          <h3 className="text-sm font-bold text-on-surface">Estado de integraciones y motores</h3>
          <IntegrationsTab integrations={integrations} cameraId={cameraId} readOnly={readOnly} />
        </section>
      )}

      <section aria-label="Módulos de analítica" className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <h3 className="text-sm font-bold text-on-surface">Módulos de analítica</h3>
          <p className="text-xs text-muted">Las analíticas se manejan como módulos: activá un módulo para ver su configuración.</p>
        </div>

        <div className="flex flex-col gap-3">
          {modules.map((m) => {
            if (FACE_IDS.has(m.id)) {
              // The two facial modules share one group, rendered once at the first of them.
              return m.id === "face_detection" && detection && recognition ? (
                <FaceSettings key="face" detection={detection} recognition={recognition} patch={patch} setPatch={setPatch} readOnly={readOnly} />
              ) : null;
            }
            return (
              <ModuleCard key={m.id} module={m} enabled={draftEnabled(patch, m)} onToggle={(v) => setPatch(enabledPatch(m.id, v))} readOnly={readOnly}>
                {m.id === "lpr" && <LprSettings cameraId={cameraId} module={m} patch={patch} onMinArea={(v) => setPatch(minAreaPatch(m.id, v))} onEnhancement={(v) => setPatch(enhancementPatch(m.id, v))} readOnly={readOnly} />}
                {m.id !== "lpr" && <Advanced items={m.advanced} />}
              </ModuleCard>
            );
          })}
        </div>
      </section>
    </div>
  );
}
