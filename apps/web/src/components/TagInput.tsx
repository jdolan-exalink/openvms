import { X } from "lucide-react";
import { type KeyboardEvent, useId, useState } from "react";

/**
 * TagInput edits a list of free-text values as removable chips. `suggestions` are offered through
 * a native datalist (not enforced: any typed value can be added). Enter, a typed comma or leaving
 * the field commits the pending text; Backspace on an empty field removes the last chip.
 */
export function TagInput({
  label, hint, value, onChange, suggestions = [],
}: {
  label: string;
  hint?: string;
  value: string[];
  onChange: (next: string[]) => void;
  suggestions?: string[];
}) {
  const id = useId();
  const [text, setText] = useState("");
  const has = (list: string[], v: string) => list.some((x) => x.toLowerCase() === v.toLowerCase());

  const commit = (raw: string[]) => {
    let next = value;
    for (const part of raw) {
      const v = part.trim();
      if (v && !has(next, v)) next = [...next, v];
    }
    if (next !== value) onChange(next);
    setText("");
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      commit([text]);
    } else if (e.key === "Backspace" && text === "" && value.length > 0) {
      onChange(value.slice(0, -1));
    }
  };

  return (
    <div className="flex flex-col gap-1 text-sm">
      <label htmlFor={id} className="font-medium">
        {label}
      </label>
      <div className="flex flex-wrap items-center gap-1.5 rounded border border-line bg-bg px-2 py-1.5 focus-within:outline-2 focus-within:outline-accent">
        {value.map((v) => (
          <span key={v} className="inline-flex items-center gap-1 rounded bg-raised px-2 py-0.5 text-xs">
            {v}
            <button type="button" aria-label={`Quitar ${v}`} onClick={() => onChange(value.filter((x) => x !== v))} className="rounded hover:text-bad focus-visible:outline-2 focus-visible:outline-accent">
              <X className="size-3" aria-hidden />
            </button>
          </span>
        ))}
        <input
          id={id}
          list={`${id}-suggestions`}
          value={text}
          onChange={(e) => {
            const parts = e.target.value.split(",");
            if (parts.length > 1) commit(parts);
            else setText(e.target.value);
          }}
          onKeyDown={onKeyDown}
          onBlur={() => commit([text])}
          className="min-w-24 flex-1 bg-transparent outline-none placeholder:text-muted/70"
        />
        <datalist id={`${id}-suggestions`}>
          {suggestions.filter((s) => !has(value, s)).map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
      </div>
      {hint && <span className="text-xs text-muted">{hint}</span>}
    </div>
  );
}
