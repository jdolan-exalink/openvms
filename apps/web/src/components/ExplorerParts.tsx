import { ChevronDown, ChevronRight } from "lucide-react";
import { Icon } from "@/components/Icon";

/** Row anatomy shared by the Live explorer and the Maps camera tree. */
export const dot = (status: string) => (status === "online" ? "bg-ok" : status === "offline" ? "bg-bad" : "bg-muted");

/** Muted node-type glyph (site, server, folder, camera) shown before the name. */
export const nodeIcon = "shrink-0 text-on-surface-variant";

export const rowBase = "flex min-w-0 flex-1 items-center gap-1.5 rounded-m3-sm px-2 py-1.5 text-left hover:bg-on-surface/8";
export const rowSelected = "bg-secondary-container font-medium text-on-secondary-container";

export function Chevron({ open }: { open: boolean }) {
  return <Icon icon={open ? ChevronDown : ChevronRight} size="xs" className="shrink-0" />;
}

export function Count({ n }: { n: number }) {
  return <span className="ml-auto shrink-0 rounded-full bg-surface-3 px-2 text-[11px] font-medium tabular-nums text-on-surface-variant">{n}</span>;
}
