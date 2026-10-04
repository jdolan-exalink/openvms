import { useT } from "@/i18n";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { camerasQuery, readinessQuery, serversQuery, sitesQuery, syncStatusQuery, systemCapacityQuery, systemInfoQuery } from "@/api/queries";
import type { Schemas } from "@/api/client";
import { PageHeader } from "@/components/ui";
import { cn } from "@/lib/cn";
import { fmtDateTime } from "@/lib/format";

const dependencyLabels: Record<string, string> = {
  postgres: "PostgreSQL",
  valkey: "Valkey",
  nats: "NATS JetStream",
  object_storage: "Object storage (S3)",
};

export function Dashboard() {
  const t = useT();
  const capacity = useQuery(systemCapacityQuery);
  const ready = useQuery(readinessQuery);
  const info = useQuery(systemInfoQuery);
  const sites = useQuery(sitesQuery);
  const servers = useQuery(serversQuery);
  const cameras = useQuery(camerasQuery());
  const syncStatus = useQuery({ ...syncStatusQuery, retry: false });
  const syncMap = new Map(syncStatus.data?.map((s) => [s.server_id, s]));
  const online = (items?: { status: string }[]) => items?.filter((i) => i.status === "online").length ?? 0;

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-8">
      <PageHeader
        title={t("nav.overview")}
        description={t("settings.dashboard")}
      />

      <CapacityPanel data={capacity.data} pending={capacity.isPending} />

      <section aria-labelledby="inventory" className="flex flex-col gap-3">
        <h2 id="inventory" className="text-xl font-bold">{t("dashboard.inventory")}</h2>
        <ul className="grid gap-3 sm:grid-cols-3">
          <Stat to="/sites" label={t("nav.sites")} value={sites.data?.length} />
          <Stat
            to="/servers"
            label={t("nav.servers")}
            value={servers.data?.length}
            detail={servers.data ? t("dashboard.onlineCount", { count: online(servers.data) }) : undefined}
            warn={!!servers.data && online(servers.data) < servers.data.length}
          />
          <Stat
            to="/cameras"
            label={t("nav.cameras")}
            value={cameras.data?.length}
            detail={cameras.data ? t("dashboard.onlineCount", { count: online(cameras.data) }) : undefined}
            warn={!!cameras.data && online(cameras.data) < cameras.data.filter((c) => c.enabled).length}
          />
        </ul>
      </section>

      <section aria-labelledby="event-sync" className="flex flex-col gap-3">
        <h2 id="event-sync" className="text-xl font-bold">{t("dashboard.eventSync")}</h2>
        {servers.data && servers.data.length === 0 && (
          <p className="text-sm text-muted">{t("dashboard.noServers")}</p>
        )}
        <ul className="grid gap-3 sm:grid-cols-2">
          {servers.data?.map((srv) => {
            const st = syncMap.get(srv.id);
            return (
              <li key={srv.id} className="flex flex-col gap-1 rounded-m3-lg bg-surface-1 p-4 text-sm">
                <div className="flex items-center justify-between">
                  <span className="font-bold">{srv.name}</span>
                  <StatusDot ok={!st?.last_error} />
                </div>
                <div className="flex items-center justify-between text-xs text-muted">
                  <span>{t("dashboard.syncedEvents", { count: st?.event_count ?? 0 })}</span>
                  {st?.last_success_at && (
                    <span className="font-mono">{t("dashboard.lastSuccess", { time: fmtDateTime(st.last_success_at) })}</span>
                  )}
                </div>
                {st?.last_error && <span role="alert" className="text-xs text-bad">{st.last_error}</span>}
              </li>
            );
          })}
        </ul>
      </section>

      <section aria-labelledby="deps" className="flex flex-col gap-3">
        <div className="flex items-baseline justify-between gap-4">
          <h2 id="deps" className="text-xl font-bold">{t("dashboard.controlPlane")}</h2>
          {ready.data && <OverallBadge status={ready.data.status} />}
        </div>
        {ready.isError && (
          <p role="alert" className="rounded-m3-lg bg-bad/10 px-4 py-3 text-sm text-bad">
            {t("dashboard.apiUnreachable", { message: ready.error.message })}
          </p>
        )}
        <ul className="grid gap-3 sm:grid-cols-2">
          {(ready.data?.checks ?? []).map((c) => (
            <li key={c.name} className="flex items-center justify-between gap-3 rounded-m3-lg bg-surface-1 px-4 py-3">
              <div className="flex flex-col">
                <span className="font-medium">{dependencyLabels[c.name] ?? c.name}</span>
                {c.error && <span role="alert" className="text-xs break-all text-bad">{c.error}</span>}
              </div>
              <div className="flex items-center gap-3">
                <span className="font-mono text-xs text-muted tabular-nums">{c.latency_ms} ms</span>
                <StatusDot ok={c.status === "ok"} />
              </div>
            </li>
          ))}
          {ready.isPending &&
            Array.from({ length: 4 }, (_, i) => (
              <li key={i} className="h-[58px] animate-pulse rounded-m3-lg bg-surface-1" />
            ))}
        </ul>
      </section>

      <section aria-labelledby="build" className="flex flex-col gap-3">
        <h2 id="build" className="text-xl font-bold">{t("common.version")}</h2>
        <dl className="grid grid-cols-2 gap-x-6 gap-y-3 rounded-m3-xl bg-surface-1 px-5 py-4 text-sm sm:grid-cols-4">
          <Field label="API" value={info.data?.version} />
          <Field label="Commit" value={info.data?.commit.slice(0, 12)} />
          <Field label={t("dashboard.dbSchema")} value={info.data ? `v${info.data.schema_version}` : undefined} />
          <Field label="Go" value={info.data?.go_version} />
        </dl>
        <p className="text-sm text-muted">
          {t("dashboard.apiContract")} <a className="text-primary underline-offset-2 hover:underline" href="/docs">/docs</a>
        </p>
      </section>
    </div>
  );
}

function CapacityPanel({ data, pending }: { data?: Schemas["SystemCapacity"]; pending: boolean }) {
  const t = useT();
  const cls = data?.classifier;
  const frames = cls?.crops_per_second;
  return (
    <section aria-labelledby="capacity" className="flex flex-col gap-3">
      <h2 id="capacity" className="text-xl font-bold">{t("dashboard.equipment")}</h2>
      {pending && !data && <p className="text-sm text-muted">{t("dashboard.readingEquipment")}</p>}
      {data && (
        <>
          <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
            <Meter label="CPU" value={data.cpu.percent.toFixed(0) + "%"} detail={`${data.cpu.online} ${t("dashboard.cores")} · ${data.cpu.model || "—"}`} />
            <Meter label={t("dashboard.memory")} value={fmtBytes(data.memory.total_bytes - data.memory.available_bytes)} detail={`${t("common.of")} ${fmtBytes(data.memory.total_bytes)}`} />
            <Meter label={t("dashboard.disk")} value={fmtBytes(data.disk.total_bytes - data.disk.free_bytes)} detail={`${t("common.of")} ${fmtBytes(data.disk.total_bytes)}`} />
            <Meter label="GPU" value={data.gpu.present ? data.gpu.name : t("dashboard.gpuMissing")} detail={data.gpu.present ? data.gpu.vendor : "—"} />
            <Meter
              label="OpenVINO"
              value={data.openvino.active ? t("dashboard.inUse") : data.openvino.installed ? t("dashboard.installed") : t("dashboard.notInstalled")}
              detail={data.openvino.runtime}
            />
          </ul>
          <div className="rounded-m3-xl bg-surface-1 px-5 py-4">
            <p className="font-mono text-[11px] uppercase tracking-wider text-muted">{t("dashboard.onnx")}</p>
            {cls?.measuring && <p className="mt-1 text-sm">{t("dashboard.measuring")}</p>}
            {!cls?.measuring && cls?.installed && frames != null && (
              <>
                <p className="mt-1 text-3xl font-extrabold tabular-nums">{Math.round(frames)} <span className="text-base font-medium">{t("dashboard.framesPerSecond")}</span></p>
                <p className="text-sm text-muted">
                  {t("dashboard.onnxDetail", {
                    ms: cls.latency_ms?.toFixed(0) ?? "—",
                    perMin: Math.round(cls.crops_per_minute ?? frames * 60),
                    threads: cls.threads ?? 2,
                    on: cls.cameras_on,
                    total: cls.cameras_total,
                  })}
                </p>
              </>
            )}
            {!cls?.measuring && !cls?.installed && (
              <p className="mt-1 text-sm text-muted">{t("dashboard.modelNotLoaded")}</p>
            )}
          </div>
        </>
      )}
    </section>
  );
}

function Meter({ label, value, detail }: { label: string; value: string; detail?: string }) {
  return (
    <li className="flex flex-col gap-1 rounded-m3-xl bg-surface-1 px-5 py-4">
      <span className="font-mono text-[11px] tracking-wider text-muted uppercase">{label}</span>
      <span className="truncate text-xl font-bold">{value}</span>
      {detail && <span className="truncate text-xs text-muted">{detail}</span>}
    </li>
  );
}

function fmtBytes(n: number) {
  if (!Number.isFinite(n) || n < 0) return "—";
  const gib = 1024 ** 3;
  if (n >= gib) return `${(n / gib).toFixed(1)} GiB`;
  return `${Math.round(n / 1024 ** 2)} MiB`;
}

function Stat({ to, label, value, detail, warn }: { to: string; label: string; value?: number; detail?: string; warn?: boolean }) {
  return (
    <li>
      <Link to={to} className="m3-press flex min-h-11 flex-col gap-1 rounded-m3-xl bg-surface-1 px-5 py-4 hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-primary">
        <span className="font-mono text-[11px] tracking-wider text-muted uppercase">{label}</span>
        <span className="text-4xl font-extrabold tabular-nums">{value ?? "—"}</span>
        {detail && <span className={cn("text-xs", warn ? "text-warn" : "text-muted")}>{detail}</span>}
      </Link>
    </li>
  );
}

function OverallBadge({ status }: { status: "ok" | "degraded" }) {
  const t = useT();
  return (
    <span
      className={cn(
        "rounded-full px-3 py-1 font-mono text-xs uppercase",
        status === "ok" ? "bg-ok/15 text-ok" : "bg-warn/15 text-warn",
      )}
    >
      {status === "ok" ? t("common.operational") : t("common.degraded")}
    </span>
  );
}

function StatusDot({ ok }: { ok: boolean }) {
  const t = useT();
  return (
    <span className="flex items-center gap-1.5 text-xs">
      <span className={cn("size-2 rounded-full", ok ? "bg-ok" : "bg-bad")} aria-hidden />
      {ok ? "OK" : t("common.error")}
    </span>
  );
}

function Field({ label, value }: { label: string; value?: string }) {
  return (
    <div className="flex flex-col">
      <dt className="font-mono text-[11px] uppercase tracking-wider text-muted">{label}</dt>
      <dd className="truncate font-mono">{value ?? "—"}</dd>
    </div>
  );
}
