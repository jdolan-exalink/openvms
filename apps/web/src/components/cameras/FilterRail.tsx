import type { ReactNode } from "react";
import type { Schemas } from "@/api/client";
import { Switch, Select, TextInput } from "@/components/ui";
import type { AgentFilter, AgentInfo, CameraStatusFilter } from "@/lib/cameraTree";
import { cn } from "@/lib/cn";
import { agentTone } from "./ServerRow";

function Chip({ pressed, onClick, children }: { pressed: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        "h-8 rounded-full border px-3 text-xs font-medium focus-visible:outline-2 focus-visible:outline-primary",
        pressed ? "border-transparent bg-secondary-container text-on-secondary-container" : "border-outline-variant text-on-surface-variant hover:bg-on-surface/8",
      )}
    >
      {children}
    </button>
  );
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <h2 className="text-xs font-bold tracking-wide text-muted uppercase">{title}</h2>
      {children}
    </div>
  );
}

const toggle = <T,>(list: readonly T[], value: T) => (list.includes(value) ? list.filter((v) => v !== value) : [...list, value]);

const STATUS_CHIPS: { value: CameraStatusFilter; label: string }[] = [
  { value: "online", label: "En línea" },
  { value: "offline", label: "Sin señal" },
  { value: "disabled", label: "Deshabilitada" },
];
const AGENT_CHIPS: { value: AgentFilter; label: string }[] = [
  { value: "with", label: "Con agente" },
  { value: "outdated", label: "Desactualizado" },
  { value: "none", label: "Sin agente" },
];

/** Left filter rail (stacked above the list on narrow widths): search, site, server, status, agent and virtual cameras. */
export function FilterRail({
  q,
  onQ,
  siteId,
  onSite,
  sites,
  serverId,
  onServer,
  servers,
  statuses,
  onStatuses,
  agents,
  onAgents,
  showVirtual,
  onShowVirtual,
  virtualCount,
}: {
  q: string;
  onQ: (v: string) => void;
  siteId: string;
  onSite: (v: string) => void;
  sites: readonly { id: string; name: string }[];
  serverId: string;
  onServer: (v: string) => void;
  servers: readonly { server: Schemas["Server"]; count: number; agent: AgentInfo }[];
  statuses: readonly CameraStatusFilter[];
  onStatuses: (v: CameraStatusFilter[]) => void;
  agents: readonly AgentFilter[];
  onAgents: (v: AgentFilter[]) => void;
  showVirtual: boolean;
  onShowVirtual: (v: boolean) => void;
  virtualCount: number;
}) {
  return (
    <aside aria-label="Filtros" className="flex w-full shrink-0 flex-col gap-5 rounded-m3-xl bg-surface-1 p-4 @3xl:w-64">
      <TextInput aria-label="Buscar cámara" type="search" placeholder="Buscar cámara o servidor" value={q} onChange={(e) => onQ(e.target.value)} />
      <Group title="Sitio">
        <Select aria-label="Filtrar por sitio" value={siteId} onChange={(e) => onSite(e.target.value)}>
          <option value="">Todos los sitios</option>
          {sites.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </Select>
      </Group>
      <Group title="Servidor">
        <ul aria-label="Filtrar por servidor" className="flex flex-col gap-1">
          <li>
            <ServerOption label="Todos los servidores" pressed={serverId === ""} onClick={() => onServer("")} />
          </li>
          {servers.map(({ server, count, agent }) => (
            <li key={server.id}>
              <ServerOption label={server.name} count={count} dot={agentTone(agent).dot} pressed={serverId === server.id} onClick={() => onServer(serverId === server.id ? "" : server.id)} />
            </li>
          ))}
        </ul>
      </Group>
      <Group title="Estado">
        <div className="flex flex-wrap gap-2">
          {STATUS_CHIPS.map((chip) => (
            <Chip key={chip.value} pressed={statuses.includes(chip.value)} onClick={() => onStatuses(toggle(statuses, chip.value))}>
              {chip.label}
            </Chip>
          ))}
        </div>
      </Group>
      <Group title="Agente">
        <div className="flex flex-wrap gap-2">
          {AGENT_CHIPS.map((chip) => (
            <Chip key={chip.value} pressed={agents.includes(chip.value)} onClick={() => onAgents(toggle(agents, chip.value))}>
              {chip.label}
            </Chip>
          ))}
        </div>
      </Group>
      <Switch checked={showVirtual} onChange={onShowVirtual} label={`Mostrar cámaras virtuales (${virtualCount})`} className="text-xs" />
    </aside>
  );
}

function ServerOption({ label, count, dot, pressed, onClick }: { label: string; count?: number; dot?: string; pressed: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        "flex min-h-9 w-full items-center gap-2 rounded-m3-md px-2 text-left text-sm focus-visible:outline-2 focus-visible:outline-primary",
        pressed ? "bg-secondary-container text-on-secondary-container" : "hover:bg-on-surface/8",
      )}
    >
      {dot && <span aria-hidden className={cn("size-2 shrink-0 rounded-full", dot)} />}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {count != null && <span className="text-xs text-muted tabular-nums">{count}</span>}
    </button>
  );
}
