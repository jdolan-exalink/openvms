// Pure helpers behind the camera list: grouping Site > Server > Camera, filtering, the agent pill and virtual cameras.

export type TreeCamera = {
  id: string;
  site_id: string;
  server_id: string;
  display_name: string;
  remote_name: string;
  enabled: boolean;
  status: string;
};

export type TreeServer = {
  id: string;
  name: string;
  site_id?: string;
  kind?: string;
  agent_version?: string;
  agent_status?: string;
  agent_last_seen_at?: string | null;
};

export type CameraStatusFilter = "online" | "offline" | "disabled";
export type AgentFilter = "with" | "outdated" | "none";

export type TreeOptions = {
  q: string;
  statuses: readonly CameraStatusFilter[];
  agents: readonly AgentFilter[];
  showVirtual: boolean;
  /** Clock used to decide whether an agent is still reporting; injectable for tests. */
  now?: number;
};

export type AgentInfo = {
  state: "none" | "current" | "outdated";
  version: string;
  offline: boolean;
  label: string;
};

export type ServerNode<C extends TreeCamera, S extends TreeServer> = { server: S; cameras: C[]; online: number; agent: AgentInfo };
export type SiteNode<C extends TreeCamera, S extends TreeServer> = { site: { id: string; name: string }; servers: ServerNode<C, S>[] };

/** An agent that has not heartbeated for this long counts as offline. */
const AGENT_STALE_MS = 10 * 60 * 1000;

/** Frigate's synthetic Birdseye view is the only camera the data model marks as virtual. */
export function isVirtualCamera(camera: { remote_name: string }): boolean {
  return camera.remote_name.trim().toLowerCase() === "birdseye";
}

function versionParts(version: string): number[] {
  const core = version.trim().replace(/^v/i, "").split(/[-+]/)[0] ?? "";
  return core.split(".").map((part) => Number.parseInt(part, 10) || 0);
}

/** Compares dotted versions numerically (1.10.0 > 1.9.0); prerelease suffixes are ignored. */
export function compareVersions(a: string, b: string): number {
  const left = versionParts(a);
  const right = versionParts(b);
  for (let i = 0; i < Math.max(left.length, right.length); i++) {
    const diff = (left[i] ?? 0) - (right[i] ?? 0);
    if (diff !== 0) return diff;
  }
  return 0;
}

/** Highest agent version reported by any of the servers; "" when none has an agent. */
export function latestAgentVersion(servers: readonly TreeServer[]): string {
  return servers.reduce((best, s) => (s.agent_version && (!best || compareVersions(s.agent_version, best) > 0) ? s.agent_version : best), "");
}

/** What the server row says about its agent. "Outdated" is relative to the newest agent among the loaded servers. */
export function agentInfo(server: TreeServer, latest: string, now: number = Date.now()): AgentInfo {
  const version = server.agent_version ?? "";
  const seen = server.agent_last_seen_at ? Date.parse(server.agent_last_seen_at) : Number.NaN;
  if (!version && Number.isNaN(seen)) return { state: "none", version: "", offline: false, label: "Sin agente" };
  const offline = server.agent_status === "offline" || Number.isNaN(seen) || now - seen > AGENT_STALE_MS;
  const outdated = !!version && !!latest && compareVersions(version, latest) < 0;
  const base = version ? `Agente v${version.replace(/^v/i, "")}` : "Agente";
  return { state: outdated ? "outdated" : "current", version, offline, label: outdated ? `${base} · actualizar` : base };
}

export function matchesStatus(camera: Pick<TreeCamera, "enabled" | "status">, statuses: readonly CameraStatusFilter[]): boolean {
  if (statuses.length === 0) return true;
  const own: CameraStatusFilter = !camera.enabled ? "disabled" : camera.status === "online" ? "online" : "offline";
  return statuses.includes(own);
}

function matchesAgent(info: AgentInfo, agents: readonly AgentFilter[]): boolean {
  if (agents.length === 0) return true;
  return agents.some((f) => (f === "with" ? info.state === "current" || info.state === "outdated" : f === "outdated" ? info.state === "outdated" : info.state === "none"));
}

/**
 * Groups cameras Site > Server > Camera. Empty servers stay visible, except while a search or status
 * filter is active, so a search only shows where it found something.
 */
export function buildCameraTree<C extends TreeCamera, S extends TreeServer>(
  cameras: readonly C[],
  servers: readonly S[],
  sites: readonly { id: string; name: string }[],
  options: TreeOptions,
): SiteNode<C, S>[] {
  const latest = latestAgentVersion(servers);
  const needle = options.q.trim().toLowerCase();
  const narrowing = needle !== "" || options.statuses.length > 0;
  const siteName = new Map(sites.map((s) => [s.id, s.name]));
  const byServer = new Map<string, C[]>();
  for (const c of cameras) {
    const list = byServer.get(c.server_id) ?? [];
    list.push(c);
    byServer.set(c.server_id, list);
  }
  const known = new Set(servers.map((s) => s.id));
  // Cameras of a server the inventory does not list still show up, under a placeholder.
  const all: S[] = [...servers, ...[...byServer.keys()].filter((id) => !known.has(id)).map((id) => ({ id, name: "—" }) as S)];

  const sitesById = new Map<string, SiteNode<C, S>>();
  for (const server of all) {
    const visible = (byServer.get(server.id) ?? []).filter((c) => {
      if (!options.showVirtual && isVirtualCamera(c)) return false;
      if (!matchesStatus(c, options.statuses)) return false;
      return !needle || [c.display_name, c.remote_name, server.name].some((v) => v.toLowerCase().includes(needle));
    });
    const agent = agentInfo(server, latest, options.now);
    if (!matchesAgent(agent, options.agents)) continue;
    if (narrowing && visible.length === 0) continue;
    const siteId = server.site_id ?? visible[0]?.site_id ?? byServer.get(server.id)?.[0]?.site_id ?? "";
    const node = sitesById.get(siteId) ?? { site: { id: siteId, name: siteName.get(siteId) ?? "—" }, servers: [] };
    node.servers.push({ server, cameras: visible, online: visible.filter((c) => c.enabled && c.status === "online").length, agent });
    sitesById.set(siteId, node);
  }
  return [...sitesById.values()].sort((a, b) => a.site.name.localeCompare(b.site.name));
}
