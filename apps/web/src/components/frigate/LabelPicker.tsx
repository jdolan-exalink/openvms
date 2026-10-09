import { Plus } from "lucide-react";
import { Icon } from "../Icon";
import { useState } from "react";
import { Button, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import { labelDisplay, labelEmoji, pickerLabels } from "@/lib/labelEmoji";

/**
 * LabelPicker chooses object labels from an emoji grid. Each tile is a native checkbox named
 * after the Spanish label (so it is reachable and announced by keyboard and screen readers);
 * the tooltip shows on hover and on keyboard focus. Labels outside the grid (custom models)
 * can be typed in and then show up as tiles too.
 */
export function LabelPicker({
  label, hint, value, onChange, extraOptions = [], disabled,
}: {
  label: string;
  hint?: string;
  value: string[];
  onChange: (next: string[]) => void;
  extraOptions?: string[];
  disabled?: boolean;
}) {
  const [custom, setCustom] = useState("");
  const options = pickerLabels(extraOptions, value);
  const toggle = (l: string, on: boolean) => onChange(on ? [...value, l] : value.filter((x) => x !== l));
  const addCustom = () => {
    const l = custom.trim().toLowerCase().replace(/\s+/g, "_");
    if (l && !value.includes(l)) onChange([...value, l]);
    setCustom("");
  };
  return (
    <fieldset className="flex flex-col gap-2 text-sm">
      <legend className="font-medium">{label}</legend>
      {hint && <p className="text-xs text-muted">{hint}</p>}
      <div className="flex flex-wrap gap-2">
        {options.map((l) => {
          const on = value.includes(l);
          const name = labelDisplay(l);
          return (
            <label
              key={l}
              className={cn(
                "group relative flex size-12 cursor-pointer items-center justify-center rounded-m3-md text-2xl select-none",
                on ? "bg-primary-container" : "bg-surface-2 opacity-70 hover:opacity-100",
                disabled && "cursor-not-allowed opacity-50",
                "focus-within:outline-2 focus-within:outline-primary",
              )}
            >
              <input type="checkbox" className="peer sr-only" aria-label={name} checked={on} disabled={disabled} onChange={(e) => toggle(l, e.target.checked)} />
              <span aria-hidden>{labelEmoji(l)}</span>
              <span aria-hidden className={cn("absolute top-0.5 right-0.5 flex size-4 items-center justify-center rounded-full text-[10px] leading-none", on ? "bg-primary text-on-primary" : "bg-surface-3 text-transparent")}>
                ✓
              </span>
              <span role="tooltip" aria-hidden className="pointer-events-none absolute -top-8 left-1/2 z-10 hidden -translate-x-1/2 rounded-m3-sm bg-surface-3 px-2 py-1 text-xs whitespace-nowrap text-on-surface shadow group-focus-within:block group-hover:block">
                {name}
              </span>
            </label>
          );
        })}
      </div>
      {!disabled && (
        <div className="flex flex-wrap items-end gap-2">
          <label className="flex min-w-40 flex-1 flex-col gap-1">
            <span className="text-xs text-muted">Otra etiqueta</span>
            <TextInput value={custom} onChange={(e) => setCustom(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); addCustom(); } }} />
          </label>
          <Button disabled={!custom.trim()} onClick={addCustom}>
            <Icon icon={Plus} size="xs" /> Añadir etiqueta
          </Button>
        </div>
      )}
    </fieldset>
  );
}
