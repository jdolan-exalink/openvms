import { LayoutGrid, List, PanelTop, type LucideIcon } from "lucide-react";
import { useT } from "@/i18n";
import { cn } from "@/lib/cn";
import type { PhoneLayout } from "@/lib/phoneLayoutPrefs";

const OPTIONS: { id: PhoneLayout; icon: LucideIcon; label: "live.phoneLayoutList" | "live.phoneLayoutGrid" | "live.phoneLayoutFocus" }[] = [
  { id: "list", icon: List, label: "live.phoneLayoutList" },
  { id: "grid", icon: LayoutGrid, label: "live.phoneLayoutGrid" },
  { id: "focus", icon: PanelTop, label: "live.phoneLayoutFocus" },
];

/** PhoneLayoutToggle is the compact list / grid / focus switch of the phone Live header. */
export function PhoneLayoutToggle({ value, onChange }: { value: PhoneLayout; onChange: (layout: PhoneLayout) => void }) {
  const tr = useT();
  return (
    <div role="group" aria-label={tr("live.phoneLayout")} className="inline-flex shrink-0 items-center rounded-full bg-surface-1 p-0.5">
      {OPTIONS.map(({ id, icon: Glyph, label }) => (
        <button
          key={id}
          type="button"
          aria-label={tr(label)}
          title={tr(label)}
          aria-pressed={value === id}
          onClick={() => onChange(id)}
          className={cn(
            "m3-press inline-flex h-11 w-10 items-center justify-center rounded-full focus-visible:outline-2 focus-visible:outline-primary",
            value === id ? "bg-primary text-on-primary" : "text-on-surface-variant",
          )}
        >
          <Glyph className="size-5" aria-hidden />
        </button>
      ))}
    </div>
  );
}
