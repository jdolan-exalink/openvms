import { cn } from "@/lib/cn";
import type { CameraTab, CameraTabId } from "@/lib/cameraTabs";

/** Top tab navigation; scrolls sideways on narrow screens instead of wrapping. */
export function CameraTabs({ tabs, active, dirtyTabs, onSelect }: { tabs: CameraTab[]; active: CameraTabId; dirtyTabs: ReadonlySet<CameraTabId>; onSelect: (tab: CameraTab) => void }) {
  return (
    <nav aria-label="Secciones de configuración" className="overflow-x-auto border-b border-outline-variant/60">
      <div role="tablist" className="flex min-w-max gap-1">
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={active === tab.id}
            onClick={() => onSelect(tab)}
            className={cn(
              "inline-flex min-h-11 items-center gap-1.5 border-b-2 px-4 text-sm font-semibold transition-colors",
              active === tab.id ? "border-primary text-primary" : "border-transparent text-on-surface-variant hover:bg-surface-2"
            )}
          >
            {tab.label}
            {dirtyTabs.has(tab.id) && <span aria-label="con cambios" className="text-primary">●</span>}
          </button>
        ))}
      </div>
    </nav>
  );
}
