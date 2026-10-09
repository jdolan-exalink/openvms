import type { Schemas } from "@/api/client";
import { CURATED, CuratedPanel } from "@/components/frigate/CuratedPanels";
import { type FormCtx, SchemaForm } from "@/components/frigate/SchemaForm";
import type { JSchema } from "@/lib/frigateSchema";

export type SectionPanelProps = {
  section: string;
  schema: JSchema | undefined;
  value: unknown;
  onChange: (v: unknown) => void;
  ctx: FormCtx;
  camera: Schemas["FrigateCameraConfigDoc"];
  config: Record<string, unknown>;
  /** Opens the visual zone editor; the zones panel offers the button only when provided. */
  onEditZones?: () => void;
};

/**
 * SectionPanel renders one camera section. Commonly used sections get a curated panel first;
 * the generic schema-driven form stays available under "Opciones avanzadas" and edits the
 * same value, so both views always agree.
 */
export function SectionPanel({ section, schema, value, onChange, ctx, config, onEditZones }: SectionPanelProps) {
  const generic = <SchemaForm name={section} schema={schema} value={value} onChange={onChange} ctx={ctx} />;
  if (!CURATED.has(section)) return generic;
  const zones = Object.keys((config.zones as Record<string, unknown> | undefined) ?? {});
  return (
    <div className="flex flex-col gap-5">
      <CuratedPanel section={section} schema={schema} value={value} onChange={onChange} ctx={ctx} zones={zones} onEditZones={onEditZones} />
      <details className="rounded-m3-lg bg-surface-2">
        <summary className="min-h-11 cursor-pointer px-4 py-2.5 text-sm font-bold">Opciones avanzadas</summary>
        <div className="border-t border-outline-variant p-4">{generic}</div>
      </details>
    </div>
  );
}
