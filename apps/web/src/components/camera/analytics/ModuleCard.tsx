import type { ReactNode } from "react";
import { Switch } from "@/components/ui";
import { cn } from "@/lib/cn";
import { type AnalyticsModule, engineLabel } from "./draft";

type Props = {
  module: AnalyticsModule;
  /** The switch as the user sees it (draft over effective). */
  enabled: boolean;
  onToggle: (next: boolean) => void;
  readOnly?: boolean;
  /** Detail of the module; only rendered while the module is on. */
  children?: ReactNode;
};

/**
 * One analytics module: a switch with its engine, the reason when it cannot be changed, and its
 * detail only while it is on. A module without evidence is shown disabled with the reason, never
 * as if it worked.
 */
export function ModuleCard({ module: m, enabled, onToggle, readOnly, children }: Props) {
  const locked = !m.available || !m.writable || !!readOnly;
  return (
    <div data-testid={`module-${m.id}`} className={cn("flex flex-col gap-3 rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4", !m.available && "opacity-80")}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <span className="text-sm font-bold text-on-surface">{m.label}</span>
          {m.available && <span className="text-xs text-muted">Motor: {engineLabel[m.engine]}</span>}
        </div>
        <Switch checked={m.available && enabled} onChange={onToggle} disabled={locked} label={m.available && enabled ? "Activo" : "Inactivo"} className="shrink-0" />
      </div>
      {m.reason && locked && <p className="text-xs text-muted">{m.reason}</p>}
      {m.available && enabled && children}
    </div>
  );
}
