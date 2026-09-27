import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { KeyRound, Plus } from "lucide-react";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { groupsQuery, meQuery, tenantsQuery, usersQuery } from "@/api/queries";
import { Button, Empty, ErrorNote, Field, PageHeader, Select, Table, TextInput, Th } from "@/components/ui";
import { fmtDateTime } from "@/lib/format";
import { can } from "@/lib/perm";

const statusText: Record<string, string> = { active: "Activo", disabled: "Deshabilitado", locked: "Bloqueado", pending: "Pendiente" };

/** Users is user administration (PRD §21-26). Permissions are assigned in Permisos. */
export function Users() {
  const me = useQuery(meQuery);
  const users = useQuery(usersQuery);
  const groups = useQuery(groupsQuery);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<Schemas["User"] | null>(null);
  const groupName = new Map(groups.data?.map((g) => [g.id, g.name]));
  const manage = can(me.data, "users.manage");

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title="Usuarios"
        description="Cada usuario ve solo lo que sus permisos, directos o por grupo, le otorgan."
        actions={
          manage && !creating ? (
            <Button variant="primary" onClick={() => setCreating(true)}>
              <Plus className="size-4" aria-hidden /> Nuevo usuario
            </Button>
          ) : null
        }
      />
      {creating && <UserForm groups={groups.data ?? []} onDone={() => setCreating(false)} />}
      {editing && <UserForm user={editing} groups={groups.data ?? []} onDone={() => setEditing(null)} />}
      <ErrorNote error={users.error} />
      {users.data?.length === 0 && <Empty>No hay usuarios visibles.</Empty>}
      {!!users.data?.length && (
        <Table label="Usuarios">
          <thead>
            <tr>
              <Th>Usuario</Th>
              <Th>Grupos</Th>
              <Th>Estado</Th>
              <Th>Último ingreso</Th>
              <Th />
            </tr>
          </thead>
          <tbody>
            {users.data.map((u) => (
              <tr key={u.id} className="border-t border-line align-top">
                <td>
                  <div className="font-medium">{u.display_name}</div>
                  <div className="font-mono text-xs text-muted">
                    {u.username}
                    {u.tenant_id === null && " · PLATAFORMA"}
                    {u.mfa_enabled && " · MFA"}
                  </div>
                </td>
                <td className="text-xs">{u.group_ids.map((g) => groupName.get(g) ?? "—").join(", ") || "—"}</td>
                <td className="text-sm">
                  {statusText[u.status] ?? u.status}
                  {!u.has_password && <div className="text-xs text-muted">Solo token</div>}
                </td>
                <td className="text-xs">{fmtDateTime(u.last_login_at)}</td>
                <td className="text-right whitespace-nowrap">
                  {can(me.data, "permissions.manage") && (
                    <Link to="/permissions" search={{ subject: `user:${u.id}` }} className="mr-2 inline-flex items-center gap-1 text-xs text-accent hover:underline">
                      <KeyRound className="size-3.5" aria-hidden /> Permisos
                    </Link>
                  )}
                  {manage && <Button onClick={() => setEditing(u)}>Editar</Button>}
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
    </div>
  );
}

function UserForm({ user, groups, onDone }: { user?: Schemas["User"]; groups: Schemas["UserGroup"][]; onDone: () => void }) {
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const tenants = useQuery({ ...tenantsQuery, enabled: me.data?.tenant_id === null && !user });
  const [f, setF] = useState({
    tenant_id: "",
    username: user?.username ?? "",
    display_name: user?.display_name ?? "",
    email: user?.email ?? "",
    password: "",
    must_change_password: true,
    status: user?.status ?? "active",
    group_ids: user?.group_ids ?? [],
    disable_mfa: false,
  });
  const tenantGroups = groups.filter((g) => (user ? g.tenant_id === user.tenant_id : !f.tenant_id || g.tenant_id === f.tenant_id));
  const save = useMutation({
    mutationFn: async () => {
      if (user) {
        return unwrap(
          await api.PATCH("/api/v1/users/{userId}", {
            params: { path: { userId: user.id } },
            body: {
              display_name: f.display_name || undefined,
              email: f.email || undefined,
              status: f.status as "active" | "disabled" | "locked",
              password: f.password || undefined,
              must_change_password: f.password ? f.must_change_password : undefined,
              group_ids: f.group_ids,
              disable_mfa: f.disable_mfa || undefined,
            },
          }),
        );
      }
      return unwrap(
        await api.POST("/api/v1/users", {
          body: {
            tenant_id: f.tenant_id || undefined,
            username: f.username.trim(),
            display_name: f.display_name.trim() || undefined,
            email: f.email.trim() || undefined,
            password: f.password || undefined,
            must_change_password: f.must_change_password,
            group_ids: f.group_ids,
          },
        }),
      );
    },
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["users"] });
      await qc.invalidateQueries({ queryKey: ["groups"] });
      onDone();
    },
  });
  const remove = useMutation({
    mutationFn: async () => unwrap(await api.DELETE("/api/v1/users/{userId}", { params: { path: { userId: user!.id } } })),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["users"] });
      onDone();
    },
  });
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate();
  };
  const toggleGroup = (id: string) =>
    setF((x) => ({ ...x, group_ids: x.group_ids.includes(id) ? x.group_ids.filter((g) => g !== id) : [...x.group_ids, id] }));

  return (
    <form onSubmit={submit} className="flex flex-col gap-4 rounded border border-line bg-surface p-4">
      <h2 className="font-semibold">{user ? `Editar ${user.username}` : "Nuevo usuario"}</h2>
      <div className="grid gap-4 sm:grid-cols-2">
        {!user && me.data?.tenant_id === null && (
          <Field label="Organización" hint="Vacío crea un usuario de plataforma.">
            <Select value={f.tenant_id} onChange={(e) => setF({ ...f, tenant_id: e.target.value, group_ids: [] })}>
              <option value="">Plataforma</option>
              {tenants.data?.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name}
                </option>
              ))}
            </Select>
          </Field>
        )}
        {!user && (
          <Field label="Usuario">
            <TextInput required minLength={3} pattern="[A-Za-z0-9._@\-]+" value={f.username} onChange={(e) => setF({ ...f, username: e.target.value })} autoComplete="off" />
          </Field>
        )}
        <Field label="Nombre para mostrar">
          <TextInput value={f.display_name} onChange={(e) => setF({ ...f, display_name: e.target.value })} />
        </Field>
        <Field label="Email">
          <TextInput type="email" value={f.email} onChange={(e) => setF({ ...f, email: e.target.value })} />
        </Field>
        <Field label={user ? "Nueva contraseña" : "Contraseña"} hint="Mínimo 10 caracteres, con letras y números o símbolos. Se guarda con Argon2id.">
          <TextInput type="password" minLength={10} value={f.password} onChange={(e) => setF({ ...f, password: e.target.value })} autoComplete="new-password" />
        </Field>
        {f.password && (
          <label className="flex items-center gap-2 self-center text-sm">
            <input type="checkbox" checked={f.must_change_password} onChange={(e) => setF({ ...f, must_change_password: e.target.checked })} /> Pedir cambio al ingresar
          </label>
        )}
        {user && (
          <Field label="Estado">
            <Select value={f.status} onChange={(e) => setF({ ...f, status: e.target.value as typeof f.status })}>
              <option value="active">Activo</option>
              <option value="disabled">Deshabilitado</option>
              <option value="locked">Bloqueado</option>
            </Select>
          </Field>
        )}
        {user?.mfa_enabled && (
          <label className="flex items-center gap-2 self-center text-sm">
            <input type="checkbox" checked={f.disable_mfa} onChange={(e) => setF({ ...f, disable_mfa: e.target.checked })} /> Quitar MFA (teléfono perdido)
          </label>
        )}
      </div>
      {tenantGroups.length > 0 && (
        <fieldset className="flex flex-col gap-1 text-sm">
          <legend className="mb-1 font-medium">Grupos</legend>
          <div className="flex flex-wrap gap-3">
            {tenantGroups.map((g) => (
              <label key={g.id} className="flex items-center gap-1.5">
                <input type="checkbox" checked={f.group_ids.includes(g.id)} onChange={() => toggleGroup(g.id)} /> {g.name}
              </label>
            ))}
          </div>
        </fieldset>
      )}
      <ErrorNote error={save.error ?? remove.error} />
      <div className="flex flex-wrap gap-2">
        <Button type="submit" variant="primary" disabled={save.isPending}>
          {save.isPending ? "Guardando…" : "Guardar"}
        </Button>
        <Button onClick={onDone}>Cancelar</Button>
        {user && user.id !== me.data?.id && (
          <Button className="ml-auto text-bad" onClick={() => confirm(`¿Eliminar a ${user.username}?`) && remove.mutate()}>
            Eliminar usuario
          </Button>
        )}
      </div>
    </form>
  );
}
