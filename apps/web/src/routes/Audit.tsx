import { useInfiniteQuery } from "@tanstack/react-query";
import { useState } from "react";
import { type AuditFilter, auditQuery } from "@/api/queries";
import { Button, Empty, ErrorNote, Field, PageHeader, Select, Table, TextInput, Th } from "@/components/ui";
import { fmtDateTime, fromLocalInput } from "@/lib/format";

const actions: Record<string, string> = {
  LOGIN_SUCCESS: "Ingreso",
  LOGIN_FAILED: "Ingreso fallido",
  LOGOUT: "Salida",
  PASSWORD_CHANGED: "Cambio de contraseña",
  MFA_ENABLED: "MFA activado",
  MFA_DISABLED: "MFA desactivado",
  USER_CREATED: "Usuario creado",
  USER_UPDATED: "Usuario modificado",
  USER_REMOVED: "Usuario eliminado",
  USER_GROUP_CHANGED: "Grupo modificado",
  PERMISSION_CHANGED: "Permiso modificado",
  TENANT_CREATED: "Organización creada",
  SITE_CREATED: "Sitio creado",
  SITE_UPDATED: "Sitio modificado",
  SITE_REMOVED: "Sitio eliminado",
  SERVER_ADDED: "Servidor agregado",
  SERVER_UPDATED: "Servidor modificado",
  SERVER_REMOVED: "Servidor eliminado",
  SERVER_SYNCED: "Servidor sincronizado",
  CAMERA_UPDATED: "Cámara modificada",
  CAMERA_GROUP_CHANGED: "Grupo de cámaras modificado",
  LIVE_VIEWED: "Vio en vivo",
  PLAYBACK_VIEWED: "Vio grabación",
  EXPORT_CREATED: "Exportación creada",
  EXPORT_DOWNLOADED: "Exportación descargada",
  SNAPSHOT_DOWNLOADED: "Captura descargada",
  VIEW_CREATED: "Vista creada",
  VIEW_UPDATED: "Vista modificada",
  VIEW_REMOVED: "Vista eliminada",
};

/** Audit is the append-only audit trail (PRD §66). */
export function Audit() {
  const [form, setForm] = useState({ action: "", from: "", to: "" });
  const [filter, setFilter] = useState<AuditFilter>({});
  const audit = useInfiniteQuery(auditQuery(filter));
  const items = audit.data?.pages.flat() ?? [];

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader title="Auditoría" description="Registro inalterable de accesos y cambios." />
      <form
        className="grid gap-3 rounded border border-line bg-surface p-3 sm:grid-cols-4"
        onSubmit={(e) => {
          e.preventDefault();
          setFilter({ action: form.action || undefined, from: fromLocalInput(form.from), to: fromLocalInput(form.to) });
        }}
      >
        <Field label="Acción">
          <Select value={form.action} onChange={(e) => setForm({ ...form, action: e.target.value })}>
            <option value="">Todas</option>
            {Object.entries(actions).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Desde">
          <TextInput type="datetime-local" value={form.from} onChange={(e) => setForm({ ...form, from: e.target.value })} />
        </Field>
        <Field label="Hasta">
          <TextInput type="datetime-local" value={form.to} onChange={(e) => setForm({ ...form, to: e.target.value })} />
        </Field>
        <div className="flex items-end">
          <Button type="submit" variant="primary">
            Filtrar
          </Button>
        </div>
      </form>
      <ErrorNote error={audit.error} />
      {audit.isSuccess && items.length === 0 && <Empty>Sin registros.</Empty>}
      {items.length > 0 && (
        <Table label="Auditoría">
          <thead>
            <tr>
              <Th>Fecha</Th>
              <Th>Usuario</Th>
              <Th>Acción</Th>
              <Th>Detalle</Th>
              <Th>IP</Th>
            </tr>
          </thead>
          <tbody>
            {items.map((e) => (
              <tr key={e.id} className="border-t border-line align-top">
                <td className="text-xs whitespace-nowrap">{fmtDateTime(e.occurred_at)}</td>
                <td className="text-sm">{e.actor_name || "—"}</td>
                <td className="text-sm">{actions[e.action] ?? e.action}</td>
                <td className="max-w-md font-mono text-[11px] break-words text-muted">
                  {e.target_type && `${e.target_type} `}
                  {Object.keys(e.details).length > 0 && JSON.stringify(e.details)}
                </td>
                <td className="font-mono text-xs">{e.ip ?? "—"}</td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {audit.hasNextPage && (
        <Button onClick={() => void audit.fetchNextPage()} disabled={audit.isFetchingNextPage} className="self-center">
          Cargar más
        </Button>
      )}
    </div>
  );
}
