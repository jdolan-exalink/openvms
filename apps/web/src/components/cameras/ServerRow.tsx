import { Link } from "@tanstack/react-router";
import { ChevronDown, ChevronRight } from "lucide-react";
import type { AgentInfo } from "@/lib/cameraTree";
import { Icon } from "@/components/Icon";
import { cn } from "@/lib/cn";

/** Dot and pill colors for the agent states, shared by the server row and the filter rail. */
export function agentTone(agent: AgentInfo) {
  if (agent.state === "none" || agent.state === "na") return { dot: "bg-outline", pill: "bg-surface-2 text-muted" };
  if (agent.offline) return { dot: "bg-bad", pill: "bg-bad/12 text-bad" };
  if (agent.state === "outdated") return { dot: "bg-warn", pill: "bg-warn/15 text-warn" };
  return { dot: "bg-ok", pill: "bg-ok/15 text-ok" };
}

export function AgentPill({ agent }: { agent: AgentInfo }) {
  if (agent.state === "na") return null;
  const tone = agentTone(agent);
  return (
    <span className={cn("inline-flex h-6 items-center gap-1.5 rounded-full px-2.5 text-xs font-medium whitespace-nowrap", tone.pill)}>
      <span aria-hidden className={cn("size-1.5 rounded-full", tone.dot)} />
      {agent.label}
      {agent.offline && <span className="font-semibold">· sin conexión</span>}
    </span>
  );
}

/** One server of the tree: collapse toggle, name, kind, agent pill and camera counts. */
export function ServerRow({
  name,
  siteId,
  kind,
  agent,
  total,
  online,
  open,
  onToggle,
}: {
  name: string;
  siteId?: string;
  kind?: string;
  agent: AgentInfo;
  total: number;
  online: number;
  /** undefined renders no toggle (flat list of a single selected server). */
  open?: boolean;
  onToggle?: () => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 bg-surface-2 px-3 py-2">
      {onToggle && (
        <button
          type="button"
          aria-expanded={open}
          aria-label={`${open ? "Contraer" : "Expandir"} ${name}`}
          onClick={onToggle}
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-full hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-primary"
        >
          <Icon icon={open ? ChevronDown : ChevronRight} size="sm" />
        </button>
      )}
      <Link to="/servers" search={{ site_id: siteId }} className="min-w-0 truncate text-sm font-semibold hover:underline">
        {name}
      </Link>
      <span className="rounded-m3-sm border border-outline-variant/60 px-1.5 py-0.5 font-mono text-[10px] text-muted">{kind === "provision_nvr" ? "NVR" : "Frigate"}</span>
      <AgentPill agent={agent} />
      <span className="ml-auto text-xs text-muted tabular-nums">
        {total} {total === 1 ? "cámara" : "cámaras"} · {online} en línea
      </span>
    </div>
  );
}
