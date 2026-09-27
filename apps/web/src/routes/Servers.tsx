import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, RefreshCw } from "lucide-react";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { meQuery, serversQuery, sitesQuery, syncStatusQuery } from "@/api/queries";
import { Button, Empty, ErrorNote, Field, PageHeader, Select, StatusBadge, Table, TextInput, Th } from "@/components/ui";
import { can } from "@/lib/perm";

export function Servers() {
  const me = useQuery(meQuery);
  const servers = useQuery(serversQuery);
  const sites = useQuery(sitesQuery);
  const [registering, setRegistering] = useState(false);
  const siteName = new Map(sites.data?.map((s) => [s.id, s.name]));
  const sync = useQuery(syncStatusQuery);
  const syncOf = new Map(sync.data?.map((s) => [s.server_id, s]));

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title="Servidores"
        description="Servidores Frigate registrados y su salud. El worker los consulta cada 30 segundos."
        actions={
          can(me.data, "servers.manage") && !registering ? (
            <Button variant="primary" onClick={() => setRegistering(true)}>
              <Plus className="size-4" aria-hidden /> Registrar servidor
            </Button>
          ) : null
        }
      />
      {registering && <RegisterServer sites={sites.data ?? []} onDone={() => setRegistering(false)} />}
      <ErrorNote error={servers.error} />
      {servers.data?.length === 0 && <Empty>No hay servidores visibles para tu usuario.</Empty>}
      {!!servers.data?.length && (
        <Table label="Servidores">
          <thead>
            <tr>
              <Th>Servidor</Th>
              <Th>Sitio</Th>
              <Th>Estado</Th>
              <Th>Frigate</Th>
              <Th className="text-right">Cámaras</Th>
              <Th>Almacenamiento</Th>
              <Th>Eventos</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {servers.data.map((s) => (
              <tr key={s.id} className="border-t border-line align-top">
                <td>
                  <div className="font-medium">{s.name}</div>
                  <div className="font-mono text-xs break-all text-muted">{s.base_url}</div>
                </td>
                <td>{siteName.get(s.site_id) ?? "—"}</td>
                <td>
                  <StatusBadge status={s.status} />
                  {s.last_error && <div className="mt-1 max-w-56 text-xs break-words text-bad">{s.last_error}</div>}
                </td>
                <td className="font-mono text-xs whitespace-nowrap">{s.frigate_version || "—"}</td>
                <td className="text-right tabular-nums">{s.camera_count}</td>
                <td className="min-w-36">
                  <StorageBar storage={s.storage} />
                </td>
                <td className="text-xs">
                  <SyncCell status={syncOf.get(s.id)} />
                </td>
                <td className="text-right">{can(me.data, "servers.manage") && <SyncButton server={s} />}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}

function StorageBar({ storage }: { storage?: Schemas["ServerStorage"] }) {
  if (!storage || storage.total_mb <= 0) return <span className="text-xs text-muted">—</span>;
  const pct = Math.round((storage.used_mb / storage.total_mb) * 100);
  return (
    <div className="flex flex-col gap-1" title={`${pct}% usado`}>
      <div className="h-1.5 overflow-hidden rounded bg-raised">
        <div className={pct > 90 ? "h-full bg-warn" : "h-full bg-accent"} style={{ width: `${pct}%` }} />
      </div>
      <span className="font-mono text-[11px] text-muted tabular-nums">
        {(storage.used_mb / 1024).toFixed(0)} / {(storage.total_mb / 1024).toFixed(0)} GB
      </span>
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
      <Button onClick={() => sync.mutate()} disabled={sync.isPending} title="Volver a leer las cámaras de Frigate">
        <RefreshCw className={sync.isPending ? "size-4 animate-spin" : "size-4"} aria-hidden />
        Sincronizar
      </Button>
      {r && (
        <span className="text-xs text-muted">
          +{r.added} nuevas · {r.updated} actualizadas · {r.missing} faltantes
        </span>
      )}
      {sync.error && <span className="text-xs text-bad">{sync.error.message}</span>}
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

// RegisterServer is the two-step wizard from PRD §59: test the connection and show what
// Frigate reports, then register it and import its cameras.
function RegisterServer({ sites, onDone }: { sites: Schemas["Site"][]; onDone: () => void }) {
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
    <form onSubmit={submit} className="flex flex-col gap-4 rounded border border-line bg-surface p-4">
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
        <label className="flex items-center gap-2 self-center text-sm">
          <input
            type="checkbox"
            checked={form.tls_skip_verify}
            onChange={(e) => {
              probe.reset();
              setForm({ ...form, tls_skip_verify: e.target.checked });
            }}
          />
          Aceptar certificado autofirmado
        </label>
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
        <section aria-label="Resultado de la prueba" className="flex flex-col gap-2 rounded border border-ok/40 bg-ok/5 p-3 text-sm">
          <p>
            Conectado a Frigate <span className="font-mono">{probe.data.frigate_version}</span> (adaptador{" "}
            <span className="font-mono">{probe.data.adapter}</span>), {probe.data.cameras.length} cámaras.
          </p>
          <ul className="flex flex-wrap gap-1.5">
            {capLabels
              .filter(([k]) => probe.data.capabilities[k])
              .map(([, label]) => (
                <li key={label} className="rounded bg-raised px-2 py-0.5 text-xs">
                  {label}
                </li>
              ))}
          </ul>
          <p className="font-mono text-xs text-muted">{probe.data.cameras.map((c) => c.remote_name).join(" · ")}</p>
          <label className="flex items-center gap-2">
            <input type="checkbox" checked={importCameras} onChange={(e) => setImportCameras(e.target.checked)} />
            Importar las cámaras
          </label>
        </section>
      )}

      <ErrorNote error={probe.error ?? create.error} />
      <div className="flex gap-2">
        <Button type="submit" variant="primary" disabled={probe.isPending || create.isPending}>
          {probe.data ? (create.isPending ? "Registrando…" : "Registrar servidor") : probe.isPending ? "Probando…" : "Probar conexión"}
        </Button>
        <Button onClick={onDone}>Cancelar</Button>
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
      {status.last_error && <span className="max-w-48 break-words text-bad">{status.last_error}</span>}
    </div>
  );
}
