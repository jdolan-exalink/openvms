import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Activity, ArrowDown, ArrowUp, Check, Copy, Pencil, Plus, RefreshCw, RotateCcw, Terminal, Trash2 } from "lucide-react";
import { Link, useSearch } from "@tanstack/react-router";
import { type FormEvent, type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { ApiError, api, type Schemas, unwrap } from "@/api/client";
import { classifyPolicyQuery, meQuery, serversQuery, sitesQuery, syncStatusQuery } from "@/api/queries";
import { BodyClassifySwitch } from "@/components/BodyClassifySwitch";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Button, Empty, ErrorNote, Field, PageHeader, Pill, Select, StatusBadge, Summary, Switch, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import { can } from "@/lib/perm";
import { useAgentJobs } from "@/lib/agentJobs/AgentJobProvider";

export function Servers() {
  const t = useT();
  const me = useQuery(meQuery);
  const servers = useQuery(serversQuery);
  const sites = useQuery(sitesQuery);
  const [registering, setRegistering] = useState(false);
  const { site_id: siteFilter, server_id: serverFilter } = useSearch({ strict: false }) as { site_id?: string; server_id?: string };
  const [q, setQ] = useState("");
  const needle = q.trim().toLowerCase();
  const visible = servers.data?.filter(
    (s) => (!siteFilter || s.site_id === siteFilter) && (!serverFilter || s.id === serverFilter) && (!needle || `${s.name} ${s.base_url}`.toLowerCase().includes(needle)),
  );
  const siteName = new Map(sites.data?.map((s) => [s.id, s.name]));
  const [editing, setEditing] = useState<Schemas["Server"] | null>(null);
  const [deleting, setDeleting] = useState<Schemas["Server"] | null>(null);
  const sync = useQuery(syncStatusQuery);
  const syncOf = new Map(sync.data?.map((s) => [s.server_id, s]));
  const policy = useQuery(classifyPolicyQuery);
  const serverOn = new Map(policy.data?.servers.map((s) => [s.id, s.enabled]));

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title={t("nav.servers")}
        description={t("settings.servers")}
        actions={
          can(me.data, "servers.manage") && !registering ? (
            <Button variant="primary" onClick={() => setRegistering(true)}>
              <Icon icon={Plus} size="sm" /> Registrar servidor
            </Button>
          ) : null
        }
      />
      {editing && <EditServerModal server={editing} sites={sites.data ?? []} onDone={() => setEditing(null)} />}
      {deleting && <DeleteServerDialog server={deleting} onDone={() => setDeleting(null)} />}
      {registering && <RegisterServer sites={sites.data ?? []} onDone={() => setRegistering(false)} />}
      <ErrorNote error={servers.error} />
      {servers.data?.length === 0 && <Empty>No hay servidores visibles para tu usuario.</Empty>}
      {!!servers.data?.length && (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-3">
            <TextInput className="sm:w-72" aria-label="Buscar servidor" placeholder="Buscar por nombre o URL" value={q} onChange={(e) => setQ(e.target.value)} />
            {siteFilter && (
              <Link to="/servers" className="text-xs font-bold text-primary hover:underline">
                Quitar filtro de sitio
              </Link>
            )}
          </div>
          <Summary>{summarize(servers.data.length, visible ?? [])}</Summary>
        </div>
      )}
      {!!servers.data?.length && visible?.length === 0 && <Empty>Ningún servidor coincide con el filtro.</Empty>}
      {!!visible?.length && (
        <ul aria-label="Servidores" className="flex flex-col gap-3">
          {visible.map((s) => (
            <li key={s.id} className="flex flex-col gap-4 rounded-m3-xl bg-surface-1 p-5">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-lg font-bold">{s.name}</div>
                  <div className="font-mono text-xs break-all text-muted">{s.base_url}</div>
                </div>
                <div className="flex flex-col items-end gap-1">
                  <StatusBadge status={s.status} />
                  {s.last_error && <div role="alert" className="max-w-56 text-xs break-words text-bad">{s.last_error}</div>}
                </div>
              </div>
              <dl className="grid grid-cols-2 gap-3 text-sm md:grid-cols-4">
                <Fact label="Sitio">
                  {siteName.has(s.site_id) ? (
                    <Link to="/servers" search={{ site_id: s.site_id }} className="hover:underline">
                      {siteName.get(s.site_id)}
                    </Link>
                  ) : (
                    "—"
                  )}
                </Fact>
                <Fact label="Cámaras">
                  <Link to="/cameras" search={{ server_id: s.id }} className="tabular-nums hover:underline">
                    {s.camera_count}
                  </Link>
                </Fact>
                <Fact label="Almacenamiento">
                  <StorageBar storage={s.storage} />
                </Fact>
                <Fact label="Eventos">
                  <span className="text-xs">
                    <SyncCell status={syncOf.get(s.id)} />
                  </span>
                </Fact>
                <Fact label="Frigate" className="col-span-2 md:col-span-2">
                  <div className="font-mono text-xs whitespace-nowrap">
                    <div>{s.frigate_version || "—"}</div>
                    <AgentMonitor serverId={s.id} />
                  </div>
                </Fact>
                <Fact label="Clasificación" className="col-span-2 md:col-span-2">
                  <BodyClassifySwitch
                    scope="server"
                    id={s.id}
                    enabled={serverOn.get(s.id) ?? true}
                    disabled={!can(me.data, "servers.manage")}
                  />
                </Fact>
              </dl>
              <div className="flex flex-wrap items-start justify-end gap-2">
                {canInstallAgent(me.data, s) && <AgentSSHControls key={s.id} server={s} />}
                {hasServerInstallPermission(me.data, "servers.manage", s) && !hasServerInstallPermission(me.data, "servers.config.secrets", s) && (
                  <span role="note" className="text-xs text-muted">{t("servers.agentInstallPermissionRequired")}</span>
                )}
                {can(me.data, "servers.restart") && <RestartButton server={s} />}
                {can(me.data, "servers.manage") && <SyncButton server={s} />}
                {can(me.data, "servers.manage") && (
                  <>
                    <Button size="sm" onClick={() => setEditing(s)} title="Editar servidor">
                      <Icon icon={Pencil} size="xs" />
                      Editar
                    </Button>
                    <Button size="sm" variant="outlined" className="text-bad" onClick={() => setDeleting(s)} title="Eliminar servidor">
                      <Icon icon={Trash2} size="xs" />
                      Eliminar
                    </Button>
                  </>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function canInstallAgent(me: Schemas["Me"] | undefined, server: Schemas["Server"]) {
  return hasServerInstallPermission(me, "servers.manage", server) && hasServerInstallPermission(me, "servers.config.secrets", server);
}

function hasServerInstallPermission(me: Schemas["Me"] | undefined, permission: string, server: Schemas["Server"]) {
  if (!me) return false;
  const matching = me.grants.filter((grant) => {
    if (grant.permission !== permission) return false;
    if (grant.scope_type === "platform") return true;
    if (grant.scope_type === "tenant") return grant.scope_id === me.tenant_id;
    if (grant.scope_type === "site") return grant.scope_id === server.site_id;
    if (grant.scope_type === "server") return grant.scope_id === server.id;
    return false;
  });
  return matching.some((grant) => grant.effect === "allow") && !matching.some((grant) => grant.effect === "deny");
}

function AgentSSHControls({ server }: { server: Schemas["Server"] }) {
  const t = useT();
  const { jobs } = useAgentJobs();
  const status = useQuery({
    queryKey: ["server-agent", server.id],
    queryFn: async () => unwrap(await api.GET("/api/v1/servers/{serverId}/agent", { params: { path: { serverId: server.id } } })),
    refetchInterval: 5000,
    retry: false,
  });
  const refreshAgentStatus = status.refetch;
  const [deployment, setDeployment] = useState<{ kind: "install" | "update" | "uninstall"; job?: Schemas["ServerAgentInstallJob"] } | null>(null);
  const refreshedTerminalJob = useRef<string | null>(null);
  const tracked = jobs.filter((entry) => entry.serverId === server.id).sort((a, b) => a.changedAt - b.changedAt).at(-1);
  useEffect(() => {
    if (!tracked || tracked.job.status !== "succeeded" || refreshedTerminalJob.current === tracked.job.id) return;
    refreshedTerminalJob.current = tracked.job.id;
    void refreshAgentStatus();
  }, [refreshAgentStatus, tracked]);
  const [installOpen, setInstallOpen] = useState(false);

  const onInstallJob = useCallback((job: Schemas["ServerAgentInstallJob"]) => {
    setDeployment({ kind: "install", job });
    if (job.status === "succeeded") {
      void refreshAgentStatus();
    }
  }, [refreshAgentStatus]);
  const onUpdateJob = useCallback((job: Schemas["ServerAgentInstallJob"]) => {
    if (job.status === "succeeded") {
      setDeployment(null);
      void refreshAgentStatus();
      return;
    }
    setDeployment({ kind: "update", job });
  }, [refreshAgentStatus]);
  const onUninstallJob = useCallback((job: Schemas["ServerAgentInstallJob"]) => {
    if (job.status === "succeeded") {
      setDeployment(null);
      void refreshAgentStatus();
      return;
    }
    setDeployment({ kind: "uninstall", job });
  }, [refreshAgentStatus]);
  const onInstallStart = useCallback(() => setDeployment({ kind: "install" }), []);
  const onUpdateStart = useCallback(() => setDeployment({ kind: "update" }), []);
  const onUninstallStart = useCallback(() => setDeployment({ kind: "uninstall" }), []);
  const onInstallFailure = useCallback(() => setDeployment((current) => current?.kind === "install" && !current.job ? null : current), []);
  const onUpdateFailure = useCallback(() => setDeployment((current) => current?.kind === "update" && !current.job ? null : current), []);
  const onUninstallFailure = useCallback(() => setDeployment((current) => current?.kind === "uninstall" && !current.job ? null : current), []);
  const trackedNeedsAttention = tracked && (
    tracked.outcome === "unknown" || tracked.job.status === "queued" || tracked.job.status === "running" || tracked.job.status === "failed"
  );
  const visibleDeployment = tracked?.job.status === "succeeded" && !installOpen
    ? null
    : trackedNeedsAttention ? { kind: tracked.kind, job: tracked.job } : deployment;
  const active = !!visibleDeployment?.job && (visibleDeployment.job.status === "queued" || visibleDeployment.job.status === "running");

  if (!status.isPending && !status.data?.installed && status.data?.binary_status === "not_installed" && visibleDeployment?.kind !== "uninstall") {
    return (
      <div className="flex flex-wrap items-center gap-2">
        <AgentInstallControl
          key={`install-${server.id}`}
          server={server}
          otherOperationActive={false}
          onJob={onInstallJob}
          onStart={onInstallStart}
          onFailure={onInstallFailure}
          open={installOpen}
          onOpenChange={(next) => {
            setInstallOpen(next);
            if (!next) setDeployment(null);
          }}
        />
      </div>
    );
  }
  const agent = status.data;
  if (status.isPending && !visibleDeployment) return <Button size="sm" variant="outlined" disabled>{t("servers.agentLifecycleChecking")}</Button>;
  if ((status.isError || !agent) && !visibleDeployment) return <div className="flex flex-wrap items-center gap-2"><span role="status" className="text-xs text-muted">{t("servers.agentLifecycleUnknown")}</span><Button size="sm" variant="outlined" disabled={status.isFetching} onClick={() => void status.refetch()}>{t("servers.agentLifecycleRefresh")}</Button></div>;

  return (
    <div className="flex flex-wrap items-center gap-2">
      {visibleDeployment?.kind === "update" || (!status.isPending && status.data?.installed && status.data.binary_upgrade_available) ? (
        <>
          {status.data?.binary_status !== "update_available" && <span role="status" className="text-xs text-muted">{t("servers.agentLifecycleUpgradeUnverified")}</span>}
          <AgentUpdateControl
            key={`update-${server.id}`}
            server={server}
            busy={false}
            ownActive={active && visibleDeployment?.kind === "update"}
            onJob={onUpdateJob}
            onStart={onUpdateStart}
            onFailure={onUpdateFailure}
          />
        </>
      ) : (agent && isAgentBinaryCurrent(agent)) ? (
        <Button size="sm" variant="outlined" disabled>{t("servers.agentLifecycleCurrent")}</Button>
      ) : (
        <>
          <span role="status" className="text-xs text-muted">{t("servers.agentLifecycleUnknown")}</span>
          <Button size="sm" variant="outlined" disabled={status.isFetching} onClick={() => void status.refetch()}>{t("servers.agentLifecycleRefresh")}</Button>
        </>
      )}
      <AgentUninstallControl
        key={`uninstall-${server.id}`}
        server={server}
        busy={false}
        ownActive={active && visibleDeployment?.kind === "uninstall"}
        onJob={onUninstallJob}
        onStart={onUninstallStart}
        onFailure={onUninstallFailure}
      />
      {installOpen && (
        <AgentInstallDialog
          server={server}
          open={installOpen}
          onClose={() => {
            setInstallOpen(false);
            setDeployment(null);
          }}
          job={visibleDeployment?.kind === "install" ? (visibleDeployment.job ?? null) : null}
          onJob={onInstallJob}
          otherOperationActive={false}
          onStart={onInstallStart}
          onFailure={onInstallFailure}
        />
      )}
    </div>
  );
}

function AgentInstallControl({
  server,
  otherOperationActive,
  onJob,
  onStart,
  onFailure,
  open: externalOpen,
  onOpenChange,
}: {
  server: Schemas["Server"];
  otherOperationActive: boolean;
  onJob: (job: Schemas["ServerAgentInstallJob"]) => void;
  onStart: () => void;
  onFailure: () => void;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const t = useT();
  const qc = useQueryClient();
  const { jobs } = useAgentJobs();
  const [internalOpen, setInternalOpen] = useState(false);
  const open = externalOpen ?? internalOpen;
  const setOpen = onOpenChange ?? setInternalOpen;
  const [job, setJob] = useState<Schemas["ServerAgentInstallJob"] | null>(null);
  const tracked = jobs.filter((entry) => entry.kind === "install" && entry.serverId === server.id).sort((a, b) => a.changedAt - b.changedAt).at(-1);
  const visibleJob = tracked?.job ?? job;
  const updateJob = useCallback((next: Schemas["ServerAgentInstallJob"]) => {
    setJob(next);
    onJob(next);
    if (next.status === "succeeded") void qc.invalidateQueries({ queryKey: ["server-agent", server.id] });
  }, [onJob, qc, server.id]);
  const active = visibleJob?.status === "queued" || visibleJob?.status === "running";
  return (
    <>
      <Button size="sm" variant="outlined" onClick={() => setOpen(true)} disabled={visibleJob?.status === "succeeded" || (otherOperationActive && !active)}>
        {active ? t("servers.agentInstallViewProgress") : t("servers.agentInstallAction")}
      </Button>
      {active && !open && <span role="status" className="text-xs text-muted">{t("servers.agentInstallContinues")}</span>}
      {visibleJob?.status === "succeeded" && <span role="status" className="text-xs text-ok">{t("servers.agentInstallSuccessUnverified")}</span>}
      {visibleJob?.status === "failed" && <span role="alert" className="text-xs text-bad">{t("servers.agentInstallFailedCheckHost")}</span>}
      {otherOperationActive && <span role="status" className="text-xs text-muted">{t("servers.agentUpdateSSHBusy")}</span>}
      <AgentInstallDialog server={server} open={open} onClose={() => setOpen(false)} job={visibleJob} onJob={updateJob} otherOperationActive={otherOperationActive} onStart={onStart} onFailure={onFailure} />
    </>
  );
}

function AgentUpdateControl({ server, busy, ownActive, onJob, onStart, onFailure }: {
  server: Schemas["Server"];
  busy: boolean;
  ownActive: boolean;
  onJob: (job: Schemas["ServerAgentInstallJob"]) => void;
  onStart: () => void;
  onFailure: () => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const { jobs } = useAgentJobs();
  const tracked = jobs.filter((entry) => entry.kind === "update" && entry.serverId === server.id).sort((a, b) => a.changedAt - b.changedAt).at(-1);
  const active = ownActive || tracked?.job.status === "queued" || tracked?.job.status === "running";
  return <>
    <Button size="sm" variant="outlined" onClick={() => setOpen(true)} disabled={busy}>
      {active ? t("servers.agentUpdateSSHViewProgress") : t("servers.agentUpdateSSHAction")}
    </Button>
    {busy && <span role="status" className="text-xs text-muted">{t("servers.agentUpdateSSHBusy")}</span>}
    <AgentUpdateDialog server={server} open={open} onClose={() => setOpen(false)} onJob={onJob} otherOperationActive={busy} onStart={onStart} onFailure={onFailure} initialJob={tracked?.job ?? null} />
  </>;
}

function AgentUninstallControl({ server, busy, ownActive, onJob, onStart, onFailure }: {
  server: Schemas["Server"];
  busy: boolean;
  ownActive: boolean;
  onJob: (job: Schemas["ServerAgentInstallJob"]) => void;
  onStart: () => void;
  onFailure: () => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const { jobs } = useAgentJobs();
  const tracked = jobs.filter((entry) => entry.kind === "uninstall" && entry.serverId === server.id).sort((a, b) => a.changedAt - b.changedAt).at(-1);
  const active = ownActive || tracked?.job.status === "queued" || tracked?.job.status === "running";
  return <>
    <Button size="sm" variant="outlined" className="text-bad hover:bg-bad/10 hover:border-bad/40" onClick={() => setOpen(true)} disabled={busy}>
      <Icon icon={Trash2} size="xs" />
      {active ? t("servers.agentUninstallProgress") : t("servers.agentUninstallAction")}
    </Button>
    {busy && <span role="status" className="text-xs text-muted">{t("servers.agentUpdateSSHBusy")}</span>}
    <AgentUninstallDialog server={server} open={open} onClose={() => setOpen(false)} onJob={onJob} otherOperationActive={busy} onStart={onStart} onFailure={onFailure} initialJob={tracked?.job ?? null} />
  </>;
}

interface AgentJobLogEntry {
  id: string;
  timestamp: string;
  level: "info" | "stage" | "success" | "error" | "warn";
  message: string;
}

function computeAgentJobProgress(status?: string, stage?: string, isPending?: boolean): number {
  if (status === "succeeded" || status === "failed") return 100;
  if (isPending) return 5;
  if (!status || status === "queued") return 10;
  switch (stage) {
    case "validating":
      return 20;
    case "connecting":
      return 40;
    case "transferring":
      return 65;
    case "activating":
      return 85;
    case "registering":
      return 95;
    case "stopping":
      return 35;
    case "removing":
      return 65;
    case "cleaning":
      return 90;
    case "complete":
      return 100;
    default:
      return 50;
  }
}

function getAgentJobStageLabel(status: string | undefined, stage: string | undefined, isPending: boolean, t: ReturnType<typeof useT>): string {
  if (isPending) return t("servers.agentInstallSubmitting");
  if (status === "succeeded") return t("servers.agentJobStageComplete");
  if (status === "failed") return t("servers.agentJobStageFailed");
  if (status === "queued") return t("servers.agentJobStageQueued");
  switch (stage) {
    case "validating":
      return t("servers.agentJobStageValidating");
    case "connecting":
      return t("servers.agentJobStageConnecting");
    case "transferring":
      return t("servers.agentJobStageTransferring");
    case "activating":
      return t("servers.agentJobStageActivating");
    case "registering":
      return t("servers.agentJobStageRegistering");
    case "stopping":
      return t("servers.agentJobStageStopping");
    case "removing":
      return t("servers.agentJobStageRemoving");
    case "cleaning":
      return t("servers.agentJobStageCleaning");
    case "complete":
      return t("servers.agentJobStageComplete");
    default:
      return t("servers.agentInstallRunning");
  }
}

function AgentJobProgressBar({
  status,
  stage,
  isPending,
}: {
  status?: string;
  stage?: string;
  isPending: boolean;
}) {
  const t = useT();
  const percent = computeAgentJobProgress(status, stage, isPending);
  const stageLabel = getAgentJobStageLabel(status, stage, isPending, t);
  const isRunning = isPending || status === "queued" || status === "running";

  return (
    <div
      role="progressbar"
      aria-valuenow={percent}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-label={t("servers.agentProgressBarLabel")}
      className="flex flex-col gap-1.5"
    >
      <div className="flex items-center justify-between text-xs font-medium">
        <span className="text-muted">{stageLabel}</span>
        <span className="font-mono text-muted tabular-nums">{percent}%</span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded-full bg-surface-3">
        <div
          className={cn(
            "h-full rounded-full transition-all duration-500 ease-out",
            status === "succeeded"
              ? "bg-ok"
              : status === "failed"
              ? "bg-bad"
              : "bg-primary",
            isRunning && "animate-pulse",
          )}
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  );
}

function useAgentJobLogs({
  job,
  isPending,
  kind,
  outcomeUnknown,
  t,
}: {
  job: Schemas["ServerAgentInstallJob"] | null | undefined;
  isPending: boolean;
  kind: "install" | "update" | "uninstall";
  outcomeUnknown?: boolean;
  t: ReturnType<typeof useT>;
}) {
  const [logs, setLogs] = useState<AgentJobLogEntry[]>([]);
  const seenStagesRef = useRef<Set<string>>(new Set());
  const seenStatusesRef = useRef<Set<string>>(new Set());
  const pendingLoggedRef = useRef(false);
  const unknownLoggedRef = useRef(false);
  const lastMessageRef = useRef<string | null>(null);

  useEffect(() => {
    if (!job && !isPending) {
      setLogs([]);
      seenStagesRef.current.clear();
      seenStatusesRef.current.clear();
      pendingLoggedRef.current = false;
      unknownLoggedRef.current = false;
      lastMessageRef.current = null;
    }
  }, [job, isPending]);

  useEffect(() => {
    if (isPending && !pendingLoggedRef.current) {
      pendingLoggedRef.current = true;
      const now = new Date().toLocaleTimeString("en-GB", { hour12: false });
      setLogs((prev) => [
        ...prev,
        {
          id: `start-${Date.now()}`,
          timestamp: now,
          level: "info",
          message:
            kind === "uninstall"
              ? t("servers.agentLogStartingUninstall")
              : kind === "update"
              ? t("servers.agentLogStartingUpdate")
              : t("servers.agentLogStartingInstall"),
        },
      ]);
    }
  }, [isPending, kind, t]);

  useEffect(() => {
    if (!job) return;
    const now = new Date().toLocaleTimeString("en-GB", { hour12: false });
    const newEntries: AgentJobLogEntry[] = [];

    if (job.status === "queued" && !seenStatusesRef.current.has("queued")) {
      seenStatusesRef.current.add("queued");
      newEntries.push({
        id: `queued-${job.id}`,
        timestamp: now,
        level: "info",
        message: t("servers.agentLogJobQueued", { id: job.id }),
      });
    }

    const stageOrder = [
      "validating", "connecting", "transferring", "activating", "registering",
      "stopping", "removing", "cleaning", "complete",
    ];
    const stageMessages: Record<string, () => string> = {
      validating: () => t("servers.agentLogStageValidating"),
      connecting: () => t("servers.agentLogStageConnecting"),
      transferring: () => t("servers.agentLogStageTransferring"),
      activating: () => t("servers.agentLogStageActivating"),
      registering: () => t("servers.agentLogStageRegistering"),
      stopping: () => t("servers.agentLogStageStopping"),
      removing: () => t("servers.agentLogStageRemoving"),
      cleaning: () => t("servers.agentLogStageCleaning"),
    };

    if (job.stage && job.stage !== "failed") {
      const targetIdx = stageOrder.indexOf(job.stage);
      if (targetIdx >= 0) {
        for (let i = 0; i <= targetIdx; i++) {
          const s = stageOrder[i]!;
          if (!seenStagesRef.current.has(s) && stageMessages[s]) {
            seenStagesRef.current.add(s);
            newEntries.push({
              id: `stage-${s}-${job.id}`,
              timestamp: now,
              level: "stage",
              message: stageMessages[s](),
            });
          }
        }
      }
    }

    if (job.message && job.message !== lastMessageRef.current && job.status !== "failed") {
      lastMessageRef.current = job.message;
      newEntries.push({
        id: `msg-${Date.now()}`,
        timestamp: now,
        level: "info",
        message: job.message,
      });
    }

    if (job.status === "succeeded" && !seenStatusesRef.current.has("succeeded")) {
      seenStatusesRef.current.add("succeeded");
      newEntries.push({
        id: `success-${job.id}`,
        timestamp: now,
        level: "success",
        message:
          kind === "uninstall"
            ? t("servers.agentLogUninstallSucceeded")
            : kind === "update"
            ? t("servers.agentLogUpdateSucceeded")
            : t("servers.agentLogInstallSucceeded"),
      });
    }

    if (job.status === "failed" && !seenStatusesRef.current.has("failed")) {
      seenStatusesRef.current.add("failed");
      const failMsg = job.message
        ? kind === "update"
          ? agentUpdateFailureMessage(job.message, t)
          : job.message
        : t("servers.agentLogFailed");
      newEntries.push({
        id: `failed-${job.id}`,
        timestamp: now,
        level: "error",
        message: failMsg,
      });
    }

    if (newEntries.length > 0) {
      setLogs((prev) => [...prev, ...newEntries]);
    }
  }, [job, kind, t]);

  useEffect(() => {
    if (outcomeUnknown && !unknownLoggedRef.current) {
      unknownLoggedRef.current = true;
      const now = new Date().toLocaleTimeString("en-GB", { hour12: false });
      setLogs((prev) => [
        ...prev,
        {
          id: `unknown-${Date.now()}`,
          timestamp: now,
          level: "warn",
          message: t("servers.agentLogOutcomeUnknown"),
        },
      ]);
    }
  }, [outcomeUnknown, t]);

  return logs;
}

function AgentJobLogConsole({
  logs,
  isRunning,
  status,
}: {
  logs: AgentJobLogEntry[];
  isRunning: boolean;
  status?: string;
}) {
  const t = useT();
  const scrollRef = useRef<HTMLDivElement>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs]);

  const copyLogs = useCallback(() => {
    const text = logs
      .map((entry) => `[${entry.timestamp}] [${entry.level.toUpperCase()}] ${entry.message}`)
      .join("\n");
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(text).then(() => {
        setCopied(true);
        window.setTimeout(() => setCopied(false), 2000);
      }).catch(() => undefined);
    }
  }, [logs]);

  return (
    <div className="flex flex-col overflow-hidden rounded-m3-lg border border-surface-3 bg-surface-base font-mono text-xs">
      <div className="flex items-center justify-between border-b border-surface-3 bg-surface-2/60 px-3 py-1.5 text-muted">
        <div className="flex items-center gap-2">
          <Icon icon={Terminal} size="xs" />
          <span className="font-sans font-medium text-surface-on">{t("servers.agentLogConsoleTitle")}</span>
          <span
            aria-hidden
            className={cn(
              "size-2 rounded-full",
              status === "succeeded"
                ? "bg-ok"
                : status === "failed"
                ? "bg-bad"
                : isRunning
                ? "animate-pulse bg-primary"
                : "bg-surface-3",
            )}
          />
        </div>
        {logs.length > 0 && (
          <button
            type="button"
            onClick={copyLogs}
            className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-sans hover:bg-surface-3 hover:text-surface-on"
            title={t("servers.agentLogCopy")}
          >
            {copied ? (
              <>
                <Icon icon={Check} size="xs" className="text-ok" />
                <span className="text-ok">{t("servers.agentLogCopied")}</span>
              </>
            ) : (
              <>
                <Icon icon={Copy} size="xs" />
                <span>{t("servers.agentLogCopy")}</span>
              </>
            )}
          </button>
        )}
      </div>
      <div
        ref={scrollRef}
        role="log"
        aria-live="polite"
        className="max-h-40 min-h-24 overflow-y-auto p-3 space-y-1.5 text-surface-on select-text"
      >
        {logs.length === 0 ? (
          <span className="text-muted/60">{t("servers.agentLogConsoleEmpty")}</span>
        ) : (
          logs.map((entry) => (
            <div key={entry.id} className="flex items-start gap-2 leading-relaxed break-all">
              <span className="text-muted/70 tabular-nums shrink-0">[{entry.timestamp}]</span>
              <span
                className={cn(
                  "font-semibold shrink-0 uppercase text-[10px] px-1 py-0.5 rounded leading-none",
                  entry.level === "stage" && "bg-primary/15 text-primary",
                  entry.level === "success" && "bg-ok/15 text-ok",
                  entry.level === "error" && "bg-bad/15 text-bad",
                  entry.level === "warn" && "bg-warn/15 text-warn",
                  entry.level === "info" && "bg-surface-3/50 text-muted",
                )}
              >
                {entry.level}
              </span>
              <span
                className={cn(
                  entry.level === "error"
                    ? "text-bad font-medium"
                    : entry.level === "success"
                    ? "text-ok font-medium"
                    : "text-surface-on",
                )}
              >
                {entry.message}
              </span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

function AgentUpdateDialog({ server, open, onClose, onJob, otherOperationActive, onStart, onFailure, initialJob }: {
  server: Schemas["Server"];
  open: boolean;
  onClose: () => void;
  onJob: (job: Schemas["ServerAgentInstallJob"]) => void;
  otherOperationActive: boolean;
  onStart: () => void;
  onFailure: () => void;
  initialJob: Schemas["ServerAgentInstallJob"] | null;
}) {
  const t = useT();
  const { jobs, registerJob } = useAgentJobs();
  const [job, setJob] = useState<Schemas["ServerAgentInstallJob"] | null>(initialJob);
  const tracked = jobs.find((entry) => entry.kind === "update" && entry.serverId === server.id && entry.job.id === job?.id);
  const snapshot = tracked?.job ?? job;
  const [port, setPort] = useState("22");
  const passwordInputRef = useRef<HTMLInputElement>(null);
  const pendingPasswordRef = useRef("");
  const [acceptedAt, setAcceptedAt] = useState<number | null>(null);
  const secureBrowser = location.protocol === "https:";
  const httpsRedirect = useHTTPSRedirect(passwordInputRef, pendingPasswordRef);
  const updateJob = useCallback((next: Schemas["ServerAgentInstallJob"]) => {
    setJob(next);
    onJob(next);
  }, [onJob]);
  const start = useMutation({
    mutationFn: async ({ port, password }: { port: number; password: string }) => {
      return unwrap(await api.POST("/api/v1/servers/{serverId}/agent/update-ssh", {
        params: { path: { serverId: server.id } },
        body: { ssh_port: port, ssh_password: password },
      }));
    },
    onSuccess: (next) => {
      setAcceptedAt(next.status === "queued" || next.status === "running" ? performance.now() : null);
      updateJob(next);
      registerJob({ kind: "update", serverId: server.id, serverName: server.name, job: next });
    },
    onError: () => { pendingPasswordRef.current = ""; onFailure(); },
  });
  const pollBudgetExpired = tracked?.outcome === "unknown";
  const submitted = !!job?.id || start.isPending;
  const timerActive = (snapshot?.status === "queued" || snapshot?.status === "running") && !pollBudgetExpired;
  const elapsedSeconds = useElapsedJob(acceptedAt, timerActive);
  const logs = useAgentJobLogs({ job: snapshot, isPending: start.isPending, kind: "update", outcomeUnknown: pollBudgetExpired, t });
  const close = () => {
    if (start.isPending) return;
    httpsRedirect.cancel();
    pendingPasswordRef.current = "";
    if (passwordInputRef.current) passwordInputRef.current.value = "";
    onClose();
  };
  useEffect(() => () => {
    pendingPasswordRef.current = "";
    if (passwordInputRef.current) passwordInputRef.current.value = "";
  }, []);

  if (!open) return null;
  return <Modal title={t("servers.agentUpdateSSHTitle", { server: server.name })} onClose={close}>
    {!submitted ? <form className="flex flex-col gap-4" aria-label={t("servers.agentUpdateSSHForm")} onSubmit={(event: FormEvent) => {
      event.preventDefault();
      if (!secureBrowser || otherOperationActive) return;
      const password = passwordInputRef.current?.value ?? "";
      if (!password) return;
      if (passwordInputRef.current) passwordInputRef.current.value = "";
      pendingPasswordRef.current = "";
      onStart();
      start.mutate({ port: Number(port), password });
    }}>
      <p className="rounded-m3-lg bg-warn/10 p-3 text-sm">{t("servers.agentUpdateSSHScope")}</p>
      {!secureBrowser && <div className="flex flex-col gap-2">
        <p role="alert" className="rounded-m3-lg bg-bad/10 p-3 text-sm text-bad">{t("servers.agentInstallHttpsRequired")}</p>
        <Button type="button" variant="outlined" disabled={httpsRedirect.pending} onClick={() => { void httpsRedirect.redirect(); }}>
          {httpsRedirect.pending ? t("servers.agentHttpsRedirectChecking") : t("servers.agentHttpsRedirectAction")}
        </Button>
        {httpsRedirect.failed && <p role="alert" className="text-sm text-bad">{t("servers.agentHttpsRedirectFailed")}</p>}
      </div>}
      <p className="text-sm text-muted">{t("servers.agentUpdateSSHHostFixed")}</p>
      <p className="rounded-m3-lg bg-warn/10 p-3 text-sm">{t("servers.agentSSHHostKeyTOFU")}</p>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("servers.agentInstallPortLabel")}><TextInput required aria-label={t("servers.agentInstallPortLabel")} type="number" min={1} max={65535} step={1} value={port} onChange={(event) => setPort(event.target.value)} autoComplete="off" /></Field>
        <Field label={t("servers.agentInstallPasswordLabel")} hint={t("servers.agentInstallPasswordHint")}>
          <input ref={passwordInputRef} required aria-label={t("servers.agentInstallPasswordLabel")} type="password" className="h-12 w-full rounded-m3-md border border-transparent bg-surface-2 px-3 text-sm focus-visible:outline-2 focus-visible:outline-primary" autoComplete="off" maxLength={4096} />
        </Field>
      </div>
      {start.error && <p role="alert" className="text-sm text-bad">{agentUpdateStartError(start.error, t)}</p>}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="text" onClick={close}>{t("common.cancel")}</Button>
        <Button type="submit" variant="primary" disabled={start.isPending || otherOperationActive || !isValidSSHPort(port) || !secureBrowser}>{t("servers.agentUpdateSSHSubmit")}</Button>
      </div>
    </form> : <section aria-label={t("servers.agentUpdateSSHProgress")} className="flex flex-col gap-4">
      {start.isPending ? <JobBusyState label={t("servers.agentStarting")} /> : <p aria-live="polite">{installJobStatusText(snapshot?.status, snapshot?.stage, t)}</p>}
      <AgentJobProgressBar status={snapshot?.status} stage={snapshot?.stage} isPending={start.isPending} />
      {acceptedAt != null && <JobBusyState busy={timerActive} elapsed={elapsedSeconds} label={t("servers.agentElapsed", { elapsed: formatElapsed(elapsedSeconds) })} />}
      <AgentJobLogConsole logs={logs} isRunning={start.isPending || timerActive} status={snapshot?.status} />
      {snapshot?.status === "succeeded" && !pollBudgetExpired && <p role="status" className="rounded-m3-lg bg-ok/10 p-3 text-sm">{t("servers.agentUpdateSSHSuccess")}</p>}
      {snapshot?.status === "failed" && <p role="alert" className="text-sm text-bad">{agentUpdateFailureMessage(snapshot.message, t)}</p>}
      {isRetryableAgentUpdateFailure(snapshot) && <Button className="self-end" variant="outlined" onClick={() => {
        setJob(null);
        setAcceptedAt(null);
        onStart();
      }}>{t("servers.agentUpdateSSHFreshPasswordRetry")}</Button>}
      {pollBudgetExpired && <p role="alert" className="text-sm text-warn">{t("servers.agentInstallOutcomeUnknown")}</p>}
      {(snapshot?.status === "queued" || snapshot?.status === "running") && <p className="text-sm text-muted">{t("servers.agentInstallCloseDoesNotCancel")}</p>}
      <Button className="self-end" onClick={close}>{t("common.close")}</Button>
    </section>}
  </Modal>;
}

function AgentInstallDialog({
  server,
  open,
  onClose,
  job,
  onJob,
  otherOperationActive,
  onStart,
  onFailure,
}: {
  server: Schemas["Server"];
  open: boolean;
  onClose: () => void;
  job: Schemas["ServerAgentInstallJob"] | null;
  onJob: (job: Schemas["ServerAgentInstallJob"]) => void;
  otherOperationActive: boolean;
  onStart: () => void;
  onFailure: () => void;
}) {
  const t = useT();
  const { jobs, registerJob } = useAgentJobs();
  const tracked = jobs.find((entry) => entry.kind === "install" && entry.serverId === server.id && entry.job.id === job?.id);
  const snapshot = tracked?.job ?? job;
  const outcomeUnknown = tracked?.outcome === "unknown";
  const [form, setForm] = useState({ host: "", port: "22", confirmed: false });
  const passwordInputRef = useRef<HTMLInputElement>(null);
  const pendingPasswordRef = useRef("");
  const [acceptedAt, setAcceptedAt] = useState<number | null>(null);
  const secureBrowser = location.protocol === "https:";
  const httpsRedirect = useHTTPSRedirect(passwordInputRef, pendingPasswordRef);
  const start = useMutation({
    mutationFn: async ({ host, port, password }: { host: string; port: number; password: string }) => {
      const body: Schemas["ServerAgentInstallRequest"] = {
        ssh_host: host.trim(),
        ssh_port: port,
        ssh_password: password,
      };
      return unwrap(await api.POST("/api/v1/servers/{serverId}/agent/install", { params: { path: { serverId: server.id } }, body }));
    },
    onSuccess: (next) => {
      setAcceptedAt(next.status === "queued" || next.status === "running" ? performance.now() : null);
      onJob(next);
      registerJob({ kind: "install", serverId: server.id, serverName: server.name, job: next });
    },
    onError: () => { pendingPasswordRef.current = ""; onFailure(); },
  });
  const hasSubmitted = !!job?.id || start.isPending;
  const polling = snapshot?.status === "queued" || snapshot?.status === "running";
  const timerActive = polling && !outcomeUnknown;
  const elapsedSeconds = useElapsedJob(acceptedAt, timerActive);
  const logs = useAgentJobLogs({ job: snapshot, isPending: start.isPending, kind: "install", outcomeUnknown, t });
  const close = () => {
    if (start.isPending) return;
    httpsRedirect.cancel();
    pendingPasswordRef.current = "";
    if (passwordInputRef.current) passwordInputRef.current.value = "";
    onClose();
  };
  useEffect(() => () => {
    pendingPasswordRef.current = "";
    if (passwordInputRef.current) passwordInputRef.current.value = "";
  }, []);

  if (!open) return null;
  return (
    <Modal title={t("servers.agentInstallTitle", { server: server.name })} onClose={close}>
      {!hasSubmitted ? (
        <form
          className="flex flex-col gap-4"
          aria-label={t("servers.agentInstallForm")}
          onSubmit={(event: FormEvent) => {
            event.preventDefault();
            if (!secureBrowser || otherOperationActive) return;
            const password = passwordInputRef.current?.value ?? "";
            if (!password) return;
            if (passwordInputRef.current) passwordInputRef.current.value = "";
            pendingPasswordRef.current = "";
            onStart();
            start.mutate({ host: form.host, port: Number(form.port), password });
          }}
        >
          {!secureBrowser && (
            <div className="flex flex-col gap-2">
              <p role="alert" className="rounded-m3-lg bg-bad/10 p-3 text-sm text-bad">{t("servers.agentInstallHttpsRequired")}</p>
              <Button type="button" variant="outlined" disabled={httpsRedirect.pending} onClick={() => { void httpsRedirect.redirect(); }}>
                {httpsRedirect.pending ? t("servers.agentHttpsRedirectChecking") : t("servers.agentHttpsRedirectAction")}
              </Button>
              {httpsRedirect.failed && <p role="alert" className="text-sm text-bad">{t("servers.agentHttpsRedirectFailed")}</p>}
            </div>
          )}
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("servers.agentInstallHostLabel")} hint={t("servers.agentInstallHostHint")}>
              <TextInput required aria-label={t("servers.agentInstallHostLabel")} value={form.host} onChange={(event) => setForm({ ...form, host: event.target.value })} autoComplete="off" inputMode="decimal" placeholder="10.20.0.11" />
            </Field>
            <Field label={t("servers.agentInstallPortLabel")}>
              <TextInput required aria-label={t("servers.agentInstallPortLabel")} type="number" min={1} max={65535} step={1} value={form.port} onChange={(event) => setForm({ ...form, port: event.target.value })} autoComplete="off" />
            </Field>
            <Field label={t("servers.agentInstallPasswordLabel")} hint={t("servers.agentInstallPasswordHint")}>
              <input ref={passwordInputRef} required aria-label={t("servers.agentInstallPasswordLabel")} type="password" className="h-12 w-full rounded-m3-md border border-transparent bg-surface-2 px-3 text-sm focus-visible:outline-2 focus-visible:outline-primary aria-[invalid=true]:border-bad" autoComplete="off" maxLength={4096} />
            </Field>
          </div>
          <label className="flex items-start gap-3 rounded-m3-lg bg-surface-2 p-3 text-sm">
            <input type="checkbox" checked={form.confirmed} onChange={(event) => setForm({ ...form, confirmed: event.target.checked })} />
            <span>{t("servers.agentInstallConfirmScope")}</span>
          </label>
          {start.error && <p role="alert" className="text-sm text-bad">{agentInstallStartError(start.error, t)}</p>}
          <div className="flex flex-wrap justify-end gap-2">
            <Button type="button" variant="text" onClick={close}>{t("common.cancel")}</Button>
            <Button type="submit" variant="primary" disabled={start.isPending || otherOperationActive || !form.confirmed || !isCanonicalIPv4(form.host.trim()) || !isValidSSHPort(form.port) || !secureBrowser}>
              {start.isPending ? t("servers.agentInstallSubmitting") : t("servers.agentInstallSubmit")}
            </Button>
          </div>
        </form>
      ) : (
        <section aria-label={t("servers.agentInstallProgress")} className="flex flex-col gap-4">
          {start.isPending ? <JobBusyState label={t("servers.agentStarting")} /> : <p aria-live="polite" className="text-sm">{installJobStatusText(snapshot?.status, snapshot?.stage, t)}</p>}
          <AgentJobProgressBar status={snapshot?.status} stage={snapshot?.stage} isPending={start.isPending} />
          {acceptedAt != null && <JobBusyState busy={timerActive} elapsed={elapsedSeconds} label={t("servers.agentElapsed", { elapsed: formatElapsed(elapsedSeconds) })} />}
          <AgentJobLogConsole logs={logs} isRunning={start.isPending || timerActive} status={snapshot?.status} />
          {snapshot?.status === "succeeded" && (
            <p role="status" className="rounded-m3-lg bg-ok/10 p-3 text-sm text-ok font-medium">
              {t("servers.agentInstallSuccessUnverified")}
            </p>
          )}
          {snapshot?.status === "failed" && <p role="alert" className="rounded-m3-lg bg-bad/10 p-3 text-sm text-bad">{t("servers.agentInstallFailedCheckHost")}</p>}
          {outcomeUnknown && <p role="alert" className="text-sm text-warn">{t("servers.agentInstallOutcomeUnknown")}</p>}
          {(snapshot?.status === "queued" || snapshot?.status === "running") && <p className="text-sm text-muted">{t("servers.agentInstallCloseDoesNotCancel")}</p>}
          <Button className="self-end" onClick={close}>{t("common.close")}</Button>
        </section>
      )}
    </Modal>
  );
}

function AgentUninstallDialog({
  server,
  open,
  onClose,
  onJob,
  otherOperationActive,
  onStart,
  onFailure,
  initialJob,
}: {
  server: Schemas["Server"];
  open: boolean;
  onClose: () => void;
  onJob: (job: Schemas["ServerAgentInstallJob"]) => void;
  otherOperationActive: boolean;
  onStart: () => void;
  onFailure: () => void;
  initialJob: Schemas["ServerAgentInstallJob"] | null;
}) {
  const t = useT();
  const qc = useQueryClient();
  const { jobs, registerJob } = useAgentJobs();
  const [job, setJob] = useState<Schemas["ServerAgentInstallJob"] | null>(initialJob);
  const tracked = jobs.find((entry) => entry.kind === "uninstall" && entry.serverId === server.id && entry.job.id === job?.id);
  const snapshot = tracked?.job ?? job;
  const [port, setPort] = useState("22");
  const passwordInputRef = useRef<HTMLInputElement>(null);
  const pendingPasswordRef = useRef("");
  const [acceptedAt, setAcceptedAt] = useState<number | null>(null);
  const secureBrowser = location.protocol === "https:";
  const httpsRedirect = useHTTPSRedirect(passwordInputRef, pendingPasswordRef);

  const updateJob = useCallback((next: Schemas["ServerAgentInstallJob"]) => {
    setJob(next);
    onJob(next);
    if (next.status === "succeeded") {
      void qc.invalidateQueries({ queryKey: ["server-agent", server.id] });
    }
  }, [onJob, qc, server.id]);

  const start = useMutation({
    mutationFn: async ({ port, password }: { port: number; password: string }) => {
      return unwrap(await api.POST("/api/v1/servers/{serverId}/agent/uninstall", {
        params: { path: { serverId: server.id } },
        body: { ssh_port: port, ssh_password: password },
      }));
    },
    onSuccess: (next) => {
      setAcceptedAt(next.status === "queued" || next.status === "running" ? performance.now() : null);
      updateJob(next);
      registerJob({ kind: "uninstall", serverId: server.id, serverName: server.name, job: next });
    },
    onError: () => {
      pendingPasswordRef.current = "";
      onFailure();
    },
  });

  const pollBudgetExpired = tracked?.outcome === "unknown";
  const submitted = !!job?.id || start.isPending;
  const timerActive = (snapshot?.status === "queued" || snapshot?.status === "running") && !pollBudgetExpired;
  const elapsedSeconds = useElapsedJob(acceptedAt, timerActive);
  const logs = useAgentJobLogs({ job: snapshot, isPending: start.isPending, kind: "uninstall", outcomeUnknown: pollBudgetExpired, t });

  const close = () => {
    if (start.isPending) return;
    httpsRedirect.cancel();
    pendingPasswordRef.current = "";
    if (passwordInputRef.current) passwordInputRef.current.value = "";
    if (snapshot?.status === "succeeded") {
      void qc.invalidateQueries({ queryKey: ["server-agent", server.id] });
    }
    onClose();
  };

  useEffect(() => () => {
    pendingPasswordRef.current = "";
    if (passwordInputRef.current) passwordInputRef.current.value = "";
  }, []);

  if (!open) return null;
  return (
    <Modal title={t("servers.agentUninstallTitle", { server: server.name })} onClose={close}>
      {!submitted ? (
        <form
          className="flex flex-col gap-4"
          aria-label={t("servers.agentUninstallTitle", { server: server.name })}
          onSubmit={(event: FormEvent) => {
            event.preventDefault();
            if (!secureBrowser || otherOperationActive) return;
            const password = passwordInputRef.current?.value ?? "";
            if (!password) return;
            if (passwordInputRef.current) passwordInputRef.current.value = "";
            pendingPasswordRef.current = "";
            onStart();
            start.mutate({ port: Number(port), password });
          }}
        >
          <p className="rounded-m3-lg bg-bad/10 p-3 text-sm text-bad font-medium">{t("servers.agentUninstallScope")}</p>
          {!secureBrowser && (
            <div className="flex flex-col gap-2">
              <p role="alert" className="rounded-m3-lg bg-bad/10 p-3 text-sm text-bad">{t("servers.agentInstallHttpsRequired")}</p>
              <Button type="button" variant="outlined" disabled={httpsRedirect.pending} onClick={() => { void httpsRedirect.redirect(); }}>
                {httpsRedirect.pending ? t("servers.agentHttpsRedirectChecking") : t("servers.agentHttpsRedirectAction")}
              </Button>
              {httpsRedirect.failed && <p role="alert" className="text-sm text-bad">{t("servers.agentHttpsRedirectFailed")}</p>}
            </div>
          )}
          <p className="text-sm text-muted">{t("servers.agentUpdateSSHHostFixed")}</p>
          <p className="rounded-m3-lg bg-warn/10 p-3 text-sm">{t("servers.agentSSHHostKeyTOFU")}</p>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("servers.agentInstallPortLabel")}>
              <TextInput required aria-label={t("servers.agentInstallPortLabel")} type="number" min={1} max={65535} step={1} value={port} onChange={(event) => setPort(event.target.value)} autoComplete="off" />
            </Field>
            <Field label={t("servers.agentInstallPasswordLabel")} hint={t("servers.agentInstallPasswordHint")}>
              <input ref={passwordInputRef} required aria-label={t("servers.agentInstallPasswordLabel")} type="password" className="h-12 w-full rounded-m3-md border border-transparent bg-surface-2 px-3 text-sm focus-visible:outline-2 focus-visible:outline-primary" autoComplete="off" maxLength={4096} />
            </Field>
          </div>
          {start.error && <p role="alert" className="text-sm text-bad">{t("servers.agentUninstallFailed")}</p>}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="text" onClick={close}>{t("common.cancel")}</Button>
            <Button type="submit" variant="primary" className="bg-bad text-surface-base hover:bg-bad/90" disabled={start.isPending || otherOperationActive || !isValidSSHPort(port) || !secureBrowser}>
              {t("servers.agentUninstallSubmit")}
            </Button>
          </div>
        </form>
      ) : (
        <section aria-label={t("servers.agentUninstallProgress")} className="flex flex-col gap-4">
          {start.isPending ? <JobBusyState label={t("servers.agentStarting")} /> : <p aria-live="polite" className="text-sm">{installJobStatusText(snapshot?.status, snapshot?.stage, t)}</p>}
          <AgentJobProgressBar status={snapshot?.status} stage={snapshot?.stage} isPending={start.isPending} />
          {acceptedAt != null && <JobBusyState busy={timerActive} elapsed={elapsedSeconds} label={t("servers.agentElapsed", { elapsed: formatElapsed(elapsedSeconds) })} />}
          <AgentJobLogConsole logs={logs} isRunning={start.isPending || timerActive} status={snapshot?.status} />
          {snapshot?.status === "succeeded" && !pollBudgetExpired && (
            <p role="status" className="rounded-m3-lg bg-ok/10 p-3 text-sm text-ok font-medium">{t("servers.agentUninstallSuccess")}</p>
          )}
          {snapshot?.status === "failed" && (
            <p role="alert" className="text-sm text-bad">{snapshot.message ?? t("servers.agentUninstallFailed")}</p>
          )}
          {pollBudgetExpired && <p role="alert" className="text-sm text-warn">{t("servers.agentInstallOutcomeUnknown")}</p>}
          {(snapshot?.status === "queued" || snapshot?.status === "running") && (
            <p className="text-sm text-muted">{t("servers.agentInstallCloseDoesNotCancel")}</p>
          )}
          <Button className="self-end" onClick={close}>{t("common.close")}</Button>
        </section>
      )}
    </Modal>
  );
}

function isCanonicalIPv4(value: string) {
  const octets = value.split(".");
  return octets.length === 4 && octets.every((octet) => /^\d{1,3}$/.test(octet) && String(Number(octet)) === octet && Number(octet) <= 255);
}

function useHTTPSRedirect(passwordInputRef: { current: HTMLInputElement | null }, pendingPasswordRef: { current: string }) {
  const [pending, setPending] = useState(false);
  const [failed, setFailed] = useState(false);
  const controllerRef = useRef<AbortController | null>(null);

  const cancel = useCallback(() => {
    const controller = controllerRef.current;
    controllerRef.current = null;
    controller?.abort();
    pendingPasswordRef.current = "";
    if (passwordInputRef.current) passwordInputRef.current.value = "";
    setPending(false);
  }, [passwordInputRef, pendingPasswordRef]);

  const redirect = useCallback(async () => {
    if (controllerRef.current) return;
    const controller = new AbortController();
    controllerRef.current = controller;
    setPending(true);
    setFailed(false);
    pendingPasswordRef.current = "";
    if (passwordInputRef.current) passwordInputRef.current.value = "";
    const timeout = window.setTimeout(() => controller.abort(), 5000);
    const aborted = new Promise<never>((_resolve, reject) => {
      const rejectAbort = () => reject(new DOMException("aborted", "AbortError"));
      if (controller.signal.aborted) rejectAbort();
      else controller.signal.addEventListener("abort", rejectAbort, { once: true });
    });

    try {
      const request = new Request(new URL("/.well-known/openvms-https-port", location.origin), {
        method: "GET",
        credentials: "omit",
        redirect: "error",
        cache: "no-store",
        signal: controller.signal,
      });
      const response = await Promise.race([fetch(request), aborted]);
      if (!response.ok) throw new Error("HTTPS port unavailable");
      const port = await Promise.race([readConfiguredHTTPSPort(response), aborted]);
      if (controller.signal.aborted || controllerRef.current !== controller) return;

      const target = new URL(location.href);
      target.protocol = "https:";
      target.port = String(port);
      target.username = "";
      target.password = "";
      target.search = "";
      target.hash = "";
      location.assign(target.toString());
    } catch {
      if (controllerRef.current === controller) setFailed(true);
    } finally {
      window.clearTimeout(timeout);
      if (controllerRef.current === controller) {
        controllerRef.current = null;
        setPending(false);
      }
    }
  }, [passwordInputRef, pendingPasswordRef]);

  useEffect(() => () => {
    controllerRef.current?.abort();
    controllerRef.current = null;
    pendingPasswordRef.current = "";
    if (passwordInputRef.current) passwordInputRef.current.value = "";
  }, [passwordInputRef, pendingPasswordRef]);

  return { cancel, failed, pending, redirect };
}

async function readConfiguredHTTPSPort(response: Response): Promise<number> {
  if (!response.ok || !/^text\/plain(?:\s*;|$)/i.test(response.headers.get("content-type") ?? "")) {
    throw new Error("Invalid HTTPS port response");
  }
  const declaredLength = response.headers.get("content-length");
  if (declaredLength && (!/^\d+$/.test(declaredLength) || Number(declaredLength) > 5)) {
    throw new Error("Invalid HTTPS port response");
  }
  const reader = response.body?.getReader();
  if (!reader) throw new Error("Missing HTTPS port response body");
  const decoder = new TextDecoder("utf-8", { fatal: true });
  let size = 0;
  let value = "";
  try {
    while (true) {
      const chunk = await reader.read();
      if (chunk.done) {
        value += decoder.decode();
        break;
      }
      size += chunk.value.byteLength;
      if (size > 5) throw new Error("HTTPS port response too large");
      value += decoder.decode(chunk.value, { stream: true });
    }
  } catch (error) {
    await reader.cancel().catch(() => undefined);
    throw error;
  } finally {
    reader.releaseLock();
  }
  if (!/^[1-9]\d{0,4}$/.test(value)) throw new Error("Invalid HTTPS port response");
  const port = Number(value);
  if (port > 65535) throw new Error("Invalid HTTPS port response");
  return port;
}

function isValidSSHPort(value: string) {
  const port = Number(value);
  return /^\d+$/.test(value) && Number.isInteger(port) && port >= 1 && port <= 65535;
}


function installJobStatusText(status: string | undefined, stage: string | undefined, t: ReturnType<typeof useT>) {
  if (status === "succeeded") return t("servers.agentInstallRegistered");
  if (status === "failed") return t("servers.agentInstallFailedCheckHost");
  if (status === "queued") return t("servers.agentInstallQueued");
  if (status === "running") {
    const stages: Record<string, string> = {
      connecting: "servers.agentInstallConnecting",
      transferring: "servers.agentInstallTransferring",
      activating: "servers.agentInstallActivating",
      registering: "servers.agentInstallRegistering",
    };
    return t((stages[stage ?? ""] ?? "servers.agentInstallRunning") as Parameters<typeof t>[0]);
  }
  return t("servers.agentInstallOutcomeUnknown");
}

function agentInstallStartError(error: unknown, t: ReturnType<typeof useT>) {
  if (error instanceof ApiError) {
    if (error.code === "conflict") return t("servers.agentInstallConflict");
    if (error.code === "secure_transport_required") return t("servers.agentInstallHttpsRequired");
    if (error.code === "forbidden") return t("servers.agentInstallPermissionRequired");
  }
  return t("servers.agentInstallStartError");
}

function agentUpdateStartError(error: unknown, t: ReturnType<typeof useT>) {
  if (error instanceof ApiError) {
    if (error.code === "secure_transport_required") return t("servers.agentInstallHttpsRequired");
    if (error.code === "forbidden") return t("servers.agentInstallPermissionRequired");
    if (error.code === "conflict") return t("servers.agentUpdateSSHConflict");
  }
  return t("servers.agentUpdateSSHFailed");
}

function agentUpdateFailureMessage(message: string | undefined, t: ReturnType<typeof useT>) {
  const safeMessages: Record<string, string> = {
    "SSH host key changed since first use; verify the server identity before retrying.": "servers.agentUpdateDiagSSHHostKeyMismatch",
    "Could not establish SSH. Verify reachability and root SSH access, then inspect the target before retrying.": "servers.agentUpdateDiagSSHConnectionFailed",
    "The target failed agent-update preflight; inspect OS, architecture, and service state before retrying.": "servers.agentUpdateDiagPreflightFailed",
    "Staging ownership could not be confirmed; inspect the target before retrying.": "servers.agentUpdateDiagStagingUncertain",
    "Agent files could not be transferred; inspect the target before retrying.": "servers.agentUpdateDiagTransferFailed",
    "Agent activation failed; the previous files were restored.": "servers.agentUpdateDiagActivationRestored",
    "Agent activation failed and rollback could not be confirmed; inspect the target before retrying.": "servers.agentUpdateDiagActivationUncertain",
    "HTTPS health verification failed; the previous agent files were restored.": "servers.agentUpdateDiagHealthRestored",
    "HTTPS health verification failed and rollback could not be confirmed; inspect the target before retrying.": "servers.agentUpdateDiagHealthUncertain",
    "Agent health was verified, but update staging cleanup failed; inspect the target before retrying.": "servers.agentUpdateDiagCleanupFailed",
    "Agent HTTPS health succeeded, but trust registration failed; inspect server configuration before retrying.": "servers.agentUpdateDiagTLSRegistrationFailed",
  };
  return t((safeMessages[message ?? ""] ?? "servers.agentUpdateSSHFailed") as Parameters<typeof t>[0]);
}

function Fact({ label, className, children }: { label: string; className?: string; children: ReactNode }) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-1 rounded-m3-lg bg-surface-2 p-3", className)}>
      <dt className="font-mono text-[11px] tracking-wider text-muted uppercase">{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

/** Compact decorative meter: the number next to it carries the information. Role color rises with load. */
function Meter({ percent }: { percent: number }) {
  const pct = Math.max(0, Math.min(100, percent));
  return (
    <div aria-hidden className="h-1.5 overflow-hidden rounded-full bg-surface-3">
      <div className={cn("h-full rounded-full", pct > 90 ? "bg-bad" : pct > 70 ? "bg-warn" : "bg-ok")} style={{ width: `${pct}%` }} />
    </div>
  );
}

function useElapsedJob(startedAt: number | null, active: boolean) {
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    if (startedAt == null || !Number.isFinite(startedAt)) {
      return;
    }
    const update = () => setSeconds(Math.max(0, Math.floor((performance.now() - startedAt) / 1000)));
    update();
    if (!active) return;
    const timer = window.setInterval(update, 1000);
    return () => window.clearInterval(timer);
  }, [active, startedAt]);
  return seconds;
}

function formatElapsed(seconds: number) {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remainder = seconds % 60;
  return hours > 0 ? `${hours}:${String(minutes).padStart(2, "0")}:${String(remainder).padStart(2, "0")}` : `${minutes}:${String(remainder).padStart(2, "0")}`;
}

function JobBusyState({ label, elapsed, busy = true }: { label: string; elapsed?: number; busy?: boolean }) {
  return <div className="flex items-center gap-2 text-sm text-muted" role={elapsed == null ? "status" : "timer"} aria-live="off">
    <span aria-hidden className={cn("size-3 rounded-full border-2 border-current border-r-transparent", busy && "motion-safe:animate-spin motion-reduce:animate-none")} />
    <span>{label}</span>
  </div>;
}

function AgentMonitor({ serverId }: { serverId: string }) {
  const t = useT();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  const agentStatus = useQuery({
    queryKey: ["server-agent", serverId],
    queryFn: async () => unwrap(await api.GET("/api/v1/servers/{serverId}/agent", { params: { path: { serverId } } })),
    refetchInterval: 5000,
    retry: false,
  });
  const metrics = agentStatus.data;
  if (agentStatus.isPending) return <div className="mt-1 text-muted">{t("servers.agent")}…</div>;
  if (agentStatus.isError || !metrics) return <div className="mt-1 text-muted">{t("servers.agentLifecycleUnknown")}</div>;
  const memory = (value?: number) => value == null ? "—" : `${(value / 1024 ** 3).toFixed(1)} GB`;
  const cpu = typeof metrics.cpu_percent === "number" && Number.isFinite(metrics.cpu_percent)
    ? Math.max(0, Math.min(100, Math.round(metrics.cpu_percent)))
    : null;
  const sampleAt = metrics.network_sampled_at ? Date.parse(metrics.network_sampled_at) : Number.NaN;
  const networkIsFresh = Number.isFinite(sampleAt) && sampleAt <= now && now - sampleAt <= 15_000;
  const network = networkIsFresh ? (metrics.network_interfaces ?? []).filter((entry) =>
    !!entry.name && isLanInterfaceName(entry.name) && Number.isFinite(entry.rx_bytes_per_second) && entry.rx_bytes_per_second >= 0 && Number.isFinite(entry.tx_bytes_per_second) && entry.tx_bytes_per_second >= 0,
  ) : [];

  const currentVersion = metrics.binary_observed?.version || metrics.version || "—";
  const newVersion = metrics.binary_available?.version;
  const hasUpdate = Boolean(metrics.binary_upgrade_available || metrics.binary_outdated);

  return (
    <div className="mt-2 flex min-w-48 flex-col gap-1 whitespace-normal text-xs">
      {!metrics.installed ? (
        <span className="font-sans text-muted">{t("servers.agentMissing")}</span>
      ) : hasUpdate ? (
        <span
          role="status"
          aria-label={t("servers.agentBinaryOutdatedStatus")}
          className="font-sans font-semibold text-warn flex items-center gap-1.5"
        >
          {newVersion && newVersion !== currentVersion ? (
            <>
              <span>{t("servers.agent")}: {currentVersion}</span>
              <span aria-hidden>→</span>
              <span className="rounded bg-warn/15 px-1 py-0.5">{newVersion}</span>
            </>
          ) : (
            <span>{t("servers.agent")}: {currentVersion}</span>
          )}
        </span>
      ) : (
        <span className="font-sans text-muted">
          {t("servers.agent")}: {currentVersion}
        </span>
      )}
      {metrics.installed && cpu != null && (
        <>
          <span className="font-sans">{t("servers.agentCpu", { percent: cpu })}</span>
          <Meter percent={cpu} />
        </>
      )}
      {metrics.installed && cpu == null && <span className="font-sans text-muted">{t("servers.agentCPUUnavailable")}</span>}
      {metrics.installed && (
        <LanNetworkMeters
          serverId={serverId}
          interfaces={network}
          sampledAt={metrics.network_sampled_at}
        />
      )}
      {metrics.memory_total_bytes != null && (
        <>
          <span className="font-sans text-muted">{t("servers.agentMemory", { available: memory(metrics.memory_available_bytes), total: memory(metrics.memory_total_bytes) })}</span>
          {metrics.memory_available_bytes != null && metrics.memory_total_bytes > 0 && <Meter percent={((metrics.memory_total_bytes - metrics.memory_available_bytes) / metrics.memory_total_bytes) * 100} />}
        </>
      )}
      {metrics.error && <span role="alert" className="font-sans text-bad">{t("servers.agentError", { error: metrics.error })}</span>}
    </div>
  );
}

export function isLanInterfaceName(name: string): boolean {
  if (name === "lo" || name.startsWith("lo:")) return false;
  const virtualPrefixes = [
    "docker", "br-", "veth", "virbr", "dummy", "tun", "tap",
    "flannel", "cni", "kube", "vnet", "sit", "ip6tnl",
  ];
  return !virtualPrefixes.some((prefix) => name.startsWith(prefix));
}

export function formatNetworkRate(bytesPerSec: number): string {
  if (!Number.isFinite(bytesPerSec) || bytesPerSec <= 0) return "0 B/s";
  if (bytesPerSec < 1024) return `${Math.round(bytesPerSec)} B/s`;
  if (bytesPerSec < 1024 * 1024) return `${(bytesPerSec / 1024).toFixed(1)} KiB/s`;
  return `${(bytesPerSec / (1024 * 1024)).toFixed(2)} MiB/s`;
}

export interface NetworkTrafficSample {
  timestamp: number;
  rx: number;
  tx: number;
}

const networkTrafficHistoryStore = new Map<string, NetworkTrafficSample[]>();
const maxTrafficSamples = 20;

export function recordNetworkSample(key: string, rx: number, tx: number): NetworkTrafficSample[] {
  const current = networkTrafficHistoryStore.get(key) ?? [];
  const next = [...current, { timestamp: Date.now(), rx, tx }].slice(-maxTrafficSamples);
  networkTrafficHistoryStore.set(key, next);
  return next;
}

export function getNetworkHistory(key: string): readonly NetworkTrafficSample[] {
  return networkTrafficHistoryStore.get(key) ?? [];
}

export function clearNetworkHistory(): void {
  networkTrafficHistoryStore.clear();
}

export function LanNetworkMeters({
  serverId,
  interfaces,
  sampledAt,
}: {
  serverId: string;
  interfaces: Schemas["ServerAgentNetworkInterface"][];
  sampledAt?: string;
}) {
  const t = useT();
  const [historyRevision, setHistoryRevision] = useState(0);

  useEffect(() => {
    if (!sampledAt || interfaces.length === 0) return;
    for (const iface of interfaces) {
      if (iface.name && Number.isFinite(iface.rx_bytes_per_second) && Number.isFinite(iface.tx_bytes_per_second)) {
        recordNetworkSample(`${serverId}:${iface.name}`, iface.rx_bytes_per_second, iface.tx_bytes_per_second);
      }
    }
    setHistoryRevision((r) => r + 1);
  }, [serverId, interfaces, sampledAt]);

  const lanInterfaces = interfaces.filter((entry) => !!entry.name && isLanInterfaceName(entry.name));

  if (lanInterfaces.length === 0) {
    return <span className="font-sans text-muted">{t("servers.agentNetworkUnavailable")}</span>;
  }

  return (
    <div className="flex flex-col gap-1.5" role="region" aria-label={t("servers.agentNetworkLive")}>
      {lanInterfaces.map((iface) => {
        const historyKey = `${serverId}:${iface.name}`;
        const samples = getNetworkHistory(historyKey);
        return (
          <LanInterfaceCard
            key={iface.name}
            iface={iface}
            samples={samples}
            revision={historyRevision}
          />
        );
      })}
    </div>
  );
}

function LanInterfaceCard({
  iface,
  samples,
  revision: _revision,
}: {
  iface: Schemas["ServerAgentNetworkInterface"];
  samples: readonly NetworkTrafficSample[];
  revision: number;
}) {
  const t = useT();
  const [isHovered, setIsHovered] = useState(false);
  const rx = iface.rx_bytes_per_second ?? 0;
  const tx = iface.tx_bytes_per_second ?? 0;
  const peakRx = samples.length > 0 ? Math.max(...samples.map((s) => s.rx), rx) : rx;
  const peakTx = samples.length > 0 ? Math.max(...samples.map((s) => s.tx), tx) : tx;
  const maxRate = Math.max(1024, peakRx, peakTx);

  const width = 200;
  const height = 44;
  const padding = 4;
  const chartWidth = width;
  const chartHeight = height - padding * 2;

  const pointsRx: string[] = [];
  const pointsTx: string[] = [];

  const effectiveSamples = samples.length < 2
    ? [{ rx, tx, timestamp: Date.now() - 5000 }, { rx, tx, timestamp: Date.now() }]
    : samples;

  effectiveSamples.forEach((sample, i) => {
    const x = (i / (effectiveSamples.length - 1)) * chartWidth;
    const yRx = height - padding - (Math.min(sample.rx, maxRate) / maxRate) * chartHeight;
    const yTx = height - padding - (Math.min(sample.tx, maxRate) / maxRate) * chartHeight;
    pointsRx.push(`${x.toFixed(1)},${yRx.toFixed(1)}`);
    pointsTx.push(`${x.toFixed(1)},${yTx.toFixed(1)}`);
  });

  const pathRx = `M ${pointsRx.join(" L ")}`;
  const pathTx = `M ${pointsTx.join(" L ")}`;
  const areaRx = `${pathRx} L ${chartWidth},${height} L 0,${height} Z`;

  return (
    <div
      tabIndex={0}
      role="group"
      aria-label={`${iface.name} RX ${formatNetworkRate(rx)} TX ${formatNetworkRate(tx)}`}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      onFocus={() => setIsHovered(true)}
      onBlur={() => setIsHovered(false)}
      className="relative flex flex-col gap-1 rounded-m3-md border border-surface-3 bg-surface-2/40 px-2.5 py-1.5 transition-all hover:border-primary/50 hover:bg-surface-2/80 focus-visible:outline-2 focus-visible:outline-primary cursor-default select-none"
    >
      <span className="sr-only">
        {t("servers.agentNetworkRate", { name: iface.name, rx: formatNetworkRate(rx), tx: formatNetworkRate(tx) })}
      </span>
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5">
          <span
            aria-hidden
            className="size-1.5 rounded-full bg-ok animate-pulse"
          />
          <span className="font-mono font-bold text-xs text-surface-on">{iface.name}</span>
          <span className="rounded bg-primary/15 px-1 py-0.2 text-[10px] font-semibold text-primary uppercase">LAN</span>
        </div>
        <div className="flex items-center gap-2.5 font-mono text-[11px]">
          <span className="inline-flex items-center gap-0.5 text-primary" title={t("servers.agentNetworkPeakRx")}>
            <Icon icon={ArrowDown} size="xs" />
            {formatNetworkRate(rx)}
          </span>
          <span className="inline-flex items-center gap-0.5 text-surface-on/70" title={t("servers.agentNetworkPeakTx")}>
            <Icon icon={ArrowUp} size="xs" />
            {formatNetworkRate(tx)}
          </span>
        </div>
      </div>

      <div className="h-1 w-full overflow-hidden rounded-full bg-surface-3/50 flex">
        <div
          className="h-full bg-primary transition-all duration-300"
          style={{ width: `${Math.min(100, Math.max(3, (rx / maxRate) * 70))}%` }}
        />
        <div
          className="h-full bg-ok/80 transition-all duration-300 ml-0.5"
          style={{ width: `${Math.min(100, Math.max(3, (tx / maxRate) * 30))}%` }}
        />
      </div>

      {isHovered && (
        <div
          role="tooltip"
          className="absolute left-0 bottom-full mb-2 z-40 w-60 rounded-m3-lg border border-surface-3 bg-surface-base/95 p-3 shadow-2xl backdrop-blur flex flex-col gap-2.5 pointer-events-none"
        >
          <div className="flex items-center justify-between border-b border-surface-3/60 pb-1.5">
            <div className="flex items-center gap-1.5 text-xs font-semibold text-surface-on">
              <Icon icon={Activity} size="xs" className="text-primary" />
              <span>{t("servers.agentNetworkHistory")}</span>
            </div>
            <span className="inline-flex items-center gap-1 rounded-full bg-ok/10 px-1.5 py-0.5 text-[10px] font-medium text-ok">
              <span className="size-1.5 rounded-full bg-ok animate-pulse" />
              {t("servers.agentNetworkLive")}
            </span>
          </div>

          <div className="flex flex-col gap-1">
            <div className="flex items-center justify-between text-[10px] text-muted font-sans">
              <span className="inline-flex items-center gap-1 text-primary">
                <span className="size-1.5 rounded-full bg-primary" />
                RX ({formatNetworkRate(rx)})
              </span>
              <span className="inline-flex items-center gap-1 text-ok">
                <span className="size-1.5 rounded-full bg-ok" />
                TX ({formatNetworkRate(tx)})
              </span>
            </div>

            <svg
              viewBox={`0 0 ${width} ${height}`}
              className="h-11 w-full overflow-visible"
              aria-hidden="true"
            >
              <defs>
                <linearGradient id={`rx-gradient-${iface.name}`} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="var(--color-primary, #3b82f6)" stopOpacity="0.25" />
                  <stop offset="100%" stopColor="var(--color-primary, #3b82f6)" stopOpacity="0.0" />
                </linearGradient>
              </defs>
              <path d={areaRx} fill={`url(#rx-gradient-${iface.name})`} />
              <path d={pathRx} fill="none" stroke="currentColor" className="text-primary" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
              <path d={pathTx} fill="none" stroke="currentColor" className="text-ok" strokeWidth="1.2" strokeLinecap="round" strokeLinejoin="round" strokeDasharray="3 2" />
            </svg>
          </div>

          <div className="grid grid-cols-2 gap-2 border-t border-surface-3/60 pt-2 text-[10px]">
            <div>
              <span className="block text-muted">{t("servers.agentNetworkPeakRx")}</span>
              <span className="font-mono font-semibold text-primary">{formatNetworkRate(peakRx)}</span>
            </div>
            <div>
              <span className="block text-muted">{t("servers.agentNetworkPeakTx")}</span>
              <span className="font-mono font-semibold text-surface-on">{formatNetworkRate(peakTx)}</span>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}


function isAgentBinaryCurrent(agent: Schemas["ServerAgent"]) {
  const observed = agent.binary_observed?.sha256;
  const available = agent.binary_available?.sha256;
  const observedArchitecture = agent.binary_observed?.architecture;
  const availableArchitecture = agent.binary_available?.architecture;
  const supportedArchitecture = (architecture: string | undefined) => architecture === "amd64" || architecture === "arm64";
  return agent.installed && agent.binary_status === "current" && agent.binary_outdated === false &&
    !!observed && !!available && /^[a-f0-9]{64}$/i.test(observed) && observed.toLowerCase() === available.toLowerCase() &&
    supportedArchitecture(observedArchitecture) && supportedArchitecture(availableArchitecture) && observedArchitecture === availableArchitecture;
}

function isRetryableAgentUpdateFailure(job: Schemas["ServerAgentInstallJob"] | null | undefined) {
  return job?.status === "failed" && job.message === "Could not establish SSH. Verify reachability and root SSH access, then inspect the target before retrying.";
}

function DeleteServerDialog({ server, onDone }: { server: Schemas["Server"]; onDone: () => void }) {
  const qc = useQueryClient();
  const remove = useMutation({
    mutationFn: async () =>
      unwrap(await api.DELETE("/api/v1/servers/{serverId}", { params: { path: { serverId: server.id } } })),
    onSuccess: async () => {
      await Promise.all(
        ["servers", "sites", "cameras", "events", "plates", "alarms"].map((k) => qc.invalidateQueries({ queryKey: [k] })),
      );
      onDone();
    },
  });
  return (
    <ConfirmDialog
      title="Eliminar servidor"
      message={`¿Está seguro de eliminar el servidor «${server.name}»? Se eliminarán de forma permanente todas sus cámaras, eventos, lecturas, alarmas y exportaciones. Esta acción no se puede deshacer.`}
      confirmLabel="Eliminar"
      pending={remove.isPending}
      error={remove.error}
      onConfirm={() => remove.mutate()}
      onCancel={onDone}
    />
  );
}

// EditServerModal patches only what changed: a rename must not re-test the Frigate
// connection, and a blank password keeps the stored one.
function EditServerModal({ server, sites, onDone }: { server: Schemas["Server"]; sites: Schemas["Site"][]; onDone: () => void }) {
  const qc = useQueryClient();
  const [form, setForm] = useState({
    name: server.name,
    site_id: server.site_id,
    base_url: server.base_url,
    auth_mode: server.auth_mode,
    username: server.username,
    password: "",
    tls_skip_verify: server.tls_skip_verify,
  });
  const creds = form.auth_mode === "credentials";
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value });
  const patch = (): Schemas["ServerUpdate"] => {
    const body: Schemas["ServerUpdate"] = {};
    if (form.name !== server.name) body.name = form.name;
    if (form.site_id !== server.site_id) body.site_id = form.site_id;
    if (form.base_url !== server.base_url) body.base_url = form.base_url;
    if (form.auth_mode !== server.auth_mode) body.auth_mode = form.auth_mode;
    if (creds && form.username !== server.username) body.username = form.username;
    if (creds && form.password) body.password = form.password;
    if (form.tls_skip_verify !== server.tls_skip_verify) body.tls_skip_verify = form.tls_skip_verify;
    return body;
  };
  const save = useMutation({
    mutationFn: async () =>
      unwrap(await api.PATCH("/api/v1/servers/{serverId}", { params: { path: { serverId: server.id } }, body: patch() })),
    onSuccess: async () => {
      await Promise.all(["servers", "sites", "cameras"].map((k) => qc.invalidateQueries({ queryKey: [k] })));
      onDone();
    },
  });
  return (
    <Modal title={`Editar servidor ${server.name}`} onClose={onDone}>
      <form
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          save.mutate();
        }}
        className="flex flex-col gap-4"
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Nombre">
            <TextInput required value={form.name} onChange={set("name")} />
          </Field>
          <Field label="Sitio">
            <Select required value={form.site_id} onChange={set("site_id")}>
              {sites.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Acceso a Frigate">
            <Select value={form.auth_mode} onChange={(e) => setForm({ ...form, auth_mode: e.target.value as Schemas["AuthMode"] })}>
              <option value="credentials">Con usuario y contraseña (puerto 8971)</option>
              <option value="none">Sin login (puerto 5000, solo redes de confianza)</option>
            </Select>
          </Field>
          <Field label="URL de Frigate" hint="Si cambia la conexión se vuelve a probar contra Frigate antes de guardar.">
            <TextInput required type="url" value={form.base_url} onChange={set("base_url")} />
          </Field>
          {creds && (
            <>
              <Field label="Usuario de Frigate">
                <TextInput required value={form.username} onChange={set("username")} autoComplete="off" />
              </Field>
              <Field label="Contraseña" hint="Dejala en blanco para conservar la actual.">
                <TextInput type="password" value={form.password} onChange={set("password")} autoComplete="new-password" />
              </Field>
            </>
          )}
          <Switch className="self-center" label="Aceptar certificado autofirmado" checked={form.tls_skip_verify} onChange={(next) => setForm({ ...form, tls_skip_verify: next })} />
        </div>
        <ErrorNote error={save.error} />
        <div className="flex justify-end gap-2">
          <Button variant="text" onClick={onDone}>Cancelar</Button>
          <Button type="submit" variant="primary" disabled={save.isPending}>
            {save.isPending ? "Guardando…" : "Guardar"}
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function summarize(total: number, visible: { status: string }[]) {
  const count = (status: string) => visible.filter((s) => s.status === status).length;
  const parts = [`${count("online")} en línea`];
  if (count("degraded")) parts.push(`${count("degraded")} degradado${count("degraded") === 1 ? "" : "s"}`);
  if (count("offline")) parts.push(`${count("offline")} fuera de línea`);
  const noun = total === 1 ? "servidor" : "servidores";
  const head = visible.length === total ? `${total} ${noun}` : `${visible.length} de ${total} ${noun}`;
  return [head, ...parts].join(" · ");
}

function StorageBar({ storage }: { storage?: Schemas["ServerStorage"] }) {
  if (!storage || storage.total_mb <= 0) return <span className="text-xs text-muted">—</span>;
  const pct = Math.round((storage.used_mb / storage.total_mb) * 100);
  return (
    <div className="flex flex-col gap-1" title={`${pct}% usado`}>
      <div className="h-1.5 overflow-hidden rounded-full bg-surface-3">
        <div className={pct > 90 ? "h-full rounded-full bg-warn" : "h-full rounded-full bg-primary"} style={{ width: `${pct}%` }} />
      </div>
      <span className="font-mono text-[11px] text-muted tabular-nums">
        {(storage.used_mb / 1024).toFixed(0)} / {(storage.total_mb / 1024).toFixed(0)} GB
      </span>
    </div>
  );
}

function RestartButton({ server }: { server: Schemas["Server"] }) {
  const qc = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const restart = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/api/v1/servers/{serverId}/restart", { params: { path: { serverId: server.id } } })),
    onSuccess: () => {
      setConfirming(false);
      return qc.invalidateQueries({ queryKey: ["servers"] });
    },
  });

  if (confirming) {
    return (
      <div className="flex items-center gap-1.5 justify-end">
        <span className="text-xs text-muted">¿Reiniciar?</span>
        <Button
          size="sm"
          variant="outlined"
          className="text-bad"
          disabled={restart.isPending}
          onClick={() => restart.mutate()}
        >
          {restart.isPending ? "Reiniciando…" : "Sí, reiniciar"}
        </Button>
        <Button size="sm" variant="text" onClick={() => setConfirming(false)}>
          Cancelar
        </Button>
      </div>
    );
  }

  return (
    <div className="flex flex-col items-end gap-1">
      <Button
        size="sm"
        disabled={restart.isPending}
        onClick={() => setConfirming(true)}
        title="Reiniciar servicio Frigate"
      >
        <Icon icon={RotateCcw} size="xs" className={restart.isPending ? "animate-spin" : undefined} />
        Reiniciar
      </Button>
      {restart.isSuccess && <span className="text-xs text-ok">Reinicio solicitado</span>}
      {restart.error && <span role="alert" className="text-xs text-bad">{restart.error.message}</span>}
    </div>
  );
}

function SyncButton({ server }: { server: Schemas["Server"] }) {
  const qc = useQueryClient();
  const sync = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/api/v1/servers/{serverId}/sync", { params: { path: { serverId: server.id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["servers"] }).then(() => qc.invalidateQueries({ queryKey: ["cameras"] })),
  });
  const r = sync.data;
  return (
    <div className="flex flex-col items-end gap-1">
      <Button size="sm" onClick={() => sync.mutate()} disabled={sync.isPending} title="Volver a leer las cámaras de Frigate">
        <Icon icon={RefreshCw} size="xs" className={sync.isPending ? "animate-spin" : undefined} />
        Sincronizar
      </Button>
      {r && (
        <span className="text-xs text-muted">
          +{r.added} nuevas · {r.updated} actualizadas · {r.missing} faltantes
        </span>
      )}
      {sync.error && <span role="alert" className="text-xs text-bad">{sync.error.message}</span>}
    </div>
  );
}

const capLabels: [keyof Schemas["Capabilities"], string][] = [
  ["review", "Review"],
  ["lpr", "Patentes"],
  ["exports", "Exportación"],
  ["preview", "Preview"],
  ["semantic_search", "Búsqueda semántica"],
  ["face_recognition", "Rostros"],
  ["audio", "Audio"],
  ["ptz", "PTZ"],
];

/** Reports "has typed data" to the wizard shell and clears it when the form unmounts. */
function useDirtyReport(dirty: boolean, report: (dirty: boolean) => void) {
  useEffect(() => {
    report(dirty);
  }, [dirty, report]);
  useEffect(() => () => report(false), [report]);
}

function ServerKindPicker({ onPick, onCancel }: { onPick: (kind: "new" | "existing") => void; onCancel: () => void }) {
  const t = useT();
  return (
    <section aria-label="Tipo de servidor" className="flex flex-col gap-4">
      <div>
        <h2 className="text-base font-semibold">¿Qué servidor vas a agregar?</h2>
        <p className="mt-1 text-sm text-muted">Elegí si vas a instalar un servidor nuevo o importar una instalación Frigate que ya existe.</p>
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        <button type="button" onClick={() => onPick("new")} className="m3-press min-h-11 rounded-m3-lg bg-surface-2 p-4 text-left hover:bg-surface-3 focus-visible:outline-2 focus-visible:outline-primary">
          <span className="block font-bold">{t("servers.freshFrigateTitle")}</span>
          <span className="mt-1 block text-sm text-muted">{t("servers.freshFrigateDescription")}</span>
        </button>
        <button type="button" onClick={() => onPick("existing")} className="m3-press min-h-11 rounded-m3-lg bg-surface-2 p-4 text-left hover:bg-surface-3 focus-visible:outline-2 focus-visible:outline-primary">
          <span className="block font-bold">{t("servers.existingFrigateTitle")}</span>
          <span className="mt-1 block text-sm text-muted">{t("servers.existingFrigateDescription")}</span>
        </button>
      </div>
      <div>
        <Button variant="text" onClick={onCancel}>Cancelar</Button>
      </div>
    </section>
  );
}

function NewFrigateHost({ sites, onBack, onDone, onCancel, onDirtyChange }: { sites: Schemas["Site"][]; onBack: () => void; onDone: () => void; onCancel: () => void; onDirtyChange: (dirty: boolean) => void }) {
  const t = useT();
  const [form, setForm] = useState({ site_id: "", server_name: "", host: "", username: "", password: "" });
  const [allowSystemDisk, setAllowSystemDisk] = useState(false);
  const passwordRef = useRef("");
  const [jobId, setJobId] = useState<string | null>(null);
  const set = (key: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [key]: e.target.value });
  const start = useMutation({
    mutationFn: async () => {
      const password = passwordRef.current;
      passwordRef.current = "";
      const body: Schemas["ServerProvisionRequest"] = { site_id: form.site_id, server_name: form.server_name.trim(), ip: form.host.trim(), ssh_user: form.username.trim(), ssh_password: password, trust_on_first_use: true, allow_system_disk: allowSystemDisk };
      return unwrap(await api.POST("/api/v1/servers/provision", { body }));
    },
    onSuccess: (job) => setJobId(job.id),
  });
  const progress = useQuery({
    queryKey: ["server-provision", jobId],
    enabled: jobId !== null,
    queryFn: async () => unwrap(await api.GET("/api/v1/servers/provision/{jobId}", { params: { path: { jobId: jobId! } } })),
    initialData: start.data,
    refetchInterval: (query) => query.state.data?.status === "running" ? 1000 : false,
    retry: false,
  });
  const selectedSite = sites.find((site) => site.id === form.site_id)?.name ?? "";
  const dirty = jobId === null && (allowSystemDisk || Object.values(form).some((value) => value !== ""));
  useDirtyReport(dirty, onDirtyChange);

  if (jobId) {
    const snapshot = progress.data;
    const stepKeys: Record<string, string> = {
      connecting: "stepConnecting", packages: "stepPackages", hardware: "stepHardware", compose: "stepCompose",
      ntp: "stepNtp", agent: "stepAgent", register: "stepRegister",
    };
    return (
      <section className="flex flex-col gap-4" aria-label="Progreso de instalación de Frigate">
        <h2 className="text-lg font-bold">{t("servers.installRunning", { site: selectedSite })}</h2>
        {snapshot?.warning?.includes("system_disk") && <p role="alert" className="rounded-m3-lg bg-warn/15 p-3 text-sm font-semibold text-warn">{t("servers.systemDiskWarning")}</p>}
        {snapshot?.warning?.includes("cpu") && <p role="alert" className="rounded-m3-lg bg-warn/10 p-3 text-sm text-warn">{t("servers.cpuWarning")}</p>}
        <div aria-hidden className="flex gap-1">
          {snapshot?.steps.map((step) => (
            <span
              key={step.id}
              className={cn("h-2 flex-1 rounded-full", step.state === "done" ? "bg-primary" : step.state === "running" ? "animate-pulse bg-primary-container" : step.state === "error" ? "bg-bad" : "bg-surface-3")}
            />
          ))}
        </div>
        <ol className="flex flex-col gap-1" aria-live="polite">
          {snapshot?.steps.map((step) => (
            <li key={step.id} className="flex min-h-9 items-center justify-between gap-3 rounded-m3-md bg-surface-2 px-3 text-sm">
              <span>{t((`servers.${stepKeys[step.id] ?? "stepConnecting"}`) as Parameters<typeof t>[0])}</span>
              <span className={cn("font-mono text-xs", step.state === "done" ? "text-ok" : step.state === "error" ? "text-bad" : "text-muted")}>{step.state === "running" ? "…" : step.state === "done" ? "✓" : step.state}</span>
            </li>
          ))}
        </ol>
        {snapshot?.status === "failed" && <p role="alert" className="text-sm text-bad">{t("servers.installFailed", { error: snapshot.error ?? "unknown error" })}</p>}
        {snapshot?.status === "succeeded" && <p role="status" className="text-sm text-ok">{t("servers.installComplete")}</p>}
        {progress.error && <ErrorNote error={progress.error} />}
        <Button className="self-start" onClick={onDone}>{snapshot?.status === "running" ? "Cerrar" : "Listo"}</Button>
      </section>
    );
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        passwordRef.current = form.password;
        setForm((current) => ({ ...current, password: "" }));
        start.mutate();
      }}
      className="flex flex-col gap-4"
      aria-label="Instalación de Frigate nuevo"
    >
      <div>
        <h2 className="text-lg font-bold">{t("servers.newInstallHeading")}</h2>
        <p className="mt-1 text-sm text-muted">{t("servers.newInstallEffects")}</p>
        <p className="mt-1 text-sm text-muted">{t("servers.newInstallRequirements")}</p>
        <p className="mt-1 text-sm text-muted">{t("servers.newInstallNetwork")}</p>
        <div className="mt-3 rounded-m3-lg bg-warn/10 p-3 text-sm">
          <Switch label={t("servers.allowSystemDiskLabel")} checked={allowSystemDisk} onChange={setAllowSystemDisk} />
        </div>
        {allowSystemDisk && <p role="alert" className="mt-2 rounded-m3-lg bg-warn/15 p-3 text-sm font-semibold text-warn">{t("servers.systemDiskWarning")}</p>}
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        <div className="rounded-m3-lg bg-surface-2 p-4 text-sm">
          <p className="font-bold">{t("servers.sshTrustWarning")}</p>
          <ol className="mt-2 list-decimal space-y-1 pl-4 text-muted">
            <li>{t("servers.sshTrustInitialStep")}</li>
            <li>Comprobar que sea Debian/Ubuntu amd64 y que no tenga Frigate previo</li>
            <li>Instalar Frigate nuevo y OpenVMS Edge Agent</li>
            <li>Habilitar servicios Docker, chrony y OpenVMS Edge Agent</li>
            <li>Registrar el servidor contra OpenVMS Central</li>
          </ol>
        </div>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Sitio">
          <Select required value={form.site_id} onChange={set("site_id")}>
            <option value="">Elegí un sitio</option>
            {sites.map((s) => (
              <option key={s.id} value={s.id}>{s.name}</option>
            ))}
          </Select>
        </Field>
        <Field label={t("servers.serverName")}>
          <TextInput required value={form.server_name} onChange={set("server_name")} autoComplete="off" maxLength={200} />
        </Field>
        <Field label="Host SSH">
          <TextInput required value={form.host} onChange={set("host")} placeholder="10.20.0.11" autoComplete="off" />
        </Field>
        <Field label="Usuario SSH">
          <TextInput required value={form.username} onChange={set("username")} autoComplete="off" />
        </Field>
        <Field label="Contraseña SSH" hint="Se usa una sola vez para instalar el agente y no queda en el servidor Frigate.">
          <TextInput required type="password" value={form.password} onChange={set("password")} autoComplete="new-password" />
        </Field>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" variant="primary" disabled={start.isPending || !form.site_id || !form.server_name.trim() || !form.host || !form.username || !form.password}>
          {t("servers.startInstall")}
        </Button>
        <Button variant="outlined" onClick={onBack}>Volver</Button>
        <Button variant="text" onClick={onCancel}>Cancelar</Button>
      </div>
      <ErrorNote error={start.error} />
    </form>
  );
}

// RegisterServer is the two-step wizard from PRD §59: test the connection and show what
// Frigate reports, then register it and import its cameras. An existing Frigate takes the
// agent path and does not use this probe.
function RegisterServer({ sites, onDone }: { sites: Schemas["Site"][]; onDone: () => void }) {
  const t = useT();
  const [kind, setKind] = useState<"new" | "existing" | null>(null);
  // The active form reports whether it holds typed data; closing then needs a confirmation so a
  // stray Escape or backdrop click does not lose it. The values stay in the form component (the
  // SSH password never leaves it) and only this boolean is lifted.
  const [dirty, setDirty] = useState(false);
  const [confirmingDiscard, setConfirmingDiscard] = useState(false);
  const requestClose = () => {
    // Both modals listen for Escape: while the confirmation is open it owns the key.
    if (confirmingDiscard) return;
    if (dirty) setConfirmingDiscard(true);
    else onDone();
  };
  return (
    <>
      <Modal title="Registrar servidor" onClose={requestClose}>
        {kind === null ? (
          <ServerKindPicker onPick={setKind} onCancel={onDone} />
        ) : kind === "existing" ? (
          <NewFrigateServer sites={sites} onBack={() => setKind(null)} onDone={onDone} onCancel={requestClose} onDirtyChange={setDirty} />
        ) : (
          <NewFrigateHost sites={sites} onBack={() => setKind(null)} onDone={onDone} onCancel={requestClose} onDirtyChange={setDirty} />
        )}
      </Modal>
      {confirmingDiscard && (
        <ConfirmDialog
          title={t("servers.discardTitle")}
          message={t("servers.discardMessage")}
          confirmLabel={t("servers.discardConfirm")}
          cancelLabel={t("servers.discardKeep")}
          onConfirm={onDone}
          onCancel={() => setConfirmingDiscard(false)}
        />
      )}
    </>
  );
}

function NewFrigateServer({ sites, onBack, onDone, onCancel, onDirtyChange }: { sites: Schemas["Site"][]; onBack: () => void; onDone: () => void; onCancel: () => void; onDirtyChange: (dirty: boolean) => void }) {
  const t = useT();
  const qc = useQueryClient();
  const [form, setForm] = useState({
    site_id: "",
    name: "",
    base_url: "https://",
    auth_mode: "credentials" as Schemas["AuthMode"],
    username: "",
    password: "",
    tls_skip_verify: false,
  });
  const creds = form.auth_mode === "credentials";
  const conn = () => ({
    site_id: form.site_id,
    base_url: form.base_url,
    auth_mode: form.auth_mode,
    username: creds ? form.username : undefined,
    password: creds ? form.password : undefined,
    tls_skip_verify: form.tls_skip_verify,
  });
  const [importCameras, setImportCameras] = useState(true);
  useDirtyReport(form.site_id !== "" || form.name !== "" || form.username !== "" || form.password !== "" || form.base_url !== "https://" || form.auth_mode !== "credentials" || form.tls_skip_verify, onDirtyChange);
  const set = (k: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value });

  const probe = useMutation({
    mutationFn: async () => {
      return unwrap(await api.POST("/api/v1/servers/probe", { body: conn() }));
    },
  });
  const create = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/servers", { body: { ...conn(), name: form.name, import_cameras: importCameras } })),
    onSuccess: async () => {
      await Promise.all(["servers", "sites", "cameras"].map((k) => qc.invalidateQueries({ queryKey: [k] })));
      onDone();
    },
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    if (probe.data) create.mutate();
    else probe.mutate();
  }
  const edit = (k: keyof typeof form) => (e: { target: { value: string } }) => {
    probe.reset();
    set(k)(e);
  };

  return (
    <form onSubmit={submit} aria-label="Importar servidor Frigate existente" className="flex flex-col gap-4">
      <div>
        <h2 className="text-lg font-bold">{t("servers.existingImportHeading")}</h2>
        <p className="mt-1 text-sm text-muted">{t("servers.existingImportDescription")}</p>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Sitio">
          <Select required value={form.site_id} onChange={edit("site_id")}>
            <option value="">Elegí un sitio</option>
            {sites.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Nombre">
          <TextInput required value={form.name} onChange={set("name")} placeholder="Frigate-H01" />
        </Field>
        <Field label="Acceso a Frigate">
          <Select
            value={form.auth_mode}
            onChange={(e) => {
              probe.reset();
              const mode = e.target.value as Schemas["AuthMode"];
              const url = mode === "none" ? form.base_url.replace(/^https:/, "http:").replace(/:8971$/, ":5000") : form.base_url;
              setForm({ ...form, auth_mode: mode, base_url: url });
            }}
          >
            <option value="credentials">Con usuario y contraseña (puerto 8971)</option>
            <option value="none">Sin login (puerto 5000, solo redes de confianza)</option>
          </Select>
        </Field>
        <Field
          label="URL de Frigate"
          hint={creds ? "Puerto autenticado 8971." : "Quien llegue a este puerto controla Frigate: usalo solo en una red privada o VPN."}
        >
          <TextInput required type="url" value={form.base_url} onChange={edit("base_url")} placeholder={creds ? "https://10.20.0.11:8971" : "http://10.20.0.11:5000"} />
        </Field>
        <Switch
          className="self-center"
          label="Aceptar certificado autofirmado"
          checked={form.tls_skip_verify}
          onChange={(next) => {
            probe.reset();
            setForm({ ...form, tls_skip_verify: next });
          }}
        />
        {creds && (
          <>
            <Field label="Usuario de Frigate">
              <TextInput required value={form.username} onChange={edit("username")} autoComplete="off" />
            </Field>
            <Field label="Contraseña" hint="Se guarda cifrada (AES-256-GCM) y nunca vuelve a mostrarse.">
              <TextInput required type="password" value={form.password} onChange={edit("password")} autoComplete="new-password" />
            </Field>
          </>
        )}
      </div>

      {probe.data && (
        <section aria-label="Resultado de la prueba" className="flex flex-col gap-2 rounded-m3-lg bg-surface-2 p-4 text-sm">
          <p>
            Conectado a Frigate <span className="font-mono">{probe.data.frigate_version}</span> (adaptador{" "}
            <span className="font-mono">{probe.data.adapter}</span>), {probe.data.cameras.length} cámaras.
          </p>
          <ul className="flex flex-wrap gap-1.5">
            {capLabels
              .filter(([k]) => probe.data.capabilities[k])
              .map(([, label]) => (
                <li key={label}>
                  <Pill tone="secondary" size="sm">{label}</Pill>
                </li>
              ))}
          </ul>
          <p className="font-mono text-xs text-muted">{probe.data.cameras.map((c) => c.remote_name).join(" · ")}</p>
          <Switch label="Importar las cámaras" checked={importCameras} onChange={setImportCameras} />
        </section>
      )}

      <ErrorNote error={probe.error ?? create.error} />
      <div className="flex flex-wrap gap-2">
        <Button type="submit" variant="primary" disabled={probe.isPending || create.isPending}>
          {probe.data ? (create.isPending ? "Registrando…" : "Registrar servidor") : probe.isPending ? "Probando…" : "Probar conexión"}
        </Button>
        <Button variant="outlined" onClick={onBack}>Volver</Button>
        <Button variant="text" onClick={onCancel}>Cancelar</Button>
      </div>
    </form>
  );
}

function SyncCell({ status }: { status?: Schemas["EventSyncStatus"] }) {
  if (!status) return <span className="text-muted">—</span>;
  return (
    <div className="flex flex-col gap-0.5">
      <span className="tabular-nums">{status.event_count.toLocaleString("es-AR")} indexados</span>
      {status.last_success_at && (
        <span className="text-muted">al día {new Date(status.last_success_at).toLocaleTimeString("es-AR")}</span>
      )}
      {status.last_error && <span role="alert" className="max-w-48 break-words text-bad">{status.last_error}</span>}
    </div>
  );
}
