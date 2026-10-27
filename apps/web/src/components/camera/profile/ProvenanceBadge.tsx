import { Pill } from "@/components/ui";
import { provenanceLabel, type Provenance, useProvenance } from "@/lib/useProvenance";

const tone = { engine: "neutral", profile: "info", override: "warn" } as const;

/** The chip itself, for a known provenance. */
export function ProvenanceChip({ provenance }: { provenance: Provenance }) {
  const title = provenance.source === "profile" && provenance.profile_name ? `Perfil: ${provenance.profile_name}` : provenanceLabel[provenance.source];
  return (
    <span title={title} data-testid="provenance-badge">
      <Pill size="sm" tone={tone[provenance.source]}>{provenanceLabel[provenance.source]}</Pill>
    </span>
  );
}

/** Small chip telling where the value of a control comes from; renders nothing when unknown. */
export function ProvenanceBadge({ path }: { path: string | string[] }) {
  const p = useProvenance(path);
  return p ? <ProvenanceChip provenance={p} /> : null;
}
