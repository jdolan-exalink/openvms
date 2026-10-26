import type { Schemas } from "@/api/client";
import { useOptionalCameraChange } from "@/components/camera/CameraChangeProvider";

export type Provenance = Schemas["CameraConfigProvenance"];
export type ProvenanceMap = Record<string, Provenance> | undefined;

/** Spanish label of each source (PRD §28). */
export const provenanceLabel: Record<Provenance["source"], string> = {
  engine: "Heredado",
  profile: "Definido por perfil",
  override: "Modificado localmente",
};

const rank: Record<Provenance["source"], number> = { engine: 0, profile: 1, override: 2 };

/**
 * Source of a control that may cover several patch paths: a local override beats the profile, which
 * beats the engine value. Undefined when none of the paths has a known origin.
 */
export function pickProvenance(map: ProvenanceMap, paths: string | string[]): Provenance | undefined {
  let best: Provenance | undefined;
  for (const p of Array.isArray(paths) ? paths : [paths]) {
    const v = map?.[p];
    if (v && (!best || rank[v.source] > rank[best.source])) best = v;
  }
  return best;
}

/** Provenance of a patch path of the camera in the surrounding CameraChangeProvider (none outside it). */
export function useProvenance(paths: string | string[]): Provenance | undefined {
  const change = useOptionalCameraChange();
  return pickProvenance(change?.config.data?.provenance, paths);
}
