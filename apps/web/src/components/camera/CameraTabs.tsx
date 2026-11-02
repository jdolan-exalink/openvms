import { Cable, Clapperboard, Compass, Cpu, Film, Gauge, Radar, SlidersHorizontal, type LucideIcon } from "lucide-react";
import { Icon } from "@/components/Icon";
import { cn } from "@/lib/cn";
import { groupCameraTabs, type CameraTab, type CameraTabId } from "@/lib/cameraTabs";

const ICONS: Record<CameraTabId, LucideIcon> = {
  summary: Gauge,
  video: Clapperboard,
  record: Film,
  detect: Radar,
  analytics: Cpu,
  ptz: Compass,
  connection: Cable,
  advanced: SlidersHorizontal,
};

/**
 * Section navigation of the camera editor: a vertical list grouped under small headings; below 900px it
 * becomes a horizontal scroller (headings hidden) above the content.
 */
export function CameraTabs({ tabs, active, dirtyTabs, onSelect }: { tabs: CameraTab[]; active: CameraTabId; dirtyTabs: ReadonlySet<CameraTabId>; onSelect: (tab: CameraTab) => void }) {
  return (
    <nav aria-label="Secciones de configuración" className="min-w-0 overflow-x-auto min-[900px]:w-56 min-[900px]:shrink-0 min-[900px]:overflow-visible">
      <div role="tablist" aria-orientation="vertical" className="flex min-w-max gap-1 min-[900px]:min-w-0 min-[900px]:flex-col min-[900px]:gap-4 min-[900px]:rounded-m3-2xl min-[900px]:border min-[900px]:border-outline-variant/60 min-[900px]:bg-surface-1 min-[900px]:p-3">
        {groupCameraTabs(tabs).map((group) => (
          <div key={group.id} role="group" aria-label={group.label} className="flex gap-1 min-[900px]:flex-col">
            <p aria-hidden className="hidden px-2 pb-1 text-[11px] font-bold uppercase tracking-wider text-muted min-[900px]:block">{group.label}</p>
            {group.tabs.map((tab) => (
              <button
                key={tab.id}
                type="button"
                role="tab"
                aria-selected={active === tab.id}
                onClick={() => onSelect(tab)}
                className={cn(
                  "inline-flex min-h-10 items-center gap-2 whitespace-nowrap rounded-m3-lg px-3 text-sm font-semibold transition-colors",
                  active === tab.id ? "bg-primary/10 text-primary" : "text-on-surface-variant hover:bg-surface-2"
                )}
              >
                <Icon icon={ICONS[tab.id]} size="xs" strokeWidth={active === tab.id ? 2 : undefined} />
                {tab.label}
                {dirtyTabs.has(tab.id) && <span aria-label="con cambios" className="text-primary">●</span>}
              </button>
            ))}
          </div>
        ))}
      </div>
    </nav>
  );
}
