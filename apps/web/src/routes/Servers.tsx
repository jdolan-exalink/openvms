import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Plus, RefreshCw, RotateCcw, Trash2 } from "lucide-react";
import { Link, useSearch } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { meQuery, serversQuery, sitesQuery, syncStatusQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Modal } from "@/components/Modal";
import { Button, Empty, ErrorNote, Field, PageHeader, Select, StatusBadge, Summary, Table, TextInput, Th } from "@/components/ui";
import { can } from "@/lib/perm";

export function Servers() {
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
              <Link to="/servers" className="text-xs text-accent hover:underline">
                Quitar filtro de sitio
              </Link>
            )}
          </div>
          <Summary>{summarize(servers.data.length, visible ?? [])}</Summary>
        </div>
      )}
      {!!servers.data?.length && visible?.length === 0 && <Empty>Ningún servidor coincide con el filtro.</Empty>}
      {!!visible?.length && (
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
            {visible.map((s) => (
              <tr key={s.id} className="border-t border-line align-top">
                <td>
                  <div className="font-medium">{s.name}</div>
                  <div className="font-mono text-xs break-all text-muted">{s.base_url}</div>
                </td>
                <td>
                  {siteName.has(s.site_id) ? (
                    <Link to="/servers" search={{ site_id: s.site_id }} className="hover:underline">
                      {siteName.get(s.site_id)}
                    </Link>
                  ) : (
                    "—"
                  )}
                </td>
                <td>
                  <StatusBadge status={s.status} />
                  {s.last_error && <div role="alert" className="mt-1 max-w-56 text-xs break-words text-bad">{s.last_error}</div>}
                </td>
                <td className="font-mono text-xs whitespace-nowrap">{s.frigate_version || "—"}</td>
                <td className="text-right tabular-nums">
                  <Link to="/cameras" search={{ server_id: s.id }} className="hover:underline">
                    {s.camera_count}
                  </Link>
                </td>
                <td className="min-w-36">
                  <StorageBar storage={s.storage} />
                </td>
                <td className="text-xs">
                  <SyncCell status={syncOf.get(s.id)} />
                </td>
                <td className="text-right">
                  <div className="flex flex-wrap items-start justify-end gap-2">
                    {can(me.data, "servers.restart") && <RestartButton server={s} />}
                    {can(me.data, "servers.manage") && <SyncButton server={s} />}
                    {can(me.data, "servers.manage") && (
                      <>
                        <Button className="text-xs px-2 py-1" onClick={() => setEditing(s)} title="Editar servidor">
                          <Pencil className="size-3.5" aria-hidden />
                          Editar
                        </Button>
                        <Button className="text-xs px-2 py-1 text-bad" onClick={() => setDeleting(s)} title="Eliminar servidor">
                          <Trash2 className="size-3.5" aria-hidden />
                          Eliminar
                        </Button>
                      </>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
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
          <label className="flex items-center gap-2 self-center text-sm">
            <input type="checkbox" checked={form.tls_skip_verify} onChange={(e) => setForm({ ...form, tls_skip_verify: e.target.checked })} />
            Aceptar certificado autofirmado
          </label>
        </div>
        <ErrorNote error={save.error} />
        <div className="flex justify-end gap-2">
          <Button onClick={onDone}>Cancelar</Button>
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
      <div className="h-1.5 overflow-hidden rounded bg-raised">
        <div className={pct > 90 ? "h-full bg-warn" : "h-full bg-accent"} style={{ width: `${pct}%` }} />
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
          className="text-bad border-bad/40 hover:bg-bad/10 text-xs px-2 py-1"
          disabled={restart.isPending}
          onClick={() => restart.mutate()}
        >
          {restart.isPending ? "Reiniciando…" : "Sí, reiniciar"}
        </Button>
        <Button className="text-xs px-2 py-1" onClick={() => setConfirming(false)}>
          Cancelar
        </Button>
      </div>
    );
  }

  return (
    <div className="flex flex-col items-end gap-1">
      <Button
        className="text-xs px-2 py-1"
        disabled={restart.isPending}
        onClick={() => setConfirming(true)}
        title="Reiniciar servicio Frigate"
      >
        <RotateCcw className={restart.isPending ? "size-3.5 animate-spin" : "size-3.5"} aria-hidden />
        Reiniciar
      </Button>
      {restart.isSuccess && <span className="text-xs text-accent">Reinicio solicitado</span>}
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
      <Button onClick={() => sync.mutate()} disabled={sync.isPending} title="Volver a leer las cámaras de Frigate">
        <RefreshCw className={sync.isPending ? "size-4 animate-spin" : "size-4"} aria-hidden />
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
      {status.last_error && <span role="alert" className="max-w-48 break-words text-bad">{status.last_error}</span>}
    </div>
  );
}
