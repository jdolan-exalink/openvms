import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRound, Plus } from "lucide-react";
import { Icon } from "@/components/Icon";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { groupsQuery, meQuery, tenantsQuery, usersQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Modal } from "@/components/Modal";
import { Button, Checkbox, Empty, ErrorNote, Field, LinkButton, PageHeader, Select, Summary, Table, TextInput, Th } from "@/components/ui";
import { can } from "@/lib/perm";

/** Groups of users. Grants given to a group apply to all its members. */
export function Groups() {
  const t = useT();
  const me = useQuery(meQuery);
  const groups = useQuery(groupsQuery);
  const users = useQuery(usersQuery);
  const [editing, setEditing] = useState<Schemas["UserGroup"] | "new" | null>(null);
  const userName = new Map(users.data?.map((u) => [u.id, u.username]));
  const manage = can(me.data, "groups.manage");
  const [q, setQ] = useState("");
  const needle = q.trim().toLowerCase();
  const visible = groups.data?.filter((g) => !needle || g.name.toLowerCase().includes(needle));

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title={t("nav.groups")}
        description={t("settings.groupPermissions")}
        actions={
          manage ? (
            <Button variant="filled" onClick={() => setEditing("new")}>
              <Icon icon={Plus} size="xs" /> Nuevo grupo
            </Button>
          ) : null
        }
      />
      {editing && <GroupForm group={editing === "new" ? undefined : editing} users={users.data ?? []} onDone={() => setEditing(null)} />}
      <ErrorNote error={groups.error} />
      {groups.data?.length === 0 && <Empty>No hay grupos.</Empty>}
      {!!groups.data?.length && (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <TextInput className="sm:w-72" aria-label="Buscar grupo" placeholder="Buscar por nombre" value={q} onChange={(e) => setQ(e.target.value)} />
          <Summary>{summarize(groups.data.length, visible?.length ?? 0)}</Summary>
        </div>
      )}
      {!!groups.data?.length && visible?.length === 0 && <Empty>Ningún grupo coincide con el filtro.</Empty>}
      {!!visible?.length && (
        <Table label="Grupos">
          <thead>
            <tr>
              <Th>Grupo</Th>
              <Th>Miembros</Th>
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
                <td className="text-xs">{g.member_ids.map((m) => userName.get(m) ?? "—").join(", ") || "—"}</td>
                <td className="text-right whitespace-nowrap">
                  <div className="inline-flex items-center gap-1">
                  {can(me.data, "permissions.manage") && (
                    <LinkButton size="sm" to="/permissions" search={{ subject: `group:${g.id}` }}>
                      <Icon icon={KeyRound} size="xs" /> Permisos
                    </LinkButton>
                  )}
                  {manage && (
                    <Button size="sm" variant="tonal" aria-label={`Editar ${g.name}`} onClick={() => setEditing(g)}>
                      Editar
                    </Button>
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

function summarize(total: number, shown: number) {
  const noun = total === 1 ? "grupo" : "grupos";
  return shown === total ? `${total} ${noun}` : `${shown} de ${total} ${noun}`;
}

function GroupForm({ group, users, onDone }: { group?: Schemas["UserGroup"]; users: Schemas["User"][]; onDone: () => void }) {
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const tenants = useQuery({ ...tenantsQuery, enabled: me.data?.tenant_id === null && !group });
  const [confirming, setConfirming] = useState(false);
  const [f, setF] = useState({
    tenant_id: group?.tenant_id ?? "",
    name: group?.name ?? "",
    description: group?.description ?? "",
    member_ids: group?.member_ids ?? [],
  });
  const candidates = users.filter((u) => (f.tenant_id ? u.tenant_id === f.tenant_id : u.tenant_id === (group ? group.tenant_id : me.data?.tenant_id ?? null)));
  const body = () => ({ tenant_id: f.tenant_id || undefined, name: f.name.trim(), description: f.description, member_ids: f.member_ids });
  const save = useMutation({
    mutationFn: async () =>
      group
        ? unwrap(await api.PUT("/api/v1/groups/{groupId}", { params: { path: { groupId: group.id } }, body: body() }))
        : unwrap(await api.POST("/api/v1/groups", { body: body() })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["groups"] });
      await qc.invalidateQueries({ queryKey: ["users"] });
      onDone();
    },
  });
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/api/v1/groups/{groupId}", { params: { path: { groupId: group!.id } } })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["groups"] });
      onDone();
    },
  });
  const toggle = (id: string) =>
    setF((x) => ({ ...x, member_ids: x.member_ids.includes(id) ? x.member_ids.filter((m) => m !== id) : [...x.member_ids, id] }));

  if (confirming && group) {
    return (
      <ConfirmDialog
        title="Eliminar grupo"
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
    <Modal title={group ? `Editar ${group.name}` : "Nuevo grupo"} onClose={onDone}>
    <form
      onSubmit={(e: FormEvent) => {
        e.preventDefault();
        save.mutate();
      }}
      className="flex flex-col gap-4"
    >
      <div className="grid gap-4 sm:grid-cols-3">
        {!group && me.data?.tenant_id === null && (
          <Field label="Organización">
            <Select value={f.tenant_id} onChange={(e) => setF({ ...f, tenant_id: e.target.value, member_ids: [] })}>
              <option value="">Plataforma</option>
              {tenants.data?.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </Select>
          </Field>
        )}
        <Field label="Nombre">
          <TextInput required value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="Operadores Helvecia" />
        </Field>
        <Field label="Descripción">
          <TextInput value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} />
        </Field>
      </div>
      <fieldset className="flex flex-col gap-1 text-sm">
        <legend className="mb-1 font-medium">Miembros</legend>
        <div className="flex flex-wrap gap-3">
          {candidates.map((u) => (
            <Checkbox key={u.id} className="rounded-full bg-surface-2 px-3" checked={f.member_ids.includes(u.id)} onChange={() => toggle(u.id)} label={u.display_name} />
          ))}
          {candidates.length === 0 && <span className="text-muted">No hay usuarios en esta organización.</span>}
        </div>
      </fieldset>
      <ErrorNote error={save.error ?? remove.error} />
      <div className="flex gap-2">
        <Button type="submit" variant="filled" disabled={save.isPending}>
          Guardar
        </Button>
        <Button variant="text" onClick={onDone}>Cancelar</Button>
        {group && (
          <Button variant="outlined" className="ml-auto border-bad/60 text-bad" onClick={() => setConfirming(true)}>
            Eliminar grupo
          </Button>
        )}
      </div>
    </form>
    </Modal>
  );
}
