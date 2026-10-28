import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCw, Trash2 } from "lucide-react";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { ChannelPicker } from "@/components/servers/ChannelPicker";
import { ProvisionCapabilities } from "@/components/servers/ProvisionCapabilities";
import { ServerDetailTabs } from "@/components/servers/ServerDetailTabs";
import { problemText } from "@/components/servers/ServerSummaryTab";
import { Icon } from "@/components/Icon";
import { Modal } from "@/components/Modal";
import { Button, ErrorNote, Field, Select, StatusBadge, Switch, TextInput } from "@/components/ui";
import { useT } from "@/i18n";
import { fmtDateTime } from "@/lib/format";

const refresh = (qc: ReturnType<typeof useQueryClient>) =>
  Promise.all(["servers", "sites", "cameras"].map((k) => qc.invalidateQueries({ queryKey: [k] })));

function Summary({ server }: { server: Schemas["Server"] }) {
  const t = useT();
  const info = server.provision;
  return (
    <div className="flex flex-col gap-4">
      <dl className="grid grid-cols-2 gap-3 text-sm md:grid-cols-4">
        <div><dt className="text-xs text-muted">{t("servers.provisionModel")}</dt><dd>{info?.model || "—"}</dd></div>
        <div><dt className="text-xs text-muted">{t("servers.provisionFirmware")}</dt><dd className="font-mono text-xs break-all">{info?.firmware || "—"}</dd></div>
        <div><dt className="text-xs text-muted">{t("servers.provisionChannelCount")}</dt><dd className="tabular-nums">{server.camera_count}{info?.channel_capacity ? ` / ${info.channel_capacity}` : ""}</dd></div>
        <div><dt className="text-xs text-muted">Estado</dt><dd><StatusBadge status={server.status} /></dd></div>
      </dl>
      {server.last_seen_at && <p className="text-xs text-muted">{fmtDateTime(server.last_seen_at)}</p>}
      {server.problems?.length ? (
        <ul className="flex flex-col gap-0.5 text-xs text-warn">
          {server.problems.map((p) => <li key={p.code}>{problemText(p, t)}</li>)}
        </ul>
      ) : server.last_error ? <p role="alert" className="text-xs break-words text-bad">{server.last_error}</p> : null}
      {info && info.capabilities.length > 0 ? (
        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-semibold">{t("servers.provisionCapabilitiesTitle")}</h3>
          <ProvisionCapabilities capabilities={info.capabilities} />
        </section>
      ) : <p className="text-sm text-muted">{t("servers.provisionNotRead")}</p>}
    </div>
  );
}

function Channels({ server }: { server: Schemas["Server"] }) {
  const t = useT();
  const qc = useQueryClient();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [result, setResult] = useState<Schemas["ProvisionNvrImportResult"] | null>(null);
  const path = { params: { path: { serverId: server.id } } };
  const list = useQuery({
    queryKey: ["provision-nvr-channels", server.id],
    enabled: false,
    retry: false,
    queryFn: async () => unwrap(await api.GET("/api/v1/servers/{serverId}/provision-nvr/channels", path)),
  });
  const add = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/api/v1/servers/{serverId}/provision-nvr/channels", { ...path, body: { channel_ids: [...selected] } })),
    onSuccess: async (res) => {
      setResult(res);
      setSelected(new Set());
      await refresh(qc);
      await list.refetch();
    },
  });
  const channels = list.data?.items ?? [];
  const pickable = channels.filter((c) => !c.imported);
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted">{t("servers.provisionAddChannelsHint")}</p>
      <div>
        <Button onClick={() => { setResult(null); void list.refetch(); }} disabled={list.isFetching}>
          <Icon icon={RefreshCw} size="xs" className={list.isFetching ? "animate-spin" : undefined} />
          {list.isFetching ? t("servers.provisionReading") : t("servers.provisionReadChannels")}
        </Button>
      </div>
      <ErrorNote error={list.error ?? add.error} />
      {list.data && (
        <>
          <ChannelPicker channels={channels} selected={selected} onChange={setSelected} />
          {pickable.length === 0 && <p className="text-sm text-muted">{t("servers.provisionNoNewChannels")}</p>}
          <div>
            <Button variant="primary" onClick={() => add.mutate()} disabled={add.isPending || selected.size === 0}>
              {add.isPending ? t("servers.provisionAdding") : t("servers.provisionAddSelected")}
            </Button>
          </div>
        </>
      )}
      {result && <p role="status" className="text-sm">{t("servers.provisionChannelsAdded", { added: String(result.added), skipped: String(result.skipped) })}</p>}
    </div>
  );
}

function splitBase(baseUrl: string) {
  try {
    const u = new URL(baseUrl);
    const https = u.protocol === "https:";
    return { host: u.hostname.replace(/^\[|\]$/g, ""), port: u.port || (https ? "443" : "80"), https };
  } catch {
    return { host: "", port: "80", https: false };
  }
}

function joinBase(host: string, port: string, https: boolean) {
  const h = host.trim();
  return `${https ? "https" : "http"}://${h.includes(":") ? `[${h}]` : h}:${port || (https ? "443" : "80")}`;
}

function Connection({ server, sites, onDone }: { server: Schemas["Server"]; sites: Schemas["Site"][]; onDone: () => void }) {
  const t = useT();
  const qc = useQueryClient();
  const initial = splitBase(server.base_url);
  const [form, setForm] = useState({ name: server.name, site_id: server.site_id, ...initial, username: server.username, password: "" });
  const save = useMutation({
    mutationFn: async () => {
      const body: Schemas["ServerUpdate"] = {};
      if (form.name !== server.name) body.name = form.name;
      if (form.site_id !== server.site_id) body.site_id = form.site_id;
      const base = joinBase(form.host, form.port, form.https);
      if (base !== joinBase(initial.host, initial.port, initial.https)) body.base_url = base;
      if (form.username !== server.username) body.username = form.username;
      if (form.password) body.password = form.password;
      return unwrap(await api.PATCH("/api/v1/servers/{serverId}", { params: { path: { serverId: server.id } }, body }));
    },
    onSuccess: async () => {
      await refresh(qc);
      onDone();
    },
  });
  const edit = (k: "host" | "port" | "username" | "password") => (e: { target: { value: string } }) => setForm({ ...form, [k]: e.target.value });
  return (
    <form onSubmit={(e: FormEvent) => { e.preventDefault(); save.mutate(); }} className="flex flex-col gap-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Nombre"><TextInput required value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></Field>
        <Field label="Sitio">
          <Select required value={form.site_id} onChange={(e) => setForm({ ...form, site_id: e.target.value })}>
            {sites.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
          </Select>
        </Field>
        <Field label={t("servers.provisionHost")}><TextInput required value={form.host} onChange={edit("host")} autoComplete="off" /></Field>
        <Field label={t("servers.provisionPort")}><TextInput required type="number" min={1} max={65535} value={form.port} onChange={edit("port")} /></Field>
        <Switch className="self-center" label={t("servers.provisionHttps")} checked={form.https} onChange={(next) => setForm({ ...form, https: next })} />
        <div className="hidden sm:block" />
        <Field label={t("servers.provisionUser")}><TextInput required value={form.username} onChange={edit("username")} autoComplete="off" /></Field>
        <Field label={t("servers.provisionPassword")} hint={t("servers.provisionPasswordKeep")}>
          <TextInput type="password" value={form.password} onChange={edit("password")} autoComplete="new-password" />
        </Field>
      </div>
      <p className="text-xs text-muted">{t("servers.provisionConnectionNote")}</p>
      <ErrorNote error={save.error} />
      <div className="flex justify-end gap-2">
        <Button variant="text" onClick={onDone}>Cancelar</Button>
        <Button type="submit" variant="primary" disabled={save.isPending}>{save.isPending ? "Guardando…" : "Guardar"}</Button>
      </div>
    </form>
  );
}

function Actions({ server, onDelete }: { server: Schemas["Server"]; onDelete: (server: Schemas["Server"]) => void }) {
  const t = useT();
  const qc = useQueryClient();
  const sync = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/servers/{serverId}/sync", { params: { path: { serverId: server.id } } })),
    onSuccess: () => refresh(qc),
  });
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button onClick={() => sync.mutate()} disabled={sync.isPending} title={t("servers.provisionSyncTitle")}>
          <Icon icon={RefreshCw} size="xs" className={sync.isPending ? "animate-spin" : undefined} />
          Sincronizar
        </Button>
        {sync.data && <span className="text-xs text-muted">{sync.data.updated} actualizadas · {sync.data.missing} faltantes</span>}
        <Button variant="outlined" onClick={() => onDelete(server)}>
          <Icon icon={Trash2} size="xs" />
          Eliminar servidor
        </Button>
      </div>
      <ErrorNote error={sync.error} />
    </div>
  );
}

/** ProvisionNvrModal is the edit dialog of a Provision-ISR recorder: it has no Frigate tabs or actions. */
export function ProvisionNvrModal({ server, sites, onDone, onDelete }: {
  server: Schemas["Server"];
  sites: Schemas["Site"][];
  onDone: () => void;
  onDelete: (server: Schemas["Server"]) => void;
}) {
  const t = useT();
  return (
    <Modal title={`Editar servidor ${server.name}`} onClose={onDone} className="max-w-4xl">
      <ServerDetailTabs
        label={t("servers.detailTabsLabel")}
        tabs={[
          { id: "summary", label: t("servers.tabSummary"), content: <Summary server={server} /> },
          { id: "channels", label: t("servers.provisionChannelsTitle"), content: <Channels server={server} /> },
          { id: "connection", label: t("servers.tabConnection"), content: <Connection server={server} sites={sites} onDone={onDone} /> },
          { id: "actions", label: t("servers.tabActions"), content: <Actions server={server} onDelete={onDelete} /> },
        ]}
      />
    </Modal>
  );
}
