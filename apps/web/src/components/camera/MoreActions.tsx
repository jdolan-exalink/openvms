import { MoreHorizontal } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Icon } from "@/components/Icon";
import { Button } from "@/components/ui";

export type MoreAction = { id: string; label: string; onSelect: () => void; disabled?: boolean };

/** "Más acciones" menu of the header: secondary actions that must stay reachable. */
export function MoreActions({ actions }: { actions: MoreAction[] }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);
  return (
    <div ref={ref} className="relative">
      <Button variant="tonal" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        <Icon icon={MoreHorizontal} size="xs" /> Más acciones
      </Button>
      {open && (
        <div role="menu" className="absolute right-0 top-full z-20 mt-1 flex min-w-56 flex-col rounded-m3-lg border border-outline-variant/60 bg-surface-1 p-1 shadow-lg">
          {actions.map((a) => (
            <button
              key={a.id}
              type="button"
              role="menuitem"
              disabled={a.disabled}
              onClick={() => { setOpen(false); a.onSelect(); }}
              className="min-h-10 rounded-m3-md px-3 text-left text-sm hover:bg-surface-2 disabled:opacity-50"
            >
              {a.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
