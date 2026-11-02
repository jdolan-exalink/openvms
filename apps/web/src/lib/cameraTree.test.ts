import { describe, expect, it } from "vitest";
import { agentInfo, buildCameraTree, compareVersions, isVirtualCamera, latestAgentVersion, type TreeCamera, type TreeServer } from "./cameraTree";

const NOW = Date.parse("2026-11-02T12:00:00Z");
const server = (id: string, extra: Partial<TreeServer> = {}): TreeServer => ({ id, name: id, site_id: "s1", kind: "frigate", ...extra });
const cam = (id: string, extra: Partial<TreeCamera> = {}): TreeCamera => ({
  id,
  site_id: "s1",
  server_id: "a",
  display_name: id,
  remote_name: id,
  enabled: true,
  status: "online",
  ...extra,
});
const opts = { q: "", statuses: [], agents: [], showVirtual: false, now: NOW } as const;

describe("compareVersions", () => {
  it("compares numerically, not lexically, ignoring a leading v and suffixes", () => {
    expect(compareVersions("1.10.0", "1.9.0")).toBeGreaterThan(0);
    expect(compareVersions("v1.2", "1.2.0")).toBe(0);
    expect(compareVersions("0.9.9", "1.0.0")).toBeLessThan(0);
    expect(compareVersions("1.2.3-rc1", "1.2.3")).toBe(0);
  });
});

describe("isVirtualCamera", () => {
  it("flags birdseye regardless of case and nothing else", () => {
    expect(isVirtualCamera({ remote_name: "birdseye" })).toBe(true);
    expect(isVirtualCamera({ remote_name: "BirdsEye" })).toBe(true);
    expect(isVirtualCamera({ remote_name: "patio" })).toBe(false);
  });
});

describe("agentInfo", () => {
  it("says there is no agent when it never reported", () => {
    expect(agentInfo(server("a"), "1.0.0", NOW)).toMatchObject({ state: "none", label: "Sin agente", offline: false });
  });
  it("shows the version of a current agent", () => {
    const s = server("a", { agent_version: "1.4.0", agent_last_seen_at: "2026-11-02T11:59:30Z", agent_status: "online" });
    expect(agentInfo(s, "1.4.0", NOW)).toMatchObject({ state: "current", label: "Agente v1.4.0", offline: false });
  });
  it("marks an agent older than the highest one as outdated", () => {
    const s = server("a", { agent_version: "1.3.0", agent_last_seen_at: "2026-11-02T11:59:30Z" });
    expect(agentInfo(s, "1.10.0", NOW)).toMatchObject({ state: "outdated", label: "Agente v1.3.0 · actualizar" });
  });
  it("marks an agent as offline when it is not seen recently or reports offline", () => {
    const stale = server("a", { agent_version: "1.0.0", agent_last_seen_at: "2026-11-02T10:00:00Z" });
    expect(agentInfo(stale, "1.0.0", NOW).offline).toBe(true);
    const down = server("a", { agent_version: "1.0.0", agent_last_seen_at: "2026-11-02T11:59:50Z", agent_status: "offline" });
    expect(agentInfo(down, "1.0.0", NOW).offline).toBe(true);
  });
  it("reports the agent of a recorder like any other server", () => {
    expect(agentInfo(server("n", { kind: "provision_nvr" }), "1.0.0", NOW).state).toBe("none");
    expect(agentInfo(server("n", { kind: "provision_nvr", agent_version: "1.0.0", agent_last_seen_at: new Date(NOW).toISOString() }), "1.0.0", NOW).state).toBe("current");
  });
});

describe("latestAgentVersion", () => {
  it("picks the highest version among the servers", () => {
    expect(latestAgentVersion([server("a", { agent_version: "1.9.0" }), server("b", { agent_version: "1.10.0" }), server("c")])).toBe("1.10.0");
    expect(latestAgentVersion([server("c")])).toBe("");
  });
});

describe("buildCameraTree", () => {
  const servers = [server("a"), server("b", { agent_version: "1.0.0", agent_last_seen_at: "2026-11-02T11:59:00Z" })];
  const cameras = [cam("c1"), cam("c2", { status: "offline" }), cam("c3", { server_id: "b" }), cam("bird", { remote_name: "birdseye" })];

  it("groups site > server > camera with counts and hides virtual cameras by default", () => {
    const tree = buildCameraTree(cameras, servers, [{ id: "s1", name: "Helvecia" }], opts);
    expect(tree).toHaveLength(1);
    expect(tree[0]!.site.name).toBe("Helvecia");
    const [a, b] = tree[0]!.servers;
    expect(a!.cameras.map((c) => c.id)).toEqual(["c1", "c2"]);
    expect(a!.online).toBe(1);
    expect(b!.cameras.map((c) => c.id)).toEqual(["c3"]);
  });

  it("includes virtual cameras when asked", () => {
    const tree = buildCameraTree(cameras, servers, [], { ...opts, showVirtual: true });
    expect(tree[0]!.servers[0]!.cameras.map((c) => c.id)).toContain("bird");
  });

  it("keeps empty servers without filters but hides them while searching", () => {
    expect(buildCameraTree([], servers, [], opts)[0]!.servers).toHaveLength(2);
    expect(buildCameraTree(cameras, servers, [], { ...opts, q: "c3" })[0]!.servers.map((s) => s.server.id)).toEqual(["b"]);
  });

  it("searches name, remote name and server name", () => {
    const named = [cam("c1", { display_name: "Acceso", remote_name: "gate_01" })];
    expect(buildCameraTree(named, servers, [], { ...opts, q: "gate" })[0]!.servers[0]!.cameras).toHaveLength(1);
    expect(buildCameraTree(named, servers, [], { ...opts, q: "acce" })[0]!.servers[0]!.cameras).toHaveLength(1);
    expect(buildCameraTree(named, [server("a", { name: "frigate-h01" })], [], { ...opts, q: "h01" })[0]!.servers[0]!.cameras).toHaveLength(1);
  });

  it("filters by camera status", () => {
    const tree = buildCameraTree([cam("c1"), cam("c2", { status: "offline" }), cam("c3", { enabled: false })], servers, [], { ...opts, statuses: ["disabled"] });
    expect(tree[0]!.servers[0]!.cameras.map((c) => c.id)).toEqual(["c3"]);
  });

  it("filters servers by agent state", () => {
    const tree = buildCameraTree(cameras, servers, [], { ...opts, agents: ["with"] });
    expect(tree[0]!.servers.map((s) => s.server.id)).toEqual(["b"]);
  });
});
