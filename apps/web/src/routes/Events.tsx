import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { CheckCheck, ChevronLeft, ChevronRight, Download, History, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { api, type Schemas, unwrap } from "@/api/client";
import { camerasQuery, type EventFilter, eventsQuery, meQuery, serversQuery } from "@/api/queries";
import { type FilterChip } from "@/components/SearchSummary";
import { Modal } from "@/components/Modal";
import { Button, Chip, Empty, ErrorNote, IconButton, Select, TextInput } from "@/components/ui";
import { cn } from "@/lib/cn";
import { VehicleFacts } from "@/components/VehicleMark";
import { fmtDateTime, fmtDuration, fromLocalInput, isClassifiedVehicleType, labelName, objectFilterOptions, vehicleColorOptions, vehicleTypeOptions } from "@/lib/format";
import { emptyEventsForm as emptyForm, type EventsForm as Form, formToSearch, parseEventsSearch, searchToForm, todayEventsRange } from "@/lib/eventsSearch";
import { can } from "@/lib/perm";
import { useT } from "@/i18n";

/** MAX_BULK mirrors the server limit of one bulk review request. */
const MAX_BULK = 200;
const PAGE_SIZE = 100;

function toFilter(f: Form): EventFilter {
  return {
    server_id: f.server ? [f.server] : undefined,
    camera_id: f.camera ? [f.camera] : undefined,
    label: f.label ? [f.label] : undefined,
    severity: f.severity || undefined,
    plate: f.plate.trim() || undefined,
    from: fromLocalInput(f.from),
    to: fromLocalInput(f.to),
    reviewed: f.pending ? false : undefined,
    has_snapshot: f.hasSnapshot ? true : undefined,
    has_preview: f.hasPreview ? true : undefined,
    vehicle_type: f.vehicleType ? [f.vehicleType] : undefined,
    vehicle_color: f.vehicleColor ? [f.vehicleColor] : undefined,
    limit: PAGE_SIZE,
  };
}

type AppliedChip = { key: string; label: string; clear: Partial<Form> };

/** appliedChips describes each non-default applied filter and the form patch that removes it. */
function appliedChips(f: Form, names: { server?: string; camera?: string }, t: ReturnType<typeof useT>): AppliedChip[] {
  const today = todayEventsRange();
  const out: AppliedChip[] = [];
  if (f.server) out.push({ key: "server", label: t("events.chipServer", { value: names.server ?? f.server }), clear: { server: "", camera: "" } });
  if (f.camera) out.push({ key: "camera", label: t("events.chipCamera", { value: names.camera ?? f.camera }), clear: { camera: "" } });
  if (f.label) out.push({ key: "label", label: t("events.chipObject", { value: labelName(f.label) }), clear: { label: "" } });
  if (f.vehicleType) out.push({ key: "vehicleType", label: t("events.chipVehicleType", { value: vehicleTypeOptions().find((o) => o.value === f.vehicleType)?.label ?? f.vehicleType }), clear: { vehicleType: "" } });
  if (f.vehicleColor) out.push({ key: "vehicleColor", label: t("events.chipColor", { value: vehicleColorOptions().find((o) => o.value === f.vehicleColor)?.label ?? f.vehicleColor }), clear: { vehicleColor: "" } });
  if (f.severity) out.push({ key: "severity", label: t("events.chipKind", { value: f.severity === "alert" ? t("events.alertsOnly") : t("events.detectionsOnly") }), clear: { severity: "" } });
  if (f.plate.trim()) out.push({ key: "plate", label: t("events.chipPlate", { value: f.plate.trim() }), clear: { plate: "" } });
  if (f.from && f.from !== today.from) out.push({ key: "from", label: t("events.chipFrom", { value: f.from.replace("T", " ") }), clear: { from: today.from } });
  if (f.to && f.to !== today.to) out.push({ key: "to", label: t("events.chipTo", { value: f.to.replace("T", " ") }), clear: { to: today.to } });
  if (f.hasSnapshot) out.push({ key: "hasSnapshot", label: t("events.withSnapshot"), clear: { hasSnapshot: false } });
  if (f.hasPreview) out.push({ key: "hasPreview", label: t("events.withPreview"), clear: { hasPreview: false } });
  return out;
}

/** Events is the federated event index (PRD §34-40): every authorized Frigate in one list. */
export function Events() {
  const t = useT();
  const me = useQuery(meQuery);
  const servers = useQuery(serversQuery);
  const cameras = useQuery(camerasQuery({}));
  // The applied filters live in the URL (shareable, survives reload); `form` is the editable draft.
  const search = useSearch({ strict: false }) as Record<string, unknown>;
  const navigate = useNavigate();
  const searchKey = JSON.stringify(search);
  // Memoized on the serialized search so `applied` keeps its identity between unrelated renders.
  const applied = useMemo(() => searchToForm(parseEventsSearch(JSON.parse(searchKey) as Record<string, unknown>)), [searchKey]);
  const [form, setForm] = useState<Form>(applied);
  const [page, setPage] = useState(0);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const apply = useCallback(
    (f: Form) => {
      setSelected(new Set());
      void navigate({ to: ".", search: formToSearch(f) as never });
    },
    [navigate],
  );
  // Back/forward or an edited URL replaces the draft with what is applied (state adjusted during
  // render, the React-recommended alternative to an effect).
  const [syncedApplied, setSyncedApplied] = useState(applied);
  if (syncedApplied !== applied) {
    setSyncedApplied(applied);
    setForm(applied);
    setPage(0);
  }
  const events = useInfiniteQuery(eventsQuery(toFilter(applied)));
  const [open, setOpen] = useState<Schemas["Event"] | null>(null);
  const closeDetail = useCallback(() => setOpen(null), []);
  const set = <K extends keyof Form>(k: K, v: Form[K]) => setForm((f) => ({ ...f, [k]: v }));
  const toggleFlag = (key: "pending" | "hasSnapshot" | "hasPreview") => {
    const next = { ...form, [key]: !form[key] };
    setForm(next);
    apply(next);
  };
  const items = events.data?.pages[page]?.items ?? [];
  const hasNext = page < (events.data?.pages.length ?? 0) - 1 || !!events.data?.pages[page]?.next_cursor;
  const canReview = can(me.data, "events.review");
  // Only events still in the list count as selected (a refresh may drop some).
  const selectedIds = items.filter((e) => selected.has(e.id)).map((e) => e.id);
  const toggle = (id: string) =>
    setSelected((cur) => {
      const next = new Set(cur);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  const chips = appliedChips(applied, {
    server: servers.data?.find((x) => x.id === applied.server)?.name,
    camera: cameras.data?.find((x) => x.id === applied.camera)?.display_name,
  }, t).map<FilterChip>((c) => ({
    ...c,
    onRemove: () => {
      apply({ ...applied, ...c.clear });
    },
  }));
  const camsOfServer = (cameras.data ?? []).filter((c) => !form.server || c.server_id === form.server);
  const allSelected = items.length > 0 && items.every((e) => selected.has(e.id));
  const goNext = () => {
    const loaded = events.data?.pages.length ?? 0;
    if (page + 1 < loaded) {
      setPage(page + 1);
      return;
    }
    void events.fetchNextPage().then((res) => {
      if (!res.isError) setPage((p) => p + 1);
    });
  };

  return (
    <div className="mx-auto flex max-w-7xl flex-col gap-3">
      <form
        className="flex flex-col gap-3 rounded-m3-xl bg-surface-1 p-3 sm:p-4"
        onSubmit={(e) => {
          e.preventDefault();
          apply(form);
        }}
      >
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
          <Select aria-label={t("common.server")} value={form.server} onChange={(e) => setForm({ ...form, server: e.target.value, camera: "" })}>
            <option value="">{t("common.server")}</option>
            {servers.data?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </Select>
          <Select aria-label={t("common.camera")} value={form.camera} onChange={(e) => set("camera", e.target.value)}>
            <option value="">{t("common.camera")}</option>
            {camsOfServer.map((c) => (
              <option key={c.id} value={c.id}>
                {c.display_name}
              </option>
            ))}
          </Select>
          <Select
            aria-label={t("common.object")}
            value={form.label || (isClassifiedVehicleType(form.vehicleType) ? form.vehicleType : "")}
            onChange={(e) => {
              const value = e.target.value;
              if (isClassifiedVehicleType(value)) {
                setForm((f) => ({ ...f, label: "", vehicleType: value }));
                return;
              }
              setForm((f) => ({ ...f, label: value, vehicleType: isClassifiedVehicleType(f.vehicleType) ? "" : f.vehicleType }));
            }}
          >
            <option value="">{t("common.object")}</option>
            {objectFilterOptions().map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </Select>
          <Select aria-label={t("events.vehicleType")} value={form.vehicleType} onChange={(e) => set("vehicleType", e.target.value)}>
            <option value="">{t("events.classification")}</option>
            {vehicleTypeOptions().map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </Select>
          <Select aria-label={t("common.color")} value={form.vehicleColor} onChange={(e) => set("vehicleColor", e.target.value)}>
            <option value="">{t("common.color")}</option>
            {vehicleColorOptions().map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </Select>
          <Select aria-label={t("events.kind")} value={form.severity} onChange={(e) => set("severity", e.target.value as Form["severity"])}>
            <option value="">{t("events.both")}</option>
            <option value="alert">{t("events.alertsOnly")}</option>
            <option value="detection">{t("events.detectionsOnly")}</option>
          </Select>
          <TextInput aria-label={t("common.from")} type="datetime-local" className="font-mono" value={form.from} onChange={(e) => set("from", e.target.value)} />
          <TextInput aria-label={t("common.to")} type="datetime-local" className="font-mono" value={form.to} onChange={(e) => set("to", e.target.value)} />
          {can(me.data, "lpr.search") && (
            <TextInput aria-label={t("common.plate")} className="font-mono" value={form.plate} onChange={(e) => set("plate", e.target.value.toUpperCase())} placeholder={t("common.plate")} />
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" variant="filled">
            {t("common.search")}
          </Button>
          <Button variant="outlined" onClick={() => apply(emptyForm)}>
            {t("common.clear")}
          </Button>
          <span className="mx-1 hidden h-6 w-px bg-outline-variant sm:block" aria-hidden />
          <Chip selected={form.pending} onChange={() => toggleFlag("pending")}>
            {t("events.unreviewed")}
          </Chip>
          <Chip selected={form.hasSnapshot} onChange={() => toggleFlag("hasSnapshot")}>
            {t("events.withSnapshot")}
          </Chip>
          <Chip selected={form.hasPreview} onChange={() => toggleFlag("hasPreview")}>
            {t("events.withPreview")}
          </Chip>
          {canReview && items.length > 0 && (
            <Chip selected={allSelected} onChange={() => setSelected(allSelected ? new Set() : new Set(items.map((e) => e.id)))}>
              {allSelected ? t("events.clearSelection") : t("events.selectAll")}
            </Chip>
          )}
          {canReview && (
            <BulkReview selectedIds={selectedIds} onClearSelection={() => setSelected(new Set())} />
          )}
        </div>
        {chips.length > 0 && (
          <ul aria-label={t("common.appliedFilters")} className="flex flex-wrap gap-2">
            {chips.map((c) => (
              <li key={c.key}>
                <button type="button" onClick={c.onRemove} aria-label={t("common.removeFilter", { label: c.label })} className="m3-press inline-flex min-h-9 items-center rounded-full bg-secondary-container px-3 text-xs font-medium text-on-secondary-container hover:brightness-110 focus-visible:outline-2 focus-visible:outline-primary">
                  {c.label}
                </button>
              </li>
            ))}
          </ul>
        )}
      </form>
      <ErrorNote error={events.error} />
      {events.isSuccess && items.length === 0 && <Empty>{t("events.empty")}</Empty>}
      <ul className="grid items-stretch gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
        {items.map((e) => (
          <li key={e.id} className="relative flex min-w-0">
            {canReview && (
              <label className="absolute left-3 top-3 z-10 flex size-8 items-center justify-center rounded-full bg-surface-dim/80">
                <input
                  type="checkbox"
                  checked={selected.has(e.id)}
                  onChange={() => toggle(e.id)}
                  aria-label={t("events.selectEvent", { camera: e.camera_name, time: fmtDateTime(e.start_time) })}
                />
              </label>
            )}
            <button
              type="button"
              onClick={() => setOpen(e)}
              className={cn(
                "m3-press flex h-full min-h-11 w-full min-w-0 flex-col overflow-hidden rounded-m3-lg bg-surface-1 text-left hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-primary",
                e.severity === "alert" && "ring-1 ring-bad/60",
                selected.has(e.id) && "bg-primary-container/40 ring-2 ring-primary",
              )}
            >
              <Thumb event={e} snapshot={can(me.data, "snapshots.view")} />
              <div className="flex min-w-0 flex-1 flex-col gap-1.5 p-3 text-sm">
                <div className="flex items-center gap-2">
                  <span className={cn("rounded-full px-2.5 py-0.5 font-mono text-[10px] uppercase", e.severity === "alert" ? "bg-bad/20 text-bad" : "bg-surface-3 text-on-surface-variant")}>
                    {e.severity === "alert" ? t("events.alert") : t("events.detection")}
                  </span>
                  <ReviewBadge reviewed={e.reviewed} />
                </div>
                <VehicleFacts labels={e.labels} vehicle={e.attributes?.vehicle} person={e.attributes?.person} serverName={e.server_name} cameraName={e.camera_name} vehicleJob={e.attributes?.vehicle_job} personJob={e.attributes?.person_job} />
                <div className="mt-auto flex items-center justify-between gap-2 pt-1 text-xs text-muted">
                  <span className="shrink-0 font-mono">{fmtDateTime(e.start_time)}</span>
                  {e.plates.length > 0 && <span className="truncate rounded-full bg-primary-container px-2 py-0.5 font-mono text-on-primary-container">{e.plates[0]}{e.plates.length > 1 ? ` +${e.plates.length - 1}` : ""}</span>}
                </div>
              </div>
            </button>
          </li>
        ))}
      </ul>
      {(page > 0 || hasNext) && (
        <nav aria-label={t("events.pages")} className="flex items-center justify-center gap-3 text-sm text-muted">
          <IconButton variant="tonal" icon={ChevronLeft} onClick={() => setPage((p) => Math.max(0, p - 1))} disabled={page === 0} aria-label={t("events.prevPage")} />
          <span className="font-mono">{t("maps.page", { page: page + 1 })}</span>
          <IconButton variant="tonal" icon={ChevronRight} onClick={goNext} disabled={!hasNext || events.isFetchingNextPage} aria-label={t("events.nextPage")} />
        </nav>
      )}
      {open && <EventDetail event={open} me={me.data} onClose={closeDetail} />}
    </div>
  );
}

/**
 * BulkReview marks the current selection reviewed in one request. It only renders once more than
 * one event is selected. The server still authorizes every id and rejects the whole batch.
 */
function BulkReview({ selectedIds, onClearSelection }: { selectedIds: string[]; onClearSelection: () => void }) {
  const t = useT();
  const qc = useQueryClient();
  const [done, setDone] = useState("");
  const bulk = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/events/review", { body: { ids: selectedIds, reviewed: true } })),
    onSuccess: (res) => {
      onClearSelection();
      void qc.invalidateQueries({ queryKey: ["events"] });
      const count = res.items.length;
      setDone(count === 1 ? t("events.markedOne", { count }) : t("events.markedMany", { count }));
    },
  });
  const tooMany = selectedIds.length > MAX_BULK;
  if (selectedIds.length < 2 && !done && !bulk.error && !bulk.isPending) return null;
  return (
    <>
      {selectedIds.length > 1 && (
        <Button variant="tonal" onClick={() => (setDone(""), bulk.mutate())} disabled={bulk.isPending || tooMany}>
          <CheckCheck className="size-4" aria-hidden /> {bulk.isPending ? t("common.saving") : t("events.markReviewed")}
        </Button>
      )}
      {tooMany && <span role="status" className="text-xs text-warn">{t("events.maxBatch", { count: MAX_BULK })}</span>}
      {bulk.error && <ErrorNote error={bulk.error} />}
      {done && <span role="status" className="text-xs text-ok">{done}</span>}
    </>
  );
}

/** ReviewBadge states the review status in text so it never depends on color alone. */
function ReviewBadge({ reviewed }: { reviewed: boolean }) {
  const t = useT();
  return reviewed ? (
    <span className="ml-auto inline-flex shrink-0 items-center gap-1 text-xs text-ok">
      <CheckCheck className="size-4" aria-hidden /> {t("events.reviewed")}
    </span>
  ) : (
    <span className="ml-auto shrink-0 rounded-full bg-warn/15 px-2.5 py-0.5 text-xs text-warn">{t("events.unreviewed")}</span>
  );
}

function Thumb({ event, snapshot }: { event: Schemas["Event"]; snapshot: boolean }) {
  const [src, setSrc] = useState(snapshot && event.has_snapshot ? `/media/v1/events/${event.id}/snapshot.jpg` : `/api/v1/events/${event.id}/thumbnail`);
  const [failed, setFailed] = useState(false);
  return (
    <div className="relative m-2 mb-0 aspect-video shrink-0 overflow-hidden rounded-m3-md bg-video">
      {!failed && (
        <img
          src={src}
          alt=""
          loading="lazy"
          className="absolute inset-0 size-full object-cover"
          onError={() => {
            const thumb = `/api/v1/events/${event.id}/thumbnail`;
            if (src !== thumb) setSrc(thumb);
            else setFailed(true);
          }}
        />
      )}
    </div>
  );
}

function EventDetail({ event, me, onClose }: { event: Schemas["Event"]; me?: Schemas["Me"]; onClose: () => void }) {
  const tr = useT();
  const qc = useQueryClient();
  const [e, setE] = useState(event);
  const review = useMutation({
    mutationFn: async () => unwrap(await api.PATCH("/api/v1/events/{eventId}", { params: { path: { eventId: e.id } }, body: { reviewed: !e.reviewed } })),
    onSuccess: (updated) => {
      setE(updated);
      void qc.invalidateQueries({ queryKey: ["events"] });
    },
  });
  const classifiable = e.labels.some((label) => ["car", "car-verified", "truck", "bus", "motorcycle", "person"].includes(label));
  const reading = e.attributes?.vehicle_job === "pending" || e.attributes?.vehicle_job === "processing" || e.attributes?.person_job === "pending" || e.attributes?.person_job === "processing";
  const reprocess = useMutation({
    mutationFn: async () => unwrap(await api.POST("/api/v1/events/{eventId}/reprocess", { params: { path: { eventId: e.id } } })),
    onSuccess: (updated) => {
      setE(updated);
      void qc.invalidateQueries({ queryKey: ["events"] });
    },
  });
  useEffect(() => {
    if (!reading) return;
    let stop = false;
    const timer = setInterval(() => {
      void (async () => {
        const next = await unwrap(await api.GET("/api/v1/events/{eventId}", { params: { path: { eventId: e.id } } }));
        if (stop) return;
        setE(next);
        const still = next.attributes?.vehicle_job === "pending" || next.attributes?.vehicle_job === "processing" || next.attributes?.person_job === "pending" || next.attributes?.person_job === "processing";
        if (!still) void qc.invalidateQueries({ queryKey: ["events"] });
      })();
    }, 2000);
    return () => {
      stop = true;
      clearInterval(timer);
    };
  }, [reading, e.id, qc]);
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
    <Modal title={tr("events.detail")} onClose={onClose}>
      <div className="flex flex-col gap-3">
        <VehicleFacts labels={e.labels} vehicle={e.attributes?.vehicle} person={e.attributes?.person} serverName={e.server_name} cameraName={e.camera_name} vehicleJob={e.attributes?.vehicle_job} personJob={e.attributes?.person_job} />
        <p className="flex flex-wrap items-center gap-2 text-sm text-muted">
          <span className={cn("rounded-full px-2.5 py-0.5 font-mono text-[10px] uppercase", e.severity === "alert" ? "bg-bad/20 text-bad" : "bg-surface-3 text-on-surface-variant")}>
            {e.severity === "alert" ? tr("events.alert") : tr("events.detection")}
          </span>
          <ReviewBadge reviewed={e.reviewed} />
        </p>
        <img
          src={can(me, "snapshots.view") ? `/media/v1/events/${e.id}/snapshot.jpg` : `/api/v1/events/${e.id}/thumbnail`}
          onError={(x) => ((x.currentTarget as HTMLImageElement).src = `/api/v1/events/${e.id}/thumbnail`)}
          alt={tr("events.captureAlt")}
          className="w-full rounded-m3-lg bg-video object-contain"
        />
        <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-4">
          <dt className="text-muted">{tr("events.started")}</dt>
          <dd className="font-mono">{fmtDateTime(e.start_time)}</dd>
          <dt className="text-muted">{tr("events.duration")}</dt>
          <dd className="font-mono">{fmtDuration(e.start_time, e.end_time)}</dd>
          <dt className="text-muted">{tr("events.zones")}</dt>
          <dd>{e.zones.join(", ") || "—"}</dd>
          <dt className="text-muted">{tr("events.plates")}</dt>
          <dd className="font-mono">{e.plates.join(" ") || "—"}</dd>
        </dl>
        <ErrorNote error={review.error ?? exp.error ?? reprocess.error} />
        {exp.data && <p role="status" className="text-sm text-ok">{tr("events.exportStarted")}</p>}
        {review.isSuccess && (
          <p role="status" className="text-sm text-ok">
            {e.reviewed ? tr("events.markedReviewed") : tr("events.markedUnreviewed")}
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          {can(me, "recordings.view") && (
            <Link to="/playback" search={{ camera: e.camera_id, t }} className="m3-press inline-flex h-11 items-center justify-center gap-2 rounded-full bg-primary px-5 text-sm font-bold text-on-primary hover:brightness-110 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">
              <History className="size-4" aria-hidden /> {tr("events.viewRecording")}
            </Link>
          )}
          {can(me, "exports.create") && (
            <Button variant="tonal" onClick={() => exp.mutate()} disabled={exp.isPending || !!exp.data}>
              <Download className="size-4" aria-hidden /> {tr("events.exportClip")}
            </Button>
          )}
          {can(me, "events.review") && classifiable && (
            <Button variant="tonal" onClick={() => reprocess.mutate()} disabled={reprocess.isPending || reading}>
              <RefreshCw className={cn("size-4", (reprocess.isPending || reading) && "animate-spin")} aria-hidden />{" "}
              {reprocess.isPending || reading ? tr("events.reprocessing") : tr("events.reprocess")}
            </Button>
          )}
          {can(me, "events.review") && (
            <Button variant="tonal" onClick={() => review.mutate()} disabled={review.isPending}>
              <CheckCheck className="size-4" aria-hidden />{" "}
              {review.isPending ? tr("common.saving") : e.reviewed ? tr("events.markUnreviewed") : tr("events.markReviewedOne")}
            </Button>
          )}
          {can(me, "snapshots.download") && (
            <a href={`/media/v1/events/${e.id}/snapshot.jpg?download=1`} className="m3-press inline-flex h-11 items-center justify-center gap-2 rounded-full border border-outline px-5 text-sm font-bold hover:bg-on-surface/8 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">
              {tr("events.downloadSnapshot")}
            </a>
          )}
        </div>
      </div>
    </Modal>
  );
}
