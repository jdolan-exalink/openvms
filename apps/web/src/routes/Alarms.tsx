import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, CheckCheck, UserPlus, Users } from "lucide-react";
import { useState } from "react";
import { api, unwrap, type Schemas } from "@/api/client";
import { alarmAssigneesQuery, alarmsQuery, camerasQuery, type AlarmFilter } from "@/api/queries";
import { Modal } from "@/components/Modal";
import { Button, Empty, ErrorNote, Field, PageHeader, Select, Table, Th } from "@/components/ui";
import { VehicleFacts } from "@/components/VehicleMark";
import { fmtDateTime } from "@/lib/format";

type Alarm = Schemas["Alarm"];
type AlarmStatus = Schemas["AlarmStatus"];

const statusBadgeStyles: Record<AlarmStatus, { label: string; bg: string; text: string }> = {
  open: { label: "Abierta", bg: "bg-bad/10 border-bad/30", text: "text-bad" },
  acknowledged: { label: "Reconocida", bg: "bg-warn/10 border-warn/30", text: "text-warn" },
  assigned: { label: "Asignada", bg: "bg-primary/10 border-primary/30", text: "text-primary" },
  investigating: { label: "En investigación", bg: "bg-warn/10 border-warn/30", text: "text-warn" },
  resolved: { label: "Resuelta", bg: "bg-ok/10 border-ok/30", text: "text-ok" },
  closed: { label: "Cerrada", bg: "bg-muted border-border", text: "text-muted-foreground" },
};

export function Alarms() {
  const t = useT();
  const qc = useQueryClient();
  const [filter, setFilter] = useState<AlarmFilter>({ status_group: "active" });
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [assignAlarm, setAssignAlarm] = useState<Alarm | null>(null);
  const [selectedUserId, setSelectedUserId] = useState<string>("");

  const filterTabs: { key: string; label: string; filter: AlarmFilter }[] = [
    { key: "active", label: "Activas", filter: { status_group: "active" } },
    { key: "open", label: "Abiertas", filter: { status: "open" } },
    { key: "acknowledged", label: "Reconocidas", filter: { status: "acknowledged" } },
    { key: "assigned", label: "Asignadas", filter: { status: "assigned" } },
    { key: "investigating", label: "En investigación", filter: { status: "investigating" } },
    { key: "resolved", label: "Resueltas", filter: { status: "resolved" } },
    { key: "closed", label: "Cerradas", filter: { status: "closed" } },
    { key: "all", label: "Todas", filter: {} },
  ];

  const alarms = useQuery(alarmsQuery(filter));
  const cameras = useQuery(camerasQuery());

  const assignees = useQuery(alarmAssigneesQuery(assignAlarm?.id));

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: ["alarms"] });
  };

  const ackMutation = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.POST("/api/v1/alarms/{alarmId}/acknowledge", { params: { path: { alarmId: id } } })),
    onSuccess: () => invalidate(),
  });

  const resolveMutation = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.POST("/api/v1/alarms/{alarmId}/resolve", { params: { path: { alarmId: id } } })),
    onSuccess: () => {
      invalidate();
      setSelectedIds((prev) => {
        const next = new Set(prev);
        if (assignAlarm) next.delete(assignAlarm.id);
        return next;
      });
    },
  });

  const assignMutation = useMutation({
    mutationFn: async ({ alarmId, userId }: { alarmId: string; userId: string }) =>
      unwrap(
        await api.POST("/api/v1/alarms/{alarmId}/assign", {
          params: { path: { alarmId } },
          body: { user_id: userId },
        }),
      ),
    onSuccess: () => {
      invalidate();
      setAssignAlarm(null);
      setSelectedUserId("");
    },
  });

  const bulkMutation = useMutation({
    mutationFn: async ({ action, alarmIds }: { action: "acknowledge" | "resolve"; alarmIds: string[] }) =>
      unwrap(
        await api.POST("/api/v1/alarms/bulk", {
          body: { action, alarm_ids: alarmIds },
        }),
      ),
    onSuccess: () => {
      invalidate();
      setSelectedIds(new Set());
    },
  });

  const items = alarms.data ?? [];

  const toggleSelect = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleSelectAll = () => {
    if (selectedIds.size === items.length) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(items.map((a) => a.id)));
    }
  };

  const handleBulkAction = (action: "acknowledge" | "resolve") => {
    if (selectedIds.size === 0) return;
    bulkMutation.mutate({ action, alarmIds: Array.from(selectedIds) });
  };

  const mutationError =
    ackMutation.error ?? resolveMutation.error ?? assignMutation.error ?? bulkMutation.error;

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title={t("nav.alarms")}
        description={t("settings.alarms")}
      />

      <ErrorNote error={alarms.error ?? mutationError} />

      {/* Filters bar */}
      <div className="flex flex-wrap items-center justify-between gap-4 rounded border border-line bg-surface p-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs font-medium text-muted">Estado:</span>
          {filterTabs.map((tab) => {
            const isSelected =
              tab.key === "active"
                ? filter.status_group === "active"
                : tab.key === "all"
                  ? !filter.status && !filter.status_group
                  : filter.status === tab.filter.status && !filter.status_group;
            return (
              <button
                key={tab.key}
                type="button"
                onClick={() => {
                  setFilter((prev) => ({
                    ...prev,
                    status: tab.filter.status,
                    status_group: tab.filter.status_group,
                  }));
                  setSelectedIds(new Set());
                }}
                className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
                  isSelected ? "bg-accent text-bg" : "bg-bg text-fg hover:bg-raised"
                }`}
              >
                {tab.label}
              </button>
            );
          })}
        </div>

        {cameras.data && cameras.data.length > 0 && (
          <div className="flex items-center gap-2">
            <label htmlFor="filter-camera" className="text-xs font-medium text-muted">
              Cámara:
            </label>
            <select
              id="filter-camera"
              value={filter.camera_id ?? ""}
              onChange={(e) => {
                const val = e.target.value;
                setFilter((prev) => ({ ...prev, camera_id: val ? val : undefined }));
                setSelectedIds(new Set());
              }}
              className="rounded border border-line bg-bg px-2 py-1 text-xs text-fg"
            >
              <option value="">Todas las cámaras</option>
              {cameras.data.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.display_name}
                </option>
              ))}
            </select>
          </div>
        )}
      </div>

      {/* Bulk action toolbar */}
      {selectedIds.size > 0 && (
        <div className="flex items-center justify-between gap-4 rounded border border-accent/40 bg-accent/10 px-4 py-2 text-sm">
          <span className="font-medium text-accent">
            {selectedIds.size} alarma{selectedIds.size > 1 ? "s" : ""} seleccionada{selectedIds.size > 1 ? "s" : ""}
          </span>
          <div className="flex items-center gap-2">
            <Button
              variant="secondary"
              onClick={() => handleBulkAction("acknowledge")}
              disabled={bulkMutation.isPending}
              className="text-xs"
            >
              <Check className="size-3.5" aria-hidden /> Reconocer seleccionadas
            </Button>
            <Button
              variant="primary"
              onClick={() => handleBulkAction("resolve")}
              disabled={bulkMutation.isPending}
              className="text-xs"
            >
              <CheckCheck className="size-3.5" aria-hidden /> Resolver seleccionadas
            </Button>
            <button
              type="button"
              onClick={() => setSelectedIds(new Set())}
              className="text-xs text-muted hover:text-fg underline ml-2"
            >
              Deseleccionar
            </button>
          </div>
        </div>
      )}

      {items.length === 0 && !alarms.isLoading && (
        <Empty>No hay alarmas para el filtro seleccionado.</Empty>
      )}

      {items.length > 0 && (
        <Table label="Bandeja de alarmas">
          <thead>
            <tr>
              <Th className="w-8">
                <input
                  type="checkbox"
                  aria-label="Seleccionar todas las alarmas"
                  checked={selectedIds.size === items.length && items.length > 0}
                  onChange={toggleSelectAll}
                  className="rounded border-line"
                />
              </Th>
              <Th>Severidad / Evento</Th>
              <Th>Cámara / Sitio</Th>
              <Th>Fecha y hora</Th>
              <Th>Estado</Th>
              <Th>Asignado a</Th>
              <Th className="text-right">Acciones</Th>
            </tr>
          </thead>
          <tbody>
            {items.map((a) => {
              const badge = statusBadgeStyles[a.status] ?? statusBadgeStyles.open;
              return (
                <tr key={a.id} className="border-t border-line align-middle">
                  <td>
                    <input
                      type="checkbox"
                      aria-label={`Seleccionar alarma ${a.id}`}
                      checked={selectedIds.has(a.id)}
                      onChange={() => toggleSelect(a.id)}
                      className="rounded border-line"
                    />
                  </td>
                  <td>
                    <div className="flex items-center gap-2">
                      <span className="rounded bg-bad px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-bg">
                        {a.event_severity}
                      </span>
                      <VehicleFacts labels={a.event_labels ?? []} vehicle={a.vehicle} person={a.person} serverName={a.server_name} />
                    </div>
                  </td>
                  <td>
                    <div className="font-medium">{a.camera_name}</div>
                    <div className="text-xs text-muted">{a.site_name}{a.server_name ? ` · ${a.server_name}` : ""}</div>
                  </td>
                  <td className="text-xs whitespace-nowrap">
                    {fmtDateTime(a.event_start_time)}
                  </td>
                  <td>
                    <span
                      className={`inline-flex items-center rounded border px-2 py-0.5 text-xs font-medium ${badge.bg} ${badge.text}`}
                    >
                      {badge.label}
                    </span>
                    {a.status === "acknowledged" && a.acknowledged_by_name && (
                      <div className="text-[11px] text-muted">
                        por {a.acknowledged_by_name}
                      </div>
                    )}
                    {a.status === "resolved" && a.resolved_by_name && (
                      <div className="text-[11px] text-muted">
                        por {a.resolved_by_name}
                      </div>
                    )}
                  </td>
                  <td className="text-sm">
                    {a.assigned_to_name ? (
                      <span className="inline-flex items-center gap-1 text-fg">
                        <Users className="size-3 text-muted" aria-hidden />
                        {a.assigned_to_name}
                      </span>
                    ) : (
                      <span className="text-xs italic text-muted">Sin asignar</span>
                    )}
                  </td>
                  <td className="text-right whitespace-nowrap">
                    <div className="inline-flex items-center justify-end gap-1.5">
                      {a.status === "open" && (
                        <Button
                          variant="secondary"
                          onClick={() => ackMutation.mutate(a.id)}
                          disabled={ackMutation.isPending}
                          title="Reconocer alarma"
                          aria-label={`Reconocer alarma ${a.id}`}
                          className="px-2 py-1 text-xs"
                        >
                          <Check className="size-3" aria-hidden /> Reconocer
                        </Button>
                      )}
                      {a.status !== "resolved" && (
                        <>
                          <Button
                            variant="secondary"
                            onClick={() => {
                              setAssignAlarm(a);
                              setSelectedUserId(a.assigned_to ?? "");
                            }}
                            title="Asignar alarma"
                            aria-label={`Asignar alarma ${a.id}`}
                            className="px-2 py-1 text-xs"
                          >
                            <UserPlus className="size-3" aria-hidden /> Asignar
                          </Button>
                          <Button
                            variant="primary"
                            onClick={() => resolveMutation.mutate(a.id)}
                            disabled={resolveMutation.isPending}
                            title="Resolver alarma"
                            aria-label={`Resolver alarma ${a.id}`}
                            className="px-2 py-1 text-xs"
                          >
                            <CheckCheck className="size-3" aria-hidden /> Resolver
                          </Button>
                        </>
                      )}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      )}

      {/* Assign Modal */}
      {assignAlarm && (
        <Modal
          title={`Asignar alarma en ${assignAlarm.camera_name}`}
          onClose={() => {
            setAssignAlarm(null);
            setSelectedUserId("");
          }}
        >
          <div className="flex flex-col gap-4">
            <p className="text-sm text-muted">
              Seleccioná un operador habilitado para gestionar alarmas en esta cámara.
            </p>

            <ErrorNote error={assignees.error ?? assignMutation.error} />

            <Field label="Operador asignado">
              <Select
                value={selectedUserId}
                onChange={(e) => setSelectedUserId(e.target.value)}
                disabled={assignees.isLoading}
              >
                <option value="">Seleccionar operador...</option>
                {assignees.data?.map((u) => (
                  <option key={u.id} value={u.id}>
                    {u.display_name} ({u.username})
                  </option>
                ))}
              </Select>
            </Field>

            <div className="flex justify-end gap-2 pt-2">
              <Button
                variant="secondary"
                onClick={() => {
                  setAssignAlarm(null);
                  setSelectedUserId("");
                }}
              >
                Cancelar
              </Button>
              <Button
                variant="primary"
                disabled={!selectedUserId || assignMutation.isPending}
                onClick={() => {
                  if (!selectedUserId) return;
                  assignMutation.mutate({
                    alarmId: assignAlarm.id,
                    userId: selectedUserId,
                  });
                }}
              >
                {assignMutation.isPending ? "Guardando..." : "Guardar asignación"}
              </Button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  );
}
