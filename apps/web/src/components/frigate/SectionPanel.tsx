import type { Schemas } from "@/api/client";
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
};

/** SectionPanel renders one camera section: the generic schema-driven form. */
export function SectionPanel({ section, schema, value, onChange, ctx }: SectionPanelProps) {
  return <SchemaForm name={section} schema={schema} value={value} onChange={onChange} ctx={ctx} />;
}
