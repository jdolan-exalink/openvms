import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CheckCheck, Download, History } from "lucide-react";
import { useCallback, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { cameraGroupsQuery, camerasQuery, type EventFilter, eventsQuery, meQuery, sitesQuery } from "@/api/queries";
import { SearchSummary, type FilterChip } from "@/components/SearchSummary";
import { Modal } from "@/components/Modal";
import { Button, Empty, ErrorNote, Field, PageHeader, Select, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import { commonLabels, fmtDateTime, fmtDuration, fromLocalInput, labelName } from "@/lib/format";
import { can } from "@/lib/perm";

type Form = {
  site: string;
  camera: string;
  cameraGroup: string;
  label: string;
  zone: string;
  subLabel: string;
  severity: "" | "alert" | "detection";
  plate: string;
  from: string;
  to: string;
  pending: boolean;
  hasSnapshot: boolean;
  hasPreview: boolean;
};

const emptyForm: Form = {
  site: "",
  camera: "",
  cameraGroup: "",
  label: "",
  zone: "",
  subLabel: "",
  severity: "",
  plate: "",
  from: "",
  to: "",
  pending: false,
  hasSnapshot: false,
  hasPreview: false,
};

function toFilter(f: Form): EventFilter {
  return {
    site_id: f.site ? [f.site] : undefined,
    camera_id: f.camera ? [f.camera] : undefined,
    camera_group_id: f.cameraGroup ? [f.cameraGroup] : undefined,
    label: f.label ? [f.label] : undefined,
    zone: f.zone.trim() ? [f.zone.trim()] : undefined,
    sub_label: f.subLabel.trim() ? [f.subLabel.trim()] : undefined,
    severity: f.severity || undefined,
    plate: f.plate.trim() || undefined,
    from: fromLocalInput(f.from),
    to: fromLocalInput(f.to),
    reviewed: f.pending ? false : undefined,
    has_snapshot: f.hasSnapshot ? true : undefined,
    has_preview: f.hasPreview ? true : undefined,
    limit: 48,
  };
}

type AppliedChip = { key: string; label: string; clear: Partial<Form> };

/** appliedChips describes each non-default applied filter and the form patch that removes it. */
function appliedChips(f: Form, names: { site?: string; camera?: string; group?: string }): AppliedChip[] {
  const out: AppliedChip[] = [];
  if (f.site) out.push({ key: "site", label: `Sitio: ${names.site ?? f.site}`, clear: { site: "", camera: "" } });
  if (f.camera) out.push({ key: "camera", label: `Cámara: ${names.camera ?? f.camera}`, clear: { camera: "" } });
  if (f.cameraGroup) out.push({ key: "group", label: `Grupo: ${names.group ?? f.cameraGroup}`, clear: { cameraGroup: "" } });
  if (f.label) out.push({ key: "label", label: `Objeto: ${labelName(f.label)}`, clear: { label: "" } });
  if (f.zone.trim()) out.push({ key: "zone", label: `Zona: ${f.zone.trim()}`, clear: { zone: "" } });
  if (f.subLabel.trim()) out.push({ key: "subLabel", label: `Sub-etiqueta: ${f.subLabel.trim()}`, clear: { subLabel: "" } });
  if (f.severity) out.push({ key: "severity", label: `Tipo: ${f.severity === "alert" ? "Solo alertas" : "Solo detecciones"}`, clear: { severity: "" } });
  if (f.plate.trim()) out.push({ key: "plate", label: `Patente: ${f.plate.trim()}`, clear: { plate: "" } });
  if (f.from) out.push({ key: "from", label: `Desde: ${f.from.replace("T", " ")}`, clear: { from: "" } });
  if (f.to) out.push({ key: "to", label: `Hasta: ${f.to.replace("T", " ")}`, clear: { to: "" } });
  if (f.pending) out.push({ key: "pending", label: "Solo sin revisar", clear: { pending: false } });
  if (f.hasSnapshot) out.push({ key: "hasSnapshot", label: "Con snapshot", clear: { hasSnapshot: false } });
  if (f.hasPreview) out.push({ key: "hasPreview", label: "Con preview", clear: { hasPreview: false } });
  return out;
}

/** Events is the federated event index (PRD §34-40): every authorized Frigate in one list. */
export function Events() {
  const me = useQuery(meQuery);
  const sites = useQuery(sitesQuery);
  const cameras = useQuery(camerasQuery({}));
  const cameraGroups = useQuery(cameraGroupsQuery);
  const [form, setForm] = useState<Form>(emptyForm);
  const [applied, setApplied] = useState<Form>(emptyForm);
  const events = useInfiniteQuery(eventsQuery(toFilter(applied)));
  const [open, setOpen] = useState<Schemas["Event"] | null>(null);
  const closeDetail = useCallback(() => setOpen(null), []);
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setForm((f) => ({ ...f, [k]: v }));
  const items = events.data?.pages.flatMap((p) => p.items) ?? [];
  const chips = appliedChips(applied, {
    site: sites.data?.find((x) => x.id === applied.site)?.name,
    camera: cameras.data?.find((x) => x.id === applied.camera)?.display_name,
    group: cameraGroups.data?.find((x) => x.id === applied.cameraGroup)?.name,
  }).map<FilterChip>((c) => ({
    ...c,
    onRemove: () => {
      const patch = c.clear;
      setForm((f) => ({ ...f, ...patch }));
      setApplied((f) => ({ ...f, ...patch }));
    },
  }));
  const camsOfSite = (cameras.data ?? []).filter((c) => !form.site || c.site_id === form.site);

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-6">
      <PageHeader title="Eventos" description="Alertas y detecciones de todos los servidores Frigate que podés ver, en una sola lista." />
      <form
        className="grid gap-3 rounded border border-line bg-surface p-3 sm:grid-cols-2 lg:grid-cols-4"
        onSubmit={(e) => {
          e.preventDefault();
          setApplied(form);
        }}
      >
        <Field label="Sitio">
          <Select value={form.site} onChange={(e) => setForm({ ...form, site: e.target.value, camera: "" })}>
            <option value="">Todos</option>
            {sites.data?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Cámara">
          <Select value={form.camera} onChange={(e) => set("camera", e.target.value)}>
            <option value="">Todas</option>
            {camsOfSite.map((c) => (
              <option key={c.id} value={c.id}>
                {c.display_name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Grupo de cámaras">
          <Select value={form.cameraGroup} onChange={(e) => set("cameraGroup", e.target.value)}>
            <option value="">Todos</option>
            {cameraGroups.data?.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Objeto">
          <Select value={form.label} onChange={(e) => set("label", e.target.value)}>
            <option value="">Todos</option>
            {commonLabels.map((l) => (
              <option key={l} value={l}>
                {labelName(l)}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Zona">
          <TextInput value={form.zone} onChange={(e) => set("zone", e.target.value)} placeholder="entrada, ingreso…" />
        </Field>
        <Field label="Sub-etiqueta">
          <TextInput value={form.subLabel} onChange={(e) => set("subLabel", e.target.value)} placeholder="placa_reconocida…" />
        </Field>
        <Field label="Tipo">
          <Select value={form.severity} onChange={(e) => set("severity", e.target.value as Form["severity"])}>
            <option value="">Alertas y detecciones</option>
            <option value="alert">Solo alertas</option>
            <option value="detection">Solo detecciones</option>
          </Select>
        </Field>
        <Field label="Desde">
          <TextInput type="datetime-local" value={form.from} onChange={(e) => set("from", e.target.value)} />
        </Field>
        <Field label="Hasta">
          <TextInput type="datetime-local" value={form.to} onChange={(e) => set("to", e.target.value)} />
        </Field>
        {can(me.data, "lpr.search") ? (
          <Field label="Patente">
            <TextInput value={form.plate} onChange={(e) => set("plate", e.target.value.toUpperCase())} placeholder="AB123CD o parte" />
          </Field>
        ) : (
          <div />
        )}
        <div className="flex flex-col justify-end gap-2">
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={form.pending} onChange={(e) => set("pending", e.target.checked)} /> Solo sin revisar
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={form.hasSnapshot} onChange={(e) => set("hasSnapshot", e.target.checked)} /> Con snapshot
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={form.hasPreview} onChange={(e) => set("hasPreview", e.target.checked)} /> Con preview
          </label>
          <div className="flex gap-2">
            <Button type="submit" variant="primary">
              Buscar
            </Button>
            <Button
              onClick={() => {
                setForm(emptyForm);
                setApplied(emptyForm);
              }}
            >
              Limpiar
            </Button>
          </div>
        </div>
      </form>

      <SearchSummary
        count={items.length}
        noun={{ one: "evento", many: "eventos" }}
        hasMore={!!events.hasNextPage}
        loading={events.isPending}
        chips={chips}
        onClear={() => {
          setForm(emptyForm);
          setApplied(emptyForm);
        }}
      />
      <ErrorNote error={events.error} />
      {events.isSuccess && items.length === 0 && <Empty>No hay eventos que coincidan.</Empty>}
      <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
        {items.map((e) => (
          <li key={e.id}>
            <button
              type="button"
              onClick={() => setOpen(e)}
              className={cn(
                "flex w-full flex-col overflow-hidden rounded border border-line border-l-4 bg-surface text-left hover:border-accent focus-visible:outline-2 focus-visible:outline-accent",
                e.severity === "alert" ? "border-l-bad" : "border-l-line",
              )}
            >
              <Thumb event={e} />
              <div className="flex flex-col gap-1 p-2 text-sm">
                <div className="flex items-center gap-2">
                  <span className={cn("rounded px-1.5 py-0.5 font-mono text-[10px] uppercase", e.severity === "alert" ? "bg-bad/20 text-bad" : "bg-raised text-muted")}>
                    {e.severity === "alert" ? "Alerta" : "Detección"}
                  </span>
                  <span className="truncate font-medium">{e.labels.map(labelName).join(", ") || "—"}</span>
                  <ReviewBadge reviewed={e.reviewed} />
                </div>
                <div className="truncate text-xs text-muted">
                  {e.camera_name} · {e.site_name}
                </div>
                <div className="flex items-center justify-between text-xs text-muted">
                  <span>{fmtDateTime(e.start_time)}</span>
                  {e.plates.length > 0 && <span className="rounded bg-accent/15 px-1.5 font-mono text-accent">{e.plates.join(" ")}</span>}
                </div>
              </div>
            </button>
          </li>
        ))}
      </ul>
      {events.hasNextPage && (
        <Button onClick={() => void events.fetchNextPage()} disabled={events.isFetchingNextPage} className="self-center">
          {events.isFetchingNextPage ? "Cargando…" : "Cargar más"}
        </Button>
      )}
      {open && <EventDetail event={open} me={me.data} onClose={closeDetail} />}
    </div>
  );
}

/** ReviewBadge states the review status in text so it never depends on color alone. */
function ReviewBadge({ reviewed }: { reviewed: boolean }) {
  return reviewed ? (
    <span className="ml-auto inline-flex shrink-0 items-center gap-1 text-xs text-ok">
      <CheckCheck className="size-4" aria-hidden /> Revisado
    </span>
  ) : (
    <span className="ml-auto shrink-0 rounded border border-warn/40 px-1.5 text-xs text-warn">Sin revisar</span>
  );
}

function Thumb({ event }: { event: Schemas["Event"] }) {
  const [failed, setFailed] = useState(false);
  return (
    <div className="aspect-video bg-raised">
      {!failed && (
        <img
          src={`/api/v1/events/${event.id}/thumbnail`}
          alt=""
          loading="lazy"
          className="size-full object-cover"
          onError={() => setFailed(true)}
        />
      )}
    </div>
  );
}

function EventDetail({ event, me, onClose }: { event: Schemas["Event"]; me?: Schemas["Me"]; onClose: () => void }) {
  const qc = useQueryClient();
  const [e, setE] = useState(event);
  const review = useMutation({
    mutationFn: async () => unwrap(await api.PATCH("/api/v1/events/{eventId}", { params: { path: { eventId: e.id } }, body: { reviewed: !e.reviewed } })),
    onSuccess: (updated) => {
      setE(updated);
      void qc.invalidateQueries({ queryKey: ["events"] });
    },
  });
  const exp = useMutation({
    mutationFn: async () => {
      const start = new Date(new Date(e.start_time).getTime() - 5000);
      const end = e.end_time ? new Date(new Date(e.end_time).getTime() + 5000) : new Date(start.getTime() + 60_000);
      return unwrap(
        await api.POST("/api/v1/exports", {
          body: { camera_id: e.camera_id, start_time: start.toISOString(), end_time: end.toISOString(), name: `${e.camera_name} ${fmtDateTime(e.start_time)}` },
        }),
      );
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: ["exports"] }),
  });
  const t = Math.floor(new Date(e.start_time).getTime() / 1000) - 5;

  return (
    <Modal title="Detalle del evento" onClose={onClose}>
      <div className="flex flex-col gap-3">
        <h3 className="text-base font-medium">{e.labels.map(labelName).join(", ") || "Evento"}</h3>
        <p className="flex flex-wrap items-center gap-2 text-sm text-muted">
          <span className={cn("rounded px-1.5 py-0.5 font-mono text-[10px] uppercase", e.severity === "alert" ? "bg-bad/20 text-bad" : "bg-raised text-muted")}>
            {e.severity === "alert" ? "Alerta" : "Detección"}
          </span>
          <span>
            {e.camera_name} · {e.server_name} · {e.site_name}
          </span>
          <ReviewBadge reviewed={e.reviewed} />
        </p>
        <img
          src={can(me, "snapshots.view") ? `/media/v1/events/${e.id}/snapshot.jpg` : `/api/v1/events/${e.id}/thumbnail`}
          onError={(x) => ((x.currentTarget as HTMLImageElement).src = `/api/v1/events/${e.id}/thumbnail`)}
          alt="Captura del evento"
          className="w-full rounded bg-black object-contain"
        />
        <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-4">
          <dt className="text-muted">Inicio</dt>
          <dd>{fmtDateTime(e.start_time)}</dd>
          <dt className="text-muted">Duración</dt>
          <dd>{fmtDuration(e.start_time, e.end_time)}</dd>
          <dt className="text-muted">Zonas</dt>
          <dd>{e.zones.join(", ") || "—"}</dd>
          <dt className="text-muted">Patentes</dt>
          <dd className="font-mono">{e.plates.join(" ") || "—"}</dd>
        </dl>
        <ErrorNote error={review.error ?? exp.error} />
        {exp.data && <p role="status" className="text-sm text-ok">Exportación iniciada. La vas a encontrar en Exportaciones.</p>}
        {review.isSuccess && (
          <p role="status" className="text-sm text-ok">
            {e.reviewed ? "Evento marcado como revisado." : "Evento marcado como sin revisar."}
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          {can(me, "recordings.view") && (
            <Link to="/playback" search={{ camera: e.camera_id, t }} className="inline-flex items-center gap-2 rounded bg-accent px-3 py-1.5 text-sm font-medium text-bg hover:bg-accent/90">
              <History className="size-4" aria-hidden /> Ver grabación
            </Link>
          )}
          {can(me, "exports.create") && (
            <Button onClick={() => exp.mutate()} disabled={exp.isPending || !!exp.data}>
              <Download className="size-4" aria-hidden /> Exportar clip
            </Button>
          )}
          {can(me, "events.review") && (
            <Button onClick={() => review.mutate()} disabled={review.isPending}>
              <CheckCheck className="size-4" aria-hidden />{" "}
              {review.isPending ? "Guardando…" : e.reviewed ? "Marcar sin revisar" : "Marcar revisado"}
            </Button>
          )}
          {can(me, "snapshots.download") && (
            <a href={`/media/v1/events/${e.id}/snapshot.jpg?download=1`} className="inline-flex items-center gap-2 rounded border border-line px-3 py-1.5 text-sm hover:bg-raised">
              Descargar captura
            </a>
          )}
        </div>
      </div>
    </Modal>
  );
}
