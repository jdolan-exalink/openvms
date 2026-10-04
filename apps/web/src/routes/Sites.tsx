import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { meQuery, serversQuery, sitesQuery, tenantsQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Modal } from "@/components/Modal";
import { Icon } from "@/components/Icon";
import { Button, Empty, ErrorNote, Field, PageHeader, Select, Summary, Table, TextInput, Th } from "@/components/ui";
import { can } from "@/lib/perm";

type Site = Schemas["Site"];

/** defaultSiteIds marks the oldest site of each tenant. That site is the default, and a tenant always keeps at least one. */
function defaultSiteIds(sites: Site[]) {
  const best = new Map<string, Site>();
  for (const site of sites) {
    const current = best.get(site.tenant_id);
    if (!current || site.created_at < current.created_at || (site.created_at === current.created_at && site.id < current.id)) {
      best.set(site.tenant_id, site);
    }
  }
  return new Set([...best.values()].map((site) => site.id));
}

export function Sites() {
  const t = useT();
  const me = useQuery(meQuery);
  const sites = useQuery(sitesQuery);
  const [creating, setCreating] = useState(false);
  const [editingId, setEditingId] = useState<string>();
  const [q, setQ] = useState("");
  const needle = q.trim().toLowerCase();
  const defaults = defaultSiteIds(sites.data ?? []);
  const counts = new Map<string, number>();
  for (const site of sites.data ?? []) counts.set(site.tenant_id, (counts.get(site.tenant_id) ?? 0) + 1);
  const visible = sites.data
    ?.filter((s) => !needle || `${s.name} ${s.address ?? ""}`.toLowerCase().includes(needle))
    .slice()
    .sort((a, b) => Number(defaults.has(b.id)) - Number(defaults.has(a.id)) || a.name.localeCompare(b.name, "es"));
  const editing = sites.data?.find((s) => s.id === editingId);
  const manage = can(me.data, "sites.manage");

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6">
      <PageHeader
        title={t("nav.sites")}
        description={t("settings.sites")}
        actions={
          manage && !creating ? (
            <Button variant="primary" onClick={() => setCreating(true)}>
              <Icon icon={Plus} size="sm" /> {t("common.newSite")}
            </Button>
          ) : null
        }
      />
      {creating && <CreateSite onDone={() => setCreating(false)} platform={me.data?.tenant_id === null} />}
      <ErrorNote error={sites.error} />
      {sites.data?.length === 0 && <Empty>No hay sitios visibles para tu usuario.</Empty>}
      {!!sites.data?.length && (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <TextInput className="sm:max-w-xs" aria-label="Buscar sitio" placeholder="Buscar por nombre o dirección" value={q} onChange={(e) => setQ(e.target.value)} />
          <Summary>
            {visible?.length === sites.data.length ? "" : `${visible?.length} de `}
            {sites.data.length} {sites.data.length === 1 ? "sitio" : "sitios"}
          </Summary>
        </div>
      )}
      {!!sites.data?.length && visible?.length === 0 && <Empty>Ningún sitio coincide con la búsqueda.</Empty>}
      {!!visible?.length && (
        <Table label="Sitios">
          <thead>
            <tr>
              <Th>Nombre</Th>
              <Th>Zona horaria</Th>
              <Th className="text-right">Servidores</Th>
              <Th className="text-right">Cámaras</Th>
              {manage && <Th className="text-right">Acciones</Th>}
            </tr>
          </thead>
          <tbody>
            {visible.map((s) => (
              <tr key={s.id} className="border-t border-outline-variant first:border-t-0">
                <td>
                  <div className="flex items-center gap-2 font-medium">
                    {s.name}
                    {defaults.has(s.id) && (
                      <span className="rounded-full bg-primary-container px-2 py-0.5 text-[10px] font-bold uppercase tracking-wide text-on-primary-container">{t("common.default")}</span>
                    )}
                  </div>
                  {s.address && <div className="text-xs text-muted">{s.address}</div>}
                </td>
                <td className="font-mono text-xs text-muted">{s.timezone}</td>
                <td className="text-right tabular-nums">
                  <Link to="/servers" search={{ site_id: s.id }} className="hover:underline">
                    {s.server_count}
                  </Link>
                </td>
                <td className="text-right tabular-nums">
                  <Link to="/cameras" search={{ site_id: s.id }} className="hover:underline">
                    {s.camera_count}
                  </Link>
                </td>
                {manage && (
                  <td className="text-right">
                    <Button size="sm" aria-label={`${t("common.editSite")} ${s.name}`} onClick={() => setEditingId(s.id)}>
                      {t("common.editSite")}
                    </Button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {editing && (
        <SiteEditor
          site={editing}
          sites={sites.data ?? []}
          isDefault={defaults.has(editing.id)}
          isLast={(counts.get(editing.tenant_id) ?? 1) <= 1}
          canMoveServers={can(me.data, "servers.manage")}
          onClose={() => setEditingId(undefined)}
        />
      )}
    </div>
  );
}

function SiteEditor({
  site,
  sites,
  isDefault,
  isLast,
  canMoveServers,
  onClose,
}: {
  site: Site;
  sites: Site[];
  isDefault: boolean;
  isLast: boolean;
  canMoveServers: boolean;
  onClose: () => void;
}) {
  const t = useT();
  const qc = useQueryClient();
  const servers = useQuery(serversQuery);
  const [name, setName] = useState(site.name);
  const [timezone, setTimezone] = useState(site.timezone);
  const [address, setAddress] = useState(site.address ?? "");
  const [moves, setMoves] = useState<Record<string, string>>({});
  const [confirming, setConfirming] = useState(false);
  const mine = (servers.data ?? []).filter((server) => server.site_id === site.id);
  const others = sites.filter((item) => item.id !== site.id && item.tenant_id === site.tenant_id);
  const destination = (serverId: string, current: string) => moves[serverId] ?? current;

  const save = useMutation({
    mutationFn: async () => {
      await unwrap(await api.PATCH("/api/v1/sites/{siteId}", { params: { path: { siteId: site.id } }, body: { name: name.trim(), timezone: timezone.trim(), address: address.trim() } }));
      for (const server of mine) {
        const next = destination(server.id, server.site_id);
        if (next === server.site_id) continue;
        await unwrap(await api.PATCH("/api/v1/servers/{serverId}", { params: { path: { serverId: server.id } }, body: { site_id: next } }));
      }
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["sites"] });
      await qc.invalidateQueries({ queryKey: ["servers"] });
      onClose();
    },
  });

  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/api/v1/sites/{siteId}", { params: { path: { siteId: site.id } } })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["sites"] });
      onClose();
    },
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    save.mutate();
  }

  const pendingMove = mine.some((server) => destination(server.id, server.site_id) !== server.site_id);
  const blocked = isLast || site.server_count > 0 || pendingMove;

  return (
    <Modal title={t("common.editSite")} onClose={onClose}>
      <form onSubmit={submit} className="flex flex-col gap-4">
        {isDefault && <p className="text-xs text-muted">Este es el sitio por defecto. Siempre tiene que quedar al menos uno.</p>}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Nombre">
            <TextInput required value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="Zona horaria">
            <TextInput required value={timezone} onChange={(e) => setTimezone(e.target.value)} />
          </Field>
          <Field label="Dirección">
            <TextInput value={address} onChange={(e) => setAddress(e.target.value)} />
          </Field>
        </div>
        <div className="flex flex-col gap-2">
          <h3 className="text-sm font-medium">Servidores</h3>
          {mine.length === 0 && <p className="text-sm text-muted">Este sitio no tiene servidores.</p>}
          {mine.map((server) => (
            <div key={server.id} className="grid items-center gap-2 rounded-m3-lg bg-surface-2 p-3 sm:grid-cols-[1fr_1fr]">
              <span className="text-sm font-medium">{server.name}</span>
              <Select
                aria-label={`Sitio de ${server.name}`}
                disabled={!canMoveServers || others.length === 0}
                value={destination(server.id, server.site_id)}
                onChange={(e) => setMoves((current) => ({ ...current, [server.id]: e.target.value }))}
              >
                <option value={site.id}>{site.name}</option>
                {others.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name}
                  </option>
                ))}
              </Select>
            </div>
          ))}
          {!!mine.length && !canMoveServers && <p className="text-xs text-muted">Necesitás servers.manage para mover servidores.</p>}
        </div>
        <ErrorNote error={save.error} />
        <div className="flex flex-wrap items-center justify-between gap-2">
          <Button
            variant="outlined"
            className="text-bad"
            disabled={blocked}
            title={isLast ? "Siempre tiene que quedar al menos un sitio" : site.server_count > 0 || pendingMove ? "Mové los servidores a otro sitio y guardá antes de eliminarlo" : undefined}
            onClick={() => setConfirming(true)}
          >
            {t("common.deleteSite")}
          </Button>
          <div className="flex gap-2">
            <Button variant="text" onClick={onClose}>Cancelar</Button>
            <Button type="submit" variant="primary" disabled={save.isPending || !name.trim()}>
              Guardar
            </Button>
          </div>
        </div>
      </form>
      {confirming && (
        <ConfirmDialog
          title={t("common.deleteSite")}
          message={`¿Eliminar ${site.name}? Esta acción no se puede deshacer.`}
          confirmLabel={t("common.deleteSite")}
          pending={remove.isPending}
          error={remove.error}
          onConfirm={() => remove.mutate()}
          onCancel={() => setConfirming(false)}
        />
      )}
    </Modal>
  );
}

function CreateSite({ onDone, platform }: { onDone: () => void; platform: boolean }) {
  const t = useT();
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const tenants = useQuery({ ...tenantsQuery, enabled: platform });
  const [name, setName] = useState("");
  const [timezone, setTimezone] = useState("America/Argentina/Buenos_Aires");
  const [address, setAddress] = useState("");
  const [tenantId, setTenantId] = useState("");

  const create = useMutation({
    mutationFn: async () => {
      const tenant_id = platform ? tenantId : (me.data?.tenant_id ?? "");
      return unwrap(await api.POST("/api/v1/sites", { body: { tenant_id, name, timezone, address } }));
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["sites"] });
      onDone();
    },
  });

  function submit(e: FormEvent) {
    e.preventDefault();
    create.mutate();
  }

  return (
    <form onSubmit={submit} className="grid gap-4 rounded-m3-xl bg-surface-1 p-5 sm:grid-cols-2">
      {platform && (
        <Field label="Tenant">
          <Select required value={tenantId} onChange={(e) => setTenantId(e.target.value)}>
            <option value="">Elegí un tenant</option>
            {tenants.data?.map((tenant) => (
              <option key={tenant.id} value={tenant.id}>
                {tenant.name}
              </option>
            ))}
          </Select>
        </Field>
      )}
      <Field label="Nombre">
        <TextInput required value={name} onChange={(e) => setName(e.target.value)} placeholder="Helvecia" />
      </Field>
      <Field label="Zona horaria">
        <TextInput required value={timezone} onChange={(e) => setTimezone(e.target.value)} />
      </Field>
      <Field label="Dirección">
        <TextInput value={address} onChange={(e) => setAddress(e.target.value)} />
      </Field>
      <div className="flex flex-col gap-3 sm:col-span-2">
        <ErrorNote error={create.error} />
        <div className="flex gap-2">
          <Button type="submit" variant="primary" disabled={create.isPending}>
            {t("common.newSite")}
          </Button>
          <Button variant="text" onClick={onDone}>Cancelar</Button>
        </div>
      </div>
    </form>
  );
}
