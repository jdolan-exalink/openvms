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
import { fmtDateTime } from "@/lib/format";
import { can } from "@/lib/perm";

const statusText: Record<string, string> = { active: "Activo", disabled: "Deshabilitado", locked: "Bloqueado", pending: "Pendiente" };

/** Users is user administration (PRD §21-26). Permissions are assigned in Permisos. */
export function Users() {
  const t = useT();
  const me = useQuery(meQuery);
  const users = useQuery(usersQuery);
  const groups = useQuery(groupsQuery);
  const [editing, setEditing] = useState<Schemas["User"] | "new" | null>(null);
  const [q, setQ] = useState("");
  const [status, setStatus] = useState("");
  const groupName = new Map(groups.data?.map((g) => [g.id, g.name]));
  const manage = can(me.data, "users.manage");
  const needle = q.trim().toLowerCase();
  const visible = users.data?.filter(
    (u) =>
      (!status || u.status === status) &&
      (!needle || [u.username, u.display_name, u.email ?? ""].some((v) => v.toLowerCase().includes(needle))),
  );

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title={t("nav.users")}
        description={t("settings.users")}
        actions={
          manage ? (
            <Button variant="filled" onClick={() => setEditing("new")}>
              <Icon icon={Plus} size="xs" /> Nuevo usuario
            </Button>
          ) : null
        }
      />
      {editing && <UserForm user={editing === "new" ? undefined : editing} groups={groups.data ?? []} onDone={() => setEditing(null)} />}
      <ErrorNote error={users.error} />
      {users.data?.length === 0 && <Empty>No hay usuarios visibles.</Empty>}
      {!!users.data?.length && (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex flex-wrap items-center gap-3">
            <TextInput className="sm:w-72" aria-label="Buscar usuario" placeholder="Buscar por usuario, nombre o email" value={q} onChange={(e) => setQ(e.target.value)} />
            <Select className="sm:w-44" aria-label="Filtrar por estado" value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="">Todos los estados</option>
              {Object.entries(statusText).map(([value, label]) => (
                <option key={value} value={value}>
                  {label}
                </option>
              ))}
            </Select>
          </div>
          <Summary>{summarize(users.data.length, visible?.length ?? 0)}</Summary>
        </div>
      )}
      {!!users.data?.length && visible?.length === 0 && <Empty>Ningún usuario coincide con el filtro.</Empty>}
      {!!visible?.length && (
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
            {visible.map((u) => (
              <tr key={u.id} className="border-t border-outline-variant align-top">
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
                <td className="font-mono text-xs">{fmtDateTime(u.last_login_at)}</td>
                <td className="text-right whitespace-nowrap">
                  <div className="inline-flex items-center gap-1">
                  {can(me.data, "permissions.manage") && (
                    <LinkButton size="sm" to="/permissions" search={{ subject: `user:${u.id}` }}>
                      <Icon icon={KeyRound} size="xs" /> Permisos
                    </LinkButton>
                  )}
                  {manage && (
                    <Button size="sm" variant="tonal" aria-label={`Editar ${u.username}`} onClick={() => setEditing(u)}>
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
  const noun = total === 1 ? "usuario" : "usuarios";
  return shown === total ? `${total} ${noun}` : `${shown} de ${total} ${noun}`;
}

function UserForm({ user, groups, onDone }: { user?: Schemas["User"]; groups: Schemas["UserGroup"][]; onDone: () => void }) {
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const tenants = useQuery({ ...tenantsQuery, enabled: me.data?.tenant_id === null && !user });
  const [confirming, setConfirming] = useState(false);
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

  if (confirming && user) {
    return (
      <ConfirmDialog
        title="Eliminar usuario"
        message={`¿Eliminar a ${user.username}?`}
        confirmLabel="Eliminar"
        pending={remove.isPending}
        error={remove.error}
        onConfirm={() => remove.mutate()}
        onCancel={() => setConfirming(false)}
      />
    );
  }

  return (
    <Modal title={user ? `Editar ${user.username}` : "Nuevo usuario"} onClose={onDone}>
    <form onSubmit={submit} className="flex flex-col gap-4">
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
          <Checkbox className="self-center" checked={f.must_change_password} onChange={(v) => setF({ ...f, must_change_password: v })} label="Pedir cambio al ingresar" />
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
          <Checkbox className="self-center" checked={f.disable_mfa} onChange={(v) => setF({ ...f, disable_mfa: v })} label="Quitar MFA (teléfono perdido)" />
        )}
      </div>
      {tenantGroups.length > 0 && (
        <fieldset className="flex flex-col gap-1 text-sm">
          <legend className="mb-1 font-medium">Grupos</legend>
          <div className="flex flex-wrap gap-3">
            {tenantGroups.map((g) => (
              <Checkbox key={g.id} className="rounded-full bg-surface-2 px-3" checked={f.group_ids.includes(g.id)} onChange={() => toggleGroup(g.id)} label={g.name} />
            ))}
          </div>
        </fieldset>
      )}
      <ErrorNote error={save.error ?? remove.error} />
      <div className="flex flex-wrap gap-2">
        <Button type="submit" variant="filled" disabled={save.isPending}>
          {save.isPending ? "Guardando…" : "Guardar"}
        </Button>
        <Button variant="text" onClick={onDone}>Cancelar</Button>
        {user && user.id !== me.data?.id && (
          <Button variant="outlined" className="ml-auto border-bad/60 text-bad" onClick={() => setConfirming(true)}>
            Eliminar usuario
          </Button>
        )}
      </div>
    </form>
    </Modal>
  );
}
