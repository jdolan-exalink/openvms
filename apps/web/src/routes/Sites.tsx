import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { api, unwrap } from "@/api/client";
import { meQuery, sitesQuery, tenantsQuery } from "@/api/queries";
import { Button, Empty, ErrorNote, Field, PageHeader, Select, Summary, Table, TextInput, Th } from "@/components/ui";
import { can } from "@/lib/perm";

export function Sites() {
  const me = useQuery(meQuery);
  const sites = useQuery(sitesQuery);
  const [creating, setCreating] = useState(false);
  const [q, setQ] = useState("");
  const needle = q.trim().toLowerCase();
  const visible = sites.data?.filter((s) => !needle || `${s.name} ${s.address ?? ""}`.toLowerCase().includes(needle));

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6">
      <PageHeader
        title="Sitios"
        description="Ubicaciones físicas. Cada sitio agrupa uno o más servidores Frigate."
        actions={
          can(me.data, "sites.manage") && !creating ? (
            <Button variant="primary" onClick={() => setCreating(true)}>
              <Plus className="size-4" aria-hidden /> Nuevo sitio
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
            </tr>
          </thead>
          <tbody>
            {visible.map((s) => (
              <tr key={s.id} className="border-t border-line first:border-t-0">
                <td>
                  <div className="font-medium">{s.name}</div>
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
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}

function CreateSite({ onDone, platform }: { onDone: () => void; platform: boolean }) {
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
    <form onSubmit={submit} className="grid gap-4 rounded border border-line bg-surface p-4 sm:grid-cols-2">
      {platform && (
        <Field label="Tenant">
          <Select required value={tenantId} onChange={(e) => setTenantId(e.target.value)}>
            <option value="">Elegí un tenant</option>
            {tenants.data?.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name}
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
            Crear sitio
          </Button>
          <Button onClick={onDone}>Cancelar</Button>
        </div>
      </div>
    </form>
  );
}
