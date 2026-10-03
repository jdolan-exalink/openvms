import { useT } from "@/i18n";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { type AuditFilter, auditQuery, usersQuery } from "@/api/queries";
import { Button, Empty, ErrorNote, Field, PageHeader, Select, Summary, Table, TextInput, Th } from "@/components/ui";
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
  ACCESS_DENIED: "Acceso denegado",
  BRANDING_UPDATED: "Marca de agua modificada",
  BRANDING_REMOVED: "Marca de agua eliminada",
  CLIP_WATERMARK_REQUESTED: "Clip con marca de agua solicitado",
  CLIP_DOWNLOADED: "Clip descargado",
};

/** Audit is the append-only audit trail (PRD §66). */
export function Audit() {
  const t = useT();
  const users = useQuery(usersQuery);
  const [form, setForm] = useState({ action: "", actor_id: "", from: "", to: "" });
  const [filter, setFilter] = useState<AuditFilter>({});
  const audit = useInfiniteQuery(auditQuery(filter));
  const items = audit.data?.pages.flat() ?? [];

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader title={t("nav.audit")} description={t("settings.audit")} />
      <form
        className="grid gap-3 rounded border border-line bg-surface p-3 sm:grid-cols-2 lg:grid-cols-5"
        onSubmit={(e) => {
          e.preventDefault();
          setFilter({
            action: form.action || undefined,
            actor_id: form.actor_id || undefined,
            from: fromLocalInput(form.from),
            to: fromLocalInput(form.to),
          });
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
        <Field label="Usuario">
          <Select aria-label="Usuario" value={form.actor_id} onChange={(e) => setForm({ ...form, actor_id: e.target.value })}>
            <option value="">Todos</option>
            {users.data?.map((u) => (
              <option key={u.id} value={u.id}>
                {u.display_name} ({u.username})
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
          <Button type="submit" variant="primary" className="w-full sm:w-auto">
            Filtrar
          </Button>
        </div>
      </form>
      <ErrorNote error={audit.error} />
      {audit.isSuccess && items.length === 0 && <Empty>Sin registros.</Empty>}
      {items.length > 0 && (
        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium text-muted">Eventos registrados</span>
            <Summary>{items.length === 1 ? "1 registro" : `${items.length} registros`}</Summary>
          </div>
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
                  <td className="text-sm font-medium">{actions[e.action] ?? e.action}</td>
                  <td className="max-w-md break-words">
                    {renderDetails(e.details, e.target_type)}
                  </td>
                  <td className="font-mono text-xs text-muted">{e.ip ?? "—"}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      )}
      {audit.hasNextPage && (
        <Button onClick={() => void audit.fetchNextPage()} disabled={audit.isFetchingNextPage} className="self-center">
          Cargar más
        </Button>
      )}
    </div>
  );
}

function renderDetails(details: Record<string, unknown>, targetType?: string) {
  const entries = Object.entries(details ?? {});
  if (entries.length === 0 && !targetType) return <span className="text-xs text-muted">—</span>;
  return (
    <div className="flex flex-col gap-0.5 text-xs">
      {targetType && <span className="font-medium text-fg/90">{targetType}</span>}
      {entries.map(([k, v]) => (
        <span key={k} className="text-muted">
          <span className="font-medium text-fg/70">{k}:</span> {typeof v === "object" && v !== null ? JSON.stringify(v) : String(v)}
        </span>
      ))}
    </div>
  );
}
