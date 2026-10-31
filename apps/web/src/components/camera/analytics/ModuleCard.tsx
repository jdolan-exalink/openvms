import type { ReactNode } from "react";
import { ProvenanceBadge } from "@/components/camera/profile/ProvenanceBadge";
import { Switch } from "@/components/ui";
import { cn } from "@/lib/cn";
import { type AnalyticsModule, displayModuleLabel, engineLabel, originLabelOf } from "./draft";

type Props = {
  module: AnalyticsModule;
  /** The switch as the user sees it (draft over effective). */
  enabled: boolean;
  onToggle: (next: boolean) => void;
  readOnly?: boolean;
  /** Detail of the module; only rendered while the module is on. */
  children?: ReactNode;
};

const originBadgeStyle = (origin: string): string => {
  switch (origin) {
    case "Internas de Frigate":
      return "border-sky-500/30 bg-sky-500/10 text-sky-400";
    case "Internas de cámaras Provision":
      return "border-amber-500/30 bg-amber-500/10 text-amber-300";
    case "ONVIF":
      return "border-purple-500/30 bg-purple-500/10 text-purple-300";
    case "OpenVMS":
      return "border-emerald-500/30 bg-emerald-500/10 text-emerald-400";
    default:
      return "border-outline-variant/60 bg-surface-3 text-muted";
  }
};

/**
 * One analytics module: a switch with its engine, the reason when it cannot be changed, and its
 * detail only while it is on. A module without evidence is shown disabled with the reason, never
 * as if it worked.
 */
export function ModuleCard({ module: m, enabled, onToggle, readOnly, children }: Props) {
  const locked = !m.available || !m.writable || !!readOnly;
  const label = displayModuleLabel(m);
  const origin = originLabelOf(m);

  return (
    <div data-testid={`module-${m.id}`} className={cn("flex flex-col gap-3 rounded-m3-xl border border-outline-variant/60 bg-surface-2 p-4", !m.available && "opacity-80")}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <span className="flex flex-wrap items-center gap-2 text-sm font-bold text-on-surface">
            {label}
            {origin && (
              <span
                data-testid={`origin-badge-${m.id}`}
                className={cn("inline-flex items-center rounded-full border px-2 py-0.5 text-[11px] font-semibold", originBadgeStyle(origin))}
              >
                {origin}
              </span>
            )}
            <ProvenanceBadge path={m.id === "lpr" ? ["analytics.lpr", "analytics.modules.lpr.enabled"] : `analytics.modules.${m.id}.enabled`} />
          </span>
          {m.available && <span className="text-xs text-muted">Motor: {engineLabel[m.engine] ?? m.engine}</span>}
        </div>
        <Switch checked={m.available && enabled} onChange={onToggle} disabled={locked} label={m.available && enabled ? "Activo" : "Inactivo"} className="shrink-0" />
      </div>
      {m.reason && locked && <p className="text-xs text-muted">{m.reason}</p>}
      {m.available && enabled && children}
    </div>
  );
}
