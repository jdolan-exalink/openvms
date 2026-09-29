import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { KeyRound, Plus } from "lucide-react";
import { type FormEvent, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { cameraGroupsQuery, camerasQuery, meQuery, tenantsQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Modal } from "@/components/Modal";
import { Button, Empty, ErrorNote, Field, PageHeader, Summary, Table, TextInput, Th } from "@/components/ui";
import { can } from "@/lib/perm";

/** Camera groups allow grouping cameras for permissions and view layouts. */
export function CameraGroups() {
  const qc = useQueryClient();
  const me = useQuery(meQuery);
  const groups = useQuery(cameraGroupsQuery);
  const cameras = useQuery(camerasQuery({}));
  const [editing, setEditing] = useState<Schemas["CameraGroup"] | "new" | null>(null);
  const [q, setQ] = useState("");
  const manage = can(me.data, "cameras.manage");

  const needle = q.trim().toLowerCase();
  const visible = groups.data?.filter(
    (g) => !needle || g.name.toLowerCase().includes(needle) || g.description.toLowerCase().includes(needle),
  );

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title="Grupos de cámaras"
        description="Agrupá cámaras para asignar permisos o visualización conjunta."
        actions={
          manage ? (
            <Button variant="primary" onClick={() => setEditing("new")}>
              <Plus className="size-4" aria-hidden /> Nuevo grupo
            </Button>
          ) : null
        }
      />
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
              <tr key={g.id} className="border-t border-line align-top">
                <td>
                  <div className="font-medium">{g.name}</div>
                  {g.description && <div className="text-xs text-muted">{g.description}</div>}
                </td>
                <td className="text-sm text-muted">
                  {g.camera_ids.length === 1 ? "1 cámara" : `${g.camera_ids.length} cámaras`}
                </td>
                <td className="text-right">
                  {can(me.data, "permissions.manage") && (
                    <Link
                      to="/permissions"
                      search={{ subject: `camera_group:${g.id}` }}
                      className="mr-2 inline-flex items-center gap-1 text-xs text-accent hover:underline"
                    >
                      <KeyRound className="size-3.5" aria-hidden /> Permisos
                    </Link>
                  )}
                  {manage && (
                    <Button aria-label={`Editar ${g.name}`} onClick={() => setEditing(g)}>
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

  const save = useMutation({
    mutationFn: async () => {
      const payload: Schemas["CameraGroupInput"] = {
        tenant_id: f.tenant_id,
        name: f.name.trim(),
        description: f.description.trim(),
        camera_ids: f.camera_ids,
      };
      if (group) {
        return unwrap(
          await api.PUT("/api/v1/camera-groups/{groupId}", {
            params: { path: { groupId: group.id } },
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
              <select
                required
                value={f.tenant_id}
                onChange={(e) => setF({ ...f, tenant_id: e.target.value })}
                className="w-full rounded border border-line bg-bg px-3 py-1.5 text-sm"
              >
                <option value="">Elegí una organización</option>
                {tenants.data?.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))}
              </select>
            </Field>
          )}
          <Field label="Nombre">
            <TextInput required value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} />
          </Field>
          <Field label="Descripción">
            <TextInput value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} />
          </Field>
        </div>
        <fieldset className="flex flex-col gap-2">
          <legend className="text-sm font-medium">Cámaras del grupo</legend>
          {cameras.length === 0 && <span className="text-xs text-muted">No hay cámaras disponibles.</span>}
          <div className="grid max-h-48 gap-2 overflow-y-auto sm:grid-cols-2">
            {cameras.map((c) => (
              <label key={c.id} className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={f.camera_ids.includes(c.id)}
                  onChange={() => toggle(c.id)}
                  className="rounded border-line"
                />
                <span>{c.display_name}</span>
              </label>
            ))}
          </div>
        </fieldset>
        <ErrorNote error={save.error} />
        <div className="flex items-center gap-2 pt-2">
          <Button type="submit" variant="primary" disabled={save.isPending}>
            {save.isPending ? "Guardando…" : "Guardar"}
          </Button>
          <Button onClick={onDone}>Cancelar</Button>
          {group && (
            <Button className="ml-auto text-bad" onClick={() => setConfirming(true)}>
              Eliminar grupo
            </Button>
          )}
        </div>
      </form>
    </Modal>
  );
}
