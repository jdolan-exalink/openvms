import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import type { Schemas } from "@/api/client";
import { cameraGroupsQuery, meQuery, type PlateFilter, platesQuery, sitesQuery } from "@/api/queries";
import { PlateDetailModal } from "@/components/PlateDetailModal";
import { PlateReadCard } from "@/components/plates/PlateReadCard";
import { type FilterChip, SearchSummary } from "@/components/SearchSummary";
import { Button, Empty, ErrorNote, Field, Select, TextInput } from "@/components/ui";
import { fromLocalInput, vehicleColorOptions, vehicleTypeOptions } from "@/lib/format";
import { todayEventsRange } from "@/lib/eventsSearch";
import { can } from "@/lib/perm";
import { useT } from "@/i18n";

type Form = { plate: string; exact: boolean; site: string; cameraGroup: string; vehicleType: string; vehicleColor: string; from: string; to: string };

const PAGE_SIZE = 12;

function defaultForm(day = todayEventsRange()): Form {
  return { plate: "", exact: false, site: "", cameraGroup: "", vehicleType: "", vehicleColor: "", from: day.from, to: day.to };
}

/** The day control ends at 23:59; the query includes the rest of that minute up to midnight. */
function rangeEnd(local: string): string | undefined {
  const instant = fromLocalInput(local);
  if (!instant || !local.endsWith("T23:59")) return instant;
  const end = new Date(instant);
  end.setMinutes(end.getMinutes() + 1);
  return end.toISOString();
}

function toFilter(f: Form): PlateFilter {
  return {
    plate: f.plate.trim() || undefined,
    exact: f.exact || undefined,
    site_id: f.site ? [f.site] : undefined,
    camera_group_id: f.cameraGroup ? [f.cameraGroup] : undefined,
    vehicle_type: f.vehicleType ? [f.vehicleType] : undefined,
    vehicle_color: f.vehicleColor ? [f.vehicleColor] : undefined,
    from: fromLocalInput(f.from),
    to: rangeEnd(f.to),
    limit: PAGE_SIZE,
  };
}

/** Plates is the global LPR search. The cards match the map sidebar and open the detail. */
export function Plates() {
  const t = useT();
  const me = useQuery(meQuery);
  const sites = useQuery(sitesQuery);
  const cameraGroups = useQuery(cameraGroupsQuery);
  const [today] = useState(() => todayEventsRange());
  const initial = defaultForm(today);
  const [form, setForm] = useState<Form>(initial);
  const [applied, setApplied] = useState<Form>(initial);
  const [page, setPage] = useState(0);
  // The plate field searches as each character lands. The other filters still wait for Buscar.
  useEffect(() => {
    const timer = window.setTimeout(() => {
      setApplied((current) => (current.plate === form.plate ? current : { ...current, plate: form.plate }));
    }, 200);
    return () => window.clearTimeout(timer);
  }, [form.plate]);
  const reads = useInfiniteQuery(platesQuery(toFilter(applied)));
  const pages = reads.data?.pages ?? [];
  const items = pages[page]?.items ?? [];
  const lastPage = Math.max(0, pages.length - 1);
  const canNext = page < lastPage || !!reads.hasNextPage;
  const [detailRead, setDetailRead] = useState<Schemas["PlateRead"] | null>(null);
  const canPreviewSnapshot = can(me.data, "snapshots.view") && can(me.data, "lpr.view");
  const canViewDetail = can(me.data, "lpr.view") && (can(me.data, "snapshots.view") || can(me.data, "recordings.view"));
  const filterKey = JSON.stringify(toFilter(applied));
  const [pageKey, setPageKey] = useState(filterKey);
  if (pageKey !== filterKey) {
    setPageKey(filterKey);
    setPage(0);
  }

  async function goNext() {
    if (page < lastPage) {
      setPage(page + 1);
      return;
    }
    if (!reads.hasNextPage || reads.isFetchingNextPage) return;
    await reads.fetchNextPage();
    setPage(page + 1);
  }

  const reset = (patch: Partial<Form>) => {
    setForm((f) => ({ ...f, ...patch }));
    setApplied((f) => ({ ...f, ...patch }));
  };
  const chips: FilterChip[] = [];
  if (applied.plate.trim()) chips.push({ key: "plate", label: t("plates.chipPlate", { value: applied.plate.trim() }), onRemove: () => reset({ plate: "" }) });
  if (applied.exact) chips.push({ key: "exact", label: t("common.exactMatch"), onRemove: () => reset({ exact: false }) });
  if (applied.site) chips.push({ key: "site", label: t("plates.chipSite", { value: sites.data?.find((x) => x.id === applied.site)?.name ?? applied.site }), onRemove: () => reset({ site: "" }) });
  if (applied.cameraGroup)
    chips.push({ key: "group", label: t("plates.chipGroup", { value: cameraGroups.data?.find((x) => x.id === applied.cameraGroup)?.name ?? applied.cameraGroup }), onRemove: () => reset({ cameraGroup: "" }) });
  if (applied.vehicleType)
    chips.push({ key: "vehicleType", label: t("plates.chipObject", { value: vehicleTypeOptions().find((o) => o.value === applied.vehicleType)?.label ?? applied.vehicleType }), onRemove: () => reset({ vehicleType: "" }) });
  if (applied.vehicleColor)
    chips.push({ key: "vehicleColor", label: t("plates.chipColor", { value: vehicleColorOptions().find((o) => o.value === applied.vehicleColor)?.label ?? applied.vehicleColor }), onRemove: () => reset({ vehicleColor: "" }) });
  if (applied.from && applied.from !== today.from) chips.push({ key: "from", label: t("plates.chipFrom", { value: applied.from.replace("T", " ") }), onRemove: () => reset({ from: today.from }) });
  if (applied.to && applied.to !== today.to) chips.push({ key: "to", label: t("plates.chipTo", { value: applied.to.replace("T", " ") }), onRemove: () => reset({ to: today.to }) });

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <h1 className="sr-only">{t("nav.plates")}</h1>
      <form
        className="grid gap-3 rounded border border-line bg-surface p-3 sm:grid-cols-2 lg:grid-cols-5"
        onSubmit={(e) => {
          e.preventDefault();
          setApplied(form);
        }}
      >
        <Field label={t("common.plate")}>
          <TextInput
            autoFocus
            value={form.plate}
            disabled={!can(me.data, "lpr.search")}
            onChange={(e) => setForm({ ...form, plate: e.target.value.toUpperCase() })}
            placeholder={t("plates.placeholder")}
          />
        </Field>
        <Field label={t("common.site")}>
          <Select value={form.site} onChange={(e) => setForm({ ...form, site: e.target.value })}>
            <option value="">{t("common.all")}</option>
            {sites.data?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t("common.cameraGroup")}>
          <Select value={form.cameraGroup} onChange={(e) => setForm({ ...form, cameraGroup: e.target.value })}>
            <option value="">{t("common.all")}</option>
            {cameraGroups.data?.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label={t("common.object")}>
          <Select value={form.vehicleType} onChange={(e) => setForm({ ...form, vehicleType: e.target.value })}>
            <option value="">{t("common.all")}</option>
            {vehicleTypeOptions().map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </Select>
        </Field>
        <Field label={t("common.color")}>
          <Select value={form.vehicleColor} onChange={(e) => setForm({ ...form, vehicleColor: e.target.value })}>
            <option value="">{t("common.all")}</option>
            {vehicleColorOptions().map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </Select>
        </Field>
        <Field label={t("common.from")}>
          <TextInput type="datetime-local" value={form.from} onChange={(e) => setForm({ ...form, from: e.target.value })} />
        </Field>
        <Field label={t("common.to")}>
          <TextInput type="datetime-local" value={form.to} onChange={(e) => setForm({ ...form, to: e.target.value })} />
        </Field>
        <div className="flex flex-col justify-end gap-2">
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={form.exact} onChange={(e) => setForm({ ...form, exact: e.target.checked })} /> {t("common.exactMatch")}
          </label>
          <Button type="submit" variant="primary">
            {t("common.search")}
          </Button>
        </div>
      </form>
      <SearchSummary
        count={items.length}
        noun={{ one: t("plates.readingOne"), many: t("plates.readingMany") }}
        hasMore={!!reads.hasNextPage && page === lastPage}
        loading={reads.isPending}
        chips={chips}
        onClear={() => {
          const next = defaultForm(today);
          setForm(next);
          setApplied(next);
        }}
      />
      <ErrorNote error={reads.error} />
      {reads.isSuccess && items.length === 0 && <Empty>{t("plates.empty")}</Empty>}
      {items.length > 0 && (
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3" aria-label={t("plates.list")}>
          {items.map((read) => (
            <PlateReadCard
              key={read.id}
              read={read}
              showPhoto={canPreviewSnapshot}
              onClick={canViewDetail ? () => setDetailRead(read) : undefined}
            />
          ))}
        </ul>
      )}
      <div className="flex items-center justify-between gap-2 text-sm">
        <Button type="button" disabled={page === 0} onClick={() => setPage(page - 1)}>{t("common.previous")}</Button>
        <span className="text-muted">{applied.from === today.from && applied.to === today.to ? t("plates.todayPage", { page: page + 1 }) : t("plates.page", { page: page + 1 })}</span>
        <Button type="button" disabled={!canNext || reads.isFetchingNextPage} onClick={() => void goNext()}>
          {reads.isFetchingNextPage ? t("common.loading") : t("common.next")}
        </Button>
      </div>
      {detailRead && <PlateDetailModal read={detailRead} onClose={() => setDetailRead(null)} />}
    </div>
  );
}
