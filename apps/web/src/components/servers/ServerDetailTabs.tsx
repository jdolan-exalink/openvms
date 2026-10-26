import { type KeyboardEvent, type ReactNode, useId, useRef, useState } from "react";
import { cn } from "@/lib/cn";

export interface DetailTab {
  id: string;
  label: string;
  content: ReactNode;
}

/**
 * ServerDetailTabs is an accessible tab set (WAI-ARIA tabs pattern with automatic activation). Only
 * the selected panel is mounted, so a hidden panel never duplicates the controls of a visible one.
 * State that must survive a tab switch belongs to the caller.
 */
export function ServerDetailTabs({ label, tabs, initial }: { label: string; tabs: DetailTab[]; initial?: string }) {
  const base = useId();
  const [selected, setSelected] = useState(initial ?? tabs[0]?.id);
  const refs = useRef(new Map<string, HTMLButtonElement>());
  const tabId = (id: string) => `${base}-tab-${id}`;
  const panelId = (id: string) => `${base}-panel-${id}`;
  const active = tabs.find((tab) => tab.id === selected) ?? tabs[0];

  const select = (id: string) => {
    setSelected(id);
    refs.current.get(id)?.focus();
  };
  const onKeyDown = (index: number) => (event: KeyboardEvent) => {
    const last = tabs.length - 1;
    const target =
      event.key === "ArrowRight" ? (index === last ? 0 : index + 1)
      : event.key === "ArrowLeft" ? (index === 0 ? last : index - 1)
      : event.key === "Home" ? 0
      : event.key === "End" ? last
      : null;
    if (target === null) return;
    event.preventDefault();
    select(tabs[target]!.id);
  };

  return (
    <div className="flex flex-col gap-4">
      <div role="tablist" aria-label={label} className="flex flex-wrap gap-1 border-b border-outline/30">
        {tabs.map((tab, index) => {
          const isActive = tab.id === active?.id;
          return (
            <button
              key={tab.id}
              ref={(node) => {
                if (node) refs.current.set(tab.id, node);
                else refs.current.delete(tab.id);
              }}
              type="button"
              role="tab"
              id={tabId(tab.id)}
              aria-selected={isActive}
              aria-controls={panelId(tab.id)}
              tabIndex={isActive ? 0 : -1}
              onClick={() => setSelected(tab.id)}
              onKeyDown={onKeyDown(index)}
              className={cn(
                "-mb-px border-b-2 px-4 py-2 text-sm font-bold focus-visible:outline-2 focus-visible:outline-primary",
                isActive ? "border-primary text-primary" : "border-transparent text-muted hover:text-surface-on",
              )}
            >
              {tab.label}
            </button>
          );
        })}
      </div>
      {active && (
        <div role="tabpanel" id={panelId(active.id)} aria-labelledby={tabId(active.id)}>
          {active.content}
        </div>
      )}
    </div>
  );
}
