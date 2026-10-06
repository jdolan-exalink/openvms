import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, RefreshCw, RotateCcw, Trash2 } from "lucide-react";
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
                    <AgentMonitor serverId={s.id} canUpdate={can(me.data, "servers.manage")} />
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
                {canInstallAgent(me.data, s) && <AgentInstallControl key={s.id} server={s} />}
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

function AgentInstallControl({ server }: { server: Schemas["Server"] }) {
  const t = useT();
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [job, setJob] = useState<Schemas["ServerAgentInstallJob"] | null>(null);
  const updateJob = useCallback((next: Schemas["ServerAgentInstallJob"]) => {
    setJob(next);
    if (next.status === "succeeded") void qc.invalidateQueries({ queryKey: ["server-agent", server.id] });
  }, [qc, server.id]);
  const active = job?.status === "queued" || job?.status === "running";
  return (
    <>
      <Button size="sm" variant="outlined" onClick={() => setOpen(true)} disabled={job?.status === "succeeded"}>
        {active ? t("servers.agentInstallViewProgress") : t("servers.agentInstallAction")}
      </Button>
      {active && !open && <span role="status" className="text-xs text-muted">{t("servers.agentInstallContinues")}</span>}
      {job?.status === "succeeded" && <span role="status" className="text-xs text-ok">{t("servers.agentInstallSuccessUnverified")}</span>}
      {job?.status === "failed" && <span role="alert" className="text-xs text-bad">{t("servers.agentInstallFailedCheckHost")}</span>}
      <AgentInstallDialog server={server} open={open} onClose={() => setOpen(false)} job={job} onJob={updateJob} />
    </>
  );
}

function AgentInstallDialog({
  server,
  open,
  onClose,
  job,
  onJob,
}: {
  server: Schemas["Server"];
  open: boolean;
  onClose: () => void;
  job: Schemas["ServerAgentInstallJob"] | null;
  onJob: (job: Schemas["ServerAgentInstallJob"]) => void;
}) {
  const maxPollingDurationMs = 11 * 60_000;
  const pollRequestTimeoutMs = 15_000;
  const t = useT();
  const [form, setForm] = useState({ host: "", port: "22", fingerprint: "", confirmed: false });
  const passwordInputRef = useRef<HTMLInputElement>(null);
  const pendingPasswordRef = useRef("");
  const pollStartedAt = useRef(0);
  const [pollTimedOut, setPollTimedOut] = useState(false);
  const secureBrowser = location.protocol === "https:";
  const start = useMutation({
    mutationFn: async () => {
      const password = pendingPasswordRef.current;
      pendingPasswordRef.current = "";
      const body: Schemas["ServerAgentInstallRequest"] = {
        ssh_host: form.host.trim(),
        ssh_port: Number(form.port),
        ssh_password: password,
        ssh_host_key_fingerprint: form.fingerprint.trim(),
      };
      return unwrap(await api.POST("/api/v1/servers/{serverId}/agent/install", { params: { path: { serverId: server.id } }, body }));
    },
    onSuccess: (next) => {
      pollStartedAt.current = Date.now();
      setPollTimedOut(false);
      onJob(next);
    },
    onError: () => { pendingPasswordRef.current = ""; },
  });
  const progress = useQuery({
    queryKey: ["server-agent-install", server.id, job?.id],
    enabled: !!job?.id && (job.status === "queued" || job.status === "running"),
    queryFn: async ({ signal }) => {
      const controller = new AbortController();
      const abort = () => controller.abort();
      const timeout = window.setTimeout(abort, pollRequestTimeoutMs);
      signal.addEventListener("abort", abort, { once: true });
      try {
        return unwrap(await api.GET("/api/v1/servers/{serverId}/agent/install/{jobId}", {
          params: { path: { serverId: server.id, jobId: job!.id } }, signal: controller.signal,
        }));
      } finally {
        window.clearTimeout(timeout);
        signal.removeEventListener("abort", abort);
      }
    },
    initialData: job ?? undefined,
    refetchInterval: (query) => {
      const current = query.state.data?.status;
      if (query.state.error || (current !== "queued" && current !== "running")) return false;
      if (pollStartedAt.current && Date.now() - pollStartedAt.current >= maxPollingDurationMs) return false;
      return 1000;
    },
    retry: false,
  });
  const snapshot = progress.data ?? job;
  const hasSubmitted = !!job?.id || start.isPending;
  const polling = job?.status === "queued" || job?.status === "running";
  const close = () => {
    if (start.isPending) return;
    pendingPasswordRef.current = "";
    if (passwordInputRef.current) passwordInputRef.current.value = "";
    onClose();
  };
  useEffect(() => () => {
    pendingPasswordRef.current = "";
    if (passwordInputRef.current) passwordInputRef.current.value = "";
  }, []);
  useEffect(() => {
    if (progress.data) onJob(progress.data);
  }, [progress.data, onJob]);
  useEffect(() => {
    if (!polling || !pollStartedAt.current || pollTimedOut) return;
    const remaining = Math.max(0, pollStartedAt.current + maxPollingDurationMs - Date.now());
    const timeout = window.setTimeout(() => setPollTimedOut(true), remaining);
    return () => window.clearTimeout(timeout);
  }, [maxPollingDurationMs, pollTimedOut, polling]);

  if (!open) return null;
  return (
    <Modal title={t("servers.agentInstallTitle", { server: server.name })} onClose={close}>
      {!hasSubmitted ? (
        <form
          className="flex flex-col gap-4"
          aria-label={t("servers.agentInstallForm")}
          onSubmit={(event: FormEvent) => {
            event.preventDefault();
            if (!secureBrowser) return;
            pendingPasswordRef.current = passwordInputRef.current?.value ?? "";
            if (passwordInputRef.current) passwordInputRef.current.value = "";
            start.mutate();
          }}
        >
          <p className="rounded-m3-lg bg-warn/10 p-3 text-sm">{t("servers.agentInstallScopeNotice")}</p>
          {!secureBrowser && <p role="alert" className="rounded-m3-lg bg-bad/10 p-3 text-sm text-bad">{t("servers.agentInstallHttpsRequired")}</p>}
          <p className="text-sm text-muted">{t("servers.agentInstallRootOnly")}</p>
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
            <Field label={t("servers.agentInstallFingerprintLabel")} hint={t("servers.agentInstallFingerprintHint")}>
              <TextInput required aria-label={t("servers.agentInstallFingerprintLabel")} value={form.fingerprint} onChange={(event) => setForm({ ...form, fingerprint: event.target.value })} autoComplete="off" placeholder={`SHA256:${"…".repeat(8)}`} pattern="SHA256:[A-Za-z0-9+/]{43}" />
            </Field>
          </div>
          <label className="flex items-start gap-3 rounded-m3-lg bg-surface-2 p-3 text-sm">
            <input type="checkbox" checked={form.confirmed} onChange={(event) => setForm({ ...form, confirmed: event.target.checked })} />
            <span>{t("servers.agentInstallConfirmScope")}</span>
          </label>
          {start.error && <p role="alert" className="text-sm text-bad">{agentInstallStartError(start.error, t)}</p>}
          <div className="flex flex-wrap justify-end gap-2">
            <Button type="button" variant="text" onClick={close}>{t("common.cancel")}</Button>
            <Button type="submit" variant="primary" disabled={start.isPending || !form.confirmed || !isCanonicalIPv4(form.host.trim()) || !isValidSSHPort(form.port) || !isValidFingerprint(form.fingerprint.trim()) || !secureBrowser}>
              {start.isPending ? t("servers.agentInstallSubmitting") : t("servers.agentInstallSubmit")}
            </Button>
          </div>
        </form>
      ) : (
        <section aria-label={t("servers.agentInstallProgress")} className="flex flex-col gap-4">
          <p aria-live="polite" className="text-sm">{installJobStatusText(snapshot?.status, snapshot?.stage, t)}</p>
          {snapshot?.status === "succeeded" && <p role="status" className="rounded-m3-lg bg-warn/10 p-3 text-sm">{t("servers.agentInstallSuccessUnverified")}</p>}
          {snapshot?.status === "failed" && <p role="alert" className="rounded-m3-lg bg-bad/10 p-3 text-sm text-bad">{t("servers.agentInstallFailedCheckHost")}</p>}
          {progress.error && <p role="alert" className="text-sm text-warn">{t("servers.agentInstallOutcomeUnknown")}</p>}
          {pollTimedOut && <p role="alert" className="text-sm text-warn">{t("servers.agentInstallOutcomeUnknown")}</p>}
          {(snapshot?.status === "queued" || snapshot?.status === "running") && <p className="text-sm text-muted">{t("servers.agentInstallCloseDoesNotCancel")}</p>}
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

function isValidSSHPort(value: string) {
  const port = Number(value);
  return /^\d+$/.test(value) && Number.isInteger(port) && port >= 1 && port <= 65535;
}

function isValidFingerprint(value: string) {
  return /^SHA256:[A-Za-z0-9+/]{43}$/.test(value);
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

function AgentMonitor({ serverId, canUpdate }: { serverId: string; canUpdate: boolean }) {
  const t = useT();
  const qc = useQueryClient();
  const agentStatus = useQuery({
    queryKey: ["server-agent", serverId],
    queryFn: async () => unwrap(await api.GET("/api/v1/servers/{serverId}/agent", { params: { path: { serverId } } })),
    refetchInterval: 5000,
    retry: false,
  });
  const update = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/servers/{serverId}/agent/update", { params: { path: { serverId } } })),
    onSuccess: async () => { await qc.invalidateQueries({ queryKey: ["server-agent", serverId] }); },
  });
  const metrics = agentStatus.data;
  if (agentStatus.isPending) return <div className="mt-1 text-muted">{t("servers.agent")}…</div>;
  if (!metrics) return <div className="mt-1 text-muted">{t("servers.agentMissing")}</div>;
  const memory = (value?: number) => value == null ? "—" : `${(value / 1024 ** 3).toFixed(1)} GB`;
  return (
    <div className="mt-2 flex min-w-48 flex-col gap-1 whitespace-normal text-xs">
      <span className="font-sans font-medium">{t("servers.agent")}: {metrics.installed ? metrics.version || "—" : t("servers.agentMissing")}</span>
      {metrics.installed && <span className="font-sans text-muted">{t("servers.agentVersion", { version: metrics.version || "—", current: metrics.current_version })}</span>}
      {metrics.cpu_percent != null && (
        <>
          <span className="font-sans">{t("servers.agentCpu", { percent: metrics.cpu_percent })}</span>
          <Meter percent={metrics.cpu_percent} />
        </>
      )}
      {metrics.memory_total_bytes != null && (
        <>
          <span className="font-sans text-muted">{t("servers.agentMemory", { available: memory(metrics.memory_available_bytes), total: memory(metrics.memory_total_bytes) })}</span>
          {metrics.memory_available_bytes != null && metrics.memory_total_bytes > 0 && <Meter percent={((metrics.memory_total_bytes - metrics.memory_available_bytes) / metrics.memory_total_bytes) * 100} />}
        </>
      )}
      {metrics.error && <span role="alert" className="font-sans text-bad">{t("servers.agentError", { error: metrics.error })}</span>}
      {canUpdate && metrics.installed && metrics.outdated && <Button size="sm" variant="tonal" className="self-start" disabled={update.isPending} onClick={() => update.mutate()}>{update.isPending ? t("servers.agentUpdating") : t("servers.agentUpdate")}</Button>}
      {update.isSuccess && <span role="status" className="font-sans text-ok">{t("servers.agentUpdated")}</span>}
      <ErrorNote error={update.error} />
    </div>
  );
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
