import { useT } from "@/i18n";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { Icon } from "@/components/Icon";
import { type FormEvent, useState } from "react";
import { ApiError, api, type Schemas, unwrap } from "@/api/client";
import { cameraGroupsQuery, camerasQuery, channelsQuery, rulesQuery, serversQuery, sitesQuery } from "@/api/queries";
import { ConfirmDialog } from "@/components/ConfirmDialog";
import { Modal } from "@/components/Modal";
import { TagInput } from "@/components/TagInput";
import { Button, Checkbox, Empty, ErrorNote, Field, PageHeader, Pill, Select, Summary, Switch, TextInput } from "@/components/ui";
import { knownLabels, vehicleTypeOptions } from "@/lib/format";

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
const wizardSteps = ["Alcance", "Condiciones", "Acciones", "Revisión y activación"] as const;

/** Rules: guided form for automation rules (no JSON). Requires rules.read (rules.manage to change). */
export function Rules() {
  const t = useT();
  const qc = useQueryClient();
  const rules = useQuery(rulesQuery);
  const [editing, setEditing] = useState<Rule | "new" | null>(null);
  const [deleting, setDeleting] = useState<Rule | null>(null);
  const [historyRule, setHistoryRule] = useState<Rule | null>(null);

  const toggle = useMutation({
    mutationFn: async (r: Rule) =>
      unwrap(await api.PATCH("/api/v1/rules/{ruleId}", { params: { path: { ruleId: r.id }, header: { "If-Match": String(r.version) } }, body: { enabled: !r.enabled } })),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["rules"] }),
    onError: async (error) => {
      if (error instanceof ApiError && error.status === 409) await qc.invalidateQueries({ queryKey: ["rules"] });
    },
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
        <PageHeader title={t("nav.rules")} />
        <Empty>No tenés permiso para administrar reglas. Pedile a un administrador el permiso de gestión de notificaciones.</Empty>
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <PageHeader
        title={t("nav.rules")}
        description={t("settings.rules")}
        actions={
          <Button variant="filled" onClick={() => setEditing("new")}>
            <Icon icon={Plus} size="xs" /> Nueva regla
          </Button>
        }
      />
      <ErrorNote error={rules.error ?? toggle.error} />
      {rules.data?.length === 0 && <Empty>No hay reglas configuradas.</Empty>}
      {!!rules.data?.length && (
        <>
          <Summary>{rules.data.length === 1 ? "1 regla" : `${rules.data.length} reglas`}</Summary>
          <ul aria-label="Reglas" className="flex flex-col gap-3">
            {rules.data.map((r) => (
              <li key={r.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-m3-lg bg-surface-1 p-4">
                <Switch
                  className="shrink-0 [&>span:last-child]:sr-only"
                  label={`Activar ${r.name}`}
                  checked={r.enabled}
                  disabled={toggle.isPending}
                  onChange={() => toggle.mutate(r)}
                />
                <div className="flex min-w-0 flex-1 basis-56 flex-col gap-1">
                  <div className="text-base font-bold break-words">{r.name}</div>
                  <div className="flex flex-wrap items-center gap-2 text-sm">
                    <Pill tone="secondary">{triggerLabel[r.trigger_type]}</Pill>
                    <span className="text-on-surface-variant">{describeActions(r.actions)}</span>
                  </div>
                </div>
                <div className="flex items-center gap-2">
          <Button size="sm" variant="text" onClick={() => setHistoryRule(r)}>Historial</Button>
          <Button size="sm" variant="tonal" aria-label={`Editar ${r.name}`} onClick={() => setEditing(r)}>
                    Editar
                  </Button>
                  <Button size="sm" variant="outlined" className="border-bad/60 text-bad" aria-label={`Eliminar ${r.name}`} onClick={() => setDeleting(r)}>
                    Eliminar
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        </>
      )}
      {editing && (
        <RuleForm
          key={editing === "new" ? "new" : `${editing.id}:${editing.version}`}
          rule={editing === "new" ? undefined : editing}
          onReloadRule={(latest) => setEditing(latest)}
          onDone={() => {
            setEditing(null);
            void qc.invalidateQueries({ queryKey: ["rules"] });
          }}
          onCancel={() => setEditing(null)}
        />
      )}
      {historyRule && <RuleHistory rule={historyRule} onClose={() => setHistoryRule(null)} />}
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

function RuleForm({ rule, onDone, onCancel, onReloadRule }: { rule?: Rule; onDone: () => void; onCancel: () => void; onReloadRule: (rule: Rule) => void }) {
  const cameras = useQuery(camerasQuery({}));
  const cameraGroups = useQuery(cameraGroupsQuery);
  const servers = useQuery(serversQuery);
  const sites = useQuery(sitesQuery);
  const channels = useQuery(channelsQuery);
  const templates = useQuery({ queryKey: ["rule-templates"], queryFn: async () => unwrap(await api.GET("/api/v1/rules/templates")).items });
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
    vehicle_types: c.vehicle_types ?? ([] as string[]),
    vehicle_colors: c.vehicle_colors ?? ([] as string[]),
    camera_group_ids: c.camera_group_ids ?? ([] as string[]),
    map_zone_ids: c.map_zone_ids ?? ([] as string[]),
    cooldown_seconds: c.cooldown_seconds === undefined ? "" : String(c.cooldown_seconds),
    schedule_days: c.schedule?.days_of_week ?? ([] as number[]),
    schedule_start: c.schedule?.start_time ?? "",
    schedule_end: c.schedule?.end_time ?? "",
    schedule_timezone: c.schedule?.timezone ?? "",
    minutes: String(c.duration_seconds ? Math.round(c.duration_seconds / 60) : defaultMinutes),
    create_alarm: rule?.actions.create_alarm ?? true,
    notify_in_app: rule?.actions.notify_in_app ?? true,
    severity: (rule?.actions.severity ?? "warning") as NotifSeverity,
    channel_ids: rule?.actions.channel_ids ?? ([] as string[]),
    enabled: rule?.enabled ?? true,
  });
  const [step, setStep] = useState(0);
  const [stepError, setStepError] = useState("");
  const nextStep = () => {
    if (step === 0 && !f.name.trim()) {
      setStepError("Ingresá un nombre para continuar.");
      return;
    }
    if (step === 1 && f.trigger === "event" && Boolean(f.schedule_start) !== Boolean(f.schedule_end)) {
      setStepError("Completá ambos horarios para definir un rango.");
      return;
    }
    if (step === 1 && f.trigger === "event" && f.cooldown_seconds.trim()) {
      const cooldown = Number(f.cooldown_seconds);
      if (!Number.isInteger(cooldown) || cooldown < 0 || cooldown > 86400) {
        setStepError("El cooldown debe estar entre 0 y 86400 segundos.");
        return;
      }
    }
    if (step === 1 && f.trigger !== "event" && (!Number.isFinite(Number(f.minutes)) || Number(f.minutes) < 1)) {
      setStepError("Ingresá una duración de al menos un minuto.");
      return;
    }
    setStepError("");
    setStep((current) => Math.min(current + 1, wizardSteps.length - 1));
  };
  const [simulation, setSimulation] = useState<Schemas["RuleSimulation"] | null>(null);
  const [conflictRule, setConflictRule] = useState<Rule | null>(null);

  const buildConditions = (): Schemas["RuleConditions"] => {
    const out: Schemas["RuleConditions"] = { ...c };
    if (f.trigger === "event") {
      delete out.duration_seconds;
      delete out.server_ids;
      if (f.camera_group_ids.length) out.camera_group_ids = f.camera_group_ids; else delete out.camera_group_ids;
      if (f.map_zone_ids.length) out.map_zone_ids = f.map_zone_ids; else delete out.map_zone_ids;
      const cooldown = Number(f.cooldown_seconds);
      if (f.cooldown_seconds.trim() && Number.isFinite(cooldown)) out.cooldown_seconds = cooldown; else delete out.cooldown_seconds;
      if (f.schedule_start || f.schedule_end || f.schedule_days.length || f.schedule_timezone.trim()) {
        out.schedule = {
          ...(f.schedule_days.length ? { days_of_week: f.schedule_days } : {}),
          ...(f.schedule_start ? { start_time: f.schedule_start } : {}),
          ...(f.schedule_end ? { end_time: f.schedule_end } : {}),
          ...(f.schedule_timezone.trim() ? { timezone: f.schedule_timezone.trim() } : {}),
        };
      } else delete out.schedule;
      if (f.site_ids.length) out.site_ids = f.site_ids; else delete out.site_ids;
      if (f.camera_ids.length) out.camera_ids = f.camera_ids; else delete out.camera_ids;
      if (f.labels.length) out.labels = f.labels; else delete out.labels;
      if (f.zones.length) out.zones = f.zones; else delete out.zones;
      if (f.severities.length) out.severities = f.severities; else delete out.severities;
      if (f.vehicle_types.length) out.vehicle_types = f.vehicle_types; else delete out.vehicle_types;
      if (f.vehicle_colors.length) out.vehicle_colors = f.vehicle_colors; else delete out.vehicle_colors;
      return out;
    }
    delete out.labels;
    delete out.zones;
    delete out.severities;
    delete out.vehicle_types;
    delete out.vehicle_colors;
    delete out.camera_group_ids;
    delete out.map_zone_ids;
    delete out.schedule;
    delete out.cooldown_seconds;
    out.duration_seconds = Math.max(1, Number(f.minutes) || defaultMinutes) * 60;
    if (f.site_ids.length) out.site_ids = f.site_ids; else delete out.site_ids;
    if (f.trigger === "camera_offline" && f.camera_ids.length) out.camera_ids = f.camera_ids; else delete out.camera_ids;
    if (f.trigger === "server_offline" && f.server_ids.length) out.server_ids = f.server_ids; else delete out.server_ids;
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
          enabled: f.enabled,
      };
      if (rule) return unwrap(await api.PATCH("/api/v1/rules/{ruleId}", { params: { path: { ruleId: rule.id }, header: { "If-Match": String(rule.version) } }, body }));
      return unwrap(await api.POST("/api/v1/rules", { body }));
    },
    onSuccess: onDone,
    onError: async (error) => {
      if (!(error instanceof ApiError) || error.status !== 409 || !rule) return;
      try { setConflictRule(unwrap(await api.GET("/api/v1/rules/{ruleId}", { params: { path: { ruleId: rule.id } } }))); }
      catch { setConflictRule(null); }
    },
  });
  const simulate = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/rules/simulate", { body: { trigger_type: f.trigger, conditions: buildConditions(), hours: 24, limit: 100 } })),
    onSuccess: setSimulation,
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
        <div className="flex items-center justify-between gap-3 rounded-m3-md bg-surface-2 p-3" aria-live="polite">
          <span>Paso {step + 1} de 4: {wizardSteps[step]}</span>
          <div className="flex gap-2">
            {step > 0 && <Button type="button" variant="text" onClick={() => { setStepError(""); setStep((current) => current - 1); }}>Anterior</Button>}
            {step < wizardSteps.length - 1 && <Button type="button" variant="tonal" onClick={nextStep}>Siguiente</Button>}
          </div>
        </div>
        {stepError && <p role="alert" className="text-sm text-bad">{stepError}</p>}
        <div style={{ display: step === 0 ? "flex" : "none" }} className="flex flex-col gap-4">
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

        <Field label="Plantilla">
          <Select value="" onChange={(e) => {
            const item = templates.data?.find((template) => template.id === e.target.value);
            if (!item) return;
            setF((current) => ({ ...current, trigger: item.trigger_type, camera_ids: [], server_ids: [], site_ids: [], labels: item.conditions.labels ?? [], zones: item.conditions.zones ?? [], severities: item.conditions.severities ?? [], vehicle_types: item.conditions.vehicle_types ?? [], vehicle_colors: item.conditions.vehicle_colors ?? [], camera_group_ids: item.conditions.camera_group_ids ?? [], map_zone_ids: item.conditions.map_zone_ids ?? [], cooldown_seconds: item.conditions.cooldown_seconds === undefined ? "" : String(item.conditions.cooldown_seconds), schedule_days: item.conditions.schedule?.days_of_week ?? [], schedule_start: item.conditions.schedule?.start_time ?? "", schedule_end: item.conditions.schedule?.end_time ?? "", schedule_timezone: item.conditions.schedule?.timezone ?? "", minutes: String(item.conditions.duration_seconds ? Math.round(item.conditions.duration_seconds / 60) : defaultMinutes), create_alarm: item.actions.create_alarm ?? true, notify_in_app: item.actions.notify_in_app ?? true, severity: item.actions.severity ?? "warning", channel_ids: item.actions.channel_ids ?? [] }));
          }}>
            <option value="">Elegir plantilla…</option>
            {(templates.data ?? []).map((template) => <option key={template.id} value={template.id}>{template.name}</option>)}
          </Select>
        </Field>

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
            <CheckGroup
              legend="Grupos de cámaras (vacío = todos)"
              options={(cameraGroups.data ?? []).map((group) => ({ value: group.id, label: group.name }))}
              selected={f.camera_group_ids}
              onToggle={(id) => setF({ ...f, camera_group_ids: flip(f.camera_group_ids, id) })}
              empty="No hay grupos disponibles."
            />
            <ErrorNote error={cameraGroups.error} />
          </>
        )}
        </div>
        <div style={{ display: step === 1 ? "flex" : "none" }} className="flex flex-col gap-4">
        {f.trigger === "event" && (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Cooldown (segundos)" hint="Vacío usa el valor predeterminado de 300 segundos.">
                <TextInput type="number" min={0} max={86400} value={f.cooldown_seconds} onChange={(e) => setF({ ...f, cooldown_seconds: e.target.value })} />
              </Field>
              <Field label="Zona horaria IANA" hint="Se prioriza la zona horaria del sitio; si no está configurada, se usa esta. Vacío usa UTC.">
                <TextInput placeholder="America/Argentina/Buenos_Aires" value={f.schedule_timezone} onChange={(e) => setF({ ...f, schedule_timezone: e.target.value })} />
              </Field>
              <Field label="Desde (HH:MM)"><TextInput type="time" required={step === 1 && !!f.schedule_end} value={f.schedule_start} onChange={(e) => setF({ ...f, schedule_start: e.target.value })} /></Field>
              <Field label="Hasta (HH:MM)"><TextInput type="time" required={step === 1 && !!f.schedule_start} value={f.schedule_end} onChange={(e) => setF({ ...f, schedule_end: e.target.value })} /></Field>
            </div>
            <CheckGroup
              legend="Días de la semana (vacío = todos)"
              options={["Domingo", "Lunes", "Martes", "Miércoles", "Jueves", "Viernes", "Sábado"].map((label, value) => ({ value: String(value), label }))}
              selected={f.schedule_days.map(String)}
              onToggle={(day) => setF({ ...f, schedule_days: flip(f.schedule_days.map(String), day).map(Number) })}
            />
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
            <CheckGroup
              legend="Tipo de vehículo (vacío = cualquiera)"
              options={vehicleTypeOptions()}
              selected={f.vehicle_types}
              onToggle={(v) => setF({ ...f, vehicle_types: flip(f.vehicle_types, v) })}
            />
            <CheckGroup
              legend="Color del vehículo (vacío = cualquiera)"
              options={[
                { value: "black", label: "Negro" },
                { value: "white", label: "Blanco" },
                { value: "gray", label: "Gris" },
                { value: "silver", label: "Plata" },
                { value: "red", label: "Rojo" },
                { value: "blue", label: "Azul" },
              ]}
              selected={f.vehicle_colors}
              onToggle={(v) => setF({ ...f, vehicle_colors: flip(f.vehicle_colors, v) })}
            />
          </>
        )}
        {f.trigger !== "event" && (
          <Field label="Minutos sin conexión" hint="La regla se dispara cuando supera este tiempo.">
            <TextInput type="number" min={1} required={step === 1} value={f.minutes} onChange={(e) => setF({ ...f, minutes: e.target.value })} />
          </Field>
        )}
        </div>

        <div style={{ display: step === 2 ? "flex" : "none" }} className="flex flex-col gap-4">
        <fieldset className="flex flex-col gap-2">
          <legend className="text-sm font-medium">Acciones</legend>
          <Checkbox checked={f.create_alarm} onChange={(v) => setF({ ...f, create_alarm: v })} label="Crear alarma" />
          <Checkbox checked={f.notify_in_app} onChange={(v) => setF({ ...f, notify_in_app: v })} label="Notificar en la app" />
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
        </div>

        <ErrorNote error={save.error} />
        {conflictRule && <div role="alert" className="rounded-m3-md bg-warning/10 p-3 text-sm">La regla cambió en el servidor. Tus cambios no se guardaron. <Button type="button" variant="tonal" onClick={() => onReloadRule(conflictRule)}>Cargar versión actual</Button></div>}
        <div style={{ display: step === 3 ? "flex" : "none" }} className="flex flex-col gap-4">
        <section aria-label="Resumen de la regla" className="rounded-m3-md border border-outline-variant p-3">
          <h3 className="font-semibold">Resumen de la regla</h3>
          <dl className="mt-2 grid gap-2 text-sm">
            <div><dt className="font-medium">Nombre y disparador</dt><dd>{f.name.trim() || "Sin nombre"} · {triggerLabel[f.trigger]}</dd></div>
            <div><dt className="font-medium">Alcance</dt><dd>{f.camera_ids.length ? cameras.data?.filter((camera) => f.camera_ids.includes(camera.id)).map((camera) => camera.display_name).join(", ") : "Todas las cámaras"}; {f.camera_group_ids.length ? cameraGroups.data?.filter((group) => f.camera_group_ids.includes(group.id)).map((group) => group.name).join(", ") : "Todos los grupos"}</dd></div>
            <div><dt className="font-medium">Condiciones</dt><dd>{f.labels.length ? `Etiquetas: ${f.labels.join(", ")}` : "Sin filtro de etiquetas"}{f.zones.length ? ` · Zonas: ${f.zones.join(", ")}` : ""}</dd></div>
            <div><dt className="font-medium">Acciones</dt><dd>{describeActions({ create_alarm: f.create_alarm, notify_in_app: f.notify_in_app, channel_ids: f.channel_ids })}</dd></div>
          </dl>
        </section>
        <div className="flex flex-col gap-2 rounded-m3-md border border-outline-variant p-3">
          <Button type="button" variant="tonal" disabled={simulate.isPending || f.trigger !== "event"} onClick={() => simulate.mutate()}>{simulate.isPending ? "Simulando…" : "Vista previa con eventos recientes"}</Button>
          <ErrorNote error={simulate.error} />
          {simulation && <div role="status" className="text-sm">Se evaluaron {simulation.evaluated} eventos recientes; {simulation.condition_matched} coinciden con las condiciones y {simulation.matched} quedan en la proyección de cooldown por cámara de {simulation.cooldown_seconds} s ({simulation.cooldown_suppressed} suprimidos). Es una estimación basada en los timestamps históricos; no consulta ejecuciones previas ni ejecuta acciones.{simulation.has_more && " Hay más eventos en la ventana; la vista previa usa los más recientes dentro del límite."}{simulation.matches.slice(0, 5).map((match) => <div key={match.event_id}>{match.camera_name} · {match.labels.join(", ")} · {new Date(match.start_time).toLocaleString()}</div>)}</div>}
        </div>
        {step === wizardSteps.length - 1 && (
          <fieldset className="rounded-m3-md border border-outline-variant p-3">
            <legend className="text-sm font-medium">Activación</legend>
            <Checkbox checked={f.enabled} onChange={(enabled) => setF({ ...f, enabled })} label="Activar regla al guardar" />
          </fieldset>
        )}
        </div>
        <div className="flex items-center gap-2 pt-2">
          {step === 3 && <Button type="submit" variant="filled" disabled={save.isPending}>
            {save.isPending ? "Guardando…" : "Guardar"}
          </Button>}
          <Button variant="text" onClick={onCancel}>Cancelar</Button>
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
          <Checkbox key={o.value} checked={selected.includes(o.value)} onChange={() => onToggle(o.value)} label={o.label} />
        ))}
      </div>
    </fieldset>
  );
}

function RuleHistory({ rule, onClose }: { rule: Rule; onClose: () => void }) {
  const versions = useQuery({
    queryKey: ["rule-versions", rule.id],
    queryFn: async () => unwrap(await api.GET("/api/v1/rules/{ruleId}/versions", { params: { path: { ruleId: rule.id } } })).items,
  });
  return (
    <Modal title={`Historial de ${rule.name}`} onClose={onClose}>
      <ErrorNote error={versions.error} />
      {versions.data?.map((item) => <article key={item.version} className="border-b border-outline-variant py-3">
        <h3 className="font-semibold">Versión {item.version} · {new Date(item.created_at).toLocaleString()}</h3>
        <p>{item.snapshot.name} · {triggerLabel[item.snapshot.trigger_type]} · {item.snapshot.enabled ? "Activa" : "Desactivada"}</p>
      </article>)}
      {versions.data?.length === 0 && <Empty>No hay versiones disponibles.</Empty>}
    </Modal>
  );
}
