import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Plus } from "lucide-react";
import { type FormEvent, useState } from "react";
import { ApiError, api, type Schemas, unwrap } from "@/api/client";
import { cameraGroupsQuery, camerasQuery, meQuery, serversQuery, sitesQuery, tenantsQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Modal } from "@/components/Modal";
import { Icon } from "@/components/Icon";
import { Button, Checkbox, Empty, ErrorNote, Field, LinkButton, PageHeader, Select, Summary, Table, TextInput, Th } from "@/components/ui";
import { BulkConfigureDialog } from "@/components/camera/bulk/BulkConfigureDialog";
import { can, canCamera } from "@/lib/perm";

/** Camera groups allow grouping cameras for permissions and view layouts. */
export function CameraGroups() {
  const t = useT();
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const groups = useQuery(cameraGroupsQuery);
  const cameras = useQuery(camerasQuery({}));
  const [editing, setEditing] = useState<Schemas["CameraGroup"] | "new" | null>(null);
  const [q, setQ] = useState("");
  const [configuring, setConfiguring] = useState<Schemas["CameraGroup"] | null>(null);
  const manage = can(me.data, "cameras.manage");

  const needle = q.trim().toLowerCase();
  const visible = groups.data?.filter(
    (g) => !needle || g.name.toLowerCase().includes(needle) || g.description.toLowerCase().includes(needle),
  );

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title={t("nav.cameraGroups")}
        description={t("settings.cameraGroups")}
        actions={
          manage ? (
            <Button variant="primary" onClick={() => setEditing("new")}>
              <Icon icon={Plus} size="sm" /> Nuevo grupo
            </Button>
          ) : null
        }
      />
      {configuring && <BulkConfigureDialog target={{ groupId: configuring.id }} targetLabel={configuring.name} onClose={() => setConfiguring(null)} />}
      {editing && (
        <CameraGroupForm
          group={editing === "new" ? undefined : editing}
          cameras={cameras.data ?? []}
          onDone={() => {
            setEditing(null);
            void qc.invalidateQueries({ queryKey: ["camera-groups"] });
          }}
        />
      )}
      <ErrorNote error={groups.error} />
      {groups.data?.length === 0 && <Empty>No hay grupos de cámaras.</Empty>}
      {!!groups.data?.length && (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <TextInput
            className="sm:w-72"
            aria-label="Buscar grupo"
            placeholder="Buscar por nombre o descripción"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
          <Summary>{summarize(groups.data.length, visible?.length ?? 0)}</Summary>
        </div>
      )}
      {!!groups.data?.length && visible?.length === 0 && <Empty>Ningún grupo coincide con el filtro.</Empty>}
      {!!visible?.length && (
        <Table label="Grupos de cámaras">
          <thead>
            <tr>
              <Th>Nombre</Th>
              <Th>Cámaras</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {visible.map((g) => (
              <tr key={g.id} className="border-t border-outline-variant align-top">
                <td>
                  <div className="font-medium">{g.name}</div>
                  {g.description && <div className="text-xs text-muted">{g.description}</div>}
                </td>
                <td className="text-sm text-muted">
                  <div>{g.member_count_total === 1 ? "1 cámara" : `${g.member_count_total} cámaras`}</div>
                  {g.filter && <div className="text-xs">Dynamic</div>}
                  {g.member_count_hidden > 0 && <div className="text-xs">{g.member_count_hidden} hidden from you</div>}
                </td>
                <td className="text-right">
                  {can(me.data, "permissions.manage") && (
                    <LinkButton size="sm" to="/permissions" search={{ subject: `camera_group:${g.id}` }} className="mr-2">
                      <Icon icon={KeyRound} size="xs" /> Permisos
                    </LinkButton>
                  )}
                  {canCamera(me.data, "cameras.config.edit") && g.member_count_total > 0 && (
                    <Button size="sm" className="mr-2" aria-label={`Configurar cámaras de ${g.name}`} onClick={() => setConfiguring(g)}>
                      Configurar cámaras
                    </Button>
                  )}
                  {manage && (
                    <Button size="sm" aria-label={`Editar ${g.name}`} onClick={() => setEditing(g)}>
                      Editar
                    </Button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}

function summarize(total: number, shown: number) {
  const noun = total === 1 ? "grupo" : "grupos";
  return shown === total ? `${total} ${noun}` : `${shown} de ${total} ${noun}`;
}

function CameraGroupForm({
  group,
  cameras,
  onDone,
}: {
  group?: Schemas["CameraGroup"];
  cameras: Schemas["Camera"][];
  onDone: () => void;
}) {
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const tenants = useQuery({ ...tenantsQuery, enabled: me.data?.tenant_id === null && !group });
  const [confirming, setConfirming] = useState(false);
  const [f, setF] = useState({
    tenant_id: group?.tenant_id ?? me.data?.tenant_id ?? "",
    name: group?.name ?? "",
    description: group?.description ?? "",
    camera_ids: group?.camera_ids ?? ([] as string[]),
  });
  const [kind, setKind] = useState<"manual" | "dynamic">(group?.filter ? "dynamic" : "manual");
  const [filter, setFilter] = useState({
    site_ids: group?.filter?.site_ids ?? ([] as string[]),
    server_ids: group?.filter?.server_ids ?? ([] as string[]),
    tags: (group?.filter?.tags ?? []).join(", "),
    name_contains: group?.filter?.name_contains ?? "",
  });
  const dynamic = kind === "dynamic";
  const sites = useQuery({ ...sitesQuery, enabled: dynamic });
  const servers = useQuery({ ...serversQuery, enabled: dynamic });
  const filterEmpty =
    !filter.site_ids.length && !filter.server_ids.length && !filter.tags.split(",").some((x) => x.trim()) && !filter.name_contains.trim();
  const pickersFailed = dynamic && (sites.isError || servers.isError);

  const save = useMutation({
    mutationFn: async () => {
      const base = { tenant_id: f.tenant_id, name: f.name.trim(), description: f.description.trim() };
      const tags = filter.tags.split(",").map((x) => x.trim()).filter(Boolean);
      const name_contains = filter.name_contains.trim();
      const payload: Schemas["CameraGroupInput"] = dynamic
        ? {
            ...base,
            filter: {
              ...(filter.site_ids.length ? { site_ids: filter.site_ids } : {}),
              ...(filter.server_ids.length ? { server_ids: filter.server_ids } : {}),
              ...(tags.length ? { tags } : {}),
              ...(name_contains ? { name_contains } : {}),
            },
          }
        : { ...base, camera_ids: f.camera_ids };
      if (group) {
        return unwrap(
          await api.PUT("/api/v1/camera-groups/{groupId}", {
            params: { path: { groupId: group.id }, header: { "If-Match": String(group.version) } },
            body: payload,
          }),
        );
      }
      return unwrap(await api.POST("/api/v1/camera-groups", { body: payload }));
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["camera-groups"] });
      onDone();
    },
  });

  const stale = save.error instanceof ApiError && save.error.status === 409 && /modified by someone else/.test(save.error.message);

  const remove = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.DELETE("/api/v1/camera-groups/{groupId}", {
          params: { path: { groupId: group!.id } },
        }),
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["camera-groups"] });
      onDone();
    },
  });

  const toggleIn = (key: "site_ids" | "server_ids", id: string) =>
    setFilter((x) => ({ ...x, [key]: x[key].includes(id) ? x[key].filter((v) => v !== id) : [...x[key], id] }));
  const toggle = (id: string) =>
    setF((x) => ({
      ...x,
      camera_ids: x.camera_ids.includes(id) ? x.camera_ids.filter((c) => c !== id) : [...x.camera_ids, id],
    }));

  if (confirming && group) {
    return (
      <ConfirmDialog
        title="Eliminar grupo de cámaras"
        message={`¿Eliminar el grupo ${group.name}?`}
        confirmLabel="Eliminar"
        pending={remove.isPending}
        error={remove.error}
        onConfirm={() => remove.mutate()}
        onCancel={() => setConfirming(false)}
      />
    );
  }

  return (
    <Modal title={group ? `Editar ${group.name}` : "Nuevo grupo de cámaras"} onClose={onDone}>
      <form
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          save.mutate();
        }}
        className="flex flex-col gap-4"
      >
        <div className="grid gap-4 sm:grid-cols-2">
          {!group && me.data?.tenant_id === null && (
            <Field label="Organización">
              <Select required value={f.tenant_id} onChange={(e) => setF({ ...f, tenant_id: e.target.value })}>
                <option value="">Elegí una organización</option>
                {tenants.data?.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))}
              </Select>
            </Field>
          )}
          <Field label="Nombre">
            <TextInput required value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} />
          </Field>
          <Field label="Descripción">
            <TextInput value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} />
          </Field>
        </div>
        <Field label="Group type">
          <Select value={kind} disabled={!!group} onChange={(e) => setKind(e.target.value as "manual" | "dynamic")}>
            <option value="manual">Manual (pick cameras)</option>
            <option value="dynamic">Dynamic (filter)</option>
          </Select>
        </Field>
        {dynamic && (
          <div className="flex flex-col gap-3">
            <p className="text-xs text-muted">
              Cameras that match every field below are members automatically. Within a field, any value matches.
            </p>
            <fieldset className="flex flex-col gap-2">
              <legend className="text-sm font-medium">Sites</legend>
              {sites.isPending && <span className="text-xs text-muted">Loading sites…</span>}
              {sites.isError && <span role="alert" className="text-xs text-bad">Could not load sites.</span>}
              <div className="flex flex-wrap gap-2">
                {sites.data?.map((x) => (
                  <Checkbox key={x.id} className="rounded-full bg-surface-2 px-3" checked={filter.site_ids.includes(x.id)} onChange={() => toggleIn("site_ids", x.id)} label={x.name} />
                ))}
              </div>
            </fieldset>
            <fieldset className="flex flex-col gap-2">
              <legend className="text-sm font-medium">Servers</legend>
              {servers.isPending && <span className="text-xs text-muted">Loading servers…</span>}
              {servers.isError && <span role="alert" className="text-xs text-bad">Could not load servers.</span>}
              <div className="flex flex-wrap gap-2">
                {servers.data?.map((x) => (
                  <Checkbox key={x.id} className="rounded-full bg-surface-2 px-3" checked={filter.server_ids.includes(x.id)} onChange={() => toggleIn("server_ids", x.id)} label={x.name} />
                ))}
              </div>
            </fieldset>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Tags (comma separated)">
                <TextInput value={filter.tags} onChange={(e) => setFilter({ ...filter, tags: e.target.value })} />
              </Field>
              <Field label="Name contains">
                <TextInput value={filter.name_contains} onChange={(e) => setFilter({ ...filter, name_contains: e.target.value })} />
              </Field>
            </div>
            {filterEmpty && <p className="text-xs text-bad">Select at least one site, server, tag or name filter.</p>}
          </div>
        )}
        {!dynamic && (
        <fieldset className="flex flex-col gap-2">
          <legend className="text-sm font-medium">Cámaras del grupo</legend>
          {cameras.length === 0 && <span className="text-xs text-muted">No hay cámaras disponibles.</span>}
          <div className="grid max-h-56 gap-2 overflow-y-auto sm:grid-cols-2">
            {cameras.map((c) => (
              <Checkbox key={c.id} className="rounded-m3-md bg-surface-2 px-3" checked={f.camera_ids.includes(c.id)} onChange={() => toggle(c.id)} label={c.display_name} />
            ))}
          </div>
        </fieldset>
        )}
        {stale ? (
          <p role="alert" className="flex flex-wrap items-center gap-2 rounded-m3-lg bg-bad/12 px-4 py-3 text-sm text-bad">
            This group was changed by someone else since you opened it.
            <Button size="sm" variant="tonal" onClick={onDone}>
              Reload latest
            </Button>
          </p>
        ) : (
          <ErrorNote error={save.error} />
        )}
        <div className="flex items-center gap-2 pt-2">
          <Button type="submit" variant="primary" disabled={save.isPending || (dynamic && (filterEmpty || pickersFailed))}>
            {save.isPending ? "Guardando…" : "Guardar"}
          </Button>
          <Button variant="text" onClick={onDone}>Cancelar</Button>
          {group && (
            <Button variant="outlined" className="ml-auto text-bad" onClick={() => setConfirming(true)}>
              Eliminar grupo
            </Button>
          )}
        </div>
      </form>
    </Modal>
  );
}
