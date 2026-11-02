import { ChevronDown } from "lucide-react";
import type { ReactNode } from "react";
import { Icon } from "@/components/Icon";
import { cn } from "@/lib/cn";

/** One collapsible entry of Avanzado; closed by default, the body only mounts while open. */
export function AdvancedAccordion({ id, title, open, dirty, badge, onToggle, children }: { id: string; title: string; open: boolean; dirty?: boolean; badge?: ReactNode; onToggle: () => void; children: ReactNode }) {
  return (
    <div className="rounded-m3-xl border border-outline-variant/60 bg-surface-1">
      <h3>
        <button
          type="button"
          aria-expanded={open}
          aria-controls={`adv-${id}`}
          onClick={onToggle}
          className="flex min-h-12 w-full items-center justify-between gap-3 rounded-m3-xl px-4 text-left text-sm font-semibold hover:bg-surface-2"
        >
          <span className="flex items-center gap-2">
            {title}
            {dirty && <span aria-label="con cambios" className="text-primary">●</span>}
            {badge}
          </span>
          <Icon icon={ChevronDown} size="xs" className={cn("text-muted transition-transform", open && "rotate-180")} />
        </button>
      </h3>
      {open && (
        <div id={`adv-${id}`} role="region" aria-label={title} className="flex flex-col gap-4 border-t border-outline-variant/60 p-4">
          {children}
        </div>
      )}
    </div>
  );
}
