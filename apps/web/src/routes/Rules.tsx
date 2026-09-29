import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { type FormEvent, useState } from "react";
import { ApiError, api, type Schemas, unwrap } from "@/api/client";
import { camerasQuery, channelsQuery, rulesQuery, serversQuery, sitesQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Modal } from "@/components/Modal";
import { TagInput } from "@/components/TagInput";
import { Button, Empty, ErrorNote, Field, PageHeader, Select, Summary, Table, TextInput, Th } from "@/components/ui";
import { knownLabels } from "@/lib/format";

type Rule = Schemas["Rule"];
type Trigger = Schemas["RuleTriggerType"];
type NotifSeverity = NonNullable<Schemas["RuleActions"]["severity"]>;

const triggerLabel: Record<Trigger, string> = {
  event: "Evento nuevo",
  camera_offline: "Cámara sin conexión",
  server_offline: "Servidor sin conexión",
};
const eventSeverities = [
  { value: "alert", label: "Alerta" },
  { value: "detection", label: "Detección" },
];
const defaultMinutes = 5;

/** Rules: guided form for automation rules (no JSON). Requires notifications.manage. */
export function Rules() {
  const qc = useQueryClient();
  const rules = useQuery(rulesQuery);
  const [editing, setEditing] = useState<Rule | "new" | null>(null);
  const [deleting, setDeleting] = useState<Rule | null>(null);

  const toggle = useMutation({
    mutationFn: async (r: Rule) =>
      unwrap(await api.PATCH("/api/v1/rules/{ruleId}", { params: { path: { ruleId: r.id } }, body: { enabled: !r.enabled } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["rules"] }),
  });
  const remove = useMutation({
    mutationFn: async (r: Rule) => unwrap(await api.DELETE("/api/v1/rules/{ruleId}", { params: { path: { ruleId: r.id } } })),
    onSuccess: () => {
      setDeleting(null);
      void qc.invalidateQueries({ queryKey: ["rules"] });
    },
  });

  const forbidden = rules.error instanceof ApiError && rules.error.status === 403;
  if (forbidden) {
    return (
      <div className="mx-auto flex max-w-6xl flex-col gap-6">
        <PageHeader title="Reglas" />
        <Empty>No tenés permiso para administrar reglas. Pedile a un administrador el permiso de gestión de notificaciones.</Empty>
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title="Reglas"
        description="Automatizá alarmas y notificaciones a partir de eventos y de la caída de cámaras o servidores."
        actions={
          <Button variant="primary" onClick={() => setEditing("new")}>
            <Plus className="size-4" aria-hidden /> Nueva regla
          </Button>
        }
      />
      <ErrorNote error={rules.error ?? toggle.error} />
      {rules.data?.length === 0 && <Empty>No hay reglas configuradas.</Empty>}
      {!!rules.data?.length && (
        <>
          <Summary>{rules.data.length === 1 ? "1 regla" : `${rules.data.length} reglas`}</Summary>
          <Table label="Reglas">
            <thead>
              <tr>
                <Th>Activa</Th>
                <Th>Nombre</Th>
                <Th>Disparador</Th>
                <Th>Acciones</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {rules.data.map((r) => (
                <tr key={r.id} className="border-t border-line align-top">
                  <td>
                    <input
                      type="checkbox"
                      role="switch"
                      aria-label={`Activar ${r.name}`}
                      checked={r.enabled}
                      disabled={toggle.isPending}
                      onChange={() => toggle.mutate(r)}
                    />
                  </td>
                  <td className="font-medium">{r.name}</td>
                  <td className="text-sm">{triggerLabel[r.trigger_type]}</td>
                  <td className="text-sm text-muted">{describeActions(r.actions)}</td>
                  <td className="text-right">
                    <span className="inline-flex gap-2">
                      <Button aria-label={`Editar ${r.name}`} onClick={() => setEditing(r)}>
                        Editar
                      </Button>
                      <Button className="text-bad" aria-label={`Eliminar ${r.name}`} onClick={() => setDeleting(r)}>
                        Eliminar
                      </Button>
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        </>
      )}
      {editing && (
        <RuleForm
          rule={editing === "new" ? undefined : editing}
          onDone={() => {
            setEditing(null);
            void qc.invalidateQueries({ queryKey: ["rules"] });
          }}
          onCancel={() => setEditing(null)}
        />
      )}
      {deleting && (
        <ConfirmDialog
          title="Eliminar regla"
          message={`¿Eliminar la regla ${deleting.name}?`}
          confirmLabel="Eliminar"
          pending={remove.isPending}
          error={remove.error}
          onConfirm={() => remove.mutate(deleting)}
          onCancel={() => setDeleting(null)}
        />
      )}
    </div>
  );
}

function describeActions(a: Schemas["RuleActions"]) {
  const n = a.channel_ids?.length ?? 0;
  const parts = [
    a.create_alarm && "Crear alarma",
    a.notify_in_app && "Notificar en la app",
    n > 0 && (n === 1 ? "1 canal externo" : `${n} canales externos`),
  ].filter(Boolean);
  return parts.length ? parts.join(" + ") : "Sin acciones";
}

function RuleForm({ rule, onDone, onCancel }: { rule?: Rule; onDone: () => void; onCancel: () => void }) {
  const cameras = useQuery(camerasQuery({}));
  const servers = useQuery(serversQuery);
  const sites = useQuery(sitesQuery);
  const channels = useQuery(channelsQuery);
  const c = rule?.conditions ?? {};
  const [f, setF] = useState({
    name: rule?.name ?? "",
    trigger: (rule?.trigger_type ?? "event") as Trigger,
    camera_ids: c.camera_ids ?? ([] as string[]),
    server_ids: c.server_ids ?? ([] as string[]),
    site_ids: c.site_ids ?? ([] as string[]),
    labels: c.labels ?? ([] as string[]),
    zones: c.zones ?? ([] as string[]),
    severities: c.severities ?? ([] as string[]),
    minutes: String(c.duration_seconds ? Math.round(c.duration_seconds / 60) : defaultMinutes),
    create_alarm: rule?.actions.create_alarm ?? true,
    notify_in_app: rule?.actions.notify_in_app ?? true,
    severity: (rule?.actions.severity ?? "warning") as NotifSeverity,
    channel_ids: rule?.actions.channel_ids ?? ([] as string[]),
  });

  const buildConditions = (): Schemas["RuleConditions"] => {
    if (f.trigger === "event") {
      const out: Schemas["RuleConditions"] = {};
      if (f.site_ids.length) out.site_ids = f.site_ids;
      if (f.camera_ids.length) out.camera_ids = f.camera_ids;
      if (f.labels.length) out.labels = f.labels;
      if (f.zones.length) out.zones = f.zones;
      if (f.severities.length) out.severities = f.severities;
      return out;
    }
    const out: Schemas["RuleConditions"] = { duration_seconds: Math.max(1, Number(f.minutes) || defaultMinutes) * 60 };
    if (f.site_ids.length) out.site_ids = f.site_ids;
    if (f.trigger === "camera_offline" && f.camera_ids.length) out.camera_ids = f.camera_ids;
    if (f.trigger === "server_offline" && f.server_ids.length) out.server_ids = f.server_ids;
    return out;
  };

  const save = useMutation({
    mutationFn: async () => {
      const body = {
        name: f.name.trim(),
        trigger_type: f.trigger,
        conditions: buildConditions(),
        actions: {
          create_alarm: f.create_alarm,
          notify_in_app: f.notify_in_app,
          severity: f.severity,
          ...(f.channel_ids.length ? { channel_ids: f.channel_ids } : {}),
        },
      };
      if (rule) return unwrap(await api.PATCH("/api/v1/rules/{ruleId}", { params: { path: { ruleId: rule.id } }, body }));
      return unwrap(await api.POST("/api/v1/rules", { body: { ...body, enabled: true } }));
    },
    onSuccess: onDone,
  });

  // Zones come from the cameras the rule watches (all of them when none is selected).
  const zoneSuggestions = [
    ...new Set((cameras.data ?? []).filter((cam) => f.camera_ids.length === 0 || f.camera_ids.includes(cam.id)).flatMap((cam) => cam.zones)),
  ].sort();

  const flip = (list: string[], id: string) => (list.includes(id) ? list.filter((x) => x !== id) : [...list, id]);

  return (
    <Modal title={rule ? `Editar ${rule.name}` : "Nueva regla"} onClose={onCancel}>
      <form
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          save.mutate();
        }}
        className="flex flex-col gap-4"
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Nombre">
            <TextInput required value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} />
          </Field>
          <Field label="Disparador">
            <Select value={f.trigger} onChange={(e) => setF({ ...f, trigger: e.target.value as Trigger })}>
              {(Object.keys(triggerLabel) as Trigger[]).map((t) => (
                <option key={t} value={t}>
                  {triggerLabel[t]}
                </option>
              ))}
            </Select>
          </Field>
        </div>

        <CheckGroup
          legend="Sitios (vacío = todos)"
          options={(sites.data ?? []).map((site) => ({ value: site.id, label: site.name }))}
          selected={f.site_ids}
          onToggle={(id) => setF({ ...f, site_ids: flip(f.site_ids, id) })}
          empty="No hay sitios disponibles."
        />
        {f.trigger !== "server_offline" && (
          <CheckGroup
            legend={f.trigger === "event" ? "Cámaras (vacío = todas)" : "Cámaras a vigilar (vacío = todas)"}
            options={(cameras.data ?? []).map((cam) => ({ value: cam.id, label: cam.display_name }))}
            selected={f.camera_ids}
            onToggle={(id) => setF({ ...f, camera_ids: flip(f.camera_ids, id) })}
            empty="No hay cámaras disponibles."
          />
        )}
        {f.trigger === "server_offline" && (
          <CheckGroup
            legend="Servidores a vigilar (vacío = todos)"
            options={(servers.data ?? []).map((s) => ({ value: s.id, label: s.name }))}
            selected={f.server_ids}
            onToggle={(id) => setF({ ...f, server_ids: flip(f.server_ids, id) })}
            empty="No hay servidores disponibles."
          />
        )}
        {f.trigger === "event" && (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <TagInput
                label="Etiquetas"
                hint="Elegí una sugerencia o escribí otra y presioná Enter."
                value={f.labels}
                onChange={(labels) => setF({ ...f, labels })}
                suggestions={knownLabels}
              />
              <TagInput
                label="Zonas"
                hint="Sugeridas según las cámaras elegidas; también podés escribir otra."
                value={f.zones}
                onChange={(zones) => setF({ ...f, zones })}
                suggestions={zoneSuggestions}
              />
            </div>
            <CheckGroup
              legend="Severidad del evento (vacío = todas)"
              options={eventSeverities}
              selected={f.severities}
              onToggle={(v) => setF({ ...f, severities: flip(f.severities, v) })}
            />
          </>
        )}
        {f.trigger !== "event" && (
          <Field label="Minutos sin conexión" hint="La regla se dispara cuando supera este tiempo.">
            <TextInput type="number" min={1} required value={f.minutes} onChange={(e) => setF({ ...f, minutes: e.target.value })} />
          </Field>
        )}

        <fieldset className="flex flex-col gap-2">
          <legend className="text-sm font-medium">Acciones</legend>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={f.create_alarm} onChange={(e) => setF({ ...f, create_alarm: e.target.checked })} className="rounded border-line" />
            Crear alarma
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={f.notify_in_app} onChange={(e) => setF({ ...f, notify_in_app: e.target.checked })} className="rounded border-line" />
            Notificar en la app
          </label>
          <Field label="Severidad de la notificación">
            <Select value={f.severity} onChange={(e) => setF({ ...f, severity: e.target.value as NotifSeverity })}>
              <option value="info">Info</option>
              <option value="warning">Advertencia</option>
              <option value="critical">Crítica</option>
            </Select>
          </Field>
        </fieldset>

        <CheckGroup
          legend="Canales externos (además de las acciones anteriores)"
          options={(channels.data ?? []).map((ch) => ({ value: ch.id, label: ch.enabled ? ch.name : `${ch.name} (desactivado)` }))}
          selected={f.channel_ids}
          onToggle={(id) => setF({ ...f, channel_ids: flip(f.channel_ids, id) })}
          empty="No hay canales configurados. Creálos en Canales."
        />

        <ErrorNote error={save.error} />
        <div className="flex items-center gap-2 pt-2">
          <Button type="submit" variant="primary" disabled={save.isPending}>
            {save.isPending ? "Guardando…" : "Guardar"}
          </Button>
          <Button onClick={onCancel}>Cancelar</Button>
        </div>
      </form>
    </Modal>
  );
}

function CheckGroup({
  legend, options, selected, onToggle, empty,
}: {
  legend: string;
  options: { value: string; label: string }[];
  selected: string[];
  onToggle: (value: string) => void;
  empty?: string;
}) {
  return (
    <fieldset className="flex flex-col gap-2">
      <legend className="text-sm font-medium">{legend}</legend>
      {options.length === 0 && empty && <span className="text-xs text-muted">{empty}</span>}
      <div className="grid max-h-40 gap-2 overflow-y-auto sm:grid-cols-2">
        {options.map((o) => (
          <label key={o.value} className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={selected.includes(o.value)} onChange={() => onToggle(o.value)} className="rounded border-line" />
            <span>{o.label}</span>
          </label>
        ))}
      </div>
    </fieldset>
  );
}
