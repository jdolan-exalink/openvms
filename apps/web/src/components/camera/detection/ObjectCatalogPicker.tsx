import type { Schemas } from "@/api/client";

export type ObjectCatalog = Schemas["ObjectCatalog"];
type Category = ObjectCatalog["categories"][number];
type CatalogObject = Category["objects"][number];

const UNSUPPORTED_HINT = "Tu modelo no lo soporta";

/** An object can be picked unless the model is known not to support it (null means unknown). */
export const isSelectable = (o: CatalogObject) => o.supported !== false;

const categoryToggleLabel = (c: Category) => (c.id === "vehicle" ? "Activar todos los vehículos" : `Activar toda la categoría ${c.label}`);

/** Next selection after flipping one object. */
export function toggleObject(selected: string[], id: string): string[] {
  return selected.includes(id) ? selected.filter((s) => s !== id) : [...selected, id];
}

/** Next selection after flipping a whole category: only objects the model may support move. */
export function toggleCategory(selected: string[], c: Category): string[] {
  const ids = c.objects.filter(isSelectable).map((o) => o.id);
  const all = ids.length > 0 && ids.every((id) => selected.includes(id));
  return all ? selected.filter((s) => !ids.includes(s)) : [...selected, ...ids.filter((id) => !selected.includes(id))];
}

/**
 * Pick what to detect from the normalized catalog (never the engine's raw label list): a toggle
 * per category and a checkbox per object. Objects the model cannot honor are disabled with a hint.
 */
export function ObjectCatalogPicker({ catalog, selected, onChange, disabled }: { catalog: ObjectCatalog; selected: string[]; onChange: (next: string[]) => void; disabled?: boolean }) {
  return (
    <div className="grid gap-3 md:grid-cols-2">
      {catalog.categories.map((c) => {
        const ids = c.objects.filter(isSelectable).map((o) => o.id);
        const all = ids.length > 0 && ids.every((id) => selected.includes(id));
        return (
          <fieldset key={c.id} className="flex flex-col gap-2 rounded-m3-lg bg-surface-2 p-3" aria-label={c.label}>
            <legend className="px-1 text-sm font-bold">{c.label}</legend>
            <label className="flex min-h-9 items-center gap-2 text-xs font-semibold">
              <input type="checkbox" checked={all} disabled={disabled || ids.length === 0} onChange={() => onChange(toggleCategory(selected, c))} />
              {categoryToggleLabel(c)}
            </label>
            <div className="flex flex-wrap gap-2">
              {c.objects.map((o) => (
                <label
                  key={o.id}
                  title={o.supported === false ? UNSUPPORTED_HINT : undefined}
                  className="inline-flex min-h-9 items-center gap-2 rounded-m3-lg bg-surface-1 px-3 text-xs aria-disabled:opacity-60"
                  aria-disabled={o.supported === false || undefined}
                >
                  <input type="checkbox" checked={selected.includes(o.id)} disabled={disabled || o.supported === false} onChange={() => onChange(toggleObject(selected, o.id))} />
                  {o.label}
                  {o.supported === false && <span className="text-muted">{UNSUPPORTED_HINT}</span>}
                </label>
              ))}
            </div>
          </fieldset>
        );
      })}
    </div>
  );
}
