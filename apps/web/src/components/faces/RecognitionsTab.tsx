import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { History } from "lucide-react";
import { useState } from "react";
import { cameraGroupsQuery, camerasQuery, type FaceRecognitionFilter, faceRecognitionsQuery, facePersonsQuery, sitesQuery } from "@/api/queries";
import { type FilterChip, SearchSummary } from "@/components/SearchSummary";
import { Button, Empty, ErrorNote, Field, LinkButton, Select, TextInput } from "@/components/ui";
import { useT } from "@/i18n";
import { rangeEnd, todayEventsRange } from "@/lib/eventsSearch";
import { fmtDateTime, fromLocalInput } from "@/lib/format";

type Form = { person: string; site: string; cameraGroup: string; camera: string; from: string; to: string };

const PAGE_SIZE = 24;

function defaultForm(day = todayEventsRange()): Form {
  return { person: "", site: "", cameraGroup: "", camera: "", from: day.from, to: day.to };
}

function toFilter(f: Form): FaceRecognitionFilter {
  return {
    person_id: f.person ? [f.person] : undefined,
    site_id: f.site ? [f.site] : undefined,
    camera_group_id: f.cameraGroup ? [f.cameraGroup] : undefined,
    camera_id: f.camera ? [f.camera] : undefined,
    from: fromLocalInput(f.from),
    to: rangeEnd(f.to),
    limit: PAGE_SIZE,
  };
}

/** RecognitionsTab is the recognition search: like the plate search, scoped by site, group, camera, person and dates. */
export function RecognitionsTab() {
  const t = useT();
  const persons = useQuery(facePersonsQuery());
  const sites = useQuery(sitesQuery);
  const groups = useQuery(cameraGroupsQuery);
  const cameras = useQuery(camerasQuery({}));
  const [today] = useState(() => todayEventsRange());
  const [form, setForm] = useState<Form>(() => defaultForm(today));
  const [applied, setApplied] = useState<Form>(form);
  const list = useInfiniteQuery(faceRecognitionsQuery(toFilter(applied)));
  const items = list.data?.pages.flatMap((p) => p.items) ?? [];

  const reset = (patch: Partial<Form>) => {
    setForm((f) => ({ ...f, ...patch }));
    setApplied((f) => ({ ...f, ...patch }));
  };
  const chips: FilterChip[] = [];
  if (applied.person)
    chips.push({ key: "person", label: t("faces.recChipPerson", { value: persons.data?.find((x) => x.id === applied.person)?.display_name ?? applied.person }), onRemove: () => reset({ person: "" }) });
  if (applied.site)
    chips.push({ key: "site", label: t("plates.chipSite", { value: sites.data?.find((x) => x.id === applied.site)?.name ?? applied.site }), onRemove: () => reset({ site: "" }) });
  if (applied.cameraGroup)
    chips.push({ key: "group", label: t("plates.chipGroup", { value: groups.data?.find((x) => x.id === applied.cameraGroup)?.name ?? applied.cameraGroup }), onRemove: () => reset({ cameraGroup: "" }) });
  if (applied.camera)
    chips.push({ key: "camera", label: t("faces.recChipCamera", { value: cameras.data?.find((x) => x.id === applied.camera)?.display_name ?? applied.camera }), onRemove: () => reset({ camera: "" }) });
  if (applied.from && applied.from !== today.from) chips.push({ key: "from", label: t("plates.chipFrom", { value: applied.from.replace("T", " ") }), onRemove: () => reset({ from: today.from }) });
  if (applied.to && applied.to !== today.to) chips.push({ key: "to", label: t("plates.chipTo", { value: applied.to.replace("T", " ") }), onRemove: () => reset({ to: today.to }) });

  return (
    <div className="flex flex-col gap-4">
      <form
        className="grid gap-3 rounded-m3-xl bg-surface-1 p-4 sm:grid-cols-2 lg:grid-cols-4"
        onSubmit={(e) => {
          e.preventDefault();
          setApplied(form);
        }}
      >
        <Field label={t("faces.person")}>
          <Select value={form.person} onChange={(e) => setForm({ ...form, person: e.target.value })}>
            <option value="">{t("faces.allPeople")}</option>
            {persons.data?.map((p) => (
              <option key={p.id} value={p.id}>{p.display_name}</option>
            ))}
          </Select>
        </Field>
        <Field label={t("common.site")}>
          <Select value={form.site} onChange={(e) => setForm({ ...form, site: e.target.value })}>
            <option value="">{t("common.all")}</option>
            {sites.data?.map((s) => (
              <option key={s.id} value={s.id}>{s.name}</option>
            ))}
          </Select>
        </Field>
        <Field label={t("common.cameraGroup")}>
          <Select value={form.cameraGroup} onChange={(e) => setForm({ ...form, cameraGroup: e.target.value })}>
            <option value="">{t("common.all")}</option>
            {groups.data?.map((g) => (
              <option key={g.id} value={g.id}>{g.name}</option>
            ))}
          </Select>
        </Field>
        <Field label={t("common.camera")}>
          <Select value={form.camera} onChange={(e) => setForm({ ...form, camera: e.target.value })}>
            <option value="">{t("common.all")}</option>
            {cameras.data?.map((c) => (
              <option key={c.id} value={c.id}>{c.display_name}</option>
            ))}
          </Select>
        </Field>
        <Field label={t("common.from")}>
          <TextInput type="datetime-local" className="font-mono" value={form.from} onChange={(e) => setForm({ ...form, from: e.target.value })} />
        </Field>
        <Field label={t("common.to")}>
          <TextInput type="datetime-local" className="font-mono" value={form.to} onChange={(e) => setForm({ ...form, to: e.target.value })} />
        </Field>
        <div className="flex items-end sm:col-span-2">
          <Button type="submit" variant="filled">{t("common.search")}</Button>
        </div>
      </form>
      <SearchSummary
        count={items.length}
        noun={{ one: t("faces.recNounOne"), many: t("faces.recNounMany") }}
        hasMore={!!list.hasNextPage}
        loading={list.isPending}
        chips={chips}
        onClear={() => {
          const next = defaultForm(today);
          setForm(next);
          setApplied(next);
        }}
      />
      <ErrorNote error={list.error} />
      {list.isSuccess && items.length === 0 && <Empty icon={History}>{t("faces.recEmpty")}</Empty>}
      {items.length > 0 && (
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3" aria-label={t("faces.recList")}>
          {items.map((r) => (
            <li key={`${r.event_id}/${r.person_id}`} className="flex gap-3 rounded-m3-xl bg-surface-1 p-3">
              {r.has_thumbnail ? (
                <img src={`/api/v1/events/${r.event_id}/thumbnail`} alt={t("faces.recThumbAlt", { camera: r.camera_name })} loading="lazy" className="size-24 shrink-0 rounded-m3-lg bg-surface-2 object-cover" />
              ) : (
                <div className="size-24 shrink-0 rounded-m3-lg bg-surface-2" aria-hidden />
              )}
              <div className="flex min-w-0 flex-col gap-1 text-sm">
                <span className="truncate font-bold">{r.person_name}</span>
                <span className="truncate">{r.camera_name}</span>
                <span className="truncate text-xs text-muted">{r.site_name} · {fmtDateTime(r.start_time)}</span>
                <LinkButton
                  to="/playback"
                  search={{ camera: r.camera_id, t: Math.floor(new Date(r.start_time).getTime() / 1000) }}
                  variant="text"
                  size="sm"
                  className="w-fit"
                >
                  {t("faces.viewPlayback")}
                </LinkButton>
              </div>
            </li>
          ))}
        </ul>
      )}
      {list.hasNextPage && (
        <Button variant="tonal" className="self-center" disabled={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>
          {list.isFetchingNextPage ? t("common.loading") : t("faces.loadMore")}
        </Button>
      )}
    </div>
  );
}
