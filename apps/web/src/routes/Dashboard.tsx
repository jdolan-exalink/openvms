import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { camerasQuery, readinessQuery, serversQuery, sitesQuery, syncStatusQuery, systemInfoQuery } from "@/api/queries";
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
        title="Resumen"
        description="Estado de la plataforma central y del inventario que tu usuario puede ver."
      />

      <section aria-labelledby="inventory" className="flex flex-col gap-3">
        <h2 id="inventory" className="text-lg font-semibold">Inventario</h2>
        <ul className="grid gap-3 sm:grid-cols-3">
          <Stat to="/sites" label="Sitios" value={sites.data?.length} />
          <Stat
            to="/servers"
            label="Servidores"
            value={servers.data?.length}
            detail={servers.data ? `${online(servers.data)} en línea` : undefined}
            warn={!!servers.data && online(servers.data) < servers.data.length}
          />
          <Stat
            to="/cameras"
            label="Cámaras"
            value={cameras.data?.length}
            detail={cameras.data ? `${online(cameras.data)} en línea` : undefined}
            warn={!!cameras.data && online(cameras.data) < cameras.data.filter((c) => c.enabled).length}
          />
        </ul>
      </section>

      <section aria-labelledby="event-sync" className="flex flex-col gap-3">
        <h2 id="event-sync" className="text-lg font-semibold">Sincronización de eventos</h2>
        {servers.data && servers.data.length === 0 && (
          <p className="text-sm text-muted">No hay servidores registrados.</p>
        )}
        <ul className="grid gap-3 sm:grid-cols-2">
          {servers.data?.map((srv) => {
            const st = syncMap.get(srv.id);
            return (
              <li key={srv.id} className="flex flex-col gap-1 rounded border border-line bg-surface p-3 text-sm">
                <div className="flex items-center justify-between">
                  <span className="font-medium">{srv.name}</span>
                  <StatusDot ok={!st?.last_error} />
                </div>
                <div className="flex items-center justify-between text-xs text-muted">
                  <span>Eventos sincronizados: {st?.event_count ?? 0}</span>
                  {st?.last_success_at && (
                    <span>Último éxito: {fmtDateTime(st.last_success_at)}</span>
                  )}
                </div>
                {st?.last_error && <span className="text-xs text-bad">{st.last_error}</span>}
              </li>
            );
          })}
        </ul>
      </section>

      <section aria-labelledby="deps" className="flex flex-col gap-3">
        <div className="flex items-baseline justify-between gap-4">
          <h2 id="deps" className="text-lg font-semibold">Servicios del control plane</h2>
          {ready.data && <OverallBadge status={ready.data.status} />}
        </div>
        {ready.isError && (
          <p role="alert" className="rounded border border-bad/40 bg-bad/10 px-3 py-2 text-sm text-bad">
            No se pudo contactar a la API: {ready.error.message}
          </p>
        )}
        <ul className="grid gap-3 sm:grid-cols-2">
          {(ready.data?.checks ?? []).map((c) => (
            <li key={c.name} className="flex items-center justify-between gap-3 rounded border border-line bg-surface px-4 py-3">
              <div className="flex flex-col">
                <span className="font-medium">{dependencyLabels[c.name] ?? c.name}</span>
                {c.error && <span className="text-xs break-all text-bad">{c.error}</span>}
              </div>
              <div className="flex items-center gap-3">
                <span className="font-mono text-xs text-muted tabular-nums">{c.latency_ms} ms</span>
                <StatusDot ok={c.status === "ok"} />
              </div>
            </li>
          ))}
          {ready.isPending &&
            Array.from({ length: 4 }, (_, i) => (
              <li key={i} className="h-[58px] animate-pulse rounded border border-line bg-surface" />
            ))}
        </ul>
      </section>

      <section aria-labelledby="build" className="flex flex-col gap-3">
        <h2 id="build" className="text-lg font-semibold">Versión</h2>
        <dl className="grid grid-cols-2 gap-x-6 gap-y-2 rounded border border-line bg-surface px-4 py-3 text-sm sm:grid-cols-4">
          <Field label="API" value={info.data?.version} />
          <Field label="Commit" value={info.data?.commit.slice(0, 12)} />
          <Field label="Esquema DB" value={info.data ? `v${info.data.schema_version}` : undefined} />
          <Field label="Go" value={info.data?.go_version} />
        </dl>
        <p className="text-sm text-muted">
          Contrato de la API: <a className="text-accent underline-offset-2 hover:underline" href="/docs">/docs</a>
        </p>
      </section>
    </div>
  );
}

function Stat({ to, label, value, detail, warn }: { to: string; label: string; value?: number; detail?: string; warn?: boolean }) {
  return (
    <li>
      <Link to={to} className="flex flex-col gap-1 rounded border border-line bg-surface px-4 py-3 hover:bg-raised">
        <span className="font-mono text-[11px] tracking-wider text-muted uppercase">{label}</span>
        <span className="text-2xl font-semibold tabular-nums">{value ?? "—"}</span>
        {detail && <span className={cn("text-xs", warn ? "text-warn" : "text-muted")}>{detail}</span>}
      </Link>
    </li>
  );
}

function OverallBadge({ status }: { status: "ok" | "degraded" }) {
  return (
    <span
      className={cn(
        "rounded px-2 py-0.5 font-mono text-xs uppercase",
        status === "ok" ? "bg-ok/15 text-ok" : "bg-warn/15 text-warn",
      )}
    >
      {status === "ok" ? "Operativo" : "Degradado"}
    </span>
  );
}

function StatusDot({ ok }: { ok: boolean }) {
  return (
    <span className="flex items-center gap-1.5 text-xs">
      <span className={cn("size-2 rounded-full", ok ? "bg-ok" : "bg-bad")} aria-hidden />
      {ok ? "OK" : "Error"}
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
